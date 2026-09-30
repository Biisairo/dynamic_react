// Package inject 는 주입 함수를 step 별 타입으로 정의하고, 그것을 core 의 Dispatcher 로 동작하게 만든다.
// step 별 ctx·신호·함수 타입은 internal/stepspec 에서 생성한다 (zz_generated.go).
package inject

//go:generate go run ./internal/gen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

var (
	ErrExpiredCtx    = errors.New("inject: ctx used after its dispatch returned")
	ErrForeignSignal = errors.New("inject: signal does not belong to this dispatch")
	ErrSignalOrder   = errors.New("inject: signal rule violation")
	ErrInputType     = errors.New("inject: unexpected input type")
	ErrPanic         = errors.New("inject: function panic")
	ErrBind          = errors.New("inject: bind")
)

// ViewSource 는 조회가 읽는 사실의 원천이다. core.View 가 그대로 만족한다 (IN-FR-47).
type ViewSource interface {
	RunID() string
	SessionID() string
	Config() json.RawMessage
	Step() core.Step
	Seq() int
	TurnIndex() int
	CallIndex() int
	Elapsed() time.Duration
	History() []contract.Message
	Transcript() []contract.Message
	TurnAppended() []contract.Message
	Ephemeral() []contract.Message
	Projection() []contract.Message
	Inbox() []contract.Message
	Request() (contract.Request, bool)
	Model() (contract.ModelInfo, bool)
	Event() (contract.StreamEvent, bool)
	Draft() (contract.Message, bool)
	Response() (contract.Message, bool)
	CallError() (contract.CallError, bool)
	Plan() (core.Plan, bool)
	ToolCalls() []contract.ToolCall
	ToolResults() []contract.ToolResult
	Edits() []contract.ContextEdit
	Signals() []core.SignalRecord
	SignalCount(kind core.SignalKind, reason string) int
	Speculative() []core.SpeculativeState
}

var _ ViewSource = core.View{}

// invocation 은 dispatch 한 번의 토큰이다. 신호와 ctx 가 이것을 가리킨다 (IN-FR-09, 12).
type invocation struct{ expired atomic.Bool }

// sig 는 봉인된 신호의 내용이다.
type sig struct {
	s   core.Signal
	inv *invocation
}

func (x sig) Kind() core.SignalKind { return x.s.Kind }

// base 는 모든 step ctx 의 공통 부분이다.
type base struct {
	inv  *invocation
	ctx  context.Context
	view ViewSource
	d    *Dispatcher
	st   *stateCtx

	mu      sync.Mutex
	dropped []core.Signal
}

// check 는 폐기된 ctx 사용을 막는다 (IN-FR-11).
func (b *base) check() {
	if b.inv.expired.Load() {
		panic(ErrExpiredCtx)
	}
}

func (b *base) context() context.Context {
	b.check()
	return b.ctx
}

func (b *base) mk(s core.Signal) sig {
	b.check()
	return sig{s: s, inv: b.inv}
}

func (b *base) drop(rs []sig) {
	b.check()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range rs {
		b.dropped = append(b.dropped, cloneSignal(r.s))
	}
}

func (b *base) groupFlow() Flow       { b.check(); return Flow{b} }
func (b *base) groupConv() Conv       { b.check(); return Conv{b} }
func (b *base) groupRequest() Request { b.check(); return Request{b} }
func (b *base) groupStream() Stream   { b.check(); return Stream{b} }
func (b *base) groupOutput() Output   { b.check(); return Output{b} }
func (b *base) groupTools() Tools     { b.check(); return Tools{b} }
func (b *base) groupEnv() Env         { b.check(); return Env{b} }
func (b *base) groupState() State     { b.check(); return State{b} }

type rawer interface{ raw() sig }

func rawAll[S rawer](in []S) ([]sig, error) {
	out := make([]sig, 0, len(in))
	for i, x := range in {
		if any(x) == nil {
			return nil, fmt.Errorf("%w: nil signal at %d", ErrForeignSignal, i)
		}
		out = append(out, x.raw())
	}
	return out, nil
}

func finish[S rawer](out any, s []S, err error) (any, []sig, error) {
	if err != nil {
		return nil, nil, err
	}
	rs, err := rawAll(s)
	return out, rs, err
}

func inputErr(step core.Step, input any) error {
	return fmt.Errorf("%w: %s got %T", ErrInputType, step, input)
}

func cloneResults(rs []contract.ToolResult) []contract.ToolResult {
	if rs == nil {
		return []contract.ToolResult{}
	}
	out := make([]contract.ToolResult, len(rs))
	for i, r := range rs {
		out[i] = cloneResult(r)
	}
	return out
}
