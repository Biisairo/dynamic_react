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
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

// llm 은 Chat Completions 호환 가짜 서버다. 호출 순서대로 handlers 를 쓰고 마지막 것을 반복한다.
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

func sse(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			io.WriteString(w, "data: "+c+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}
}

func textReply(s string) http.HandlerFunc {
	q, _ := json.Marshal(s)
	return sse(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":` + string(q) + `},"finish_reason":"stop"}]}`)
}

func toolReply(name, args string) http.HandlerFunc {
	q, _ := json.Marshal(args)
	return sse(`{"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"` + name + `","arguments":` + string(q) + `}}]},"finish_reason":"tool_calls"}]}`)
}

// setup 은 HOME·cwd 를 임시 디렉터리로 두고 가짜 서버를 provider "test" 로 등록한다.
func setup(t *testing.T, l *llm) string {
	t.Helper()
	home, cwd := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	url := "http://127.0.0.1:1"
	if l != nil {
		url = l.URL
	}
	cfg := `{"sessionDir":"~/sess","agent":{"model":{"provider":"test","id":"m"}},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}}`
	p := filepath.Join(home, ".dynamic_react", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(cfg), 0o644)
	return cwd
}

type result struct {
	code           int
	stdout, stderr string
}

func call(t *testing.T, ctx context.Context, stdin string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runWith(ctx, args, strings.NewReader(stdin), &out, &errOut, func(o *agent.Options) { o.NoRemoteCatalog = true })
	return result{code, out.String(), errOut.String()}
}

// DP-FR-01, 02
func TestFlagsAndPrompt(t *testing.T) {
	ctx := context.Background()
	if r := call(t, ctx, "", "-nope", "x"); r.code != 1 || !strings.Contains(r.stderr, "-nope") {
		t.Errorf("bad flag: %+v", r)
	}
	cwd := setup(t, nil)
	if r := call(t, ctx, "  \n", "-cwd", cwd); r.code != 1 || !strings.Contains(r.stderr, "dr-print: empty prompt") {
		t.Errorf("empty prompt: %+v", r)
	}
	l := newLLM(t, textReply("ok"))
	cwd = setup(t, l)
	if r := call(t, ctx, "stdin-unused", "-cwd", cwd, "hello", "world"); r.code != 0 || !strings.Contains(l.body(0), "hello world") || strings.Contains(l.body(0), "stdin-unused") {
		t.Errorf("args prompt: %+v %.300s", r, l.body(0))
	}
	if r := call(t, ctx, "from stdin\n", "-cwd", cwd); r.code != 0 || !strings.Contains(l.body(1), "from stdin") {
		t.Errorf("stdin prompt: %+v", r)
	}
}

// DP-FR-01, 04: -model 이 설정을 덮고, 키가 없으면 provider 를 알린다 (환경변수의 키는 보지 않음, R21).
func TestModelAndMissingKey(t *testing.T) {
	cwd := setup(t, nil)
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-env")
	r := call(t, context.Background(), "", "-cwd", cwd, "-model", "anthropic/claude-x", "hi")
	if r.code != 1 || r.stderr != "dr-print: missing API key for \"anthropic\"\n" {
		t.Fatalf("%+v", r)
	}
	if r := call(t, context.Background(), "", "-cwd", cwd, "-config", filepath.Join(cwd, "none.json"), "hi"); r.code != 1 {
		t.Fatalf("-config not used: %+v", r)
	}
}

// DP-FR-05, 06
func TestTextAndTools(t *testing.T) {
	l := newLLM(t, toolReply("bash", `{"command":"echo hi"}`), textReply("hello"))
	cwd := setup(t, l)
	r := call(t, context.Background(), "", "-cwd", cwd, "go")
	if r.code != 0 || r.stdout != "hello\n" {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(r.stderr, "session: ") || !strings.Contains(r.stderr, `[tool] bash {"command":"echo hi"}`) {
		t.Fatalf("stderr %q", r.stderr)
	}
	l2 := newLLM(t, textReply("line\n"))
	cwd = setup(t, l2)
	if r := call(t, context.Background(), "", "-cwd", cwd, "go"); r.stdout != "line\n" {
		t.Fatalf("extra newline: %q", r.stdout)
	}
	long := strings.Repeat("x", 300)
	l3 := newLLM(t, toolReply("bash", `{"command":"`+long+`"}`), textReply("done"))
	cwd = setup(t, l3)
	r = call(t, context.Background(), "", "-cwd", cwd, "go")
	line := r.stderr[strings.Index(r.stderr, "[tool]"):]
	line = line[:strings.Index(line, "\n")]
	if !strings.HasSuffix(line, "…") || len(line) > len("[tool] bash ")+200+len("…") {
		t.Fatalf("tool args not truncated: %q", line)
	}
}

// DP-FR-07, 3.1
func TestJSON(t *testing.T) {
	l := newLLM(t, textReply("hello"))
	cwd := setup(t, l)
	r := call(t, context.Background(), "", "-cwd", cwd, "-json", "go")
	if r.code != 0 || !strings.HasPrefix(r.stderr, "session: ") {
		t.Fatalf("%+v", r)
	}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	var evs []map[string]any
	for _, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("not json: %q", ln)
		}
		evs = append(evs, m)
	}
	if evs[0]["kind"] != "run_started" || evs[len(evs)-1]["kind"] != "run_settled" {
		t.Fatalf("first/last %v %v", evs[0], evs[len(evs)-1])
	}
	if s, _ := evs[len(evs)-1]["settle"].(map[string]any); s["reason"] != "completed" {
		t.Fatalf("settle %v", evs[len(evs)-1])
	}
	var sawDelta, sawStep bool
	for _, e := range evs {
		if s, ok := e["stream"].(map[string]any); ok && s["kind"] == "text_delta" && s["delta"] == "hello" {
			sawDelta = true
		}
		if e["kind"] == "step_ended" && e["runId"] != "" && e["unit"] != nil {
			sawStep = true
		}
		if e["step"] == "step(invalid)" || (e["kind"] == "stream_event" && e["step"] != nil) {
			t.Fatalf("invalid step emitted: %v", e)
		}
	}
	if !sawDelta || !sawStep || strings.Contains(r.stdout, "hello\n{") {
		t.Fatalf("events %s", r.stdout)
	}
}

// DP-FR-08
func TestExitCodes(t *testing.T) {
	for reason, want := range map[core.SettleReason]int{
		core.SettleCompleted: 0, core.SettleStopped: 0, core.SettleFailed: 1, core.SettleGuard: 2, core.SettleAborted: 130,
	} {
		if got := exitCode(reason); got != want {
			t.Errorf("%s: %d, want %d", reason, got, want)
		}
	}
	l := newLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
	})
	cwd := setup(t, l)
	r := call(t, context.Background(), "", "-cwd", cwd, "go")
	if r.code != 1 || !strings.Contains(r.stderr, "dr-print: failed") || !strings.Contains(r.stderr, "[error] ") {
		t.Fatalf("%+v", r)
	}
}

// DP-FR-09: ctx 취소 → aborted → 130.
func TestCancel(t *testing.T) {
	l := newLLM(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"c","choices":[{"index":0,"delta":{"role":"assistant","content":"wait"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	cwd := setup(t, l)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	if r := call(t, ctx, "", "-cwd", cwd, "go"); r.code != 130 {
		t.Fatalf("%+v", r)
	}
}

// DP-FR-01 -session: 같은 id 로 이어서.
func TestSession(t *testing.T) {
	l := newLLM(t, textReply("first answer"), textReply("second"))
	cwd := setup(t, l)
	r := call(t, context.Background(), "", "-cwd", cwd, "remember this")
	id := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(r.stderr, "\n", 2)[0], "session: "))
	if r.code != 0 || id == "" {
		t.Fatalf("%+v", r)
	}
	r = call(t, context.Background(), "", "-cwd", cwd, "-session", id, "next")
	if r.code != 0 || !strings.HasPrefix(r.stderr, "session: "+id+"\n") || !strings.Contains(l.body(1), "first answer") || !strings.Contains(l.body(1), "remember this") {
		t.Fatalf("%+v %.300s", r, l.body(1))
	}
}

// DP-FR-06: 200 byte 에서 자르되 UTF-8 문자 중간에서 자르지 않는다.
func TestTruncate(t *testing.T) {
	if got := truncate("short", 200); got != "short" {
		t.Errorf("short %q", got)
	}
	got := truncate(strings.Repeat("가", 100), 200) // 300 byte, 200 은 문자 중간
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") || len(got) != 198+len("…") {
		t.Errorf("multibyte %q (%d bytes)", got, len(got))
	}
}

// DP-FR-05: release 전에 나온 stderr 줄은 session 줄 뒤로 간다.
func TestSessionLineFirst(t *testing.T) {
	var out, errOut bytes.Buffer
	p := newPrinter(false, &out, &errOut)
	p.Observe(core.ObserverEvent{Kind: core.ObsStreamEvent, Stream: &contract.StreamEvent{Kind: contract.EventToolCallEnd,
		ToolCall: &contract.ToolCall{ID: "c", Name: "bash", Args: json.RawMessage(`{}`)}}})
	p.release("session: s1\n")
	p.Observe(core.ObserverEvent{Kind: core.ObsStreamEvent, Stream: &contract.StreamEvent{Kind: contract.EventError,
		Error: &contract.CallError{Class: contract.ClassAuth, Message: "m"}}})
	p.finish()
	if errOut.String() != "session: s1\n[tool] bash {}\n[error] auth: m\n" || out.Len() != 0 {
		t.Fatalf("stderr %q stdout %q", errOut.String(), out.String())
	}
}
