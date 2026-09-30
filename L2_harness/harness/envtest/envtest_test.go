package envtest_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

var _ env.Env = (*envtest.Env)(nil)

// HN-FR-04: 메모리 FS 가 FileSystem 계약을 통과한다.
func TestConformanceFSOnMemory(t *testing.T) {
	envtest.ConformanceFS(t, envtest.New("/w", "/h"), "/root")
}

func TestRelativePathRejected(t *testing.T) {
	e := envtest.New("/w", "/h")
	if _, err := e.ReadFile(context.Background(), "rel.txt"); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("relative path: %v", err)
	}
}

func TestHelpersAndFailOn(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/w", "/h")
	if e.Cwd() != "/w" || e.Home() != "/h" {
		t.Fatal("cwd/home")
	}
	e.SetFile("/w/a.txt", "hi")
	if got, ok := e.File("/w/a.txt"); !ok || got != "hi" {
		t.Fatalf("file %q %v", got, ok)
	}
	if info, err := e.Stat(ctx, "/w"); err != nil || !info.IsDir {
		t.Fatalf("parent dir %+v %v", info, err)
	}
	boom := errors.New("disk")
	e.FailOn("/w/a.txt", boom)
	if _, err := e.ReadFile(ctx, "/w/a.txt"); !errors.Is(err, boom) {
		t.Fatalf("FailOn read %v", err)
	}
	if err := e.AppendFile(ctx, "/w/a.txt", []byte("x")); !errors.Is(err, boom) {
		t.Fatalf("FailOn append %v", err)
	}
	e.AppendFile(ctx, "/w/log", []byte("a"))
	e.AppendFile(ctx, "/w/log", []byte("b"))
	if e.AppendCount("/w/log") != 2 {
		t.Fatalf("append count %d", e.AppendCount("/w/log"))
	}
}

func TestScriptedShell(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/w", "/h")
	if _, err := e.Exec(ctx, env.ExecRequest{Command: "x"}, io.Discard); err == nil {
		t.Fatal("exec without script")
	}
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		io.WriteString(out, "ran "+req.Command)
		return env.ExecResult{ExitCode: 2}, nil
	})
	var buf stringWriter
	res, err := e.Exec(ctx, env.ExecRequest{Command: "ls", Dir: "/w"}, &buf)
	if err != nil || res.ExitCode != 2 || string(buf) != "ran ls" {
		t.Fatalf("exec %+v %v %q", res, err, buf)
	}
	if reqs := e.ExecRequests(); len(reqs) != 2 || reqs[1].Command != "ls" || reqs[1].Dir != "/w" {
		t.Fatalf("requests %+v", reqs)
	}
}

type stringWriter []byte

func (w *stringWriter) Write(p []byte) (int, error) { *w = append(*w, p...); return len(p), nil }
