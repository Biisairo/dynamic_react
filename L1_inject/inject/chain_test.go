package inject_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/combine"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

func bindChain(t *testing.T, opts inject.BindOptions, sets ...inject.FunctionSet) *inject.Dispatcher {
	t.Helper()
	sets[0] = injecttest.Fill(sets[0])
	d, err := inject.BindWith(opts, sets...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func tfSet(name string, fn inject.TurnFinishFn) inject.FunctionSet {
	return inject.FunctionSet{Name: name, TurnFinish: fn}
}

func kinds(sigs []core.Signal) string {
	var s []string
	for _, x := range sigs {
		s = append(s, string(x.Kind)+"/"+x.Reason)
	}
	return strings.Join(s, ",")
}

// IN-FR-19, 60: 위 층은 Next 로 아래 층의 결과·신호를 받아 고치거나 그대로 돌려준다. 아래 층 신호는 같은 invocation 이라 토큰 검사를 통과한다.
func TestChainNext(t *testing.T) {
	var order []string
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		order = append(order, "low")
		return []inject.TurnFinishSignal{ctx.Settle().WithReason("low")}, nil
	})
	mid := tfSet("mid", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		order = append(order, "mid")
		return ctx.Next()
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		order = append(order, "top")
		s, err := ctx.Next()
		if err != nil {
			return nil, err
		}
		return append([]inject.TurnFinishSignal{ctx.InjectInput(userMsg("n", "x"))}, s...), nil
	})
	d := bindChain(t, inject.BindOptions{}, low, mid, top)
	res, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(res.Signals); got != "inject_input/,settle/low" {
		t.Fatalf("signals %s", got)
	}
	if strings.Join(order, ">") != "top>mid>low" {
		t.Fatalf("order %v", order)
	}
	if res.Origin != "top" {
		t.Fatalf("origin %q", res.Origin)
	}
}

// IN-FR-60: 입출력이 있는 step 은 Next 가 인자를 받고 출력을 돌려준다. 위 층이 출력을 고친다.
func TestChainNextWithInputs(t *testing.T) {
	low := inject.FunctionSet{Name: "low",
		ToolAfter: func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			res.Content = append(res.Content, contract.Block{Kind: contract.BlockText, Text: call.Name})
			return res, []inject.ToolAfterSignal{ctx.NextSignal().WithReason("low")}, nil
		},
	}
	top := inject.FunctionSet{Name: "top",
		ToolAfter: func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			call.Name = "renamed"
			out, s, err := ctx.Next(call, res)
			out.Status = contract.StatusError
			ctx.Dropped(s...)
			return out, nil, err
		},
	}
	d := bindChain(t, inject.BindOptions{}, low, top)
	in := core.ToolAfterInput{Call: contract.ToolCall{ID: "c1", Name: "read"}, Result: contract.ToolResult{CallID: "c1", Status: contract.StatusOK}}
	res, err := d.DispatchSource(context.Background(), core.StepToolAfter, in, facts(core.StepToolAfter).View())
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(contract.ToolResult)
	if out.Status != contract.StatusError || len(out.Content) != 1 || out.Content[0].Text != "renamed" || len(res.Signals) != 0 {
		t.Fatalf("out %+v signals %v", out, res.Signals)
	}
}

// IN-FR-19: Next 를 부르지 않는 위 층은 아래 층을 덮는다 (이전 동작).
func TestChainNoNextOverrides(t *testing.T) {
	called := false
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		called = true
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Stop()}, nil
	})
	d := bindChain(t, inject.BindOptions{}, low, top)
	res, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if err != nil || called || kinds(res.Signals) != "stop/" {
		t.Fatalf("err %v called %v signals %s", err, called, kinds(res.Signals))
	}
}

// IN-FR-62: 가장 아래 층의 Next 는 ErrNoNext 를 감싼 에러다. 돌려주면 dispatch 실패.
func TestChainNoLower(t *testing.T) {
	var nextErr error
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		_, nextErr = ctx.Next()
		return nil, nextErr
	})
	d := bindChain(t, inject.BindOptions{}, low)
	_, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if !errors.Is(nextErr, inject.ErrNoNext) || !errors.Is(err, inject.ErrNoNext) {
		t.Fatalf("next err %v, dispatch err %v", nextErr, err)
	}
	if inject.ErrNoNext.Error() != "inject: no lower function" {
		t.Fatalf("message %q", inject.ErrNoNext)
	}
	// 출력이 있는 step 도 0 값과 에러
	tb := inject.FunctionSet{Name: "low",
		ToolBefore: func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			out, s, err := ctx.Next(call)
			if out.Call.ID != "" || s != nil || !errors.Is(err, inject.ErrNoNext) {
				t.Errorf("out %+v s %v err %v", out, s, err)
			}
			return core.BeforeOut{Call: call}, nil, nil
		},
	}
	d = bindChain(t, inject.BindOptions{}, tb)
	if _, err := d.DispatchSource(context.Background(), core.StepToolBefore, contract.ToolCall{ID: "c"}, facts(core.StepToolBefore).View()); err != nil {
		t.Fatal(err)
	}
}

// IN-FR-62, 61: Next 는 부를 때마다 아래 층을 다시 부른다. 같은 dispatch 안의 두 번째 호출은 첫 호출의 쓰기를 본다.
func TestChainNextTwice(t *testing.T) {
	n := 0
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		n++
		v, _ := ctx.State().Get("n")
		if want := map[int]string{1: "", 2: "1"}[n]; string(v) != want {
			t.Errorf("call %d sees %q", n, v)
		}
		ctx.State().Set("n", n)
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		if _, err := ctx.Next(); err != nil {
			return nil, err
		}
		return ctx.Next()
	})
	d := bindChain(t, inject.BindOptions{}, low, top)
	if _, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View()); err != nil || n != 2 {
		t.Fatalf("err %v calls %d", err, n)
	}
}

// IN-FR-62, 11: 아래 층의 panic 은 dispatch 의 error 다.
func TestChainLowerPanic(t *testing.T) {
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { panic("boom") })
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { return ctx.Next() })
	d := bindChain(t, inject.BindOptions{}, low, top)
	_, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if !errors.Is(err, inject.ErrPanic) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
}

// IN-FR-11: dispatch 가 끝난 ctx 의 Next 는 panic 이다.
func TestChainNextAfterExpiry(t *testing.T) {
	var kept *inject.TurnFinishCtx
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		kept = ctx
		return ctx.Next()
	})
	d := bindChain(t, inject.BindOptions{}, low, top)
	if _, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if p := recover(); p != inject.ErrExpiredCtx {
			t.Fatalf("recovered %v", p)
		}
	}()
	kept.Next()
}

// IN-FR-61: 층마다 자기 세트의 네임스페이스에 쓰고, 성공하면 모두 반영·저장, 실패하면 모두 버린다.
func TestChainStateNamespaces(t *testing.T) {
	fail := false
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		ctx.State().Set("k", "low")
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		ctx.State().Set("k", "top")
		s, err := ctx.Next()
		if v, _ := ctx.State().Get("k"); string(v) != `"top"` {
			t.Errorf("top sees %s", v)
		}
		if fail {
			return nil, errors.New("x")
		}
		return s, err
	})
	var got []string
	reader := inject.FunctionSet{Name: "reader",
		RunBeforeSettle: func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
			lv, _ := ctx.State().Read("low", "k")
			tv, _ := ctx.State().Read("top", "k")
			got = append(got, string(lv)+string(tv))
			return nil, nil
		},
	}
	low.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		ctx.State().Publish("k")
		return nil, nil, nil
	}
	top.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		ctx.State().Publish("k")
		return ctx.Next()
	}
	for _, f := range []bool{false, true} {
		fail, got = f, nil
		store := &memStore{}
		d := bindChain(t, inject.BindOptions{StateStore: store}, low, top, reader)
		v := facts(core.StepRunStart)
		v.RunID = "chain"
		if _, err := d.DispatchSource(context.Background(), core.StepTurnStart, nil, v.View()); err != nil {
			t.Fatal(err)
		}
		store.saves = nil
		_, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, v.View())
		if (err != nil) != f {
			t.Fatalf("fail=%v err %v", f, err)
		}
		if _, err := d.DispatchSource(context.Background(), core.StepRunBeforeSettle, nil, v.View()); err != nil {
			t.Fatal(err)
		}
		want, saves := `"low""top"`, `[low:k="low" del[] pub[] top:k="top" del[] pub[]]`
		if f {
			want, saves = "", "[]"
		}
		if got[0] != want || fmtSaves(store.saves) != saves {
			t.Errorf("fail=%v read %q saves %v", f, got[0], store.saves)
		}
	}
}

func fmtSaves(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	sorted := append([]string(nil), s...)
	if len(sorted) == 2 && sorted[0] > sorted[1] {
		sorted[0], sorted[1] = sorted[1], sorted[0]
	}
	return "[" + strings.Join(sorted, " ") + "]"
}

// IN-FR-21: Binding·트레이스의 Set 은 가장 위 층이다.
func TestChainBindingAndTrace(t *testing.T) {
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	})
	top := tfSet("top", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { return ctx.Next() })
	tr := &tracer{}
	d := bindChain(t, inject.BindOptions{Tracer: tr}, low, top)
	if d.Binding()[core.StepTurnFinish] != "top" || d.Binding()[core.StepRunStart] != "low" {
		t.Fatalf("binding %v", d.Binding())
	}
	if _, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View()); err != nil {
		t.Fatal(err)
	}
	if len(tr.recs) != 1 || tr.recs[0].Set != "top" {
		t.Fatalf("trace %+v", tr.recs)
	}
}

// IN-FR-63: 조합 안의 함수가 Next 를 부르면 그 세트의 아래 층이 불린다.
func TestChainInsideCombine(t *testing.T) {
	low := tfSet("low", func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Settle().WithReason("low")}, nil
	})
	pass := func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) { return nil, nil }
	top := tfSet("top", combine.TurnFinish.FirstDecisive(pass, func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return ctx.Next()
	}))
	d := bindChain(t, inject.BindOptions{}, low, top)
	res, err := d.DispatchSource(context.Background(), core.StepTurnFinish, nil, facts(core.StepTurnFinish).View())
	if err != nil || kinds(res.Signals) != "settle/low" {
		t.Fatalf("err %v signals %s", err, kinds(res.Signals))
	}
}

// IN-FR-18: Description 은 바인딩에 쓰이지 않는다.
func TestDescriptionIgnored(t *testing.T) {
	set := workingSet("base")
	set.Description = "기본"
	if s := runCore(t, mustBind(t, set)); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %s", s.Reason)
	}
}
