package sets_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// fakeModel 은 호출마다 script 로 스트림을 만든다.
type fakeModel struct {
	mu     sync.Mutex
	reqs   []contract.Request
	script func(n int, req contract.Request) []contract.StreamEvent
	info   map[string]contract.ModelInfo
}

func (m *fakeModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	m.mu.Lock()
	n := len(m.reqs)
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	evs := m.script(n, req)
	return func(yield func(contract.StreamEvent, error) bool) {
		for _, e := range evs {
			if !yield(e, nil) {
				return
			}
		}
	}
}

func (m *fakeModel) Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool) {
	i, ok := m.info[provider+"/"+id]
	return i, ok
}

func (m *fakeModel) requests() []contract.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]contract.Request(nil), m.reqs...)
}

func assistant(id string, u contract.Usage, stop contract.StopKind, blocks ...contract.Block) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleAssistant, Timestamp: ts, Content: blocks, Usage: &u,
		StopReason: &contract.StopReason{Kind: stop}}
}

func doneEv(m contract.Message) []contract.StreamEvent {
	return []contract.StreamEvent{{Kind: contract.EventStart}, {Kind: contract.EventDone, Message: &m}}
}

func errEv(ce contract.CallError) []contract.StreamEvent {
	return []contract.StreamEvent{{Kind: contract.EventError, Error: &ce}}
}

func text(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

func toolCall(id, name, args string) contract.Block {
	return contract.Block{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}}
}

func user(id, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Content: []contract.Block{text(s)}, Timestamp: ts}
}

func resultMsg(id, callID, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleToolResult, Timestamp: ts,
		Result: &contract.ToolResult{CallID: callID, Status: contract.StatusOK, Content: []contract.Block{text(s)}}}
}

var echoSchema = json.RawMessage(`{"type":"object","properties":{"s":{"type":"string"}},"required":["s"]}`)

func echoTool(runs *[]string) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "echo", Description: "echo", InputSchema: echoSchema},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult {
			var a struct{ S string }
			json.Unmarshal(c.Args, &a)
			if runs != nil {
				*runs = append(*runs, a.S)
			}
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text("echo:" + a.S)}}
		},
	}
}

type fakeSummarizer struct {
	mu   sync.Mutex
	reqs []sets.SummaryRequest
	err  error
}

func (f *fakeSummarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, r)
	if f.err != nil {
		return "", f.err
	}
	return fmt.Sprintf("summary of %d", len(r.Messages)), nil
}

func idGen() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("id%d", n)
	}
}

func deps(m *fakeModel, tools ...sets.Tool) sets.Deps {
	return sets.Deps{
		Model:  m,
		Tools:  newTools(tools...),
		Store:  newStore(),
		System: []contract.Block{text("sys")},
		Sleep:  func(ctx context.Context, d time.Duration) error { return ctx.Err() },
		Now:    func() time.Time { return ts },
		NewID:  idGen(),
	}
}

const cfgJSON = `{"model":{"provider":"p","id":"m"},"maxOutputTokens":1000}`

// stack 은 v0.6 의 base(+resilient) 와 같은 동작이 되는 세트 조합이다 (ST-FR-51). top 은 그 위에 쌓인다.
func stack(d sets.Deps, resilient bool, top ...inject.FunctionSet) []inject.FunctionSet {
	fs := []inject.FunctionSet{sets.Base(d), sets.Require(d), sets.Cache(d), sets.Guard(d), sets.Truncate(d)}
	if resilient {
		fs = append(fs, sets.Resilient(d))
	}
	return append(fs, top...)
}

// agent 는 stack 으로 core run 하나를 돌린다.
func agent(t *testing.T, d sets.Deps, resilient bool, cfg string, input ...contract.Message) core.Settle {
	t.Helper()
	disp, err := inject.BindWith(inject.BindOptions{StateStore: stateStoreOf(d.Store)}, stack(d, resilient)...)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan core.Settle, 1)
	go func() {
		done <- core.New(disp, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1", Input: input, Config: json.RawMessage(cfg)}).Wait()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("run did not settle")
		return core.Settle{}
	}
}

// step 은 set 의 step 하나를 가짜 View 로 dispatch 한다.
func step(t *testing.T, set inject.FunctionSet, st core.Step, input any, f injecttest.Facts) (core.Result, error) {
	t.Helper()
	return injecttest.Dispatch(context.Background(), set, st, input, stepFacts(st, f).View())
}

// stackStep 은 sets 를 쌓아 step 하나를 가짜 View 로 dispatch 한다. sets[0] 의 빈 step 은 Fill 로 채운다.
func stackStep(t *testing.T, st core.Step, input any, f injecttest.Facts, sets ...inject.FunctionSet) (core.Result, error) {
	t.Helper()
	sets[0] = injecttest.Fill(sets[0])
	disp, err := inject.Bind(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return disp.DispatchSource(context.Background(), st, input, stepFacts(st, f).View())
}

func stepFacts(st core.Step, f injecttest.Facts) injecttest.Facts {
	if f.Config == nil {
		f.Config = json.RawMessage(cfgJSON)
	}
	if f.RunID == "" {
		f.RunID, f.SessionID = "r1", "s1"
	}
	f.Step = st
	return f
}

func kinds(sigs []core.Signal) string {
	var out []string
	for _, s := range sigs {
		out = append(out, string(s.Kind))
	}
	return fmt.Sprint(out)
}

// toolList 는 테스트용 도구 목록이다 (레지스트리 구현은 하네스, R39).
type toolList []sets.Tool

func newTools(tools ...sets.Tool) toolList { return toolList(tools) }

func (l toolList) Get(name string) (sets.Tool, bool) {
	for _, t := range l {
		if t.Spec.Name == name {
			return t, true
		}
	}
	return sets.Tool{}, false
}

func (l toolList) Names() []string {
	var out []string
	for _, t := range l {
		out = append(out, t.Spec.Name)
	}
	return out
}

// memStore 는 테스트용 메모리 Store 다 (메모리 Store 구현은 하네스, R39).
type memStore struct {
	mu       sync.Mutex
	messages map[string][]contract.Message
	edits    map[string][]contract.ContextEdit
	state    map[string]map[string]inject.NamespaceState
}

func newStore() *memStore {
	return &memStore{messages: map[string][]contract.Message{}, edits: map[string][]contract.ContextEdit{},
		state: map[string]map[string]inject.NamespaceState{}}
}

func (s *memStore) Load(ctx context.Context, id string) (sets.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := sets.Session{Messages: append([]contract.Message(nil), s.messages[id]...),
		Edits: append([]contract.ContextEdit(nil), s.edits[id]...), State: map[string]inject.NamespaceState{}}
	for name, ns := range s.state[id] {
		c := inject.NamespaceState{Values: map[string]json.RawMessage{}, Published: append([]string(nil), ns.Published...)}
		for k, v := range ns.Values {
			c.Values[k] = append(json.RawMessage(nil), v...)
		}
		out.State[name] = c
	}
	return out, nil
}

func (s *memStore) AppendMessages(ctx context.Context, id string, msgs []contract.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[id] = append(s.messages[id], msgs...)
	return nil
}

func (s *memStore) AppendEdits(ctx context.Context, id string, edits []contract.ContextEdit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edits[id] = append(s.edits[id], edits...)
	return nil
}

func (s *memStore) SaveState(ctx context.Context, id, ns string, d inject.StateDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state[id] == nil {
		s.state[id] = map[string]inject.NamespaceState{}
	}
	cur := s.state[id][ns]
	if cur.Values == nil {
		cur.Values = map[string]json.RawMessage{}
	}
	for k, v := range d.Set {
		cur.Values[k] = append(json.RawMessage(nil), v...)
	}
	for _, k := range d.Delete {
		delete(cur.Values, k)
	}
	pub := map[string]bool{}
	for _, k := range append(cur.Published, d.Publish...) {
		pub[k] = true
	}
	cur.Published = nil
	for k := range pub {
		cur.Published = append(cur.Published, k)
	}
	sort.Strings(cur.Published)
	s.state[id][ns] = cur
	return nil
}

// stateStoreOf 는 Store 를 inject StateStore 로 감싼다 (하네스의 연결과 같음, R39).
func stateStoreOf(s sets.Store) inject.StateStore {
	if s == nil {
		return nil
	}
	return stateStore{s}
}

type stateStore struct{ s sets.Store }

func (x stateStore) Load(ctx context.Context, id string) (map[string]inject.NamespaceState, error) {
	sess, err := x.s.Load(ctx, id)
	return sess.State, err
}

func (x stateStore) Save(ctx context.Context, id, ns string, d inject.StateDelta) error {
	return x.s.SaveState(ctx, id, ns, d)
}
