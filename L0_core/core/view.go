package core

import (
	"encoding/json"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// View 는 dispatch 시작 시점의 읽기 전용 스냅샷이다 (CORE-FR-28, 29).
// 슬라이스를 돌려주는 메서드는 복사본을 돌려준다.
type View struct {
	s *snapshot
}

type snapshot struct {
	runID, sessionID string
	config           json.RawMessage
	step             Step
	seq, turn, call  int
	elapsed          time.Duration

	history, transcript, turnAppended, ephemeral, projection, inbox []contract.Message

	request  *contract.Request
	model    *contract.ModelInfo
	draft    []contract.Block
	event    *contract.StreamEvent
	response *contract.Message
	callErr  *contract.CallError
	plan     *Plan

	toolCalls   []contract.ToolCall
	toolResults []contract.ToolResult
	edits       []contract.ContextEdit
	records     []SignalRecord
	spec        []SpeculativeState
}

func clone[T any](s []T) []T { return append([]T(nil), s...) }

func opt[T any](p *T) (T, bool) {
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}

func (v View) RunID() string                       { return v.s.runID }
func (v View) SessionID() string                   { return v.s.sessionID }
func (v View) Config() json.RawMessage             { return clone(v.s.config) }
func (v View) Step() Step                          { return v.s.step }
func (v View) Unit() Unit                          { return v.s.step.Unit() }
func (v View) Seq() int                            { return v.s.seq }
func (v View) TurnIndex() int                      { return v.s.turn }
func (v View) CallIndex() int                      { return v.s.call }
func (v View) Elapsed() time.Duration              { return v.s.elapsed }
func (v View) History() []contract.Message         { return clone(v.s.history) }
func (v View) Transcript() []contract.Message      { return clone(v.s.transcript) }
func (v View) TurnAppended() []contract.Message    { return clone(v.s.turnAppended) }
func (v View) Ephemeral() []contract.Message       { return clone(v.s.ephemeral) }
func (v View) Projection() []contract.Message      { return clone(v.s.projection) }
func (v View) Inbox() []contract.Message           { return clone(v.s.inbox) }
func (v View) Request() (contract.Request, bool)   { return opt(v.s.request) }
func (v View) Model() (contract.ModelInfo, bool)   { return opt(v.s.model) }
func (v View) Event() (contract.StreamEvent, bool) { return opt(v.s.event) }
func (v View) Response() (contract.Message, bool)  { return opt(v.s.response) }
func (v View) CallError() (contract.CallError, bool) {
	return opt(v.s.callErr)
}
func (v View) ToolCalls() []contract.ToolCall     { return clone(v.s.toolCalls) }
func (v View) ToolResults() []contract.ToolResult { return clone(v.s.toolResults) }
func (v View) Edits() []contract.ContextEdit      { return clone(v.s.edits) }
func (v View) Signals() []SignalRecord            { return clone(v.s.records) }
func (v View) Speculative() []SpeculativeState    { return clone(v.s.spec) }

// Plan 은 이번 턴의 도구 계획이다 (15 이후).
func (v View) Plan() (Plan, bool) {
	p, ok := opt(v.s.plan)
	p.Groups = clone(p.Groups)
	return p, ok
}

// Draft 는 지금까지의 스트림 이벤트로 조립한 부분 응답이다 (CORE-FR-46). 블록이 없으면 false.
func (v View) Draft() (contract.Message, bool) {
	if len(v.s.draft) == 0 {
		return contract.Message{}, false
	}
	return contract.Message{Role: contract.RoleAssistant, Content: clone(v.s.draft)}, true
}

// SignalCount 는 신호 기록에서 kind 의 횟수를 센다. reason 이 비면 사유와 무관하게 센다 (CORE-FR-30).
func (v View) SignalCount(kind SignalKind, reason string) int {
	n := 0
	for _, r := range v.s.records {
		for _, s := range r.Signals {
			if s.Kind == kind && (reason == "" || s.Reason == reason) {
				n++
			}
		}
	}
	return n
}
