package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

type llm struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newLLM(t *testing.T, handlers ...http.HandlerFunc) *llm {
	l := &llm{}
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		n := len(l.bodies)
		l.bodies = append(l.bodies, string(b))
		l.mu.Unlock()
		handlers[min(n, len(handlers)-1)](w, r)
	}))
	t.Cleanup(l.Close)
	return l
}

func (l *llm) body(i int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.bodies) {
		return ""
	}
	return l.bodies[i]
}

func sse(chunk string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: "+chunk+"\n\ndata: [DONE]\n\n")
	}
}

func textReply(s string) http.HandlerFunc {
	q, _ := json.Marshal(s)
	return sse(`{"id":"t","choices":[{"index":0,"delta":{"role":"assistant","content":` + string(q) + `},"finish_reason":"stop"}]}`)
}

func toolReply(id, name, args string) http.HandlerFunc {
	q, _ := json.Marshal(args)
	return sse(`{"id":"` + id + `","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + string(q) + `}}]},"finish_reason":"tool_calls"}]}`)
}

type fixture struct{ home, cwd string }

func setup(t *testing.T, url string) *fixture {
	t.Helper()
	f := &fixture{home: t.TempDir(), cwd: t.TempDir()}
	t.Setenv("HOME", f.home)
	cfg := `{"agent":{"model":{"provider":"test","id":"m"}},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}}`
	p := filepath.Join(f.home, ".dynamic_react", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

func runTest(ctx context.Context, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := runWith(ctx, args, &out, &errOut, hooks{agentOptions: func(o *agent.Options) { o.NoRemoteCatalog = true }})
	return code, out.String(), errOut.String()
}

// VX-FR-01
func TestShellCheck(t *testing.T) {
	dir := t.TempDir()
	var log bytes.Buffer
	r, err := ShellCheck(dir, "true", &log)(context.Background())
	if err != nil || !r.OK || log.String() != "[verify] true\n[verify] ok\n" {
		t.Fatalf("true: %+v %v %q", r, err, log.String())
	}
	log.Reset()
	r, err = ShellCheck(dir, "echo bad; pwd; echo err >&2; exit 3", &log)(context.Background())
	if err != nil || r.OK || !strings.Contains(r.Report, "bad\n") || !strings.Contains(r.Report, "err\n") || log.String() != "[verify] echo bad; pwd; echo err >&2; exit 3\n[verify] failed (exit 3)\n" {
		t.Fatalf("fail: %+v %v %q", r, err, log.String())
	}
	if !strings.Contains(r.Report, dir) {
		t.Fatalf("dir not used: %q", r.Report)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ShellCheck(dir, "true", io.Discard)(ctx); err == nil {
		t.Fatal("canceled ctx: no error")
	}
}

// VX-FR-02~04: 실패 → 보고 → 고침 → 통과.
func TestVerifyLoop(t *testing.T) {
	l := newLLM(t,
		toolReply("c1", "write", `{"path":"a.txt","content":"a"}`),
		textReply("done"),
		toolReply("c2", "write", `{"path":"ok","content":"fixed"}`),
		textReply("fixed"))
	f := setup(t, l.URL)
	code, out, errOut := runTest(context.Background(), "-cwd", f.cwd, "-check", "test -f ok", "-attempts", "2", "make", "it")
	if code != 0 || out != "done\nfixed\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	if !strings.Contains(l.body(2), "Verification failed (attempt 1 of 2)") {
		t.Fatalf("report not sent: %.300s", l.body(2))
	}
	if strings.Count(errOut, "[verify] test -f ok\n") != 2 || !strings.Contains(errOut, "[verify] failed (exit 1)\n") || !strings.HasSuffix(errOut, "[verify] ok\n") {
		t.Fatalf("stderr %q", errOut)
	}
}

// VX-FR-04: 상한 도달 → failed → 1.
func TestVerifyGivesUp(t *testing.T) {
	l := newLLM(t, toolReply("c1", "write", `{"path":"a.txt","content":"a"}`), textReply("done"))
	f := setup(t, l.URL)
	code, _, errOut := runTest(context.Background(), "-cwd", f.cwd, "-check", "echo nope; false", "-attempts", "2", "x")
	if code != 1 || !strings.Contains(errOut, "verify-example: failed: core: turn.finish: verify: checks still failing after 2 attempts: nope\n") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

// VX-FR-02: 텍스트만 답하면 검증 없음, -check 필수.
func TestArgs(t *testing.T) {
	l := newLLM(t, textReply("hi"))
	f := setup(t, l.URL)
	code, out, errOut := runTest(context.Background(), "-cwd", f.cwd, "-check", "false", "hello")
	if code != 0 || out != "hi\n" || strings.Contains(errOut, "[verify]") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no check":   {[]string{"-cwd", f.cwd, "x"}, "verify-example: -check is required"},
		"empty":      {[]string{"-cwd", f.cwd, "-check", "true", " "}, "verify-example: empty prompt"},
		"bad flag":   {[]string{"-nope"}, "-nope"},
		"bad config": {[]string{"-cwd", f.cwd, "-check", "true", "-config", "missing.json", "x"}, "verify-example: agent: config"},
	} {
		if code, _, errOut := runTest(context.Background(), c.args...); code != 1 || !strings.Contains(errOut, c.want) {
			t.Errorf("%s: code %d err %q", name, code, errOut)
		}
	}
}

// VX-FR-04: ctx 취소 → aborted → 130.
func TestAbort(t *testing.T) {
	arrived := make(chan struct{})
	l := newLLM(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
	})
	f := setup(t, l.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
		}
		cancel()
	}()
	if code, _, errOut := runTest(ctx, "-cwd", f.cwd, "-check", "true", "x"); code != 130 {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

// VX-FR-05: 대화형 — 실패 보고가 화면에 » 줄로 나오고, 고친 뒤 통과한다.
func TestInteractive(t *testing.T) {
	l := newLLM(t,
		toolReply("c1", "write", `{"path":"a.txt","content":"a"}`),
		textReply("done"),
		toolReply("c2", "write", `{"path":"ok","content":"fixed"}`),
		textReply("all fixed"))
	f := setup(t, l.URL)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close(); out.Close() })
	restored := 0
	h := hooks{stdin: r, stdout: out,
		isTerminal: func(int) bool { return true },
		open: func(*os.File) (func() error, func() int, error) {
			return func() error { restored++; return nil }, func() int { return 100 }, nil
		},
		agentOptions: func(o *agent.Options) {
			o.NoRemoteCatalog = true
			o.Permissions = agent.Permissions{Allow: []string{"write"}}
		}}
	screen := func() string { b, _ := os.ReadFile(out.Name()); return string(b) }
	done := make(chan int, 1)
	var errOut bytes.Buffer
	go func() {
		done <- runWith(context.Background(), []string{"-i", "-no-color", "-cwd", f.cwd, "-check", "test -f ok", "-attempts", "2"}, io.Discard, &errOut, h)
	}()
	w.Write([]byte("make it\r"))
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(screen(), "── completed") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	w.Write([]byte("/quit\r"))
	if code := <-done; code != 0 || restored == 0 {
		t.Fatalf("code %d restored %d err %q", code, restored, errOut.String())
	}
	s := screen()
	for _, want := range []string{"> make it", "done", "» Verification failed (attempt 1 of 2). Fix the problems below, then finish.", "(no output)", "all fixed", "── completed"} {
		if !strings.Contains(s, want) {
			t.Fatalf("screen lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "[verify]") || strings.Contains(s, "» make it") || errOut.Len() != 0 {
		t.Fatalf("log leaked or own input redrawn: %q / %q", s, errOut.String())
	}
}

// VX-FR-05: 비TTY·위치 인자 → 2.
func TestInteractiveArgs(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		tty  bool
		want string
	}{
		"not tty":    {[]string{"-i", "-check", "true"}, false, "verify-example: -i needs an interactive terminal"},
		"positional": {[]string{"-i", "-check", "true", "x"}, true, "verify-example: -i takes no prompt arguments"},
	} {
		var errOut bytes.Buffer
		h := hooks{isTerminal: func(int) bool { return c.tty }}
		if code := runWith(context.Background(), c.args, io.Discard, &errOut, h); code != 2 || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%s: code %d err %q", name, code, errOut.String())
		}
	}
}
