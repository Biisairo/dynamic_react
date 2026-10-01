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
	code := runWith(ctx, args, &out, &errOut, func(o *agent.Options) { o.NoRemoteCatalog = true })
	return code, out.String(), errOut.String()
}

func toolResult(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "tool" {
			return req.Messages[i].Content
		}
	}
	return ""
}

// EX-FR-01~03, 05, 06
func TestRuleAndBlock(t *testing.T) {
	l := newLLM(t,
		toolReply("c1", "bash", `{"command":"rm -rf x"}`),
		toolReply("c2", "bash", `{"command":"echo hello-from-bash"}`),
		toolReply("c3", "nope", `{"command":"rm -rf x"}`),
		textReply("done"))
	f := setup(t, l.URL)
	os.WriteFile(filepath.Join(f.cwd, "x"), []byte("keep"), 0o644)
	code, out, errOut := runTest(context.Background(), "-cwd", f.cwd, "clean", "up")
	if code != 0 || out != "done\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	for i := 0; i < 4; i++ {
		if !strings.Contains(l.body(i), Rule) {
			t.Fatalf("request %d lacks the rule", i)
		}
	}
	if got := toolResult(t, l.body(1)); got != "blocked by example: rm -rf" {
		t.Fatalf("rm -rf result %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(f.cwd, "x")); string(b) != "keep" {
		t.Fatal("blocked command ran")
	}
	if got := toolResult(t, l.body(2)); !strings.Contains(got, "hello-from-bash") {
		t.Fatalf("echo result %q", got)
	}
	if got := toolResult(t, l.body(3)); !strings.Contains(got, "unknown tool: nope") {
		t.Fatalf("base check lost: %q", got)
	}
	if !strings.Contains(errOut, "[tool] bash error\n") || !strings.Contains(errOut, "[tool] bash ok\n") || !strings.Contains(errOut, "[tool] nope error\n") {
		t.Fatalf("stderr %q", errOut)
	}
	files, _ := filepath.Glob(filepath.Join(f.home, ".dynamic_react", "sessions", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("session files %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.Contains(string(b), Rule) {
		t.Fatal("injected rule was stored in the session")
	}
}

// EX-FR-04, 06
func TestArgs(t *testing.T) {
	l := newLLM(t, textReply("hi"))
	f := setup(t, l.URL)
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"empty":      {[]string{"-cwd", f.cwd, "  "}, "inject-example: empty prompt"},
		"no args":    {[]string{"-cwd", f.cwd}, "inject-example: empty prompt"},
		"bad flag":   {[]string{"-nope"}, "-nope"},
		"bad config": {[]string{"-cwd", f.cwd, "-config", "missing.json", "x"}, "inject-example: agent: config"},
		"bad model":  {[]string{"-cwd", f.cwd, "-model", "x", "x"}, "inject-example: "},
	} {
		code, _, errOut := runTest(context.Background(), c.args...)
		if code != 1 || !strings.Contains(errOut, c.want) {
			t.Errorf("%s: code %d err %q", name, code, errOut)
		}
	}
	code, out, _ := runTest(context.Background(), "-cwd", f.cwd, "-model", "test/m", "hello")
	if code != 0 || out != "hi\n" || !strings.Contains(l.body(0), `"model":"m"`) {
		t.Fatalf("code %d out %q body %.200s", code, out, l.body(0))
	}
}

// EX-FR-06: 오류 응답 → failed → 1.
func TestFailed(t *testing.T) {
	l := newLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad"}}`, http.StatusBadRequest)
	})
	f := setup(t, l.URL)
	code, _, errOut := runTest(context.Background(), "-cwd", f.cwd, "x")
	if code != 1 || !strings.Contains(errOut, "inject-example: failed: ") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

// EX-FR-06: ctx 취소 → aborted → 130.
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
	if code, _, errOut := runTest(ctx, "-cwd", f.cwd, "x"); code != 130 {
		t.Fatalf("code %d err %q", code, errOut)
	}
}
