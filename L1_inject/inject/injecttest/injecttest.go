// Package injecttest 는 inject 주입 함수의 단위 테스트 도구다 (sets ST-NFR-01, inject IN-FR-47).
package injecttest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

func missing(step core.Step) error {
	return fmt.Errorf("injecttest: %s not provided", step)
}

// Facts 는 가짜 View 의 사실이다. 포인터 필드가 nil 이면 "없음" 이다.
type Facts struct {
	RunID, SessionID string
	Config           json.RawMessage
	Step             core.Step
	Seq              int
	TurnIndex        int
	CallIndex        int
	Elapsed          time.Duration

	History, Transcript, TurnAppended, Ephemeral, Projection, Inbox []contract.Message

	Request   *contract.Request
	Model     *contract.ModelInfo
	Event     *contract.StreamEvent
	Draft     *contract.Message
	Response  *contract.Message
	CallError *contract.CallError
	Plan      *core.Plan

	ToolCalls   []contract.ToolCall
	ToolResults []contract.ToolResult
	Edits       []contract.ContextEdit
	Signals     []core.SignalRecord
	Speculative []core.SpeculativeState
}

// View 는 Facts 를 inject.ViewSource 로 돌려준다.
func (f Facts) View() inject.ViewSource { return view{f} }

// Dispatch 는 set 의 빈 step 을 Fill 로 채워 바인딩하고 step 하나를 dispatch 한다.
func Dispatch(ctx context.Context, set inject.FunctionSet, step core.Step, input any, v inject.ViewSource) (core.Result, error) {
	d, err := inject.Bind(Fill(set))
	if err != nil {
		return core.Result{}, err
	}
	return d.DispatchSource(ctx, step, input, v)
}

type view struct{ f Facts }

func cp[T any](s []T) []T { return append([]T(nil), s...) }

func opt[T any](p *T) (T, bool) {
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}

func (v view) RunID() string                         { return v.f.RunID }
func (v view) SessionID() string                     { return v.f.SessionID }
func (v view) Config() json.RawMessage               { return cp(v.f.Config) }
func (v view) Step() core.Step                       { return v.f.Step }
func (v view) Seq() int                              { return v.f.Seq }
func (v view) TurnIndex() int                        { return v.f.TurnIndex }
func (v view) CallIndex() int                        { return v.f.CallIndex }
func (v view) Elapsed() time.Duration                { return v.f.Elapsed }
func (v view) History() []contract.Message           { return cp(v.f.History) }
func (v view) Transcript() []contract.Message        { return cp(v.f.Transcript) }
func (v view) TurnAppended() []contract.Message      { return cp(v.f.TurnAppended) }
func (v view) Ephemeral() []contract.Message         { return cp(v.f.Ephemeral) }
func (v view) Projection() []contract.Message        { return cp(v.f.Projection) }
func (v view) Inbox() []contract.Message             { return cp(v.f.Inbox) }
func (v view) Request() (contract.Request, bool)     { return opt(v.f.Request) }
func (v view) Model() (contract.ModelInfo, bool)     { return opt(v.f.Model) }
func (v view) Event() (contract.StreamEvent, bool)   { return opt(v.f.Event) }
func (v view) Draft() (contract.Message, bool)       { return opt(v.f.Draft) }
func (v view) Response() (contract.Message, bool)    { return opt(v.f.Response) }
func (v view) CallError() (contract.CallError, bool) { return opt(v.f.CallError) }
func (v view) Plan() (core.Plan, bool)               { return opt(v.f.Plan) }
func (v view) ToolCalls() []contract.ToolCall        { return cp(v.f.ToolCalls) }
func (v view) ToolResults() []contract.ToolResult    { return cp(v.f.ToolResults) }
func (v view) Edits() []contract.ContextEdit         { return cp(v.f.Edits) }
func (v view) Signals() []core.SignalRecord          { return cp(v.f.Signals) }
func (v view) Speculative() []core.SpeculativeState  { return cp(v.f.Speculative) }

func (v view) SignalCount(kind core.SignalKind, reason string) int {
	n := 0
	for _, r := range v.f.Signals {
		for _, s := range r.Signals {
			if s.Kind == kind && (reason == "" || s.Reason == reason) {
				n++
			}
		}
	}
	return n
}
