package envtest

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

// ConformanceFS 는 FileSystem 구현이 HN-FR-01~03 의 계약을 지키는지 검사한다. root 는 비어 있는 절대 경로 디렉터리다.
func ConformanceFS(t *testing.T, fsys env.FileSystem, root string) {
	t.Helper()
	ctx := context.Background()
	missing := root + "/missing"
	if _, err := fsys.ReadFile(ctx, missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile missing: %v", err)
	}
	if _, err := fsys.Stat(ctx, missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat missing: %v", err)
	}
	if _, err := fsys.ListDir(ctx, missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ListDir missing: %v", err)
	}

	deep := root + "/a/b/c.txt"
	if err := fsys.WriteFile(ctx, deep, []byte("one")); err != nil {
		t.Fatalf("WriteFile creates parents: %v", err)
	}
	if err := fsys.WriteFile(ctx, deep, []byte("two")); err != nil {
		t.Fatalf("WriteFile overwrite: %v", err)
	}
	if b, err := fsys.ReadFile(ctx, deep); err != nil || string(b) != "two" {
		t.Errorf("ReadFile after overwrite: %q %v", b, err)
	}

	log := root + "/x/y.log"
	for _, s := range []string{"a\n", "b\n"} {
		if err := fsys.AppendFile(ctx, log, []byte(s)); err != nil {
			t.Fatalf("AppendFile: %v", err)
		}
	}
	if b, _ := fsys.ReadFile(ctx, log); string(b) != "a\nb\n" {
		t.Errorf("AppendFile content %q", b)
	}

	if info, err := fsys.Stat(ctx, deep); err != nil || info.IsDir || info.Size != 3 || info.Name != "c.txt" {
		t.Errorf("Stat file %+v %v", info, err)
	}
	if info, err := fsys.Stat(ctx, root+"/a"); err != nil || !info.IsDir {
		t.Errorf("Stat dir %+v %v", info, err)
	}

	fsys.WriteFile(ctx, root+"/a/z", nil)
	fsys.WriteFile(ctx, root+"/a/m", nil)
	list, err := fsys.ListDir(ctx, root+"/a")
	var names []string
	for _, f := range list {
		names = append(names, f.Name)
	}
	if err != nil || strings.Join(names, ",") != "b,m,z" || !list[0].IsDir {
		t.Errorf("ListDir %v %v", names, err)
	}

	p, w, err := fsys.CreateTemp(ctx, "conf-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	w.Write([]byte("tmp"))
	if err := w.Close(); err != nil {
		t.Fatalf("temp close: %v", err)
	}
	if !strings.HasPrefix(p, "/") {
		t.Errorf("temp path not absolute: %q", p)
	}
	if b, err := fsys.ReadFile(ctx, p); err != nil || string(b) != "tmp" {
		t.Errorf("temp content %q %v", b, err)
	}
}

// ConformanceShell 은 실제 셸 구현이 HN-FR-03 의 계약을 지키는지 검사한다. dir 은 존재하는 절대 경로다.
func ConformanceShell(t *testing.T, sh env.Shell, dir string) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	res, err := sh.Exec(ctx, env.ExecRequest{Command: "echo out; echo err 1>&2", Dir: dir}, &out)
	if err != nil || res.ExitCode != 0 || !strings.Contains(out.String(), "out") || !strings.Contains(out.String(), "err") {
		t.Errorf("combined output %q %+v %v", out.String(), res, err)
	}
	if res, err := sh.Exec(ctx, env.ExecRequest{Command: "exit 3", Dir: dir}, &out); err != nil || res.ExitCode != 3 {
		t.Errorf("exit code %+v %v", res, err)
	}
	out.Reset()
	sh.Exec(ctx, env.ExecRequest{Command: "pwd", Dir: dir}, &out)
	if base := dir[strings.LastIndex(dir, "/")+1:]; !strings.Contains(out.String(), base) {
		t.Errorf("dir: pwd = %q, want %s", out.String(), dir)
	}

	start := time.Now()
	res, err = sh.Exec(ctx, env.ExecRequest{Command: "sleep 5 & sleep 5", Dir: dir, Timeout: 200 * time.Millisecond}, &out)
	if err != nil || !res.TimedOut || time.Since(start) > 3*time.Second {
		t.Errorf("timeout %+v %v after %v", res, err, time.Since(start))
	}

	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	sh.Exec(cctx, env.ExecRequest{Command: "sleep 5 & sleep 5", Dir: dir}, &out)
	if time.Since(start) > 3*time.Second {
		t.Errorf("cancel did not stop children: %v", time.Since(start))
	}
}
