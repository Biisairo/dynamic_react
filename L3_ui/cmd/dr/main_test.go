package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

func llm(t *testing.T, reply string) *httptest.Server {
	q, _ := json.Marshal(reply)
	body := "data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":" + string(q) +
		"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fixture struct {
	stdin, stdinW, stdout *os.File
	stderr                strings.Builder
	restored              atomic.Int32
	hooks                 hooks
}

// setup 은 HOME·설정·가짜 TTY 를 준비한다. stdin 은 파이프, stdout 은 임시 파일.
func setup(t *testing.T, url string) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := `{"agent":{"model":{"provider":"test","id":"m"}},"providers":{"test":{"api":"openai-chat","baseUrl":"` + url +
		`"}},"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}}`
	os.MkdirAll(filepath.Join(home, ".dynamic_react"), 0o755)
	os.WriteFile(filepath.Join(home, ".dynamic_react", "config.json"), []byte(cfg), 0o644)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{stdin: r, stdinW: w, stdout: out}
	t.Cleanup(func() { r.Close(); w.Close(); out.Close() })
	f.hooks = hooks{
		isTerminal: func(int) bool { return true },
		open: func(*os.File) (func() error, func() int, error) {
			return func() error { f.restored.Add(1); return nil }, func() int { return 80 }, nil
		},
		agentOptions: func(o *agent.Options) { o.NoRemoteCatalog = true; o.Cwd = t.TempDir() },
	}
	return f
}

func (f *fixture) run(ctx context.Context, args ...string) int {
	return runWith(ctx, args, f.stdin, f.stdout, &f.stderr, f.hooks)
}

func (f *fixture) out(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(f.stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }

// DR-FR-01: 잘못된 플래그·위치 인자·배타 플래그 → 2.
func TestFlags(t *testing.T) {
	f := setup(t, "http://127.0.0.1:1")
	for _, args := range [][]string{{"-nope"}, {"prompt"}, {"-c", "-r"}, {"-session", "x", "-c"}} {
		f.stderr.Reset()
		if code := f.run(context.Background(), args...); code != 2 || f.stderr.Len() == 0 {
			t.Fatalf("%v: code %d stderr %q", args, code, f.stderr.String())
		}
	}
}

// DR-FR-02: TTY 가 아니면 2 (agent 를 만들기 전).
func TestNotTerminal(t *testing.T) {
	f := setup(t, "http://127.0.0.1:1")
	f.hooks.isTerminal = nil
	if code := f.run(context.Background()); code != 2 || !strings.Contains(f.stderr.String(), "dr: 대화형 터미널이 필요합니다 (비대화형은 dr-print)") {
		t.Fatalf("code %d stderr %q", code, f.stderr.String())
	}
	calls := 0
	f.hooks.isTerminal = func(fd int) bool { calls++; return fd == int(f.stdin.Fd()) }
	if code := f.run(context.Background()); code != 2 || calls != 2 {
		t.Fatalf("stdout not checked: code %d calls %d", code, calls)
	}
}

// DR-FR-03: agent.New 에러 → 1, raw mode 복구. Open 에러 → 1.
func TestInitErrors(t *testing.T) {
	f := setup(t, "http://127.0.0.1:1")
	var restoredBeforeWrite int32 = -1
	probe := writerFunc(func(b []byte) (int, error) {
		if restoredBeforeWrite < 0 {
			restoredBeforeWrite = f.restored.Load()
		}
		return f.stderr.Write(b)
	})
	if code := runWith(context.Background(), []string{"-config", "/nonexistent/config.json"}, f.stdin, f.stdout, probe, f.hooks); code != 1 || !strings.HasPrefix(f.stderr.String(), "dr: ") || restoredBeforeWrite < 1 {
		t.Fatalf("code %d stderr %q restored before write %d", code, f.stderr.String(), restoredBeforeWrite)
	}
	f.stderr.Reset()
	f.hooks.open = func(*os.File) (func() error, func() int, error) { return nil, nil, io.ErrClosedPipe }
	if code := f.run(context.Background()); code != 1 || !strings.Contains(f.stderr.String(), "dr: ") {
		t.Fatalf("code %d stderr %q", code, f.stderr.String())
	}
}

// DR-FR-01, 03, 04: /quit → 0, 색, 복구. -no-color 면 SGR 없음 (NO_COLOR 환경변수는 보지 않음). 플래그 전달.
func TestQuit(t *testing.T) {
	srv := llm(t, "hello")
	t.Setenv("NO_COLOR", "1")
	for _, noColor := range []bool{false, true} {
		f := setup(t, srv.URL)
		args := []string{"-session", "s-1", "-model", "test/m"}
		if noColor {
			args = append(args, "-no-color")
		}
		go func() {
			f.stdinW.Write([]byte("hi\r"))
			time.Sleep(300 * time.Millisecond)
			f.stdinW.Write([]byte("/quit\r"))
		}()
		if code := f.run(context.Background(), args...); code != 0 {
			t.Fatalf("code %d stderr %q", code, f.stderr.String())
		}
		out := f.out(t)
		if !strings.Contains(out, "hello") || f.restored.Load() == 0 {
			t.Fatalf("out %q restored %d", out, f.restored.Load())
		}
		if hasSGR := strings.Contains(out, "\x1b[1m"); hasSGR == noColor {
			t.Fatalf("-no-color=%v but SGR=%v", noColor, hasSGR)
		}
		if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".dynamic_react", "sessions", "s-1.jsonl")); err != nil {
			t.Fatalf("-session not used: %v", err)
		}
	}
}

// DR-FR-04: ctx 취소 → 130.
func TestCancel(t *testing.T) {
	f := setup(t, llm(t, "x").URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if code := f.run(ctx); code != 130 || f.restored.Load() == 0 {
		t.Fatalf("code %d restored %d", code, f.restored.Load())
	}
}

// DR-FR-03: 모델 없는 설정으로도 시작하고 /quit 으로 0 (R20)
func TestNoModel(t *testing.T) {
	f := setup(t, "http://127.0.0.1:1")
	os.WriteFile(filepath.Join(os.Getenv("HOME"), ".dynamic_react", "config.json"), []byte(`{}`), 0o644)
	go func() {
		time.Sleep(300 * time.Millisecond)
		f.stdinW.Write([]byte("\x1b"))
		time.Sleep(300 * time.Millisecond)
		f.stdinW.Write([]byte("/quit\r"))
	}()
	if code := f.run(context.Background()); code != 0 {
		t.Fatalf("code %d stderr %q", code, f.stderr.String())
	}
	if out := f.out(t); !strings.Contains(out, "모델이 설정되지 않았습니다") {
		t.Fatalf("out %q", out)
	}
}
