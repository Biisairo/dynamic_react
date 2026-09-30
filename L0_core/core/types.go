package core

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

const (
	// DefaultMaxDispatches 는 전이 수 상한의 기본값이다 (R6).
	DefaultMaxDispatches = 10000
	// DefaultSettleTimeout 은 run.settled dispatch 기한의 기본값이다.
	DefaultSettleTimeout = 5 * time.Second
)

var (
	ErrFailSignal = errors.New("core: fail signal")
	ErrSignalRule = errors.New("core: signal rule violation")
	ErrOutputType = errors.New("core: unexpected output type")
	ErrPanic      = errors.New("core: dispatcher panic")
	ErrPlan       = errors.New("core: invalid plan")
	ErrCommit     = errors.New("core: invalid tools.commit output")
	ErrGuard      = errors.New("core: dispatch limit reached")
)

// Dispatcher 는 step 하나의 실제 동작을 수행한다. 동시 호출에 안전해야 한다 (A2).
type Dispatcher interface {
	Dispatch(ctx context.Context, step Step, input any, view View) (Result, error)
}

// Result 는 dispatch 한 번의 결과다. Origin 은 신호 기록에 남는 설명이다.
type Result struct {
	Output  any
	Signals []Signal
	Origin  string
}

// Signal 은 제어 약속어 하나다. Kind 에 해당하는 payload 만 채운다 (CORE-FR-40).
type Signal struct {
	Kind    SignalKind
	Reason  string
	Edit    *contract.ContextEdit
	Message *contract.Message
	Call    *contract.ToolCall
	Results []contract.ToolResult
	Err     error
}

// 신호 기록의 효과 적용 결과.
const (
	ResultApplied       = "applied"
	ResultDeliveredLive = "delivered_live"
	ResultDeferred      = "deferred"
	ResultDuplicate     = "duplicate"
)

// SignalRecord 는 신호를 낸 dispatch 하나의 기록이다 (CORE-FR-30). Results 는 Signals 와 같은 위치다.
type SignalRecord struct {
	Seq     int
	Step    Step
	Unit    Unit
	Signals []Signal
	Origin  string
	Results []string
}

type RunInput struct {
	SessionID string
	Input     []contract.Message
	Config    json.RawMessage
}

type RunInit struct {
	History []contract.Message
}

// LiveInput 은 스트림 도중 입력을 받는 통로다.
type LiveInput interface {
	Send(msg contract.Message) error
}

type CallStream struct {
	Events iter.Seq2[contract.StreamEvent, error]
	Live   LiveInput
}

type StreamEndOut struct {
	Response contract.Message
	Discard  []string
}

type Plan struct {
	Groups []Group
}

type Group struct {
	Calls       []contract.ToolCall
	MaxParallel int
}

type BeforeOut struct {
	Call         contract.ToolCall
	ShortCircuit *contract.ToolResult
}

type ToolAfterInput struct {
	Call   contract.ToolCall
	Result contract.ToolResult
}

type ToolsCommitInput struct {
	Calls   []contract.ToolCall
	Results []contract.ToolResult
}

// SettleReason 은 run 이 끝난 방식이다 (CORE-FR-14).
type SettleReason string

const (
	SettleCompleted SettleReason = "completed"
	SettleStopped   SettleReason = "stopped"
	SettleFailed    SettleReason = "failed"
	SettleAborted   SettleReason = "aborted"
	SettleGuard     SettleReason = "guard"
)

type Settle struct {
	Reason SettleReason
	Err    error
}

type Options struct {
	MaxDispatches int           // 0 이하면 DefaultMaxDispatches
	SettleTimeout time.Duration // 0 이하면 DefaultSettleTimeout
	Observer      Observer
}

// Observer 는 생명주기 이벤트를 받는다. 호출은 동기이고 core 가 직렬화한다 (CORE-FR-37, 38, 47).
type Observer interface {
	Observe(ev ObserverEvent)
}

type ObserverEventKind string

const (
	ObsRunStarted  ObserverEventKind = "run_started"
	ObsStepStarted ObserverEventKind = "step_started"
	ObsStepEnded   ObserverEventKind = "step_ended"
	ObsStreamEvent ObserverEventKind = "stream_event"
	ObsRunSettled  ObserverEventKind = "run_settled"
)

type ObserverEvent struct {
	Kind     ObserverEventKind
	RunID    string
	Step     Step
	Seq      int
	Unit     Unit
	Signals  []Signal
	Err      error
	Duration time.Duration
	Stream   *contract.StreamEvent
	Settle   *Settle
}

// SpeculativeState 는 선행 실행 핸들 하나의 상태다.
type SpeculativeState struct {
	CallID     string
	ArgsDigest string
	Done       bool
}
