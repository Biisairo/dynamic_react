package core_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

func TestPlainRun(t *testing.T) {
	f := newFake()
	s := run(t, f)
	wantReason(t, s, core.SettleCompleted)
	if s.Err != nil {
		t.Fatalf("err = %v", s.Err)
	}
	wantSteps(t, f.steps(), seqOf(one(core.StepRunStart), plainTurn, one(core.StepRunBeforeSettle, core.StepRunSettled))...)
}

// CORE-FR-02, 39: step 별 입력 타입.
func TestInputTypes(t *testing.T) {
	f := newFake()
	run(t, f)
	want := map[core.Step]string{
		core.StepRunStart: "core.RunInput", core.StepTurnStart: "<nil>", core.StepContextMaintain: "<nil>",
		core.StepContextInject: "<nil>", core.StepContextProject: "<nil>", core.StepRequestBuild: "<nil>",
		core.StepRequestFinalize: "contract.Request", core.StepModelSelect: "contract.Request",
		core.StepModelCall: "contract.Request", core.StepStreamEvent: "contract.StreamEvent",
		core.StepStreamEnd: "contract.Message", core.StepResponseClassify: "<nil>", core.StepResponseCommit: "<nil>",
		core.StepToolsPlan: "[]contract.ToolCall", core.StepToolsCommit: "core.ToolsCommitInput",
		core.StepTurnFinish: "<nil>", core.StepRunBeforeSettle: "<nil>", core.StepRunSettled: "core.Settle",
	}
	for step, typ := range want {
		cs := f.callsOf(step)
		if len(cs) == 0 {
			t.Fatalf("%s not dispatched", step)
		}
		if got := fmt.Sprintf("%T", cs[0].Input); got != typ {
			t.Errorf("%s input %s, want %s", step, got, typ)
		}
	}
	in := f.callsOf(core.StepRunStart)[0].Input.(core.RunInput)
	if in.SessionID != "s1" {
		t.Errorf("RunInput.SessionID = %q", in.SessionID)
	}
	req := f.callsOf(core.StepModelCall)[0].Input.(contract.Request)
	if req.Model != modelInfo().Ref {
		t.Errorf("model.call request model = %+v, want selected ref", req.Model)
	}
}

// CORE-FR-02, 39: 잘못된 출력 타입.
func TestOutputTypeMismatch(t *testing.T) {
	cases := []struct {
		step core.Step
		out  any
	}{
		{core.StepRunStart, "x"},
		{core.StepTurnStart, contract.Message{}},
		{core.StepContextMaintain, 1},
		{core.StepContextInject, nil},
		{core.StepRequestBuild, &contract.Request{}},
		{core.StepModelSelect, contract.ModelRef{}},
		{core.StepModelCall, nil},
		{core.StepStreamEvent, "x"},
		{core.StepStreamEnd, contract.Message{}},
		{core.StepResponseClassify, 0},
		{core.StepToolsPlan, []core.Group{}},
		{core.StepToolsCommit, []contract.ToolResult{}},
		{core.StepTurnFinish, true},
		{core.StepRunBeforeSettle, "x"},
	}
	for _, c := range cases {
		t.Run(c.step.String(), func(t *testing.T) {
			f := newFake().on(c.step, func(call) (core.Result, error) {
				sigs := []core.Signal(nil)
				if c.step == core.StepTurnFinish {
					sigs = []core.Signal{sig(core.SigSettle)}
				}
				return core.Result{Output: c.out, Signals: sigs}, nil
			})
			s := run(t, f)
			wantReason(t, s, core.SettleFailed)
			if !errors.Is(s.Err, core.ErrOutputType) {
				t.Fatalf("err = %v, want ErrOutputType", s.Err)
			}
		})
	}
}

// CORE-FR-34, 35
func TestDispatcherErrorAndPanic(t *testing.T) {
	boom := errors.New("boom")
	f := newFake().on(core.StepRequestBuild, func(call) (core.Result, error) { return core.Result{}, boom })
	s := run(t, f)
	wantReason(t, s, core.SettleFailed)
	if !errors.Is(s.Err, boom) {
		t.Fatalf("err = %v", s.Err)
	}

	f = newFake().on(core.StepContextProject, func(call) (core.Result, error) { panic("kaboom") })
	s = run(t, f)
	wantReason(t, s, core.SettleFailed)
	if !errors.Is(s.Err, core.ErrPanic) {
		t.Fatalf("err = %v", s.Err)
	}
	if f.count(core.StepRunSettled) != 1 {
		t.Fatal("run.settled not dispatched once after panic")
	}
}

// CORE-FR-13, 14: 모든 종료 경로에서 22 가 정확히 한 번, Reason 이 맞음.
func TestSettleExactlyOnce(t *testing.T) {
	failWith := func(k core.SignalKind) handler {
		return func(call) (core.Result, error) { return out(nil, sig(k)) }
	}
	cases := []struct {
		name   string
		step   core.Step
		h      handler
		reason core.SettleReason
		errIs  error
	}{
		{"run.start stop", core.StepRunStart, func(call) (core.Result, error) { return out(core.RunInit{}, sig(core.SigStop)) }, core.SettleStopped, nil},
		{"run.start fail", core.StepRunStart, func(call) (core.Result, error) { return out(core.RunInit{}, sig(core.SigFail)) }, core.SettleFailed, core.ErrFailSignal},
		{"turn.start settle", core.StepTurnStart, func(call) (core.Result, error) { return out([]contract.Message(nil), sig(core.SigSettle)) }, core.SettleCompleted, nil},
		{"maintain stop", core.StepContextMaintain, failWith(core.SigStop), core.SettleStopped, nil},
		{"call.error fail", core.StepModelCall, func(call) (core.Result, error) {
			return out(rawStream(item{ev: errEvent(contract.ClassAuth)}).cs)
		}, core.SettleFailed, core.ErrFailSignal},
		{"classify stop", core.StepResponseClassify, failWith(core.SigStop), core.SettleStopped, nil},
		{"turn.finish stop", core.StepTurnFinish, failWith(core.SigStop), core.SettleStopped, nil},
		{"beforeSettle stop", core.StepRunBeforeSettle, failWith(core.SigStop), core.SettleStopped, nil},
		{"beforeSettle fail", core.StepRunBeforeSettle, failWith(core.SigFail), core.SettleFailed, core.ErrFailSignal},
		{"panic", core.StepToolsPlan, func(call) (core.Result, error) { panic(1) }, core.SettleFailed, core.ErrPanic},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake().on(c.step, c.h)
			s := run(t, f)
			wantReason(t, s, c.reason)
			if c.errIs != nil && !errors.Is(s.Err, c.errIs) {
				t.Fatalf("err = %v, want %v", s.Err, c.errIs)
			}
			if c.errIs == nil && s.Err != nil {
				t.Fatalf("err = %v, want nil", s.Err)
			}
			if n := f.count(core.StepRunSettled); n != 1 {
				t.Fatalf("run.settled dispatched %d times", n)
			}
			st := f.callsOf(core.StepRunSettled)[0].Input.(core.Settle)
			if st.Reason != c.reason {
				t.Fatalf("settle input reason %s", st.Reason)
			}
			steps := f.steps()
			if steps[len(steps)-1] != core.StepRunSettled {
				t.Fatal("run.settled is not last dispatch")
			}
		})
	}
}

func TestFailSignalCarriesError(t *testing.T) {
	cause := errors.New("cause")
	f := newFake().on(core.StepContextMaintain, func(call) (core.Result, error) {
		return out(nil, core.Signal{Kind: core.SigFail, Err: cause})
	})
	s := run(t, f)
	wantReason(t, s, core.SettleFailed)
	if !errors.Is(s.Err, cause) {
		t.Fatalf("err = %v", s.Err)
	}
}

// 22 의 신호·에러는 결과를 바꾸지 않는다 (CORE-FR-13, 48).
func TestRunSettledIgnoresSignalsAndErrors(t *testing.T) {
	obs := &recorder{}
	f := newFake().on(core.StepRunSettled, func(call) (core.Result, error) {
		return core.Result{Signals: []core.Signal{sig(core.SigFail)}}, errors.New("flush failed")
	})
	s := run(t, f, core.Options{Observer: obs})
	wantReason(t, s, core.SettleCompleted)
	ended := obs.find(core.ObsStepEnded, core.StepRunSettled)
	if len(ended) != 1 || ended[0].Err == nil {
		t.Fatalf("run.settled StepEnded = %+v", ended)
	}

	f = newFake().on(core.StepRunSettled, func(call) (core.Result, error) { panic("x") })
	wantReason(t, run(t, f), core.SettleCompleted)
}

// CORE-FR-33: 22 는 취소되지 않은 context, SettleTimeout 기한.
func TestSettleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var settleErr error
	var dl time.Time
	var hasDL bool
	f := newFake().
		on(core.StepContextMaintain, func(call) (core.Result, error) { cancel(); return out(nil) }).
		on(core.StepRunSettled, func(c call) (core.Result, error) {
			settleErr = c.Ctx.Err()
			dl, hasDL = c.Ctx.Deadline()
			return out(nil)
		})
	s := core.New(f, core.Options{SettleTimeout: time.Minute}).Start(ctx, core.RunInput{}).Wait()
	wantReason(t, s, core.SettleAborted)
	if !errors.Is(s.Err, context.Canceled) {
		t.Fatalf("err = %v", s.Err)
	}
	if settleErr != nil {
		t.Fatal("run.settled context canceled")
	}
	if !hasDL || time.Until(dl) > time.Minute || time.Until(dl) < 50*time.Second {
		t.Fatalf("deadline = %v, %v", dl, hasDL)
	}
	// 취소 뒤에는 새 dispatch 를 시작하지 않는다 (CORE-FR-32, 44).
	wantSteps(t, f.steps(), core.StepRunStart, core.StepTurnStart, core.StepContextMaintain, core.StepRunSettled)
}

func TestCancelBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFake()
	s := core.New(f, core.Options{}).Start(ctx, core.RunInput{}).Wait()
	wantReason(t, s, core.SettleAborted)
	wantSteps(t, f.steps(), core.StepRunSettled)
}

func TestDefaultSettleTimeout(t *testing.T) {
	var dl time.Time
	f := newFake().on(core.StepRunSettled, func(c call) (core.Result, error) { dl, _ = c.Ctx.Deadline(); return out(nil) })
	run(t, f)
	if d := time.Until(dl); d > core.DefaultSettleTimeout || d < core.DefaultSettleTimeout-time.Second {
		t.Fatalf("default settle deadline in %v", d)
	}
	if core.DefaultSettleTimeout != 5*time.Second || core.DefaultMaxDispatches != 10000 {
		t.Fatal("defaults changed")
	}
}

// CORE-FR-36, 48: 10·16·17·18·22 는 세지 않는다.
func TestGuard(t *testing.T) {
	f := newFake().on(core.StepTurnFinish, func(call) (core.Result, error) { return out(nil, sig(core.SigContinueTurn)) })
	s := run(t, f, core.Options{MaxDispatches: 50})
	wantReason(t, s, core.SettleGuard)
	if !errors.Is(s.Err, core.ErrGuard) {
		t.Fatalf("err = %v", s.Err)
	}
	counted := 0
	for _, st := range f.steps() {
		switch st {
		case core.StepStreamEvent, core.StepToolBefore, core.StepToolExecute, core.StepToolAfter, core.StepRunSettled:
		default:
			counted++
		}
	}
	if counted != 50 {
		t.Fatalf("counted dispatches = %d, want 50", counted)
	}
	if f.count(core.StepRunSettled) != 1 {
		t.Fatal("run.settled not once")
	}
}

func TestGuardDefault(t *testing.T) {
	f := newFake().on(core.StepTurnFinish, func(call) (core.Result, error) { return out(nil, sig(core.SigContinueTurn)) })
	wantReason(t, run(t, f), core.SettleGuard)
	if n := f.count(core.StepTurnStart); n < 700 || n > 800 {
		t.Fatalf("turns = %d, want ~10000/13", n)
	}
}

// CORE-NFR-02
func TestDeterministicOrder(t *testing.T) {
	script := func() *fake {
		n := 0
		return newFake().
			on(core.StepModelCall, func(c call) (core.Result, error) {
				if c.N == 0 {
					return out(streamOf(toolResponse("a1", tc("c1", "read", `{}`), tc("c2", "bash", `{}`)), textDelta(1, "x")).cs)
				}
				return out(streamOf(textResponse("a2")).cs)
			}).
			on(core.StepTurnFinish, func(call) (core.Result, error) {
				n++
				if n == 1 {
					return out(nil, sig(core.SigContinueTurn))
				}
				return out(nil, sig(core.SigSettle))
			})
	}
	a, b := script(), script()
	run(t, a)
	run(t, b)
	wantSteps(t, a.steps(), b.steps()...)
}

// CORE-FR-11, CR-C2: 동시 run 100개.
func TestConcurrentRuns(t *testing.T) {
	fakes := make([]*fake, 100)
	for i := range fakes {
		turns := i % 5
		n := 0
		fakes[i] = newFake().on(core.StepTurnFinish, func(call) (core.Result, error) {
			n++
			if n <= turns {
				return out(nil, sig(core.SigContinueTurn))
			}
			return out(nil, sig(core.SigSettle))
		})
	}
	k := core.New(router{fakes}, core.Options{})
	var wg sync.WaitGroup
	for i := range fakes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := k.Start(context.Background(), core.RunInput{SessionID: fmt.Sprint(i)}).Wait()
			if s.Reason != core.SettleCompleted {
				t.Errorf("run %d: %s %v", i, s.Reason, s.Err)
			}
		}(i)
	}
	wg.Wait()
	for i, f := range fakes {
		if got, want := f.count(core.StepTurnStart), i%5+1; got != want {
			t.Errorf("run %d: turns %d, want %d", i, got, want)
		}
	}
}

type router struct{ fakes []*fake }

func (r router) Dispatch(ctx context.Context, s core.Step, in any, v core.View) (core.Result, error) {
	var i int
	fmt.Sscan(v.SessionID(), &i)
	return r.fakes[i].Dispatch(ctx, s, in, v)
}

// CORE-FR-42
func TestTurnAndCallIndex(t *testing.T) {
	f := newFake().
		on(core.StepStreamEnd, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(core.StreamEndOut{Response: c.Input.(contract.Message)}, sig(core.SigRecall))
			}
			return out(core.StreamEndOut{Response: c.Input.(contract.Message)})
		}).
		on(core.StepResponseClassify, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(nil, sig(core.SigRestartTurn))
			}
			return out(nil)
		}).
		on(core.StepTurnFinish, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(nil, sig(core.SigContinueTurn))
			}
			return out(nil, sig(core.SigSettle))
		})
	run(t, f)
	var got []string
	for _, c := range f.callsOf(core.StepModelCall) {
		got = append(got, fmt.Sprintf("%d.%d", c.View.TurnIndex(), c.View.CallIndex()))
	}
	// 턴1: 호출1 → recall → 호출2 → classify restart. 턴2: 호출1 → continue. 턴3: 호출1.
	if fmt.Sprint(got) != "[1.1 1.2 2.1 3.1]" {
		t.Fatalf("turn.call = %v", got)
	}
	if v := f.callsOf(core.StepRunStart)[0].View; v.TurnIndex() != 0 || v.CallIndex() != 0 {
		t.Fatal("indices before first turn not zero")
	}
}

// CORE-FR-37, 38, 47
func TestObserverOrder(t *testing.T) {
	obs := &recorder{}
	f := newFake()
	s := run(t, f, core.Options{Observer: obs})
	evs := obs.all()
	if evs[0].Kind != core.ObsRunStarted || evs[len(evs)-1].Kind != core.ObsRunSettled {
		t.Fatalf("first/last = %s/%s", evs[0].Kind, evs[len(evs)-1].Kind)
	}
	if evs[len(evs)-1].Settle == nil || evs[len(evs)-1].Settle.Reason != s.Reason {
		t.Fatal("RunSettled lacks settle")
	}
	runID := evs[0].RunID
	var open *core.ObserverEvent
	streams := 0
	for i := 1; i < len(evs)-1; i++ {
		e := evs[i]
		if e.RunID != runID || runID == "" {
			t.Fatalf("event %d run id %q", i, e.RunID)
		}
		switch e.Kind {
		case core.ObsStepStarted:
			if open != nil {
				t.Fatalf("event %d: step %s started inside %s", i, e.Step, open.Step)
			}
			open = &evs[i]
		case core.ObsStepEnded:
			if open == nil || open.Step != e.Step || open.Seq != e.Seq {
				t.Fatalf("event %d: unmatched end %s", i, e.Step)
			}
			open = nil
		case core.ObsStreamEvent:
			streams++
			if e.Stream == nil || open != nil {
				t.Fatalf("event %d: stream event misplaced", i)
			}
			if next := evs[i+1]; next.Kind != core.ObsStepStarted || next.Step != core.StepStreamEvent {
				t.Fatalf("stream event not followed by stream.event dispatch")
			}
		default:
			t.Fatalf("unexpected %s", e.Kind)
		}
	}
	if streams != 2 {
		t.Fatalf("stream events = %d", streams)
	}
	starts := obs.find(core.ObsStepStarted, 0)
	if len(starts) != len(f.steps()) {
		t.Fatalf("StepStarted %d, dispatches %d", len(starts), len(f.steps()))
	}
	for i, e := range starts {
		if e.Seq != i+1 || e.Unit != e.Step.Unit() {
			t.Fatalf("StepStarted %d: seq %d unit %s", i, e.Seq, e.Unit)
		}
	}
	end := obs.find(core.ObsStepEnded, core.StepTurnFinish)[0]
	if len(end.Signals) != 1 || end.Signals[0].Kind != core.SigSettle {
		t.Fatalf("turn.finish StepEnded signals %v", end.Signals)
	}
}

type recorder struct {
	mu     sync.Mutex
	evs    []core.ObserverEvent
	active int
	maxAct int
}

func (r *recorder) Observe(e core.ObserverEvent) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxAct {
		r.maxAct = r.active
	}
	r.evs = append(r.evs, e)
	r.mu.Unlock()
	time.Sleep(time.Microsecond)
	r.mu.Lock()
	r.active--
	r.mu.Unlock()
}

func (r *recorder) all() []core.ObserverEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]core.ObserverEvent(nil), r.evs...)
}

func (r *recorder) find(k core.ObserverEventKind, s core.Step) []core.ObserverEvent {
	var out []core.ObserverEvent
	for _, e := range r.all() {
		if e.Kind == k && (s == 0 || e.Step == s) {
			out = append(out, e)
		}
	}
	return out
}
