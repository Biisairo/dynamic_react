package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// Kernel 은 Dispatcher 와 옵션만 가진 값이다. Start 를 여러 번·동시에 불러도 run 끼리 상태를 공유하지 않는다 (CORE-FR-11).
type Kernel struct {
	d    Dispatcher
	opts Options
}

// Run 은 실행 중인 run 하나다.
type Run struct {
	r      *runner
	done   chan struct{}
	settle Settle
}

func New(d Dispatcher, opts Options) *Kernel {
	if opts.MaxDispatches <= 0 {
		opts.MaxDispatches = DefaultMaxDispatches
	}
	if opts.SettleTimeout <= 0 {
		opts.SettleTimeout = DefaultSettleTimeout
	}
	return &Kernel{d: d, opts: opts}
}

func (k *Kernel) Start(ctx context.Context, in RunInput) *Run {
	r := &runner{
		d:     k.d,
		opts:  k.opts,
		ctx:   ctx,
		input: in,
		st: state{
			runID:     newRunID(),
			sessionID: in.SessionID,
			config:    clone(in.Config),
			started:   time.Now(),
		},
		spec: map[specKey]*handle{},
	}
	run := &Run{r: r, done: make(chan struct{})}
	go func() {
		run.settle = r.run()
		close(run.done)
	}()
	return run
}

// Input 은 메시지를 입력함에 추가한다. 어느 goroutine 에서든 부를 수 있다 (CORE-FR-12).
func (run *Run) Input(msg contract.Message) {
	run.r.mu.Lock()
	run.r.st.inbox = append(run.r.st.inbox, msg)
	run.r.mu.Unlock()
}

// Wait 는 run 이 끝날 때까지 기다린다.
func (run *Run) Wait() Settle {
	<-run.done
	return run.settle
}

func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// state 는 run 의 가변 상태다. runner.mu 로 보호한다.
type state struct {
	runID, sessionID string
	config           []byte
	started          time.Time
	seq, turn, call  int

	history, transcript, turnAppended, ephemeral, projection, inbox []contract.Message

	request  *contract.Request
	model    *contract.ModelInfo
	draft    draft
	event    *contract.StreamEvent
	response *contract.Message
	callErr  *contract.CallError
	plan     *Plan

	toolCalls   []contract.ToolCall
	toolResults []contract.ToolResult
	edits       []contract.ContextEdit
	records     []SignalRecord
}

type runner struct {
	d     Dispatcher
	opts  Options
	ctx   context.Context
	input RunInput

	mu      sync.Mutex
	st      state
	counted int
	live    LiveInput
	discard []string // 11 의 Discard
	spec    map[specKey]*handle
	specWG  sync.WaitGroup

	// 메인 goroutine 만 쓰는 step 간 전달값.
	built    contract.Request // 6 → 7
	doneMsg  contract.Message // 10(done) → 11
	commitIn ToolsCommitInput // 15 → 19

	obsMu sync.Mutex
}

func capped[T any](s []T) []T { return s[:len(s):len(s)] }

// snapshot 은 r.mu 를 잡은 상태에서 부른다.
func (r *runner) snapshot(step Step, seq int) *snapshot {
	st := &r.st
	s := &snapshot{
		runID: st.runID, sessionID: st.sessionID, config: st.config,
		step: step, seq: seq, turn: st.turn, call: st.call, elapsed: time.Since(st.started),
		history: capped(st.history), transcript: capped(st.transcript), turnAppended: capped(st.turnAppended),
		ephemeral: capped(st.ephemeral), projection: capped(st.projection), inbox: capped(st.inbox),
		request: st.request, model: st.model, draft: clone(st.draft.blocks), event: st.event,
		response: st.response, callErr: st.callErr, plan: st.plan,
		toolCalls: clone(st.toolCalls), toolResults: capped(st.toolResults),
		edits: capped(st.edits), records: capped(st.records),
	}
	for _, h := range r.spec {
		s.spec = append(s.spec, SpeculativeState{CallID: h.key.id, ArgsDigest: h.key.digest, Done: h.isDone()})
	}
	sort.Slice(s.spec, func(i, j int) bool {
		a, b := s.spec[i], s.spec[j]
		return a.CallID < b.CallID || a.CallID == b.CallID && a.ArgsDigest < b.ArgsDigest
	})
	return s
}

func (r *runner) observe(ev ObserverEvent) {
	if r.opts.Observer == nil {
		return
	}
	ev.RunID = r.st.runID
	r.obsMu.Lock()
	defer r.obsMu.Unlock()
	r.opts.Observer.Observe(ev)
}

// outcome 은 dispatch 한 번의 해석 결과다. end 가 있으면 run 을 그것으로 끝낸다.
type outcome struct {
	out   any
	tr    Signal
	end   *Settle
	goErr error // model.call 의 Go error (call.error 로 감)
}

func failed(step Step, err error) *Settle {
	return &Settle{Reason: SettleFailed, Err: fmt.Errorf("core: %s: %w", step, err)}
}

func ruleErr(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrSignalRule}, args...)...)
}

// guarded 는 전이 수 상한 계산에서 빠지는 step 이다 (CORE-FR-36, 48).
func guarded(s Step) bool {
	switch s {
	case StepStreamEvent, StepToolBefore, StepToolExecute, StepToolAfter, StepRunSettled:
		return false
	}
	return true
}

func (r *runner) aborted() *Settle {
	return &Settle{Reason: SettleAborted, Err: r.ctx.Err()}
}

// do 는 step 하나를 dispatch 하고 출력·신호를 검증한 뒤 효과를 적용한다 (22 제외).
func (r *runner) do(ctx context.Context, step Step, input any) outcome {
	if r.ctx.Err() != nil {
		return outcome{end: r.aborted()}
	}
	r.mu.Lock()
	if guarded(step) {
		r.counted++
		if r.counted > r.opts.MaxDispatches {
			r.mu.Unlock()
			return outcome{end: &Settle{Reason: SettleGuard, Err: ErrGuard}}
		}
	}
	r.st.seq++
	seq := r.st.seq
	view := View{r.snapshot(step, seq)}
	r.mu.Unlock()

	r.observe(ObserverEvent{Kind: ObsStepStarted, Step: step, Seq: seq, Unit: step.Unit()})
	began := time.Now()
	res, err := r.call(ctx, step, input, view)
	o, applyErr := r.interpret(step, seq, res, err)
	if applyErr != nil {
		err = applyErr
	}
	r.observe(ObserverEvent{Kind: ObsStepEnded, Step: step, Seq: seq, Unit: step.Unit(), Signals: res.Signals, Err: err, Duration: time.Since(began)})
	if r.ctx.Err() != nil {
		return outcome{end: r.aborted()}
	}
	return o
}

func (r *runner) call(ctx context.Context, step Step, input any, view View) (res Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			res, err = Result{}, fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	return r.d.Dispatch(ctx, step, input, view)
}

// interpret 는 dispatch 결과를 검증하고 효과를 적용한다. 두 번째 반환값은 Observer 에 알릴 에러다.
func (r *runner) interpret(step Step, seq int, res Result, err error) (outcome, error) {
	if err != nil {
		if step == StepModelCall && !errors.Is(err, ErrPanic) {
			return outcome{goErr: err}, nil
		}
		return outcome{end: failed(step, err)}, err
	}
	if !outputOK(step, res.Output) {
		e := fmt.Errorf("%w: %T", ErrOutputType, res.Output)
		return outcome{end: failed(step, e)}, e
	}
	tr, e := checkSignals(step, res.Signals)
	if e != nil {
		return outcome{end: failed(step, e)}, e
	}
	results := r.applyEffects(step, res.Signals)
	if len(res.Signals) > 0 {
		r.mu.Lock()
		r.st.records = append(r.st.records, SignalRecord{
			Seq: seq, Step: step, Unit: step.Unit(), Signals: clone(res.Signals), Origin: res.Origin, Results: results,
		})
		r.mu.Unlock()
	}
	o := outcome{out: res.Output, tr: tr}
	switch tr.Kind {
	case SigFail:
		cause := tr.Err
		if cause == nil {
			cause = ErrFailSignal
		}
		o.end = failed(step, cause)
	case SigStop:
		o.end = &Settle{Reason: SettleStopped}
	}
	return o, nil
}

func outputOK(step Step, out any) bool {
	var ok bool
	switch step {
	case StepRunStart:
		_, ok = out.(RunInit)
	case StepTurnStart, StepContextInject, StepContextProject, StepToolsCommit:
		_, ok = out.([]contract.Message)
	case StepRequestBuild, StepRequestFinalize:
		_, ok = out.(contract.Request)
	case StepModelSelect:
		_, ok = out.(contract.ModelInfo)
	case StepModelCall:
		var cs CallStream
		cs, ok = out.(CallStream)
		ok = ok && cs.Events != nil
	case StepStreamEnd:
		_, ok = out.(StreamEndOut)
	case StepToolsPlan:
		_, ok = out.(Plan)
	case StepToolBefore:
		_, ok = out.(BeforeOut)
	case StepToolExecute, StepToolAfter:
		_, ok = out.(contract.ToolResult)
	default:
		ok = out == nil
	}
	return ok
}

// checkSignals 는 배열 규칙·허용 표·payload 규칙을 검사하고 전이를 돌려준다 (CORE-FR-05, 06, 09, 40).
func checkSignals(step Step, sigs []Signal) (Signal, error) {
	for i, s := range sigs {
		if !s.Kind.Valid() {
			return Signal{}, ruleErr("unknown signal %q", s.Kind)
		}
		if !Allowed(step, s.Kind) {
			return Signal{}, ruleErr("%s not allowed at %s", s.Kind, step)
		}
		if s.Kind.IsTransition() && i != len(sigs)-1 {
			return Signal{}, ruleErr("transition %s must be last", s.Kind)
		}
		if err := checkPayload(s); err != nil {
			return Signal{}, err
		}
	}
	if n := len(sigs); n > 0 && sigs[n-1].Kind.IsTransition() {
		return sigs[n-1], nil
	}
	if !Allowed(step, SigNext) {
		return Signal{}, ruleErr("%s requires an explicit transition", step)
	}
	return Signal{Kind: SigNext}, nil
}

func checkPayload(s Signal) error {
	if (s.Edit != nil) != (s.Kind == SigRewrite) || (s.Message != nil) != (s.Kind == SigInjectInput) ||
		(s.Call != nil) != (s.Kind == SigStartTool) ||
		(s.Results != nil && s.Kind != SigSkipTools) || (s.Err != nil && s.Kind != SigFail) {
		return ruleErr("%s: payload mismatch", s.Kind)
	}
	var err error
	switch s.Kind {
	case SigRewrite:
		err = s.Edit.Validate()
	case SigInjectInput:
		err = s.Message.Validate()
	case SigStartTool:
		err = s.Call.Validate()
	case SigSkipTools:
		for _, res := range s.Results {
			if err = res.Validate(); err != nil {
				break
			}
		}
	}
	if err != nil {
		return ruleErr("%s: %v", s.Kind, err)
	}
	return nil
}

// applyEffects 는 효과 신호를 순서대로 적용하고 신호별 결과를 돌려준다 (CORE-FR-07, 18, 19, 25).
func (r *runner) applyEffects(step Step, sigs []Signal) []string {
	results := make([]string, len(sigs))
	for i, s := range sigs {
		results[i] = ResultApplied
		switch s.Kind {
		case SigRewrite:
			r.mu.Lock()
			r.st.edits = append(r.st.edits, *s.Edit)
			r.mu.Unlock()
		case SigInjectInput:
			results[i] = r.injectInput(step, *s.Message)
		case SigStartTool:
			results[i] = r.startSpeculative(*s.Call)
		}
	}
	return results
}

func (r *runner) injectInput(step Step, msg contract.Message) string {
	r.mu.Lock()
	live := r.live
	r.mu.Unlock()
	if step == StepStreamEvent && live != nil && sendLive(live, msg) == nil {
		return ResultDeliveredLive
	}
	r.mu.Lock()
	r.st.inbox = append(r.st.inbox, msg)
	r.mu.Unlock()
	return ResultDeferred
}

func sendLive(l LiveInput, msg contract.Message) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	return l.Send(msg)
}

// settle 은 run.settled 를 정확히 한 번 dispatch 한다 (CORE-FR-13, 33, 48).
func (r *runner) settle(s Settle) Settle {
	r.cancelSpeculative()
	r.specWG.Wait()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), r.opts.SettleTimeout)
	defer cancel()
	r.mu.Lock()
	r.st.seq++
	seq := r.st.seq
	view := View{r.snapshot(StepRunSettled, seq)}
	r.mu.Unlock()
	r.observe(ObserverEvent{Kind: ObsStepStarted, Step: StepRunSettled, Seq: seq, Unit: UnitRun})
	began := time.Now()
	res, err := r.call(ctx, StepRunSettled, s, view)
	r.observe(ObserverEvent{Kind: ObsStepEnded, Step: StepRunSettled, Seq: seq, Unit: UnitRun, Signals: res.Signals, Err: err, Duration: time.Since(began)})
	r.observe(ObserverEvent{Kind: ObsRunSettled, Settle: &s})
	return s
}
