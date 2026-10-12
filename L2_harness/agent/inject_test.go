package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/storetest"
)

const injectedRule = "INJECTED-RULE-7"

// AG-FR-40, 42: 덧붙인 context.inject 가 요청에 들어가고, base 없는 tool.before 로 덮어도 deny 규칙이 이긴다.
func TestInjectSets(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"rm -rf x"}`), textReply("done"))
	os.WriteFile(filepath.Join(f.cwd, "x"), []byte("keep"), 0o644)
	a := newAgent(t, f, testConfig(l.URL, `,"permissions":{"deny":["bash(rm *)"]}`), func(o *agent.Options) {
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			own := inject.FunctionSet{Name: "own",
				ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
					m := contract.Message{ID: "rule-" + ctx.Flow().RunID(), Role: contract.RoleUser, Timestamp: time.Now(),
						Content: []contract.Block{{Kind: contract.BlockText, Text: injectedRule}}}
					return []contract.Message{m}, nil, nil
				},
				ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
					return core.BeforeOut{Call: c}, nil, nil
				}}
			return append(harness.DefaultSets(d), harness.SetEntry{Set: own, On: true})
		}
	})
	if s, _ := runOnce(t, a, "s1", "go"); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(l.body(0), injectedRule) || !strings.Contains(l.body(1), injectedRule) {
		t.Fatal("injected message missing from a request")
	}
	if !strings.Contains(l.body(1), "denied by rule bash(rm *)") {
		t.Fatalf("deny rule bypassed: %.400s", l.body(1))
	}
	if b, _ := os.ReadFile(filepath.Join(f.cwd, "x")); string(b) != "keep" {
		t.Fatal("denied command ran")
	}
}

func extraTool(name string) sets.Tool {
	return sets.Tool{Spec: contract.ToolSpec{Name: name, Description: name + " tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Run: func(_ context.Context, c contract.ToolCall) contract.ToolResult {
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockText, Text: name + " ran"}}}
		}}
}

func requestTools(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range req.Tools {
		names = append(names, tl.Function.Name)
	}
	return strings.Join(names, ",")
}

// AG-FR-43: 추가 도구는 상한·agent.tools·이름 규칙의 대상이고, 패턴 규칙은 쓸 수 없다.
func TestInjectToolSet(t *testing.T) {
	add := func(o *agent.Options) {
		o.ToolSet = func(def []sets.Tool) []sets.Tool { return append(def, extraTool("mytool")) }
	}
	f := setup(t)
	l := newLLM(t, toolReply("mytool", `{}`), textReply("done"))
	a := newAgent(t, f, testConfig(l.URL, `,"permissions":{"deny":["mytool"]}`), func(o *agent.Options) {
		add(o)
		o.Tools = []string{"read", "mytool"}
	})
	runOnce(t, a, "s1", "go")
	if got := requestTools(t, l.body(0)); got != "read,mytool" {
		t.Fatalf("request tools %s", got)
	}
	if !strings.Contains(l.body(1), "denied by rule mytool") || strings.Contains(l.body(1), "mytool ran") {
		t.Fatalf("name rule not applied: %.400s", l.body(1))
	}

	f2 := setup(t)
	l2 := newLLM(t, textReply("ok"))
	cfg := `{"agent":{"model":{"provider":"test","id":"m"},"tools":["mytool"]},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + l2.URL + `"}},"models":{"test/m":{"contextWindow":100000}}}`
	b := newAgent(t, f2, cfg, add)
	runOnce(t, b, "s1", "go")
	if got := requestTools(t, l2.body(0)); got != "mytool" {
		t.Fatalf("agent.tools on added tool: %s", got)
	}

	f3 := setup(t)
	f3.writeConfig(t, testConfig("http://127.0.0.1:1", `,"permissions":{"allow":["mytool(x)"]}`))
	o := f3.opts()
	add(&o)
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("pattern rule on added tool accepted")
	}
}

// AG-FR-14, 43: command 인자가 없는 bash 로 바꾸면 패턴 deny 는 맞지 않고 이름 deny 는 맞는다.
func TestInjectReplacedBash(t *testing.T) {
	replace := func(o *agent.Options) {
		o.ToolSet = func(def []sets.Tool) []sets.Tool { return append(def, extraTool("bash")) }
	}
	for _, c := range []struct {
		rule, want string
	}{{"bash(rm *)", "bash ran"}, {"bash", "denied by rule bash"}} {
		f := setup(t)
		l := newLLM(t, toolReply("bash", `{"cmd":"rm -rf x"}`), textReply("done"))
		a := newAgent(t, f, testConfig(l.URL, `,"permissions":{"deny":["`+c.rule+`"]}`), replace)
		runOnce(t, a, "s1", "go")
		if !strings.Contains(l.body(1), c.want) {
			t.Fatalf("%s: want %q in %.400s", c.rule, c.want, l.body(1))
		}
	}
}

// AG-FR-40: System 결과가 요청의 system 이다.
func TestInjectSystem(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	a := newAgent(t, f, testConfig(l.URL, ""), func(o *agent.Options) {
		o.System = func([]contract.Block) []contract.Block {
			return []contract.Block{{Kind: contract.BlockText, Text: "CUSTOM-SYSTEM"}}
		}
	})
	runOnce(t, a, "s1", "go")
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal([]byte(l.body(0)), &req)
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" || req.Messages[0].Content != "CUSTOM-SYSTEM" {
		t.Fatalf("system message %.300s", l.body(0))
	}
}

type countSummarizer struct {
	inner sets.Summarizer
	mu    sync.Mutex
	n     int
}

func (c *countSummarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Summarize(ctx, r)
}

// AG-FR-40: 감싼 요약기가 Compact 에 쓰인다.
func TestInjectSummarizer(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	var cs *countSummarizer
	a := newAgent(t, f, compactConfig(l.URL), func(o *agent.Options) {
		o.Summarizer = func(def sets.Summarizer) sets.Summarizer {
			cs = &countSummarizer{inner: def}
			return cs
		}
	})
	for _, p := range []string{"first", "second", "third"} {
		runOnce(t, a, "s1", p+strings.Repeat(" abcd", 50))
	}
	if ok, err := a.Compact(context.Background(), "s1"); err != nil || !ok {
		t.Fatalf("compact %v %v", ok, err)
	}
	if cs.n != 1 {
		t.Fatalf("wrapped summarizer calls %d", cs.n)
	}
}

type memStore struct {
	*storetest.MemoryStore
	mu  sync.Mutex
	ids []string
}

func (s *memStore) AppendMessages(ctx context.Context, id string, msgs []contract.Message) error {
	s.mu.Lock()
	if !strings.Contains(","+strings.Join(s.ids, ",")+",", ","+id+",") {
		s.ids = append(s.ids, id)
	}
	s.mu.Unlock()
	return s.MemoryStore.AppendMessages(ctx, id, msgs)
}

func (s *memStore) List(context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...), nil
}

type failList struct{ *memStore }

func (failList) List(context.Context) ([]string, error) { return nil, errors.New("list failed") }

// AG-FR-29, 30, 40: 바꾼 저장소가 Sessions·History 에 쓰인다 (파일이 없어도).
func TestInjectStore(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	ms := &memStore{MemoryStore: storetest.New()}
	a := newAgent(t, f, testConfig(l.URL, ""), func(o *agent.Options) {
		o.Store = func(harness.Store) harness.Store { return ms }
	})
	runOnce(t, a, "mem1", "hello")
	if _, err := os.Stat(filepath.Join(a.SessionDir(), "mem1.jsonl")); err == nil {
		t.Fatal("session file written despite replaced store")
	}
	list, err := a.Sessions(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "mem1" || list[0].Title != "hello" || list[0].Messages != 2 {
		t.Fatalf("sessions %+v %v", list, err)
	}
	h, err := a.History(context.Background(), "mem1")
	if err != nil || len(h.Messages) != 2 {
		t.Fatalf("history %+v %v", h, err)
	}
	if a.Harness().Store() != harness.Store(ms) {
		t.Fatal("Harness().Store() is not the injected store")
	}

	fl := newAgent(t, setup(t), testConfig(l.URL, ""), func(o *agent.Options) {
		o.Store = func(harness.Store) harness.Store { return failList{ms} }
	})
	if _, err := fl.Sessions(context.Background()); err == nil || err.Error() != "list failed" {
		t.Fatalf("List error: %v", err)
	}

	f2 := setup(t)
	f2.writeConfig(t, testConfig(l.URL, ""))
	o := f2.opts()
	o.Store = func(harness.Store) harness.Store { return nil }
	if _, err := agent.New(context.Background(), o); err == nil || !strings.Contains(err.Error(), "agent: ") {
		t.Fatalf("nil store: %v", err)
	}
}

type countCaller struct {
	inner sets.ModelCaller
	mu    sync.Mutex
	n     int
}

func (c *countCaller) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.Call(ctx, req)
}

func (c *countCaller) Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool) {
	return c.inner.Lookup(ctx, provider, id)
}

// AG-FR-41
func TestInjectModelCaller(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	var cc *countCaller
	a := newAgent(t, f, testConfig(l.URL, ""), func(o *agent.Options) {
		o.ModelCaller = func(def sets.ModelCaller) sets.ModelCaller {
			cc = &countCaller{inner: def}
			return cc
		}
	})
	if s, _ := runOnce(t, a, "s1", "go"); s.Reason != core.SettleCompleted || cc.n != 1 {
		t.Fatalf("settle %+v calls %d", s, cc.n)
	}
	if ms := a.Models(); !hasModel(ms, "test", "m") {
		t.Fatalf("models %+v", ms)
	}

	f2 := setup(t)
	f2.writeConfig(t, testConfig(l.URL, ""))
	o := f2.opts()
	o.ModelCaller = func(sets.ModelCaller) sets.ModelCaller { return nil }
	if _, err := agent.New(context.Background(), o); err == nil || !strings.Contains(err.Error(), "ModelCaller returned nil") {
		t.Fatalf("nil caller: %v", err)
	}
}
