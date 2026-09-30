package adapters_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

const testKey = "sk-test-SECRETKEY"

// fake 는 요청을 기록하고 경로별 응답을 돌려주는 provider 서버다.
type fake struct {
	*httptest.Server
	mu     sync.Mutex
	bodies map[string][]byte
	heads  map[string]http.Header
	routes map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T) *fake {
	f := &fake{bodies: map[string][]byte{}, heads: map[string]http.Header{}, routes: map[string]func(http.ResponseWriter, *http.Request){}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies[r.URL.Path] = b
		f.heads[r.URL.Path] = r.Header.Clone()
		h := f.routes[r.Method+" "+r.URL.Path]
		f.mu.Unlock()
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fake) on(method, path string, h func(w http.ResponseWriter, r *http.Request)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[method+" "+path] = h
}

func (f *fake) sse(path, fixture string) {
	b, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		panic(err)
	}
	f.sseText(path, string(b))
}

func (f *fake) sseText(path, text string) {
	f.on("POST", path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, text)
	})
}

func (f *fake) body(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[path]
}

func (f *fake) header(path string) http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.heads[path]
}

// client 는 provider 프로필을 fake 서버로 돌린 Client 다. patch 는 프로필 덮어쓰기 JSON.
func client(t *testing.T, f *fake, provider, patch string) *adapters.Client {
	t.Helper()
	base := f.URL
	if patch == "" {
		patch = "{}"
	}
	var p map[string]any
	json.Unmarshal([]byte(patch), &p)
	if _, ok := p["baseUrl"]; !ok {
		p["baseUrl"] = base
	}
	pb, _ := json.Marshal(map[string]any{"providers": map[string]any{provider: p}})
	cfg, err := adapters.ParseConfig(pb)
	if err != nil {
		t.Fatal(err)
	}
	return adapters.New(adapters.Options{
		Config:          cfg,
		Getenv:          func(string) string { return testKey },
		Now:             func() time.Time { return ts },
		NoRemoteCatalog: true,
	})
}

func collect(t *testing.T, c *adapters.Client, req contract.Request) []contract.StreamEvent {
	t.Helper()
	return collectCtx(t, context.Background(), c, req)
}

func collectCtx(t *testing.T, ctx context.Context, c *adapters.Client, req contract.Request) []contract.StreamEvent {
	t.Helper()
	var evs []contract.StreamEvent
	for ev, err := range c.Call(ctx, req) {
		if err != nil {
			t.Fatalf("iterator error (AD-C4): %v", err)
		}
		evs = append(evs, ev)
	}
	if err := contract.ValidateStream(evs); err != nil {
		t.Fatalf("invalid stream: %v\n%+v", err, evs)
	}
	return evs
}

func last(evs []contract.StreamEvent) contract.StreamEvent { return evs[len(evs)-1] }

func done(t *testing.T, evs []contract.StreamEvent) contract.Message {
	t.Helper()
	e := last(evs)
	if e.Kind != contract.EventDone {
		t.Fatalf("stream ended with %s: %+v", e.Kind, e.Error)
	}
	return *e.Message
}

func failed(t *testing.T, evs []contract.StreamEvent) contract.CallError {
	t.Helper()
	e := last(evs)
	if e.Kind != contract.EventError {
		t.Fatalf("stream ended with %s", e.Kind)
	}
	return *e.Error
}

func kinds(evs []contract.StreamEvent) string {
	var out []string
	for _, e := range evs {
		out = append(out, string(e.Kind))
	}
	return strings.Join(out, ",")
}

// sameJSON 은 두 JSON 이 의미상 같은지 본다.
func sameJSON(t *testing.T, got []byte, wantFile string) {
	t.Helper()
	want, err := os.ReadFile("testdata/" + wantFile)
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("request not JSON: %v\n%s", err, got)
	}
	json.Unmarshal(want, &w)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.MarshalIndent(g, "", "  ")
		t.Fatalf("request differs from %s\n got: %s", wantFile, gb)
	}
}

func jsonOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not JSON: %s", b)
	}
	return m
}

var readSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)

func text(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

func user(id, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Content: []contract.Block{text(s)}, Timestamp: ts}
}

func result(id, callID, s string, status contract.ToolStatus, extra ...contract.Block) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleToolResult, Timestamp: ts,
		Result: &contract.ToolResult{CallID: callID, Status: status, Content: append([]contract.Block{text(s)}, extra...)}}
}

// scenario 는 05 문서 0장의 두 번째 요청을 중립 형식으로 만든 것이다.
// thinking·tool_call 블록의 ProviderData 는 방언별로 채운다.
func scenario(provider, model, callID string, thinkPD, callPD *contract.ProviderData) contract.Request {
	call := contract.ToolCall{ID: callID, Name: "read", Args: json.RawMessage(`{"path":"config.py"}`)}
	return contract.Request{
		Model:  contract.ModelRef{Provider: provider, ID: model},
		System: []contract.Block{text("You are a coding agent.")},
		Tools:  []contract.ToolSpec{{Name: "read", Description: "Read a file", InputSchema: readSchema}},
		Messages: []contract.Message{
			user("u1", "config.py 의 DEBUG 값 알려줘"),
			{ID: "a1", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{
				{Kind: contract.BlockThinking, Text: "config.py 를 읽어야 한다.", ProviderData: thinkPD},
				{Kind: contract.BlockToolCall, ToolCall: &call, ProviderData: callPD},
			}},
			result("r1", callID, "DEBUG = True", contract.StatusOK),
		},
		Options: contract.Options{MaxOutputTokens: 16384, Thinking: "medium"},
	}
}

func simple(provider, model string) contract.Request {
	return contract.Request{
		Model:    contract.ModelRef{Provider: provider, ID: model},
		Messages: []contract.Message{user("u1", "hi")},
		Options:  contract.Options{MaxOutputTokens: 100},
	}
}
