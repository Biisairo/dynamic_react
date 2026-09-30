package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// call 은 가짜 Dispatcher 가 받은 dispatch 하나의 기록이다.
type call struct {
	Step  core.Step
	N     int // 이 step 의 몇 번째 dispatch 인지 (0부터)
	Input any
	View  core.View
	Ctx   context.Context
}

type handler func(c call) (core.Result, error)

// fake 는 step 별 handler 를 가진 스크립트된 Dispatcher 다. handler 가 없으면 기본 동작을 한다.
type fake struct {
	mu       sync.Mutex
	handlers map[core.Step]handler
	calls    []call
	counts   map[core.Step]int
}

func newFake() *fake {
	return &fake{handlers: map[core.Step]handler{}, counts: map[core.Step]int{}}
}

func (f *fake) on(s core.Step, h handler) *fake {
	f.handlers[s] = h
	return f
}

func (f *fake) Dispatch(ctx context.Context, step core.Step, input any, view core.View) (core.Result, error) {
	f.mu.Lock()
	c := call{Step: step, N: f.counts[step], Input: input, View: view, Ctx: ctx}
	f.counts[step]++
	f.calls = append(f.calls, c)
	h := f.handlers[step]
	f.mu.Unlock()
	if h != nil {
		return h(c)
	}
	return defaultResult(c)
}

func (f *fake) steps() []core.Step {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]core.Step, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Step
	}
	return out
}

func (f *fake) callsOf(s core.Step) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Step == s {
			out = append(out, c)
		}
	}
	return out
}

func (f *fake) count(s core.Step) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[s]
}

func out(v any, sigs ...core.Signal) (core.Result, error) {
	return core.Result{Output: v, Signals: sigs}, nil
}

func sig(k core.SignalKind) core.Signal { return core.Signal{Kind: k} }

func defaultResult(c call) (core.Result, error) {
	switch c.Step {
	case core.StepRunStart:
		return out(core.RunInit{})
	case core.StepTurnStart, core.StepContextInject, core.StepContextProject:
		return out([]contract.Message(nil))
	case core.StepRequestBuild:
		return out(contract.Request{Messages: []contract.Message{userMsg("u1", "hi")}})
	case core.StepRequestFinalize:
		return out(c.Input.(contract.Request))
	case core.StepModelSelect:
		return out(modelInfo())
	case core.StepModelCall:
		return out(streamOf(textResponse("a1")).cs)
	case core.StepStreamEnd:
		return out(core.StreamEndOut{Response: c.Input.(contract.Message)})
	case core.StepCallError:
		return out(nil, sig(core.SigFail))
	case core.StepToolsPlan:
		calls := c.Input.([]contract.ToolCall)
		var groups []core.Group
		for _, tc := range calls {
			groups = append(groups, core.Group{Calls: []contract.ToolCall{tc}, MaxParallel: 1})
		}
		return out(core.Plan{Groups: groups})
	case core.StepToolBefore:
		return out(core.BeforeOut{Call: c.Input.(contract.ToolCall)})
	case core.StepToolExecute:
		tc := c.Input.(contract.ToolCall)
		return out(okResult(tc.ID, "ran "+tc.Name))
	case core.StepToolAfter:
		return out(c.Input.(core.ToolAfterInput).Result)
	case core.StepToolsCommit:
		return out(resultMessages(c.Input.(core.ToolsCommitInput).Results))
	case core.StepTurnFinish:
		return out(nil, sig(core.SigSettle))
	}
	return out(nil)
}

func modelInfo() contract.ModelInfo {
	return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "fake"}, ContextWindow: 1000}
}

func userMsg(id, text string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Content: []contract.Block{{Kind: contract.BlockText, Text: text}}, Timestamp: ts}
}

func textResponse(id string) contract.Message {
	return contract.Message{
		ID: id, Role: contract.RoleAssistant, Timestamp: ts,
		Content:    []contract.Block{{Kind: contract.BlockText, Text: "hello"}},
		Usage:      &contract.Usage{Input: 1, Output: 1},
		StopReason: &contract.StopReason{Kind: contract.StopStop},
	}
}

func tc(id, name, args string) contract.ToolCall {
	return contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func toolResponse(id string, calls ...contract.ToolCall) contract.Message {
	m := textResponse(id)
	m.StopReason = &contract.StopReason{Kind: contract.StopToolUse}
	for i := range calls {
		m.Content = append(m.Content, contract.Block{Kind: contract.BlockToolCall, ToolCall: &calls[i]})
	}
	return m
}

func okResult(callID, text string) contract.ToolResult {
	return contract.ToolResult{CallID: callID, Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

func resultMessages(rs []contract.ToolResult) []contract.Message {
	out := make([]contract.Message, len(rs))
	for i := range rs {
		r := rs[i]
		out[i] = contract.Message{ID: "tr-" + r.CallID, Role: contract.RoleToolResult, Result: &r, Timestamp: ts}
	}
	return out
}

// stream 은 스크립트된 이벤트 스트림과 실제로 꺼내진 이벤트 수를 가진다.
type stream struct {
	cs     core.CallStream
	pulled *atomic.Int32
}

type item struct {
	ev  contract.StreamEvent
	err error
}

func streamOf(final contract.Message, pre ...contract.StreamEvent) stream {
	evs := []item{{ev: contract.StreamEvent{Kind: contract.EventStart}}}
	for _, e := range pre {
		evs = append(evs, item{ev: e})
	}
	m := final
	evs = append(evs, item{ev: contract.StreamEvent{Kind: contract.EventDone, Message: &m}})
	return rawStream(evs...)
}

func rawStream(items ...item) stream {
	var pulled atomic.Int32
	seq := func(yield func(contract.StreamEvent, error) bool) {
		for _, it := range items {
			pulled.Add(1)
			if !yield(it.ev, it.err) {
				return
			}
		}
	}
	return stream{cs: core.CallStream{Events: iter.Seq2[contract.StreamEvent, error](seq)}, pulled: &pulled}
}

func textDelta(i int, s string) contract.StreamEvent {
	return contract.StreamEvent{Kind: contract.EventTextDelta, Index: i, Delta: s}
}

func errEvent(class contract.ErrorClass) contract.StreamEvent {
	return contract.StreamEvent{Kind: contract.EventError, Error: &contract.CallError{Class: class, Message: string(class)}}
}

// run 은 기본 옵션으로 run 하나를 끝까지 실행한다.
func run(t *testing.T, d core.Dispatcher, opts ...core.Options) core.Settle {
	t.Helper()
	o := core.Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	done := make(chan core.Settle, 1)
	go func() { done <- core.New(d, o).Start(context.Background(), core.RunInput{SessionID: "s1"}).Wait() }()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("run did not settle")
		return core.Settle{}
	}
}

func wantSteps(t *testing.T, got []core.Step, want ...core.Step) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("steps\n got: %v\nwant: %v", got, want)
	}
}

func wantReason(t *testing.T, s core.Settle, r core.SettleReason) {
	t.Helper()
	if s.Reason != r {
		t.Fatalf("reason = %s (err %v), want %s", s.Reason, s.Err, r)
	}
}

// 한 턴, 도구 없음의 기본 경로. 스트림 이벤트는 start 와 done 이라 10 은 두 번.
var plainTurn = []core.Step{
	core.StepTurnStart, core.StepContextMaintain, core.StepContextInject, core.StepContextProject,
	core.StepRequestBuild, core.StepRequestFinalize, core.StepModelSelect, core.StepModelCall,
	core.StepStreamEvent, core.StepStreamEvent, core.StepStreamEnd, core.StepResponseClassify, core.StepResponseCommit,
	core.StepToolsPlan, core.StepToolsCommit, core.StepTurnFinish,
}

func seqOf(parts ...[]core.Step) []core.Step {
	var out []core.Step
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func one(s ...core.Step) []core.Step { return s }
