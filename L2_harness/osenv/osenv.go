//go:build unix

// Package osenv 는 env.Env 의 OS 구현이다 (osenv SPEC).
package osenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

// waitDelay 는 그룹을 벗어난 자손이 파이프를 잡고 있을 때 기다리는 최대 시간이다 (OS-FR-11).
const waitDelay = time.Second

const (
	dirMode  = 0o755
	fileMode = 0o644
)

type Env struct {
	cwd, home, shell string
}

var _ env.Env = (*Env)(nil)

// defaultShell 은 shell 인자가 비었을 때의 셸이다 (OS-FR-01).
const defaultShell = "/bin/bash"

// New 는 cwd 에서 동작하는 Env 다. shell 이 비면 defaultShell (OS-FR-01).
func New(cwd, shell string) (*Env, error) {
	if !path.IsAbs(cwd) {
		return nil, fmt.Errorf("osenv: cwd %q is not absolute", cwd)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return nil, fmt.Errorf("osenv: cwd: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("osenv: cwd %q is not a directory", cwd)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	if shell == "" {
		shell = defaultShell
	}
	return &Env{cwd: path.Clean(cwd), home: home, shell: shell}, nil
}

func (e *Env) Cwd() string  { return e.cwd }
func (e *Env) Home() string { return e.home }

// check 는 ctx 취소와 절대 경로를 검사한다 (OS-FR-02, 03).
func check(ctx context.Context, op, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !path.IsAbs(p) {
		return &fs.PathError{Op: op, Path: p, Err: fs.ErrInvalid}
	}
	return nil
}

func (e *Env) ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := check(ctx, "read", p); err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (e *Env) WriteFile(ctx context.Context, p string, data []byte) error {
	if err := check(ctx, "write", p); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return err
	}
	return os.WriteFile(p, data, fileMode)
}

// AppendFile 은 Write 한 번으로 쓴다 (OS-FR-06).
func (e *Env) AppendFile(ctx context.Context, p string, data []byte) error {
	if err := check(ctx, "append", p); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func (e *Env) Stat(ctx context.Context, p string) (env.FileInfo, error) {
	if err := check(ctx, "stat", p); err != nil {
		return env.FileInfo{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return env.FileInfo{}, err
	}
	return env.FileInfo{Name: path.Base(p), IsDir: info.IsDir(), Size: info.Size()}, nil
}

// ListDir 은 이름순이고 심볼릭 링크는 대상으로 판별한다. 깨진 링크는 파일로 둔다 (OS-FR-08).
func (e *Env) ListDir(ctx context.Context, p string) ([]env.FileInfo, error) {
	if err := check(ctx, "listdir", p); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]env.FileInfo, 0, len(entries))
	for _, ent := range entries {
		var info fs.FileInfo
		if ent.Type()&fs.ModeSymlink != 0 {
			info, err = os.Stat(path.Join(p, ent.Name()))
			if err != nil {
				out = append(out, env.FileInfo{Name: ent.Name()})
				continue
			}
		} else if info, err = ent.Info(); err != nil {
			return nil, err
		}
		out = append(out, env.FileInfo{Name: ent.Name(), IsDir: info.IsDir(), Size: info.Size()})
	}
	return out, nil
}

func (e *Env) CreateTemp(ctx context.Context, pattern string) (string, io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	if !filepath.IsAbs(f.Name()) {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("osenv: temp path %q is not absolute", f.Name())
	}
	return f.Name(), f, nil
}

// Exec 는 셸 -c 로 새 프로세스 그룹에서 실행하고, 취소·Timeout 이면 그룹 전체를 끝낸다 (OS-FR-10~12).
func (e *Env) Exec(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
	dir := req.Dir
	if dir == "" {
		dir = e.cwd
	}
	if err := check(ctx, "exec", dir); err != nil {
		return env.ExecResult{}, err
	}
	runCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, e.shell, "-c", req.Command)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if ctx.Err() != nil {
		return env.ExecResult{}, ctx.Err()
	}
	if runCtx.Err() != nil {
		return env.ExecResult{ExitCode: -1, TimedOut: true}, nil
	}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return env.ExecResult{ExitCode: 128 + int(ws.Signal())}, nil
		}
		return env.ExecResult{ExitCode: exitErr.ExitCode()}, nil
	case errors.Is(err, exec.ErrWaitDelay):
		return env.ExecResult{ExitCode: cmd.ProcessState.ExitCode()}, nil
	case err != nil:
		return env.ExecResult{}, err
	}
	return env.ExecResult{}, nil
}
