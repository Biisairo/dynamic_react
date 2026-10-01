package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

// llm 은 Chat Completions 호환 가짜 서버다. 호출 순서대로 replies 를 돌려주고 마지막 것을 반복한다.
type llm struct {
	*httptest.Server
	mu      sync.Mutex
	bodies  []string
	heads   []http.Header
	replies []string
}

func newLLM(t *testing.T, replies ...string) *llm {
	l := &llm{replies: replies}
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		n := len(l.bodies)
		l.bodies = append(l.bodies, string(b))
		l.heads = append(l.heads, r.Header.Clone())
		l.mu.Unlock()
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, l.replies[min(n, len(l.replies)-1)])
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

func (l *llm) header(i int) http.Header {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.heads) {
		return http.Header{}
	}
	return l.heads[i]
}

func sse(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: " + c + "\n\n")
	}
	return b.String() + "data: [DONE]\n\n"
}

func textReply(s string) string {
	q, _ := json.Marshal(s)
	return sse(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":` + string(q) + `},"finish_reason":"stop"}]}`)
}

func toolReply(name, args string) string {
	q, _ := json.Marshal(args)
	return sse(`{"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"` + name + `","arguments":` + string(q) + `}}]},"finish_reason":"tool_calls"}]}`)
}

// testConfig 는 가짜 서버를 provider "test" 로 등록한 설정이다.
func testConfig(url string, extra string) string {
	return `{"agent":{"model":{"provider":"test","id":"m"}},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}` + extra + `}`
}

type fixture struct {
	home, cwd string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{home: t.TempDir(), cwd: t.TempDir()}
	t.Setenv("HOME", f.home)
	return f
}

func (f *fixture) writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(f.home, ".dynamic_react", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) writeAuth(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(f.home, ".dynamic_react", "auth.json")
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) opts() agent.Options {
	return agent.Options{Cwd: f.cwd, NoRemoteCatalog: true}
}

func user(s string) []contract.Message {
	return []contract.Message{{ID: "u-" + s, Role: contract.RoleUser, Timestamp: time.Now(), Content: []contract.Block{{Kind: contract.BlockText, Text: s}}}}
}

func runOnce(t *testing.T, a *agent.Agent, session, prompt string) (core.Settle, string) {
	t.Helper()
	r, err := a.Start(context.Background(), session, user(prompt))
	if err != nil {
		t.Fatal(err)
	}
	return r.Wait(), r.SessionID()
}

// AG-FR-01
func TestConfigPath(t *testing.T) {
	f := setup(t)
	o := f.opts()
	o.Model = "ollama/llama3"
	if _, err := agent.New(context.Background(), o); err != nil {
		t.Fatalf("missing default config: %v", err)
	}
	o.ConfigPath = filepath.Join(f.cwd, "nope.json")
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("missing explicit config accepted")
	}
	os.WriteFile(filepath.Join(f.cwd, "c.json"), []byte(`{"sessionDir":"/x/sessions"}`), 0o644)
	o.ConfigPath = "c.json"
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionDir() != "/x/sessions" {
		t.Fatalf("relative config not resolved against cwd: %q", a.SessionDir())
	}
}

// AG-FR-02, 03
func TestConfigValidation(t *testing.T) {
	for name, content := range map[string]string{
		"unknown top key":      `{"agent":{},"theme":"dark"}`,
		"not json":             `{`,
		"trailing data":        `{} {}`,
		"unknown provider key": `{"providers":{"x":{"apiKey":"sk-1"}}}`,
		"unknown agent key":    `{"agent":{"colour":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.writeConfig(t, content)
			o := f.opts()
			o.Model = "ollama/llama3"
			_, err := agent.New(context.Background(), o)
			if err == nil || !strings.HasPrefix(err.Error(), "agent: config:") {
				t.Fatalf("err %v", err)
			}
		})
	}
}

// AG-FR-04, 05: Options.Model 이 설정을 덮고, 첫 "/" 에서 나눈다.
func TestModelOverride(t *testing.T) {
	for model, wantID := range map[string]string{"": "m", "test/b": "b", "test/x/y": "x/y"} {
		t.Run(model, func(t *testing.T) {
			f := setup(t)
			l := newLLM(t, textReply("ok"))
			f.writeConfig(t, testConfig(l.URL, ""))
			o := f.opts()
			o.Model = model
			a, err := agent.New(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if s, _ := runOnce(t, a, "", "hi"); s.Reason != core.SettleCompleted {
				t.Fatalf("%+v", s)
			}
			var req struct{ Model string }
			json.Unmarshal([]byte(l.body(0)), &req)
			if req.Model != wantID {
				t.Fatalf("model %q, want %q", req.Model, wantID)
			}
		})
	}
	for _, bad := range []string{"test/", "/m", "testm"} {
		f := setup(t)
		o := f.opts()
		o.Model = bad
		if _, err := agent.New(context.Background(), o); err == nil {
			t.Errorf("model %q accepted", bad)
		}
	}
	f := setup(t)
	a, err := agent.New(context.Background(), f.opts())
	if err != nil {
		t.Fatalf("no model: %v", err)
	}
	if ref, err := a.Model(); ref != "" || !errors.Is(err, agent.ErrNoModel) {
		t.Fatalf("no model: %q %v", ref, err)
	}
}

// AG-FR-06, 07
func TestSessionDir(t *testing.T) {
	cases := []struct {
		name, cfg, want string
	}{
		{"default", "", "HOME/.dynamic_react/sessions"},
		{"config", "/c/s", "/c/s"},
		{"tilde", "~/mine", "HOME/mine"},
		{"tilde alone", "~", "HOME"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			t.Setenv("DR_SESSION_DIR", "/e/s") // 환경변수는 보지 않는다 (R21)
			if c.cfg != "" {
				f.writeConfig(t, `{"sessionDir":"`+c.cfg+`"}`)
			}
			o := f.opts()
			o.Model = "ollama/llama3"
			a, err := agent.New(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Replace(c.want, "HOME", f.home, 1); a.SessionDir() != want {
				t.Fatalf("got %q, want %q", a.SessionDir(), want)
			}
			if _, err := os.Stat(a.SessionDir()); err == nil && c.name != "tilde alone" {
				t.Fatal("session dir created in New")
			}
		})
	}
	f := setup(t)
	f.writeConfig(t, `{"sessionDir":"rel/s"}`)
	o := f.opts()
	o.Model = "ollama/llama3"
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Error("relative config accepted")
	}
}

// AG-FR-08, AG-C1: 모델 문제는 New 를 실패시키지 않고 Model()·Start 로 알린다.
func TestAPIKey(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, `{"providers":{"mine":{"api":"openai-chat","baseUrl":"`+l.URL+`","requiresKey":true}}}`)
	newWith := func(model string) *agent.Agent {
		t.Helper()
		o := f.opts()
		o.Model = model
		a, err := agent.New(context.Background(), o)
		if err != nil {
			t.Fatalf("%s: New %v", model, err)
		}
		return a
	}
	a := newWith("nope/m")
	if _, err := a.Model(); err == nil || err.Error() != `unknown provider "nope"` {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := a.Start(context.Background(), "", user("hi")); err == nil || err.Error() != `unknown provider "nope"` {
		t.Fatalf("unknown provider Start: %v", err)
	}
	a = newWith("mine/m")
	if _, err := a.Model(); !errors.Is(err, agent.ErrNoAPIKey) || err.Error() != `missing API key for "mine"` {
		t.Fatalf("missing key: %v", err)
	}
	if _, err := a.Start(context.Background(), "", user("hi")); !errors.Is(err, agent.ErrNoAPIKey) {
		t.Fatalf("missing key Start: %v", err)
	}
	f.writeAuth(t, `{"mine":{"type":"api_key","key":"sk-secret-value"},"anthropic":{"type":"api_key","key":""}}`)
	a = newWith("mine/m")
	if ref, err := a.Model(); ref != "mine/m" || err != nil {
		t.Fatalf("key present: %q %v", ref, err)
	}
	if s, _ := runOnce(t, a, "", "hi"); s.Reason != core.SettleCompleted || l.header(0).Get("Authorization") != "Bearer sk-secret-value" {
		t.Fatalf("%+v %v", s, l.header(0))
	}
	_, err := newWith("anthropic/claude-x").Model()
	if !errors.Is(err, agent.ErrNoAPIKey) || !strings.Contains(err.Error(), `"anthropic"`) || strings.Contains(err.Error(), "sk-secret-value") {
		t.Fatalf("anthropic: %v", err)
	}
}

// AG-FR-09~11: 실제 osenv + 가짜 서버로 bash 도구 라운드, 세션 파일, 재개.
func TestEndToEnd(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"echo hi > out.txt; echo done"}`), textReply("finished"), textReply("again"))
	f.writeConfig(t, testConfig(l.URL, `,"sessionDir":"~/sess"`))
	var mu sync.Mutex
	var kinds []core.ObserverEventKind
	o := f.opts()
	o.Observer = observerFunc(func(ev core.ObserverEvent) {
		mu.Lock()
		kinds = append(kinds, ev.Kind)
		mu.Unlock()
	})
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.Harness().System()[0].Text, "<cwd>\n"+f.cwd+"\n</cwd>") {
		t.Fatal("system prompt lacks cwd")
	}
	s, id := runOnce(t, a, "", "make a file")
	if s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	if b, err := os.ReadFile(filepath.Join(f.cwd, "out.txt")); err != nil || string(b) != "hi\n" {
		t.Fatalf("bash did not run in cwd: %q %v", b, err)
	}
	if !strings.Contains(l.body(0), `"max_completion_tokens":1000`) {
		t.Fatalf("models config not applied: %.200s", l.body(0))
	}
	if !strings.Contains(l.body(1), "done") {
		t.Fatalf("tool result not sent: %s", l.body(1))
	}
	if _, err := os.Stat(filepath.Join(f.home, "sess", id+".jsonl")); err != nil {
		t.Fatalf("session file: %v", err)
	}
	if s, id2 := runOnce(t, a, id, "again"); s.Reason != core.SettleCompleted || id2 != id {
		t.Fatalf("%+v %s", s, id2)
	}
	if !strings.Contains(l.body(2), "finished") || !strings.Contains(l.body(2), "make a file") {
		t.Fatalf("history not resumed: %s", l.body(2))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(kinds) == 0 || kinds[len(kinds)-1] != core.ObsRunSettled {
		t.Fatalf("observer events %v", kinds)
	}
}

type observerFunc func(core.ObserverEvent)

func (f observerFunc) Observe(ev core.ObserverEvent) { f(ev) }

// AG-NFR-02: 실제 provider 는 테스트 전용 DR_E2E_MODEL·DR_E2E_KEY 가 있을 때만.
func TestLiveProvider(t *testing.T) {
	model, key := os.Getenv("DR_E2E_MODEL"), os.Getenv("DR_E2E_KEY")
	if model == "" || key == "" {
		t.Skip("DR_E2E_MODEL or DR_E2E_KEY not set")
	}
	f := setup(t)
	provider, _, _ := strings.Cut(model, "/")
	auth, _ := json.Marshal(map[string]any{provider: map[string]string{"type": "api_key", "key": key}})
	f.writeAuth(t, string(auth))
	a, err := agent.New(context.Background(), agent.Options{Cwd: f.cwd, Model: model})
	if err != nil {
		t.Skipf("agent: %v", err)
	}
	if s, _ := runOnce(t, a, "", "Reply with the single word: ok"); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
}
