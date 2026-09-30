package inject

import (
	"encoding/json"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// 조회 그룹 (inject 3.4). 모두 읽기 전용이고, 같은 dispatch 안에서는 같은 스냅샷을 본다.
// 없는 값은 zero value 로 돌려준다.

type Flow struct{ b *base }

func (q Flow) Step() core.Step        { q.b.check(); return q.b.view.Step() }
func (q Flow) Seq() int               { q.b.check(); return q.b.view.Seq() }
func (q Flow) RunID() string          { q.b.check(); return q.b.view.RunID() }
func (q Flow) SessionID() string      { q.b.check(); return q.b.view.SessionID() }
func (q Flow) TurnIndex() int         { q.b.check(); return q.b.view.TurnIndex() }
func (q Flow) CallIndex() int         { q.b.check(); return q.b.view.CallIndex() }
func (q Flow) Elapsed() time.Duration { q.b.check(); return q.b.view.Elapsed() }

// Signals 는 신호 기록이다 (core CORE-FR-30).
func (q Flow) Signals() []core.SignalRecord {
	q.b.check()
	rs := q.b.view.Signals()
	for i := range rs {
		rs[i].Signals = cloneSignals(rs[i].Signals)
		rs[i].Results = append([]string(nil), rs[i].Results...)
	}
	return rs
}

// Count 는 신호 기록에서 kind 의 횟수를 센다. reason 이 비면 사유와 무관하게 센다.
func (q Flow) Count(kind core.SignalKind, reason string) int {
	q.b.check()
	return q.b.view.SignalCount(kind, reason)
}

type Conv struct{ b *base }

func (q Conv) History() []contract.Message { q.b.check(); return cloneMessages(q.b.view.History()) }
func (q Conv) Transcript() []contract.Message {
	q.b.check()
	return cloneMessages(q.b.view.Transcript())
}
func (q Conv) TurnAppended() []contract.Message {
	q.b.check()
	return cloneMessages(q.b.view.TurnAppended())
}
func (q Conv) Ephemeral() []contract.Message { q.b.check(); return cloneMessages(q.b.view.Ephemeral()) }
func (q Conv) Projection() []contract.Message {
	q.b.check()
	return cloneMessages(q.b.view.Projection())
}
func (q Conv) Inbox() []contract.Message { q.b.check(); return cloneMessages(q.b.view.Inbox()) }

func (q Conv) Edits() []contract.ContextEdit {
	q.b.check()
	es := q.b.view.Edits()
	for i := range es {
		es[i] = cloneEdit(es[i])
	}
	return es
}

type Request struct{ b *base }

// Current 는 이번 호출의 확정 요청이다 (request.finalize 이후).
func (q Request) Current() contract.Request {
	q.b.check()
	r, _ := q.b.view.Request()
	return cloneRequest(r)
}

// Model 은 마지막으로 선택된 모델이다.
func (q Request) Model() contract.ModelInfo {
	q.b.check()
	m, _ := q.b.view.Model()
	return m
}

type Stream struct{ b *base }

func (q Stream) Draft() contract.Message {
	q.b.check()
	m, _ := q.b.view.Draft()
	return cloneMessage(m)
}

func (q Stream) Event() contract.StreamEvent {
	q.b.check()
	e, _ := q.b.view.Event()
	return cloneEvent(e)
}

type Output struct{ b *base }

func (q Output) Response() contract.Message {
	q.b.check()
	m, _ := q.b.view.Response()
	return cloneMessage(m)
}

func (q Output) CallError() contract.CallError {
	q.b.check()
	e, _ := q.b.view.CallError()
	return e
}

// LastUsage 는 이 run 에서 마지막으로 완료된 응답의 usage 다 (IN-FR-46).
func (q Output) LastUsage() contract.Usage {
	q.b.check()
	if r, ok := q.b.view.Response(); ok && r.Usage != nil {
		return *r.Usage
	}
	tr := q.b.view.Transcript()
	for i := len(tr) - 1; i >= len(q.b.view.History()); i-- {
		if tr[i].Role == contract.RoleAssistant && tr[i].Usage != nil {
			return *tr[i].Usage
		}
	}
	return contract.Usage{}
}

type Tools struct{ b *base }

func (q Tools) Plan() core.Plan {
	q.b.check()
	p, _ := q.b.view.Plan()
	return clonePlan(p)
}

func (q Tools) Calls() []contract.ToolCall {
	q.b.check()
	cs := q.b.view.ToolCalls()
	for i := range cs {
		cs[i] = cloneToolCall(cs[i])
	}
	return cs
}

func (q Tools) Results() []contract.ToolResult {
	q.b.check()
	rs := q.b.view.ToolResults()
	for i := range rs {
		rs[i] = cloneResult(rs[i])
	}
	return rs
}

func (q Tools) Speculative() []core.SpeculativeState {
	q.b.check()
	return q.b.view.Speculative()
}

type Env struct{ b *base }

func (q Env) Config() json.RawMessage { q.b.check(); return q.b.view.Config() }
func (q Env) ContractVersion() int    { q.b.check(); return contract.ContractVersion }

// Binding 은 step 별로 바인딩된 세트 이름이다 (IN-FR-21).
func (q Env) Binding() map[core.Step]string {
	q.b.check()
	return q.b.d.Binding()
}
