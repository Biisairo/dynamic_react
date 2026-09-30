package core_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

func edit(id string) *contract.ContextEdit {
	return &contract.ContextEdit{ID: id, Kind: contract.EditHide, Timestamp: ts, Target: "u1"}
}

// payload 가 올바른 신호 하나.
func validSig(k core.SignalKind) core.Signal {
	s := core.Signal{Kind: k}
	switch k {
	case core.SigRewrite:
		s.Edit = edit("e-" + string(k))
	case core.SigInjectInput:
		m := userMsg("i1", "nudge")
		s.Message = &m
	case core.SigStartTool:
		c := tc("c1", "read", `{}`)
		s.Call = &c
	case core.SigSkipTools:
		s.Results = []contract.ToolResult{}
	}
	return s
}

func signalRuleFails(t *testing.T, step core.Step, sigs []core.Signal) {
	t.Helper()
	f := newFake().on(step, func(c call) (core.Result, error) {
		r, _ := defaultResult(c)
		r.Signals = sigs
		return r, nil
	})
	s := run(t, f)
	wantReason(t, s, core.SettleFailed)
	if !errors.Is(s.Err, core.ErrSignalRule) {
		t.Fatalf("err = %v, want ErrSignalRule", s.Err)
	}
}

// CORE-FR-05
func TestSignalArrayRules(t *testing.T) {
	st := core.StepContextMaintain
	t.Run("effect after transition", func(t *testing.T) {
		signalRuleFails(t, st, []core.Signal{sig(core.SigNext), validSig(core.SigRewrite)})
	})
	t.Run("two transitions", func(t *testing.T) {
		signalRuleFails(t, st, []core.Signal{sig(core.SigNext), sig(core.SigStop)})
	})
	t.Run("not allowed transition", func(t *testing.T) {
		signalRuleFails(t, st, []core.Signal{sig(core.SigRecall)})
	})
	t.Run("not allowed effect", func(t *testing.T) {
		signalRuleFails(t, st, []core.Signal{validSig(core.SigInjectInput)})
	})
	t.Run("unknown kind", func(t *testing.T) {
		signalRuleFails(t, st, []core.Signal{sig("jump")})
	})
}

// CORE-FR-06, R2
func TestNoNextAtCallErrorAndTurnFinish(t *testing.T) {
	for _, sigs := range [][]core.Signal{nil, {sig(core.SigNext)}, {validSig(core.SigRewrite)}} {
		signalRuleFails(t, core.StepTurnFinish, sigs)
	}
	errStream := func(call) (core.Result, error) { return out(rawStream(item{ev: errEvent(contract.ClassAuth)}).cs) }
	for _, sigs := range [][]core.Signal{nil, {sig(core.SigNext)}} {
		f := newFake().on(core.StepModelCall, errStream).
			on(core.StepCallError, func(call) (core.Result, error) { return out(nil, sigs...) })
		s := run(t, f)
		if !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("call.error %v: err = %v", sigs, s.Err)
		}
	}
}

// CORE-FR-40
func TestSignalPayloadRules(t *testing.T) {
	bad := edit("e1")
	bad.Target = ""
	msg := userMsg("m", "x")
	badCall := tc("", "read", `{}`)
	cases := map[string]core.Signal{
		"rewrite without edit": {Kind: core.SigRewrite},
		"rewrite invalid edit": {Kind: core.SigRewrite, Edit: bad},
		"next with edit":       {Kind: core.SigNext, Edit: edit("e1")},
		"stop with message":    {Kind: core.SigStop, Message: &msg},
		"rewrite with message": {Kind: core.SigRewrite, Edit: edit("e1"), Message: &msg},
		"fail with results":    {Kind: core.SigFail, Results: []contract.ToolResult{}},
		"rewrite with call":    {Kind: core.SigRewrite, Edit: edit("e1"), Call: &badCall},
		"next with error":      {Kind: core.SigNext, Err: errors.New("x")},
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) { signalRuleFails(t, core.StepContextMaintain, []core.Signal{s}) })
	}
	t.Run("inject_input without message", func(t *testing.T) {
		f := newFake().on(core.StepTurnFinish, func(call) (core.Result, error) {
			return out(nil, core.Signal{Kind: core.SigInjectInput}, sig(core.SigSettle))
		})
		if s := run(t, f); !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("err = %v", s.Err)
		}
	})
	t.Run("inject_input invalid message", func(t *testing.T) {
		bad := userMsg("", "x")
		f := newFake().on(core.StepTurnFinish, func(call) (core.Result, error) {
			return out(nil, core.Signal{Kind: core.SigInjectInput, Message: &bad}, sig(core.SigSettle))
		})
		if s := run(t, f); !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("err = %v", s.Err)
		}
	})
	t.Run("start_tool invalid call", func(t *testing.T) {
		f := newFake().on(core.StepStreamEvent, func(call) (core.Result, error) {
			return out(nil, core.Signal{Kind: core.SigStartTool, Call: &badCall})
		})
		if s := run(t, f); !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("err = %v", s.Err)
		}
	})
	t.Run("skip_tools with invalid result", func(t *testing.T) {
		f := newFake().on(core.StepToolsPlan, func(call) (core.Result, error) {
			return out(core.Plan{}, core.Signal{Kind: core.SigSkipTools, Results: []contract.ToolResult{{}}})
		})
		if s := run(t, f); !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("err = %v", s.Err)
		}
	})
}

// CORE-FR-05~07 속성 테스트: 3 context.maintain 에서 무작위 신호 배열.
// 적법 = 허용된 효과 0개 이상 + (허용된 전이 최대 1개, 마지막). 적법하면 효과가 순서대로 편집 로그에 쌓인다.
func TestSignalArrayProperty(t *testing.T) {
	st := core.StepContextMaintain
	kinds := core.SignalKinds()
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 400; iter++ {
		n := rng.Intn(5)
		sigs := make([]core.Signal, n)
		var wantEdits []string
		legal := true
		transitions := 0
		for i := range sigs {
			k := kinds[rng.Intn(len(kinds))]
			s := validSig(k)
			if k == core.SigRewrite {
				s.Edit = edit(string(rune('a' + i)))
			}
			sigs[i] = s
			if !core.Allowed(st, k) {
				legal = false
			}
			if k.IsTransition() {
				transitions++
				if i != n-1 {
					legal = false
				}
			} else if k == core.SigRewrite {
				wantEdits = append(wantEdits, s.Edit.ID)
			}
		}
		if transitions > 1 {
			legal = false
		}

		var gotEdits []string
		f := newFake().
			on(st, func(call) (core.Result, error) { return out(nil, sigs...) }).
			on(core.StepContextInject, func(c call) (core.Result, error) {
				for _, e := range c.View.Edits() {
					gotEdits = append(gotEdits, e.ID)
				}
				return out([]contract.Message(nil))
			})
		s := run(t, f)
		isRule := errors.Is(s.Err, core.ErrSignalRule)
		if legal == isRule {
			t.Fatalf("iter %d %v: legal=%v, settle=%s %v", iter, kindsOf(sigs), legal, s.Reason, s.Err)
		}
		if legal && f.count(core.StepContextInject) > 0 && !equal(gotEdits, wantEdits) {
			t.Fatalf("iter %d: edits %v, want %v", iter, gotEdits, wantEdits)
		}
	}
}

func kindsOf(sigs []core.Signal) []core.SignalKind {
	out := make([]core.SignalKind, len(sigs))
	for i, s := range sigs {
		out[i] = s.Kind
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 3.2 전이 목적지 (3.3 의 next 외).
func TestTransitionTargets(t *testing.T) {
	once := func(k core.SignalKind, o any) handler {
		return func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(o, sig(k))
			}
			return defaultResult(c)
		}
	}
	endOut := func(c call) any { return core.StreamEndOut{Response: c.Input.(contract.Message)} }
	cases := []struct {
		name  string
		step  core.Step
		h     handler
		after core.Step // 신호를 낸 dispatch 바로 다음 step
	}{
		{"recall from stream.event", core.StepStreamEvent, once(core.SigRecall, nil), core.StepModelCall},
		{"reselect from stream.event", core.StepStreamEvent, once(core.SigReselect, nil), core.StepModelSelect},
		{"restart from stream.event", core.StepStreamEvent, once(core.SigRestartTurn, nil), core.StepTurnStart},
		{"recall from stream.end", core.StepStreamEnd, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(endOut(c), sig(core.SigRecall))
			}
			return defaultResult(c)
		}, core.StepModelCall},
		{"rebuild from model.select", core.StepModelSelect, once(core.SigRebuild, modelInfo()), core.StepContextProject},
		{"rebuild from classify", core.StepResponseClassify, once(core.SigRebuild, nil), core.StepContextProject},
		{"reselect from classify", core.StepResponseClassify, once(core.SigReselect, nil), core.StepModelSelect},
		{"restart from classify", core.StepResponseClassify, once(core.SigRestartTurn, nil), core.StepTurnStart},
		{"skip_tools from plan", core.StepToolsPlan, once(core.SigSkipTools, core.Plan{}), core.StepToolsCommit},
		{"continue from turn.finish", core.StepTurnFinish, once(core.SigContinueTurn, nil), core.StepTurnStart},
		{"settle from turn.finish", core.StepTurnFinish, once(core.SigSettle, nil), core.StepRunBeforeSettle},
		{"continue from beforeSettle", core.StepRunBeforeSettle, once(core.SigContinueTurn, nil), core.StepTurnStart},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := c.h
			if c.name == "skip_tools from plan" {
				h = func(cl call) (core.Result, error) {
					return out(core.Plan{}, core.Signal{Kind: core.SigSkipTools, Results: []contract.ToolResult{}})
				}
			}
			f := newFake().on(c.step, h)
			s := run(t, f)
			wantReason(t, s, core.SettleCompleted)
			steps := f.steps()
			for i, st := range steps {
				if st == c.step {
					if steps[i+1] != c.after {
						t.Fatalf("after %s: %s, want %s\n%v", c.step, steps[i+1], c.after, steps)
					}
					return
				}
			}
			t.Fatal("step not dispatched")
		})
	}
}

func TestCallErrorTransitions(t *testing.T) {
	errOnce := func(c call) (core.Result, error) {
		if c.N == 0 {
			return out(rawStream(item{ev: errEvent(contract.ClassRateLimit)}).cs)
		}
		return defaultResult(c)
	}
	for k, after := range map[core.SignalKind]core.Step{
		core.SigRecall: core.StepModelCall, core.SigReselect: core.StepModelSelect,
		core.SigRebuild: core.StepContextProject, core.SigRestartTurn: core.StepTurnStart,
	} {
		t.Run(string(k), func(t *testing.T) {
			var in contract.CallError
			f := newFake().on(core.StepModelCall, errOnce).
				on(core.StepCallError, func(c call) (core.Result, error) {
					in = c.Input.(contract.CallError)
					return out(nil, validSig(core.SigRewrite), sig(k))
				})
			s := run(t, f)
			wantReason(t, s, core.SettleCompleted)
			if in.Class != contract.ClassRateLimit {
				t.Fatalf("call.error input %+v", in)
			}
			steps := f.steps()
			for i, st := range steps {
				if st == core.StepCallError && steps[i+1] != after {
					t.Fatalf("after call.error: %s, want %s", steps[i+1], after)
				}
			}
		})
	}
}
