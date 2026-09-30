//go:build unix

package osenv_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/osenv"
)

func mustNew(t *testing.T, cwd string) *osenv.Env {
	t.Helper()
	e, err := osenv.New(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// OS-C1, OS-FR-04~09
func TestConformanceFS(t *testing.T) {
	envtest.ConformanceFS(t, mustNew(t, t.TempDir()), t.TempDir())
}

// OS-C1, OS-FR-10~12
func TestConformanceShell(t *testing.T) {
	dir := t.TempDir()
	envtest.ConformanceShell(t, mustNew(t, dir), dir)
}

// OS-FR-01
func TestNew(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)
	for name, cwd := range map[string]string{"relative": ".", "missing": filepath.Join(dir, "nope"), "file": file} {
		if _, err := osenv.New(cwd); err == nil {
			t.Errorf("%s cwd accepted", name)
		}
	}
	e := mustNew(t, dir+"/./")
	if e.Cwd() != dir {
		t.Errorf("cwd %q", e.Cwd())
	}
	if home, _ := os.UserHomeDir(); e.Home() != home {
		t.Errorf("home %q, want %q", e.Home(), home)
	}
}

func run(t *testing.T, e *osenv.Env, cmd string) string {
	t.Helper()
	var out bytes.Buffer
	if _, err := e.Exec(context.Background(), env.ExecRequest{Command: cmd, Dir: e.Cwd()}, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// OS-FR-01: 셸 = $SHELL, 비면 /bin/sh. New 에서 한 번 읽는다.
func TestShellSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHELL", "")
	if got := run(t, mustNew(t, dir), "echo $0"); strings.TrimSpace(got) != "/bin/sh" {
		t.Errorf("fallback shell %q", got)
	}
	custom := filepath.Join(dir, "myshell")
	os.WriteFile(custom, []byte("#!/bin/sh\necho custom-shell\nexec /bin/sh \"$@\"\n"), 0o755)
	t.Setenv("SHELL", custom)
	e := mustNew(t, dir)
	t.Setenv("SHELL", "")
	if got := run(t, e, "echo hi"); got != "custom-shell\nhi\n" {
		t.Errorf("custom shell output %q", got)
	}
}

// OS-FR-02
func TestRelativePathsRejected(t *testing.T) {
	e := mustNew(t, t.TempDir())
	ctx := context.Background()
	_, err1 := e.ReadFile(ctx, "rel")
	err2 := e.WriteFile(ctx, "rel", nil)
	err3 := e.AppendFile(ctx, "rel", nil)
	_, err4 := e.Stat(ctx, "rel")
	_, err5 := e.ListDir(ctx, "rel")
	_, err6 := e.Exec(ctx, env.ExecRequest{Command: "true", Dir: "rel"}, &bytes.Buffer{})
	for i, err := range []error{err1, err2, err3, err4, err5, err6} {
		if !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("op %d: %v", i+1, err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.Cwd(), "rel")); err == nil {
		t.Error("relative path written against process cwd")
	}
}

// OS-FR-03
func TestCanceledContext(t *testing.T) {
	dir := t.TempDir()
	e := mustNew(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := filepath.Join(dir, "x")
	_, err1 := e.ReadFile(ctx, p)
	err2 := e.WriteFile(ctx, p, []byte("a"))
	err3 := e.AppendFile(ctx, p, []byte("a"))
	_, err4 := e.Stat(ctx, dir)
	_, err5 := e.ListDir(ctx, dir)
	_, _, err6 := e.CreateTemp(ctx, "x-*")
	for i, err := range []error{err1, err2, err3, err4, err5, err6} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("op %d: %v", i+1, err)
		}
	}
	if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file created under canceled ctx: %v", err)
	}
}

// OS-FR-05, 06: 새 파일 0644·디렉터리 0755 (umask 적용), 덮어쓰기는 권한 유지.
func TestModes(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	dir := t.TempDir()
	e := mustNew(t, dir)
	ctx := context.Background()
	w, a := filepath.Join(dir, "w/new.txt"), filepath.Join(dir, "a/new.log")
	if err := e.WriteFile(ctx, w, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := e.AppendFile(ctx, a, []byte("x")); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{w: 0o644, a: 0o644, filepath.Dir(w): 0o755 | os.ModeDir, filepath.Dir(a): 0o755 | os.ModeDir} {
		if info, err := os.Stat(p); err != nil || info.Mode() != want {
			t.Errorf("%s mode %v, want %v (%v)", p, info.Mode(), want, err)
		}
	}
	priv := filepath.Join(dir, "priv")
	os.WriteFile(priv, []byte("old"), 0o600)
	if err := e.WriteFile(ctx, priv, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(priv); info.Mode().Perm() != 0o600 {
		t.Errorf("overwrite changed mode to %v", info.Mode())
	}
}

// OS-FR-08
func TestListDirSymlinks(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "real"), 0o755)
	os.WriteFile(filepath.Join(dir, "f"), []byte("abc"), 0o644)
	os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link"))
	os.Symlink(filepath.Join(dir, "f"), filepath.Join(dir, "flink"))
	os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken"))
	list, err := mustNew(t, dir).ListDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]env.FileInfo{}
	var names []string
	for _, f := range list {
		got[f.Name] = f
		names = append(names, f.Name)
	}
	if !sort.StringsAreSorted(names) || strings.Join(names, ",") != "broken,f,flink,link,real" {
		t.Fatalf("names %v", names)
	}
	if !got["link"].IsDir || !got["real"].IsDir || got["f"].IsDir || got["f"].Size != 3 || got["flink"].IsDir || got["flink"].Size != 3 {
		t.Errorf("entries %+v", got)
	}
	if b := got["broken"]; b.IsDir || b.Size != 0 {
		t.Errorf("broken %+v", b)
	}
}

// OS-FR-07
func TestStatFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "real"), 0o755)
	os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link"))
	info, err := mustNew(t, dir).Stat(context.Background(), filepath.Join(dir, "link"))
	if err != nil || !info.IsDir || info.Name != "link" {
		t.Errorf("%+v %v", info, err)
	}
}

// OS-FR-11: 취소·Timeout 은 그룹 전체를 즉시 끝낸다 (WaitDelay 를 기다리지 않는다).
func TestExecKillsGroup(t *testing.T) {
	dir := t.TempDir()
	e := mustNew(t, dir)
	pidFile := filepath.Join(dir, "pid")
	cmd := "sleep 30 & echo $! > " + pidFile + "; sleep 30"
	for name, exec := range map[string]func() error{
		"timeout": func() error {
			_, err := e.Exec(context.Background(), env.ExecRequest{Command: cmd, Dir: dir, Timeout: 200 * time.Millisecond}, &bytes.Buffer{})
			return err
		},
		"cancel": func() error {
			cctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(200*time.Millisecond, cancel)
			_, err := e.Exec(cctx, env.ExecRequest{Command: cmd, Dir: dir}, &bytes.Buffer{})
			return err
		},
	} {
		os.Remove(pidFile)
		start := time.Now()
		exec()
		if d := time.Since(start); d > 800*time.Millisecond {
			t.Errorf("%s: returned after %v", name, d)
		}
		b, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var pid int
		fmt.Sscan(string(b), &pid)
		alive := true
		for i := 0; i < 50 && alive; i++ {
			alive = syscall.Kill(pid, 0) == nil
			time.Sleep(10 * time.Millisecond)
		}
		if alive {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("%s: background child %d survived", name, pid)
		}
	}
}

// OS-FR-10: Dir 가 비면 Cwd, stdin 은 비어 있다.
func TestExecDefaults(t *testing.T) {
	dir := t.TempDir()
	e := mustNew(t, dir)
	var out bytes.Buffer
	if _, err := e.Exec(context.Background(), env.ExecRequest{Command: "pwd -P"}, &out); err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(dir); strings.TrimSpace(out.String()) != want {
		t.Errorf("pwd %q, want %q", out.String(), want)
	}
	start := time.Now()
	if got := run(t, e, "cat; echo done"); got != "done\n" || time.Since(start) > 3*time.Second {
		t.Errorf("stdin not empty: %q after %v", got, time.Since(start))
	}
}

// OS-FR-12
func TestExecResults(t *testing.T) {
	dir := t.TempDir()
	e := mustNew(t, dir)
	ctx := context.Background()
	var out bytes.Buffer
	if res, err := e.Exec(ctx, env.ExecRequest{Command: "kill -TERM $$", Dir: dir}, &out); err != nil || res.ExitCode != 143 || res.TimedOut {
		t.Errorf("signal exit %+v %v", res, err)
	}
	if res, err := e.Exec(ctx, env.ExecRequest{Command: "sleep 5", Dir: dir, Timeout: 100 * time.Millisecond}, &out); err != nil || !res.TimedOut || res.ExitCode != -1 {
		t.Errorf("timeout %+v %v", res, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := e.Exec(cctx, env.ExecRequest{Command: "sleep 5", Dir: dir}, &out); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	start := time.Now()
	if res, err := e.Exec(ctx, env.ExecRequest{Command: "sleep 3 & echo bg", Dir: dir}, &out); err != nil || res.ExitCode != 0 || time.Since(start) > 2500*time.Millisecond {
		t.Errorf("held pipe: %+v %v after %v", res, err, time.Since(start))
	}
	if _, err := e.Exec(ctx, env.ExecRequest{Command: "true", Dir: filepath.Join(dir, "nope")}, &out); err == nil {
		t.Error("missing dir accepted")
	}
	t.Setenv("SHELL", filepath.Join(dir, "noshell"))
	if _, err := mustNew(t, dir).Exec(ctx, env.ExecRequest{Command: "true", Dir: dir}, &out); err == nil {
		t.Error("missing shell accepted")
	}
}
