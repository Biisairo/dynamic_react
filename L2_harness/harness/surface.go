package harness

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// GateInput 은 Gate 가 받는 호출 정보다 (HN-FR-30).
type GateInput struct {
	RunID, SessionID string
	TurnIndex        int
	InboxConsumed    int // base 공개 상태 inbox.consumed (ST-FR-43)
	Call             contract.ToolCall
}

type EventKind string

// 이벤트 종류 (HN-FR-31).
const (
	EventMessageStart  EventKind = "message_start"
	EventMessageUpdate EventKind = "message_update"
	EventMessageEnd    EventKind = "message_end"
	EventToolStart     EventKind = "tool_start"
	EventToolUpdate    EventKind = "tool_update"
	EventToolEnd       EventKind = "tool_end"
	EventApproval      EventKind = "approval"
	EventRetry         EventKind = "retry"
	EventCompaction    EventKind = "compaction"
	EventInput         EventKind = "input"
	EventTurnEnd       EventKind = "turn_end"
	EventRunEnd        EventKind = "run_end"
	EventMode          EventKind = "mode" // R27, HN-FR-67
)

type Event struct {
	Kind      EventKind
	RunID     string
	SessionID string
	Message   *contract.Message
	Delta     *contract.StreamEvent
	Call      *contract.ToolCall
	Result    *contract.ToolResult
	Results   []contract.ToolResult
	Output    string
	Approval  *Approval
	Error     *contract.CallError
	Attempt   int
	Delay     time.Duration
	Phase     string
	Err       error
	Settle    *core.Settle
	Mode      *ModeChange
}

type Approval struct {
	Rule     string
	Target   string
	Decision string // "allow" | "allow_session" | "deny" | "rule_deny"
	Reason   string
}

const (
	baseSetName      = "base"
	keyInboxConsumed = "inbox.consumed"
)

// surface 는 OnEvent·Gate 를 base·resilient 위에 감싼다 (3.6a).
type surface struct {
	mu      sync.Mutex // OnEvent 직렬화 (HN-FR-34)
	onEvent func(Event)
	gate    func(ctx context.Context, in GateInput) *contract.ToolResult
}

func (s *surface) active() bool { return s.onEvent != nil || s.gate != nil }

func (s *surface) emit(ev Event) {
	if s.onEvent == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent(ev)
}

type runKey struct{}

// runState 는 run 하나의 이벤트 상태다. Start 가 run ctx 에 싣는다.
type runState struct {
	mu         sync.Mutex
	runID, sid string
	calls      map[string]contract.ToolCall
	retryErr   *contract.CallError
	retryTurn  int
	retryCount int
}

func newRunState() *runState {
	return &runState{calls: map[string]contract.ToolCall{}}
}

func stateOf(ctx context.Context) *runState {
	rs, _ := ctx.Value(runKey{}).(*runState)
	return rs
}

func (rs *runState) ids() (string, string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.runID, rs.sid
}

func (s *surface) emitRun(rs *runState, ev Event) {
	ev.RunID, ev.SessionID = rs.ids()
	s.emit(ev)
}

// progress 는 bash 출력 조각을 tool_update 로 바꾼다 (HN-FR-33).
func (s *surface) progress(rs *runState) func(callID, chunk string) {
	return func(callID, chunk string) {
		rs.mu.Lock()
		c, ok := rs.calls[callID]
		rs.mu.Unlock()
		if ok {
			s.emitRun(rs, Event{Kind: EventToolUpdate, Call: &c, Output: chunk})
		}
	}
}

// sleep 은 재시도 대기다. call.error 가 남긴 에러가 있으면 대기 전에 retry 를 낸다.
func (s *surface) sleep(ctx context.Context, d time.Duration) error {
	if rs := stateOf(ctx); rs != nil {
		rs.mu.Lock()
		ce := rs.retryErr
		if ce != nil {
			rs.retryCount++
		}
		n := rs.retryCount
		rs.mu.Unlock()
		if ce != nil {
			s.emitRun(rs, Event{Kind: EventRetry, Error: ce, Attempt: n, Delay: d})
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type eventSummarizer struct {
	s     *surface
	inner sets.Summarizer
}

func (e eventSummarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	rs := stateOf(ctx)
	if rs == nil {
		return e.inner.Summarize(ctx, r)
	}
	e.s.emitRun(rs, Event{Kind: EventCompaction, Phase: "start"})
	text, err := e.inner.Summarize(ctx, r)
	e.s.emitRun(rs, Event{Kind: EventCompaction, Phase: "end", Err: err})
	return text, err
}

// wrap 은 step 마다 가장 위 층 함수(nil 이 아닌 마지막 것, IN-FR-19)를 **그 세트 안에서** 감싼다 (HN-FR-41).
// 위 층이 ctx.Next 로 아래 층을 부르므로 체인 전체가 감싸진다. 다른 세트로 겹치면 inner 의 상태 네임스페이스가 바뀌므로 제자리에서 바꾼다.
func (s *surface) wrap(fs []inject.FunctionSet) []inject.FunctionSet {
	fs = append([]inject.FunctionSet(nil), fs...)
	last := func(has func(inject.FunctionSet) bool) *inject.FunctionSet {
		for i := len(fs) - 1; i >= 0; i-- {
			if has(fs[i]) {
				return &fs[i]
			}
		}
		return nil
	}
	if f := last(func(f inject.FunctionSet) bool { return f.ToolBefore != nil }); f != nil {
		f.ToolBefore = s.toolBefore(f.ToolBefore)
	}
	if s.onEvent == nil {
		return fs
	}
	if f := last(func(f inject.FunctionSet) bool { return f.RunStart != nil }); f != nil {
		inner := f.RunStart
		f.RunStart = func(ctx *inject.RunStartCtx, ri core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
			if rs := stateOf(ctx.Context()); rs != nil {
				rs.mu.Lock()
				rs.runID, rs.sid = ctx.Flow().RunID(), ctx.Flow().SessionID()
				rs.mu.Unlock()
			}
			return inner(ctx, ri)
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.TurnStart != nil }); f != nil {
		inner := f.TurnStart
		f.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
			msgs, sigs, err := inner(ctx)
			if rs := stateOf(ctx.Context()); rs != nil && err == nil {
				for _, m := range msgs {
					s.emitRun(rs, Event{Kind: EventInput, Message: &m})
				}
			}
			return msgs, sigs, err
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.StreamEvent != nil }); f != nil {
		inner := f.StreamEvent
		f.StreamEvent = func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
			if rs := stateOf(ctx.Context()); rs != nil {
				switch ev.Kind {
				case contract.EventStart:
					m := ctx.Stream().Draft()
					s.emitRun(rs, Event{Kind: EventMessageStart, Message: &m})
				case contract.EventTextDelta, contract.EventThinkingDelta, contract.EventToolCallStart, contract.EventToolCallDelta, contract.EventToolCallEnd:
					m, d := ctx.Stream().Draft(), ev
					s.emitRun(rs, Event{Kind: EventMessageUpdate, Message: &m, Delta: &d})
				}
			}
			return inner(ctx, ev)
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.StreamEnd != nil }); f != nil {
		inner := f.StreamEnd
		f.StreamEnd = func(ctx *inject.StreamEndCtx, msg contract.Message) (core.StreamEndOut, []inject.StreamEndSignal, error) {
			if rs := stateOf(ctx.Context()); rs != nil {
				m := msg
				s.emitRun(rs, Event{Kind: EventMessageEnd, Message: &m})
			}
			return inner(ctx, msg)
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.CallError != nil }); f != nil {
		inner := f.CallError
		f.CallError = func(ctx *inject.CallErrorCtx, ce contract.CallError) ([]inject.CallErrorSignal, error) {
			if rs := stateOf(ctx.Context()); rs != nil {
				rs.mu.Lock()
				if turn := ctx.Flow().TurnIndex(); turn != rs.retryTurn {
					rs.retryTurn, rs.retryCount = turn, 0
				}
				e := ce
				rs.retryErr = &e
				rs.mu.Unlock()
			}
			return inner(ctx, ce)
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.ToolAfter != nil }); f != nil {
		inner := f.ToolAfter
		f.ToolAfter = func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			r, sigs, err := inner(ctx, call, res)
			if rs := stateOf(ctx.Context()); rs != nil && err == nil {
				c, res := call, r
				s.emitRun(rs, Event{Kind: EventToolEnd, Call: &c, Result: &res})
			}
			return r, sigs, err
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.TurnFinish != nil }); f != nil {
		inner := f.TurnFinish
		f.TurnFinish = func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			if rs := stateOf(ctx.Context()); rs != nil {
				m := ctx.Output().Response()
				s.emitRun(rs, Event{Kind: EventTurnEnd, Message: &m, Results: ctx.Tools().Results()})
			}
			return inner(ctx)
		}
	}
	if f := last(func(f inject.FunctionSet) bool { return f.RunSettled != nil }); f != nil {
		inner := f.RunSettled
		f.RunSettled = func(ctx *inject.RunSettledCtx, st core.Settle) error {
			if rs := stateOf(ctx.Context()); rs != nil {
				v := st
				s.emitRun(rs, Event{Kind: EventRunEnd, Settle: &v})
			}
			return inner(ctx, st)
		}
	}
	return fs
}

// toolBefore 는 tool_start 를 내고, base 검사를 통과한 호출에 Gate 를 적용한다 (HN-FR-30).
func (s *surface) toolBefore(inner inject.ToolBeforeFn) inject.ToolBeforeFn {
	return func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
		rs := stateOf(ctx.Context())
		if rs != nil && s.onEvent != nil {
			rs.mu.Lock()
			rs.calls[call.ID] = call
			rs.mu.Unlock()
			c := call
			s.emitRun(rs, Event{Kind: EventToolStart, Call: &c})
		}
		out, sigs, err := inner(ctx, call)
		if err != nil || out.ShortCircuit != nil || s.gate == nil {
			return out, sigs, err
		}
		fl := ctx.Flow()
		in := GateInput{RunID: fl.RunID(), SessionID: fl.SessionID(), TurnIndex: fl.TurnIndex(), Call: out.Call}
		if raw, ok := ctx.State().Read(baseSetName, keyInboxConsumed); ok {
			json.Unmarshal(raw, &in.InboxConsumed)
		}
		if res := s.gate(ctx.Context(), in); res != nil {
			r := *res
			r.CallID = call.ID
			out.ShortCircuit = &r
		}
		return out, sigs, err
	}
}
