package harness_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

func textModel() *fakeModel {
	return &fakeModel{script: func(int, contract.Request) []contract.StreamEvent {
		return reply("a", contract.StopStop, text("ok"))
	}}
}

func toolNames(req contract.Request) string {
	var names []string
	for _, t := range req.Tools {
		names = append(names, t.Name)
	}
	return strings.Join(names, ",")
}

func objTool(name, desc string) sets.Tool {
	return sets.Tool{Spec: contract.ToolSpec{Name: name, Description: desc, InputSchema: json.RawMessage(`{"type":"object"}`)},
		Run: func(_ context.Context, c contract.ToolCall) contract.ToolResult {
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text(name + " ran")}}
		}}
}

// HN-FR-38 ①: 추가·제거·같은 이름 교체·빈 결과.
func TestToolSet(t *testing.T) {
	m := textModel()
	var got []string
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.ToolSet = func(def []sets.Tool) []sets.Tool {
			for _, d := range def {
				got = append(got, d.Spec.Name)
			}
			var out []sets.Tool
			for _, d := range def {
				if d.Spec.Name != "bash" {
					out = append(out, d)
				}
			}
			return append(out, objTool("mytool", "My tool.\nMore."), objTool("read", "replaced"))
		}
	})
	if strings.Join(got, ",") != "read,bash,edit,write,grep,find,ls" {
		t.Fatalf("default tools %v", got)
	}
	if names := strings.Join(a.Tools().Names(), ","); names != "read,edit,write,grep,find,ls,mytool" {
		t.Fatalf("registry %s", names)
	}
	if tl, _ := a.Tools().Get("read"); tl.Spec.Description != "replaced" {
		t.Fatal("read not replaced in place")
	}
	if sys := a.System()[0].Text; !strings.Contains(sys, "- mytool: My tool.\n") || strings.Contains(sys, "- bash:") {
		t.Fatalf("system prompt tools:\n%s", sys)
	}
	start(t, a, "s1", user("u1", "hi"))
	if names := toolNames(m.requests()[0]); names != "read,edit,write,grep,find,ls,mytool" {
		t.Fatalf("request tools %s", names)
	}

	empty := newAgent(t, envtest.New("/w", "/h"), textModel(), func(o *harness.Options) {
		o.ToolSet = func([]sets.Tool) []sets.Tool { return nil }
	})
	if n := len(empty.Tools().Names()); n != 0 {
		t.Fatalf("empty ToolSet left %d tools", n)
	}
	if s, _ := start(t, empty, "s1", user("u1", "hi")); s.Reason != core.SettleCompleted {
		t.Fatalf("no-tool agent %+v", s)
	}
}

// listStore 는 기본 저장소를 감싸 호출을 센다.
type listStore struct {
	harness.Store
	mu      sync.Mutex
	appends int
	states  int
}

func (s *listStore) AppendMessages(ctx context.Context, id string, msgs []contract.Message) error {
	s.mu.Lock()
	s.appends++
	s.mu.Unlock()
	return s.Store.AppendMessages(ctx, id, msgs)
}

func (s *listStore) SaveState(ctx context.Context, id, ns string, d inject.StateDelta) error {
	s.mu.Lock()
	s.states++
	s.mu.Unlock()
	return s.Store.SaveState(ctx, id, ns, d)
}

// memStore 는 파일을 쓰지 않는 저장소다.
type memStore struct {
	*sets.MemoryStore
	mu  sync.Mutex
	ids []string
}

func (s *memStore) AppendMessages(ctx context.Context, id string, msgs []contract.Message) error {
	s.mu.Lock()
	s.ids = append(s.ids, id)
	s.mu.Unlock()
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

func (s *memStore) List(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...), nil
}

// HN-FR-38 ②, 40, 42
func TestStore(t *testing.T) {
	e := envtest.New("/w", "/h")
	var wrapped *listStore
	a := newAgent(t, e, textModel(), func(o *harness.Options) {
		o.Store = func(def harness.Store) harness.Store {
			wrapped = &listStore{Store: def}
			return wrapped
		}
	})
	if a.Store() != harness.Store(wrapped) {
		t.Fatal("Agent.Store is not the wrapped store")
	}
	start(t, a, "s1", user("u1", "hi"))
	if wrapped.appends == 0 || wrapped.states == 0 {
		t.Fatalf("run bypassed the store: appends %d states %d", wrapped.appends, wrapped.states)
	}
	if _, ok := e.File("/h/sessions/s1.jsonl"); !ok {
		t.Fatal("wrapped default store did not write the session file")
	}
	if ids, err := a.Store().List(context.Background()); err != nil || strings.Join(ids, ",") != "s1" {
		t.Fatalf("list %v %v", ids, err)
	}

	e2 := envtest.New("/w", "/h")
	ms := &memStore{MemoryStore: sets.NewMemoryStore()}
	b := newAgent(t, e2, textModel(), func(o *harness.Options) { o.Store = func(harness.Store) harness.Store { return ms } })
	start(t, b, "s2", user("u1", "hi"))
	if _, ok := e2.File("/h/sessions/s2.jsonl"); ok {
		t.Fatal("replaced store still wrote the default file")
	}
	if sess, _ := ms.Load(context.Background(), "s2"); len(sess.Messages) != 2 {
		t.Fatalf("memory store messages %d", len(sess.Messages))
	}

	o := options(envtest.New("/w", "/h"), textModel())
	o.Store = func(harness.Store) harness.Store { return nil }
	if _, err := harness.New(context.Background(), o); err == nil || !strings.Contains(err.Error(), "Store returned nil") {
		t.Fatalf("nil store: %v", err)
	}
}

// HN-FR-38 ③
func TestSystem(t *testing.T) {
	m := textModel()
	var def []contract.Block
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.System = func(d []contract.Block) []contract.Block {
			def = d
			return append(d, text("extra rule"))
		}
	})
	if len(def) != 1 || !strings.Contains(def[0].Text, "<cwd>") {
		t.Fatalf("default system %+v", def)
	}
	start(t, a, "s1", user("u1", "hi"))
	if sys := m.requests()[0].System; len(sys) != 2 || sys[1].Text != "extra rule" {
		t.Fatalf("request system %+v", sys)
	}
	if got := a.System(); len(got) != 2 {
		t.Fatalf("Agent.System %+v", got)
	}

	m2 := textModel()
	b := newAgent(t, envtest.New("/w", "/h"), m2, func(o *harness.Options) {
		o.System = func([]contract.Block) []contract.Block { return nil }
	})
	start(t, b, "s1", user("u1", "hi"))
	if sys := m2.requests()[0].System; len(sys) != 0 {
		t.Fatalf("nil System still sent %+v", sys)
	}
}

type countingSummarizer struct {
	inner sets.Summarizer
	mu    sync.Mutex
	n     int
}

func (c *countingSummarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Summarize(ctx, r)
}

// HN-FR-38 ④, HN-FR-36
func TestSummarizer(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/w", "/h")
	var cs *countingSummarizer
	rec := &recorder{}
	a := newAgent(t, e, compactModel(okSummary), func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Summarizer = func(def sets.Summarizer) sets.Summarizer {
			cs = &countingSummarizer{inner: def}
			return cs
		}
	})
	longRun(t, a, "s1")
	if ok, err := a.Compact(ctx, "s1", json.RawMessage(compactCfg)); err != nil || !ok {
		t.Fatalf("compact %v %v", ok, err)
	}
	if cs.n != 1 {
		t.Fatalf("wrapped summarizer calls %d", cs.n)
	}

	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}}}
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec2 := &recorder{}
	b := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec2.on
		o.Summarizer = func(sets.Summarizer) sets.Summarizer { return nil }
	})
	if s, _ := start(t, b, "s1", user("u1", "hi")); s.Reason != core.SettleFailed {
		t.Fatalf("overflow without summarizer %+v", s)
	}
	if len(m.requests()) != 1 || strings.Contains(strings.Join(rec2.kinds(), ","), "compaction") {
		t.Fatalf("compaction ran without summarizer: %d requests, %v", len(m.requests()), rec2.kinds())
	}
	if _, err := b.Compact(ctx, "s1", json.RawMessage(compactCfg)); err == nil || !strings.Contains(err.Error(), "no summarizer") {
		t.Fatalf("Compact without summarizer: %v", err)
	}
}

// HN-FR-36: 바꾼 저장소에서도 sessionID 를 검사한다.
func TestCompactChecksIDWithCustomStore(t *testing.T) {
	ms := &memStore{MemoryStore: sets.NewMemoryStore()}
	a := newAgent(t, envtest.New("/w", "/h"), compactModel(okSummary), func(o *harness.Options) {
		o.Store = func(harness.Store) harness.Store { return ms }
	})
	if _, err := a.Compact(context.Background(), "../x", json.RawMessage(compactCfg)); err == nil {
		t.Fatal("invalid session ID accepted")
	}
}

const ruleText = "injected rule"

func ruleSet() inject.FunctionSet {
	return inject.FunctionSet{Name: "rules", ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
		return []contract.Message{user("rule-"+ctx.Flow().RunID(), ruleText)}, nil, nil
	}}
}

func hasRule(req contract.Request) bool {
	for _, msg := range req.Messages {
		for _, b := range msg.Content {
			if b.Text == ruleText {
				return true
			}
		}
	}
	return false
}

// HN-FR-38 ⑤, 39
func TestSets(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := textModel()
	var deps sets.Deps
	a := newAgent(t, e, m, func(o *harness.Options) {
		o.Sets = func(d sets.Deps) []inject.FunctionSet {
			deps = d
			return append(harness.DefaultSets(d), ruleSet())
		}
	})
	if deps.Model == nil || deps.Tools == nil || deps.Store == nil || len(deps.System) != 1 || deps.Summarizer == nil || deps.Now == nil || deps.NewID == nil {
		t.Fatalf("deps not filled %+v", deps)
	}
	if deps.Store != sets.Store(a.Store()) {
		t.Fatal("deps.Store is not Agent.Store")
	}
	start(t, a, "s1", user("u1", "hi"))
	if !hasRule(m.requests()[0]) {
		t.Fatal("context.inject message not in the request")
	}
	data, _ := e.File("/h/sessions/s1.jsonl")
	if strings.Contains(data, ruleText) {
		t.Fatal("ephemeral message was stored")
	}

	def := harness.DefaultSets(deps)
	if len(def) != 2 || def[0].Name != "base" || def[1].Name != "resilient" {
		t.Fatalf("DefaultSets %+v", def)
	}

	for name, fn := range map[string]func(sets.Deps) []inject.FunctionSet{
		"missing steps":  func(sets.Deps) []inject.FunctionSet { return []inject.FunctionSet{ruleSet()} },
		"duplicate name": func(d sets.Deps) []inject.FunctionSet { return append(harness.DefaultSets(d), ruleSet(), ruleSet()) },
	} {
		o := options(envtest.New("/w", "/h"), textModel())
		o.Sets = fn
		if _, err := harness.New(context.Background(), o); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// HN-FR-41: 사용자 tool.before 를 덮어도 Gate·이벤트는 유지되고, base 를 부르지 않으면 기본 검사가 빠진다.
func TestSetsToolBeforeKeepsGate(t *testing.T) {
	for _, keepBase := range []bool{false, true} {
		m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
			if n == 0 {
				return reply("a0", contract.StopToolUse, call("c1", "nope", `{}`), call("c2", "bash", `{"command":"ls"}`))
			}
			return reply("a1", contract.StopStop, text("done"))
		}}
		rec := &recorder{}
		var mu sync.Mutex
		var gated []string
		var userCalls int
		a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
			o.OnEvent = rec.on
			o.Gate = func(_ context.Context, in harness.GateInput) *contract.ToolResult {
				mu.Lock()
				gated = append(gated, in.Call.Name)
				mu.Unlock()
				return &contract.ToolResult{Status: contract.StatusError, Content: []contract.Block{text("gated")}}
			}
			o.Sets = func(d sets.Deps) []inject.FunctionSet {
				base := sets.BaseToolBefore(d)
				own := inject.FunctionSet{Name: "own", ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
					mu.Lock()
					userCalls++
					mu.Unlock()
					if keepBase {
						return base(ctx, c)
					}
					return core.BeforeOut{Call: c}, nil, nil
				}}
				return append(harness.DefaultSets(d), own)
			}
		})
		if s, _ := start(t, a, "s1", user("u1", "go")); s.Reason != core.SettleCompleted {
			t.Fatalf("keepBase=%v %+v", keepBase, s)
		}
		sent := map[string]string{}
		for _, msg := range m.requests()[1].Messages {
			if msg.Result != nil {
				sent[msg.Result.CallID] = msg.Result.Content[0].Text
			}
		}
		mu.Lock()
		want := "nope,bash"
		wantNope := "gated"
		if keepBase {
			want, wantNope = "bash", "unknown tool: nope"
		}
		if userCalls != 2 || strings.Join(gated, ",") != want {
			t.Fatalf("keepBase=%v user calls %d gated %v", keepBase, userCalls, gated)
		}
		mu.Unlock()
		if sent["c2"] != "gated" || sent["c1"] != wantNope {
			t.Fatalf("keepBase=%v results %v", keepBase, sent)
		}
		kinds := strings.Join(rec.kinds(), ",")
		if strings.Count(kinds, "tool_start") != 2 || strings.Count(kinds, "tool_end") != 2 {
			t.Fatalf("keepBase=%v events %s", keepBase, kinds)
		}
	}
}
