package combine_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/combine"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

type tracer struct{ recs []inject.TraceRecord }

func (t *tracer) Trace(r inject.TraceRecord) { t.recs = append(t.recs, r) }

// run 은 set 의 step 하나를 dispatch 하고 결과와 트레이스를 돌려준다.
func run(t *testing.T, set inject.FunctionSet, step core.Step, input any) (core.Result, error, inject.TraceRecord) {
	t.Helper()
	tr := &tracer{}
	d, err := inject.BindWith(inject.BindOptions{Tracer: tr}, injecttest.Fill(set))
	if err != nil {
		t.Fatal(err)
	}
	f := injecttest.Facts{RunID: "r", Step: step, Seq: 1}
	res, err := d.DispatchSource(context.Background(), step, input, f.View())
	return res, err, tr.recs[0]
}

func kinds(sigs []core.Signal) string {
	var out []string
	for _, s := range sigs {
		out = append(out, string(s.Kind)+":"+s.Reason)
	}
	return fmt.Sprint(out)
}

// fin 은 호출 순서를 log 에 남기고 build 가 만든 신호를 내는 turn.finish 함수다.
func fin(log *[]string, name string, build func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal) inject.TurnFinishFn {
	return func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		*log = append(*log, name)
		return build(ctx), nil
	}
}

func none(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal { return nil }

func edit() contract.ContextEdit {
	return contract.ContextEdit{ID: "e", Kind: contract.EditHide, Timestamp: ts, Target: "m"}
}

// IN-FR-55
func TestFirstDecisive(t *testing.T) {
	var log []string
	fn := combine.TurnFinish.FirstDecisive(
		fin(&log, "a", none),
		fin(&log, "b", func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
			return []inject.TurnFinishSignal{ctx.Rewrite(edit()), ctx.ContinueTurn().WithReason("b")}
		}),
		fin(&log, "c", func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
			return []inject.TurnFinishSignal{ctx.Settle()}
		}),
	)
	res, err, _ := run(t, inject.FunctionSet{Name: "x", TurnFinish: fn}, core.StepTurnFinish, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(log) != "[a b]" || kinds(res.Signals) != "[rewrite: continue_turn:b]" {
		t.Fatalf("log %v signals %s", log, kinds(res.Signals))
	}
}

// 판정 step 에서 [next] 는 결정적이 아니고 dropped 로 남는다. 10 은 입력이 있는 판정 step.
func TestFirstDecisiveSkipsNextAndDropsIt(t *testing.T) {
	next := func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
		return []inject.StreamEventSignal{ctx.NextSignal().WithReason("n")}, nil
	}
	recall := func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
		if ev.Kind != contract.EventStart {
			t.Error("input not passed")
		}
		return []inject.StreamEventSignal{ctx.Recall()}, nil
	}
	fn := combine.StreamEvent.FirstDecisive(next, recall)
	res, _, tr := run(t, inject.FunctionSet{Name: "x", StreamEvent: fn}, core.StepStreamEvent, contract.StreamEvent{Kind: contract.EventStart})
	if kinds(res.Signals) != "[recall:]" || kinds(tr.Dropped) != "[next:n]" {
		t.Fatalf("signals %s dropped %s", kinds(res.Signals), kinds(tr.Dropped))
	}
}

func TestFirstDecisiveAllNext(t *testing.T) {
	var log []string
	fn := combine.TurnFinish.FirstDecisive(fin(&log, "a", none), fin(&log, "b", none))
	res, err, _ := run(t, inject.FunctionSet{Name: "x", TurnFinish: fn}, core.StepTurnFinish, nil)
	if err != nil || res.Signals != nil || fmt.Sprint(log) != "[a b]" {
		t.Fatalf("res %+v err %v log %v", res, err, log)
	}
}

func TestErrorStops(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	failing := func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		log = append(log, "err")
		return nil, boom
	}
	for name, fn := range map[string]inject.TurnFinishFn{
		"first":  combine.TurnFinish.FirstDecisive(failing, fin(&log, "after", none)),
		"bykind": combine.TurnFinish.ByKind([]core.SignalKind{core.SigStop}, failing, fin(&log, "after", none)),
	} {
		log = nil
		_, err, _ := run(t, inject.FunctionSet{Name: "x", TurnFinish: fn}, core.StepTurnFinish, nil)
		if !errors.Is(err, boom) || fmt.Sprint(log) != "[err]" {
			t.Fatalf("%s: err %v log %v", name, err, log)
		}
	}
}

// IN-FR-56
func TestByKind(t *testing.T) {
	var log []string
	settle := fin(&log, "settle", func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
		return []inject.TurnFinishSignal{ctx.Settle()}
	})
	cont := fin(&log, "cont", func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
		return []inject.TurnFinishSignal{ctx.Rewrite(edit()), ctx.ContinueTurn()}
	})
	stop := fin(&log, "stop", func(ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
		return []inject.TurnFinishSignal{ctx.Stop()}
	})
	cases := []struct {
		name  string
		order []core.SignalKind
		fns   []inject.TurnFinishFn
		want  string
	}{
		{"order wins over position", []core.SignalKind{core.SigStop, core.SigContinueTurn, core.SigSettle}, []inject.TurnFinishFn{settle, cont, stop}, "[stop:]"},
		{"second in order", []core.SignalKind{core.SigContinueTurn, core.SigSettle}, []inject.TurnFinishFn{settle, cont}, "[rewrite: continue_turn:]"},
		{"unlisted ranks last", []core.SignalKind{core.SigSettle}, []inject.TurnFinishFn{stop, settle}, "[settle:]"},
		{"tie goes to earlier", []core.SignalKind{core.SigStop}, []inject.TurnFinishFn{settle, cont}, "[settle:]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log = nil
			fn := combine.TurnFinish.ByKind(c.order, c.fns...)
			res, err, tr := run(t, inject.FunctionSet{Name: "x", TurnFinish: fn}, core.StepTurnFinish, nil)
			if err != nil {
				t.Fatal(err)
			}
			if kinds(res.Signals) != c.want {
				t.Fatalf("signals %s, want %s", kinds(res.Signals), c.want)
			}
			if len(log) != len(c.fns) {
				t.Fatalf("called %v", log)
			}
			if len(tr.Dropped) == 0 {
				t.Fatal("losers not dropped")
			}
		})
	}
}

// next 는 order 에 넣어 순위를 줄 수 있다 (전이가 없는 배열 = next).
func TestByKindNext(t *testing.T) {
	var log []string
	fn := combine.ContextMaintain.ByKind([]core.SignalKind{core.SigNext, core.SigStop},
		func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
			return []inject.ContextMaintainSignal{ctx.Stop()}, nil
		},
		func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
			log = append(log, "rw")
			return []inject.ContextMaintainSignal{ctx.Rewrite(edit())}, nil
		})
	res, _, _ := run(t, inject.FunctionSet{Name: "x", ContextMaintain: fn}, core.StepContextMaintain, nil)
	if kinds(res.Signals) != "[rewrite:]" {
		t.Fatalf("signals %s", kinds(res.Signals))
	}
}

// IN-FR-57
func TestPipeRequest(t *testing.T) {
	add := func(n int) inject.RequestFinalizeFn {
		return func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
			req.Options.MaxOutputTokens = req.Options.MaxOutputTokens*10 + n
			return req, nil, nil
		}
	}
	res, err, _ := run(t, inject.FunctionSet{Name: "x", RequestFinalize: combine.RequestFinalize.Pipe(add(1), add(2), add(3))},
		core.StepRequestFinalize, contract.Request{})
	if err != nil || res.Output.(contract.Request).Options.MaxOutputTokens != 123 {
		t.Fatalf("output %+v err %v", res.Output, err)
	}
}

func TestPipeStopsAtDecisive(t *testing.T) {
	var log []string
	step := func(name string, fail bool) inject.ToolAfterFn {
		return func(ctx *inject.ToolAfterCtx, call contract.ToolCall, r contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			log = append(log, name)
			if call.ID != "c1" {
				t.Error("call not passed")
			}
			r.Content = append(r.Content, contract.Block{Kind: contract.BlockText, Text: name})
			if fail {
				return r, []inject.ToolAfterSignal{ctx.Fail(errors.New(name))}, nil
			}
			return r, []inject.ToolAfterSignal{ctx.NextSignal()}, nil
		}
	}
	fn := combine.ToolAfter.Pipe(step("a", false), step("b", true), step("c", false))
	in := core.ToolAfterInput{Call: contract.ToolCall{ID: "c1"}, Result: contract.ToolResult{CallID: "c1", Status: contract.StatusOK}}
	res, _, _ := run(t, inject.FunctionSet{Name: "x", ToolAfter: fn}, core.StepToolAfter, in)
	out := res.Output.(contract.ToolResult)
	if fmt.Sprint(log) != "[a b]" || len(out.Content) != 2 || out.Content[1].Text != "b" || kinds(res.Signals) != "[fail:]" {
		t.Fatalf("log %v out %+v signals %s", log, out, kinds(res.Signals))
	}
}

func msg(id string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Timestamp: ts}
}

// IN-FR-58
func TestConcat(t *testing.T) {
	mk := func(id string, settle bool) inject.TurnStartFn {
		return func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
			if settle {
				return []contract.Message{msg(id)}, []inject.TurnStartSignal{ctx.Settle().WithReason(id)}, nil
			}
			return []contract.Message{msg(id)}, nil, nil
		}
	}
	fn := combine.TurnStart.Concat(mk("a", false), mk("b", true), mk("c", true))
	res, err, tr := run(t, inject.FunctionSet{Name: "x", TurnStart: fn}, core.StepTurnStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := res.Output.([]contract.Message)
	if len(ms) != 3 || ms[0].ID != "a" || ms[2].ID != "c" {
		t.Fatalf("output %v", ms)
	}
	if kinds(res.Signals) != "[settle:b]" || kinds(tr.Dropped) != "[settle:c]" {
		t.Fatalf("signals %s dropped %s", kinds(res.Signals), kinds(tr.Dropped))
	}
}

// IN-FR-59
func TestWrap(t *testing.T) {
	var log []string
	inner := func(ctx *inject.ToolExecuteCtx, call contract.ToolCall) (contract.ToolResult, []inject.ToolExecuteSignal, error) {
		log = append(log, "inner")
		return contract.ToolResult{CallID: call.ID, Status: contract.StatusOK}, nil, nil
	}
	fn := combine.ToolExecute.Wrap(inner,
		func(ctx *inject.ToolExecuteCtx, call contract.ToolCall) { log = append(log, "before:"+call.ID) },
		func(ctx *inject.ToolExecuteCtx, call contract.ToolCall, r contract.ToolResult, s []inject.ToolExecuteSignal, err error) (contract.ToolResult, []inject.ToolExecuteSignal, error) {
			log = append(log, "after")
			r.Status = contract.StatusError
			return r, s, err
		})
	res, err, _ := run(t, inject.FunctionSet{Name: "x", ToolExecute: fn}, core.StepToolExecute, contract.ToolCall{ID: "c1"})
	if err != nil || res.Output.(contract.ToolResult).Status != contract.StatusError || fmt.Sprint(log) != "[before:c1 inner after]" {
		t.Fatalf("res %+v err %v log %v", res, err, log)
	}
	// nil 훅, 신호 없는 step(22)
	var got core.SettleReason
	settled := combine.RunSettled.Wrap(func(ctx *inject.RunSettledCtx, s core.Settle) error {
		got = s.Reason
		return nil
	}, nil, nil)
	if _, err, _ := run(t, inject.FunctionSet{Name: "x", RunSettled: settled}, core.StepRunSettled, core.Settle{Reason: core.SettleGuard}); err != nil || got != core.SettleGuard {
		t.Fatalf("settled %v %s", err, got)
	}
}

// 하위 함수들은 같은 ctx 를 받는다: 같은 dispatch 의 상태 쓰기가 다음 함수에 보인다.
func TestSharedCtx(t *testing.T) {
	var seen string
	fn := combine.TurnFinish.FirstDecisive(
		func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			ctx.State().Set("k", "v")
			return nil, nil
		},
		func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			v, _ := ctx.State().Get("k")
			seen = string(v)
			return []inject.TurnFinishSignal{ctx.Settle()}, nil
		})
	run(t, inject.FunctionSet{Name: "x", TurnFinish: fn}, core.StepTurnFinish, nil)
	if seen != `"v"` {
		t.Fatalf("seen %q", seen)
	}
}
