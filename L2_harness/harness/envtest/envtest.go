// Package envtest 는 env.Env 의 메모리 가짜와 Env 구현의 계약 테스트다 (HN-FR-04).
package envtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

// Script 는 가짜 Shell 의 동작이다.
type Script func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error)

// Env 는 메모리 FileSystem 과 스크립트된 Shell 이다. 동시 사용에 안전하다.
type Env struct {
	cwd, home string

	mu      sync.Mutex
	files   map[string][]byte
	dirs    map[string]bool
	fail    map[string]error
	appends map[string]int
	links   map[string]bool
	temps   int
	script  Script
	reqs    []env.ExecRequest
}

func New(cwd, home string) *Env {
	e := &Env{cwd: cwd, home: home, files: map[string][]byte{}, dirs: map[string]bool{"/": true},
		fail: map[string]error{}, appends: map[string]int{}, links: map[string]bool{}}
	e.mkdirAll(cwd)
	if home != "" {
		e.mkdirAll(home)
	}
	return e
}

func (e *Env) Cwd() string  { return e.cwd }
func (e *Env) Home() string { return e.home }

// SetFile 은 파일을 만든다 (상위 디렉터리 포함).
func (e *Env) SetFile(p, content string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mkdirAll(path.Dir(p))
	e.files[p] = []byte(content)
}

// SetDir 은 빈 디렉터리를 만든다.
func (e *Env) SetDir(p string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mkdirAll(p)
}

// SetSymlink 은 이미 있는 파일·디렉터리 p 를 링크로 표시한다. ListDir 만 Symlink=true 로 돌려준다.
func (e *Env) SetSymlink(p string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.links[p] = true
}

func (e *Env) File(p string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, ok := e.files[p]
	return string(b), ok
}

// FailOn 은 p 에 대한 모든 FileSystem 호출이 err 를 돌려주게 한다.
func (e *Env) FailOn(p string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fail[p] = err
}

// AppendCount 는 p 에 대한 AppendFile 호출 수다.
func (e *Env) AppendCount(p string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.appends[p]
}

func (e *Env) SetScript(s Script) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.script = s
}

func (e *Env) ExecRequests() []env.ExecRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]env.ExecRequest(nil), e.reqs...)
}

// mkdirAll 은 e.mu 를 잡은 상태에서 부른다.
func (e *Env) mkdirAll(p string) {
	for p != "/" && p != "." && p != "" {
		e.dirs[p] = true
		p = path.Dir(p)
	}
}

// check 는 e.mu 를 잡은 상태에서 부른다.
func (e *Env) check(op, p string) error {
	if !path.IsAbs(p) {
		return &fs.PathError{Op: op, Path: p, Err: fs.ErrInvalid}
	}
	if err := e.fail[p]; err != nil {
		return &fs.PathError{Op: op, Path: p, Err: err}
	}
	return nil
}

func notExist(op, p string) error { return &fs.PathError{Op: op, Path: p, Err: fs.ErrNotExist} }

func (e *Env) ReadFile(ctx context.Context, p string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check("read", p); err != nil {
		return nil, err
	}
	b, ok := e.files[p]
	if !ok {
		if e.dirs[p] {
			return nil, &fs.PathError{Op: "read", Path: p, Err: errors.New("is a directory")}
		}
		return nil, notExist("read", p)
	}
	return bytes.Clone(b), nil
}

func (e *Env) WriteFile(ctx context.Context, p string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check("write", p); err != nil {
		return err
	}
	if e.dirs[p] {
		return &fs.PathError{Op: "write", Path: p, Err: errors.New("is a directory")}
	}
	e.mkdirAll(path.Dir(p))
	e.files[p] = bytes.Clone(data)
	return nil
}

func (e *Env) AppendFile(ctx context.Context, p string, data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check("append", p); err != nil {
		return err
	}
	if e.dirs[p] {
		return &fs.PathError{Op: "append", Path: p, Err: errors.New("is a directory")}
	}
	e.mkdirAll(path.Dir(p))
	e.files[p] = append(e.files[p], data...)
	e.appends[p]++
	return nil
}

func (e *Env) Stat(ctx context.Context, p string) (env.FileInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check("stat", p); err != nil {
		return env.FileInfo{}, err
	}
	if b, ok := e.files[p]; ok {
		return env.FileInfo{Name: path.Base(p), Size: int64(len(b))}, nil
	}
	if e.dirs[p] {
		return env.FileInfo{Name: path.Base(p), IsDir: true}, nil
	}
	return env.FileInfo{}, notExist("stat", p)
}

func (e *Env) ListDir(ctx context.Context, p string) ([]env.FileInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.check("list", p); err != nil {
		return nil, err
	}
	if !e.dirs[p] {
		return nil, notExist("list", p)
	}
	var out []env.FileInfo
	for f, b := range e.files {
		if path.Dir(f) == p {
			out = append(out, env.FileInfo{Name: path.Base(f), Size: int64(len(b)), Symlink: e.links[f]})
		}
	}
	for d := range e.dirs {
		if d != p && path.Dir(d) == p {
			out = append(out, env.FileInfo{Name: path.Base(d), IsDir: true, Symlink: e.links[d]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (e *Env) CreateTemp(ctx context.Context, pattern string) (string, io.WriteCloser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.temps++
	name := strings.Replace(pattern, "*", fmt.Sprint(e.temps), 1)
	if name == pattern {
		name = fmt.Sprintf("%s%d", pattern, e.temps)
	}
	p := "/tmp/" + name
	e.mkdirAll("/tmp")
	e.files[p] = nil
	return p, &tempWriter{e: e, path: p}, nil
}

type tempWriter struct {
	e    *Env
	path string
}

func (w *tempWriter) Write(b []byte) (int, error) {
	w.e.mu.Lock()
	defer w.e.mu.Unlock()
	w.e.files[w.path] = append(w.e.files[w.path], b...)
	return len(b), nil
}

func (w *tempWriter) Close() error { return nil }

func (e *Env) Exec(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
	e.mu.Lock()
	e.reqs = append(e.reqs, req)
	s := e.script
	e.mu.Unlock()
	if s == nil {
		return env.ExecResult{}, errors.New("envtest: no shell script")
	}
	return s(ctx, req, out)
}
