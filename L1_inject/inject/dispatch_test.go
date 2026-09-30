package inject_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

// 판정 step 하나만 채운 세트로 turn.finish 를 dispatch 한다.
func finishWith(fn inject.TurnFinishFn) (core.Result, error) {
	set := inject.FunctionSet{Name: "t", TurnFinish: fn}
	return injecttest.Dispatch(context.Background(), set, core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
}

func TestSignalsConverted(t *testing.T) {
	edit := contract.ContextEdit{ID: "e1", Kind: contract.EditHide, Timestamp: ts, Target: "m1"}
	res, err := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Rewrite(edit), ctx.ContinueTurn().WithReason("more")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Signals) != 2 || res.Signals[0].Kind != core.SigRewrite || res.Signals[0].Edit.ID != "e1" ||
		res.Signals[1].Kind != core.SigContinueTurn || res.Signals[1].Reason != "more" {
		t.Fatalf("signals %+v", res.Signals)
	}
	if res.Origin != "t" {
		t.Fatalf("origin %q", res.Origin)
	}
}

func TestWithReasonDoesNotMutate(t *testing.T) {
	res, _ := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		s := ctx.Settle()
		_ = s.WithReason("x")
		if s.Kind() != core.SigSettle {
			t.Error("Kind()")
		}
		return []inject.TurnFinishSignal{s}, nil
	})
	if res.Signals[0].Reason != "" {
		t.Fatal("WithReason mutated original")
	}
}

// IN-FR-09, 12
func TestForeignSignalRejected(t *testing.T) {
	var saved inject.TurnFinishSignal
	set := inject.FunctionSet{Name: "t", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		if saved == nil {
			saved = ctx.Settle()
			return []inject.TurnFinishSignal{saved}, nil
		}
		return []inject.TurnFinishSignal{saved}, nil
	}}
	d, err := inject.Bind(injecttest.Fill(set))
	if err != nil {
		t.Fatal(err)
	}
	v := facts(core.StepTurnFinish).View()
	if _, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, v); err != nil {
		t.Fatal(err)
	}
	_, err = d.DispatchSource(context.Background(), core.StepTurnFinish, nil, v)
	if !errors.Is(err, inject.ErrForeignSignal) {
		t.Fatalf("err = %v", err)
	}
}

func TestNilSignalRejected(t *testing.T) {
	_, err := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{nil}, nil
	})
	if !errors.Is(err, inject.ErrForeignSignal) {
		t.Fatalf("err = %v", err)
	}
}

// IN-FR-10, 11
func TestExpiredCtxPanics(t *testing.T) {
	var saved *inject.TurnFinishCtx
	finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		saved = ctx
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	for name, use := range map[string]func(){
		"signal":  func() { saved.Settle() },
		"query":   func() { saved.Flow() },
		"context": func() { saved.Context() },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != inject.ErrExpiredCtx {
					t.Fatalf("recover = %v", p)
				}
			}()
			use()
		})
	}
	// 보관한 조회 그룹도 폐기된다.
	var flow inject.Flow
	finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		flow = ctx.Flow()
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	defer func() {
		if p := recover(); p != inject.ErrExpiredCtx {
			t.Fatalf("group recover = %v", p)
		}
	}()
	flow.Seq()
}

// 에러·panic 으로 끝나도 ctx 는 폐기된다 (IN-FR-10).
func TestCtxExpiresOnError(t *testing.T) {
	var saved *inject.TurnFinishCtx
	finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		saved = ctx
		panic("boom")
	})
	defer func() {
		if p := recover(); p != inject.ErrExpiredCtx {
			t.Fatalf("recover = %v", p)
		}
	}()
	saved.Settle()
}

func TestPanicBecomesError(t *testing.T) {
	_, err := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { panic("boom") })
	if !errors.Is(err, inject.ErrPanic) {
		t.Fatalf("err = %v", err)
	}
	// 폐기된 다른 ctx 를 쓰다 난 panic 도 error 로 돌아온다.
	var saved *inject.TurnFinishCtx
	finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		saved = ctx
		return nil, nil
	})
	_, err = finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{saved.Settle()}, nil
	})
	if !errors.Is(err, inject.ErrPanic) {
		t.Fatalf("err = %v", err)
	}
}

func TestFunctionErrorPassesThrough(t *testing.T) {
	boom := errors.New("boom")
	_, err := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

// IN-FR-40 [5]: 배열 규칙
func TestSignalOrderChecked(t *testing.T) {
	edit := contract.ContextEdit{ID: "e1", Kind: contract.EditHide, Timestamp: ts, Target: "m1"}
	for name, fn := range map[string]inject.TurnFinishFn{
		"effect after transition": func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			return []inject.TurnFinishSignal{ctx.Settle(), ctx.Rewrite(edit)}, nil
		},
		"two transitions": func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			return []inject.TurnFinishSignal{ctx.Stop(), ctx.Settle()}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finishWith(fn); !errors.Is(err, inject.ErrSignalOrder) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestInputTypeChecked(t *testing.T) {
	set := inject.FunctionSet{Name: "t"}
	for step, in := range map[core.Step]any{
		core.StepRunStart:        "x",
		core.StepTurnFinish:      1,
		core.StepToolAfter:       contract.ToolCall{},
		core.StepRequestFinalize: nil,
	} {
		_, err := dispatch(t, set, step, in, facts(step))
		if !errors.Is(err, inject.ErrInputType) {
			t.Errorf("%s: err = %v", step, err)
		}
	}
}

// IN-FR-13
func TestCtxContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var got context.Context
	set := inject.FunctionSet{Name: "t", TurnFinish: func(c *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		got = c.Context()
		cancel()
		return []inject.TurnFinishSignal{c.Settle()}, nil
	}}
	injecttest.Dispatch(ctx, set, core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if got.Err() == nil {
		t.Fatal("ctx.Context() not canceled with dispatch context")
	}
}

// 입력 구조체를 풀어서 받는다 (IN-FR-44).
func TestUnpackedInputs(t *testing.T) {
	call := contract.ToolCall{ID: "c1", Name: "read", Args: []byte(`{}`)}
	res := contract.ToolResult{CallID: "c1", Status: contract.StatusOK}
	set := inject.FunctionSet{Name: "t", ToolAfter: func(ctx *inject.ToolAfterCtx, c contract.ToolCall, r contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
		if c.ID != "c1" || r.CallID != "c1" {
			t.Errorf("inputs %+v %+v", c, r)
		}
		r.Status = contract.StatusError
		return r, nil, nil
	}}
	out, err := dispatch(t, set, core.StepToolAfter, core.ToolAfterInput{Call: call, Result: res}, facts(core.StepToolAfter))
	if err != nil {
		t.Fatal(err)
	}
	if out.Output.(contract.ToolResult).Status != contract.StatusError {
		t.Fatalf("output %+v", out.Output)
	}
}

func TestRunSettledHasNoSignals(t *testing.T) {
	var got core.Settle
	set := inject.FunctionSet{Name: "t", RunSettled: func(ctx *inject.RunSettledCtx, s core.Settle) error {
		got = s
		return nil
	}}
	res, err := dispatch(t, set, core.StepRunSettled, core.Settle{Reason: core.SettleStopped}, facts(core.StepRunSettled))
	if err != nil || res.Signals != nil || got.Reason != core.SettleStopped {
		t.Fatalf("res %+v err %v got %+v", res, err, got)
	}
}

// IN-FR-41
func TestParallelDispatch(t *testing.T) {
	d := mustBind(t, workingSet("w"))
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f := facts(core.StepToolExecute)
			f.RunID = "r"
			_, err := d.DispatchSource(context.Background(), core.StepToolExecute, contract.ToolCall{ID: "c", Name: "t", Args: []byte(`{}`)}, f.View())
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

func TestEndToEndWithCore(t *testing.T) {
	set := workingSet("w")
	call := contract.ToolCall{ID: "c1", Name: "read", Args: []byte(`{}`)}
	set.ModelCall = func(ctx *inject.ModelCallCtx, req contract.Request) (core.CallStream, []inject.ModelCallSignal, error) {
		if ctx.Flow().TurnIndex() == 1 {
			return streamOf(reply("a1", call)), nil, nil
		}
		return streamOf(reply("a2")), nil, nil
	}
	s := runCore(t, mustBind(t, set))
	if s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
}
