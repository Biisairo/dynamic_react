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

func newLLM(t *testing.T, replies ...string) *llm {
	l := &llm{}
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		n := len(l.bodies)
		l.bodies = append(l.bodies, string(b))
		l.mu.Unlock()
		q, _ := json.Marshal(replies[min(n, len(replies)-1)])
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"t","choices":[{"index":0,"delta":{"role":"assistant","content":`+string(q)+`},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
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

func setup(t *testing.T, url string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := `{"agent":{"model":{"provider":"test","id":"m"}},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}}`
	p := filepath.Join(home, ".dynamic_react", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return t.TempDir()
}

// MX-FR-08, 09: /modes, /mode plan → 계획 지시가 다음 메시지에만, run 끝 이탈이 그다음 메시지에서 적용
func TestInteractive(t *testing.T) {
	l := newLLM(t, "hello", "1. plan step", "back to normal")
	cwd := setup(t, l.URL)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close(); out.Close() })
	h := hooks{stdin: r, stdout: out,
		isTerminal: func(int) bool { return true },
		open: func(*os.File) (func() error, func() int, error) {
			return func() error { return nil }, func() int { return 120 }, nil
		},
		agentOptions: func(o *agent.Options) { o.NoRemoteCatalog = true }}
	screen := func() string { b, _ := os.ReadFile(out.Name()); return string(b) }
	waitFor := func(want string, n int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for strings.Count(screen(), want) < n && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if strings.Count(screen(), want) < n {
			t.Fatalf("screen lacks %q x%d:\n%s", want, n, screen())
		}
	}
	done := make(chan int, 1)
	var errOut bytes.Buffer
	go func() { done <- runWith(context.Background(), []string{"-no-color", "-cwd", cwd}, &errOut, h) }()
	w.Write([]byte("/modes\r"))
	waitFor("  goal  keep working until a judge says the goal is met (arg = goal)", 1)
	waitFor("  plan  plan only; write/edit/bash are blocked (arg = task)", 1)
	w.Write([]byte("hi\r"))
	waitFor("── completed", 1)
	w.Write([]byte("/mode plan add login\r"))
	waitFor("── 모드 예약: plan (다음 메시지부터)", 1)
	w.Write([]byte("go\r"))
	waitFor("── 모드 기본 → plan (user)", 1)
	waitFor("── completed", 2)
	w.Write([]byte("again\r"))
	waitFor("── 모드 plan → 기본 (set): plan done", 1)
	waitFor("── completed", 3)
	w.Write([]byte("/quit\r"))
	if code := <-done; code != 0 {
		t.Fatalf("code %d err %q", code, errOut.String())
	}
	if strings.Contains(l.body(0), "[plan mode]") || !strings.Contains(l.body(1), "[plan mode]") ||
		!strings.Contains(l.body(1), "Task: add login") || strings.Contains(l.body(2), "[plan mode]") {
		t.Fatal("plan mode applied to the wrong requests")
	}
	if strings.Contains(l.body(1), `"name":"write"`) || !strings.Contains(l.body(2), `"name":"write"`) {
		t.Fatal("plan mode did not remove write from its request only")
	}
}

// MX-FR-08, 09: 위치 인자·비TTY → 2
func TestArgs(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		tty  bool
		want string
	}{
		"not tty":    {nil, false, "modes-example: needs an interactive terminal"},
		"positional": {[]string{"x"}, true, "modes-example: takes no prompt arguments"},
	} {
		var errOut bytes.Buffer
		h := hooks{isTerminal: func(int) bool { return c.tty }}
		if code := runWith(context.Background(), c.args, &errOut, h); code != 2 || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%s: code %d err %q", name, code, errOut.String())
		}
	}
}
