package core_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

func withTools(f *fake, calls ...contract.ToolCall) *fake {
	return f.on(core.StepModelCall, func(c call) (core.Result, error) {
		if c.View.TurnIndex() == 1 {
			return out(streamOf(toolResponse("a1", calls...)).cs)
		}
		return defaultResult(c)
	}).on(core.StepTurnFinish, func(c call) (core.Result, error) {
		if c.N == 0 {
			return out(nil, sig(core.SigContinueTurn))
		}
		return out(nil, sig(core.SigSettle))
	})
}

func toolCalls(n int) []contract.ToolCall {
	out := make([]contract.ToolCall, n)
	for i := range out {
		out[i] = tc(string(rune('a'+i)), "t", `{}`)
	}
	return out
}

// CORE-FR-20, 24: 그룹 2개(병렬 3, 순차 1), 결과는 Plan 순서.
func TestToolGroups(t *testing.T) {
	calls := toolCalls(4)
	var active, peak atomic.Int32
	var mu sync.Mutex
	var order []string
	barrier := make(chan struct{})
	var arrived atomic.Int32
	f := withTools(newFake(), calls...).
		on(core.StepToolsPlan, func(c call) (core.Result, error) {
			if len(c.Input.([]contract.ToolCall)) == 0 {
				return out(core.Plan{})
			}
			return out(core.Plan{Groups: []core.Group{{Calls: calls[:3], MaxParallel: 3}, {Calls: calls[3:], MaxParallel: 0}}})
		}).
		on(core.StepToolExecute, func(c call) (core.Result, error) {
			id := c.Input.(contract.ToolCall).ID
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			if id != "d" && arrived.Add(1) == 3 {
				close(barrier)
			}
			if id != "d" {
				<-barrier
			}
			// 완료 순서를 계획 순서와 반대로
			time.Sleep(time.Duration('d'-id[0]) * 5 * time.Millisecond)
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			active.Add(-1)
			return out(okResult(id, id))
		})
	wantReason(t, run(t, f), core.SettleCompleted)
	if peak.Load() != 3 {
		t.Fatalf("peak parallel = %d, want 3", peak.Load())
	}
	if order[3] != "d" {
		t.Fatalf("second group ran before first finished: %v", order)
	}
	in := f.callsOf(core.StepToolsCommit)[0].Input.(core.ToolsCommitInput)
	for i, r := range in.Results {
		if r.CallID != calls[i].ID || in.Calls[i].ID != calls[i].ID {
			t.Fatalf("commit order %d: %s", i, r.CallID)
		}
	}
	// 각 호출은 16 → 17 → 18 (CORE-FR-21)
	if f.count(core.StepToolBefore) != 4 || f.count(core.StepToolAfter) != 4 {
		t.Fatal("tool steps count")
	}
}

func TestToolSequentialDefault(t *testing.T) {
	calls := toolCalls(2)
	f := withTools(newFake(), calls...)
	run(t, f)
	wantSteps(t, f.steps()[14:21], core.StepToolsPlan,
		core.StepToolBefore, core.StepToolExecute, core.StepToolAfter,
		core.StepToolBefore, core.StepToolExecute, core.StepToolAfter)
}

// CORE-FR-21: 16 의 수정된 Call 이 17·18 로, ShortCircuit 은 17 을 건너뜀.
func TestToolBeforeRewriteAndShortCircuit(t *testing.T) {
	calls := []contract.ToolCall{tc("a", "t", `{"x":1}`), tc("b", "t", `{}`)}
	blocked := contract.ToolResult{CallID: "b", Status: contract.StatusError, Content: []contract.Block{{Kind: contract.BlockText, Text: "denied"}}}
	f := withTools(newFake(), calls...).on(core.StepToolBefore, func(c call) (core.Result, error) {
		tcall := c.Input.(contract.ToolCall)
		if tcall.ID == "b" {
			return out(core.BeforeOut{Call: tcall, ShortCircuit: &blocked})
		}
		tcall.Args = []byte(`{"x":2}`)
		return out(core.BeforeOut{Call: tcall})
	})
	run(t, f)
	ex := f.callsOf(core.StepToolExecute)
	if len(ex) != 1 || string(ex[0].Input.(contract.ToolCall).Args) != `{"x":2}` {
		t.Fatalf("tool.execute calls %+v", ex)
	}
	after := f.callsOf(core.StepToolAfter)
	if string(after[0].Input.(core.ToolAfterInput).Call.Args) != `{"x":2}` {
		t.Fatal("tool.after did not get modified call")
	}
	if r := after[1].Input.(core.ToolAfterInput).Result; r.Status != contract.StatusError {
		t.Fatalf("short circuit result %+v", r)
	}
	commit := f.callsOf(core.StepToolsCommit)[0].Input.(core.ToolsCommitInput)
	if string(commit.Calls[0].Args) != `{"x":2}` {
		t.Fatal("tools.commit did not get modified call")
	}
}

// CORE-FR-22
func TestPlanValidation(t *testing.T) {
	calls := toolCalls(2)
	for name, plan := range map[string]core.Plan{
		"unknown call":   {Groups: []core.Group{{Calls: []contract.ToolCall{tc("zz", "t", `{}`)}}}},
		"duplicate call": {Groups: []core.Group{{Calls: calls[:1]}, {Calls: calls[:1]}}},
	} {
		t.Run(name, func(t *testing.T) {
			f := withTools(newFake(), calls...).on(core.StepToolsPlan, func(call) (core.Result, error) { return out(plan) })
			s := run(t, f)
			if !errors.Is(s.Err, core.ErrPlan) {
				t.Fatalf("err = %v", s.Err)
			}
		})
	}
	// 응답의 호출이 Plan 에서 빠진 것은 막지 않는다.
	f := withTools(newFake(), calls...).on(core.StepToolsPlan, func(c call) (core.Result, error) {
		if len(c.Input.([]contract.ToolCall)) == 0 {
			return out(core.Plan{})
		}
		return out(core.Plan{Groups: []core.Group{{Calls: calls[1:]}}})
	})
	wantReason(t, run(t, f), core.SettleCompleted)
	if f.count(core.StepToolExecute) != 1 {
		t.Fatal("partial plan not executed")
	}
	// 빈 Plan 은 바로 19.
	f = withTools(newFake(), calls...).on(core.StepToolsPlan, func(call) (core.Result, error) { return out(core.Plan{}) })
	run(t, f)
	if f.count(core.StepToolBefore) != 0 || len(f.callsOf(core.StepToolsCommit)[0].Input.(core.ToolsCommitInput).Results) != 0 {
		t.Fatal("empty plan executed tools")
	}
}

// CORE-FR-23
func TestToolFailCancelsOthers(t *testing.T) {
	calls := toolCalls(3)
	var canceled atomic.Int32
	started := make(chan struct{}, 3)
	f := withTools(newFake(), calls...).
		on(core.StepToolsPlan, func(call) (core.Result, error) {
			return out(core.Plan{Groups: []core.Group{{Calls: calls, MaxParallel: 3}}})
		}).
		on(core.StepToolExecute, func(c call) (core.Result, error) {
			id := c.Input.(contract.ToolCall).ID
			started <- struct{}{}
			if id == "b" {
				for range 3 {
					<-started
				}
				return out(contract.ToolResult{}, sig(core.SigFail))
			}
			select {
			case <-c.Ctx.Done():
				canceled.Add(1)
			case <-time.After(5 * time.Second):
			}
			return out(okResult(id, id))
		})
	s := run(t, f)
	wantReason(t, s, core.SettleFailed)
	if canceled.Load() != 2 {
		t.Fatalf("canceled = %d, want 2", canceled.Load())
	}
	if f.count(core.StepToolsCommit) != 0 {
		t.Fatal("tools.commit after tool fail")
	}
}

func TestToolStepSignalsRestricted(t *testing.T) {
	for _, st := range []core.Step{core.StepToolBefore, core.StepToolExecute, core.StepToolAfter} {
		f := withTools(newFake(), toolCalls(1)...).on(st, func(c call) (core.Result, error) {
			r, _ := defaultResult(c)
			r.Signals = []core.Signal{sig(core.SigStop)}
			return r, nil
		})
		if s := run(t, f); !errors.Is(s.Err, core.ErrSignalRule) {
			t.Fatalf("%s stop: err = %v", st, s.Err)
		}
	}
}

// CORE-FR-24: skip_tools
func TestSkipTools(t *testing.T) {
	calls := toolCalls(2)
	results := []contract.ToolResult{okResult("a", "cached"), okResult("b", "cached")}
	f := withTools(newFake(), calls...).on(core.StepToolsPlan, func(call) (core.Result, error) {
		return out(core.Plan{}, core.Signal{Kind: core.SigSkipTools, Results: results})
	})
	run(t, f)
	if f.count(core.StepToolBefore)+f.count(core.StepToolExecute)+f.count(core.StepToolAfter) != 0 {
		t.Fatal("tool steps ran with skip_tools")
	}
	in := f.callsOf(core.StepToolsCommit)[0].Input.(core.ToolsCommitInput)
	if len(in.Calls) != 2 || in.Calls[1].ID != "b" || len(in.Results) != 2 || in.Results[0].Content[0].Text != "cached" {
		t.Fatalf("commit input %+v", in)
	}
}

// CORE-FR-24a
func TestToolsCommitOutputValidation(t *testing.T) {
	calls := toolCalls(2)
	cases := map[string]func([]contract.ToolResult) []contract.Message{
		"too few": func(rs []contract.ToolResult) []contract.Message { return resultMessages(rs[:1]) },
		"wrong order": func(rs []contract.ToolResult) []contract.Message {
			return resultMessages([]contract.ToolResult{rs[1], rs[0]})
		},
		"wrong role": func(rs []contract.ToolResult) []contract.Message {
			m := resultMessages(rs)
			m[0] = userMsg("x", "y")
			return m
		},
		"invalid message": func(rs []contract.ToolResult) []contract.Message {
			m := resultMessages(rs)
			m[1].ID = ""
			return m
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			f := withTools(newFake(), calls...).on(core.StepToolsCommit, func(c call) (core.Result, error) {
				return out(mk(c.Input.(core.ToolsCommitInput).Results))
			})
			if s := run(t, f); !errors.Is(s.Err, core.ErrCommit) {
				t.Fatalf("err = %v", s.Err)
			}
		})
	}
}

// CORE-FR-32: 도구 도중 취소.
func TestCancelDuringTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var toolCtx context.Context
	f := withTools(newFake(), toolCalls(1)...).on(core.StepToolExecute, func(c call) (core.Result, error) {
		toolCtx = c.Ctx
		cancel()
		<-c.Ctx.Done()
		return out(okResult("a", "x"))
	})
	s := core.New(f, core.Options{}).Start(ctx, core.RunInput{}).Wait()
	wantReason(t, s, core.SettleAborted)
	if toolCtx.Err() == nil {
		t.Fatal("tool context not canceled")
	}
	if f.count(core.StepToolAfter) != 0 || f.count(core.StepRunSettled) != 1 {
		t.Fatal("dispatched after cancel")
	}
}

func TestCancelDuringStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := newFake().on(core.StepModelCall, func(c call) (core.Result, error) {
		cctx := c.Ctx
		return out(core.CallStream{Events: func(yield func(contract.StreamEvent, error) bool) {
			if !yield(contract.StreamEvent{Kind: contract.EventStart}, nil) {
				return
			}
			cancel()
			<-cctx.Done()
			yield(contract.StreamEvent{}, cctx.Err())
		}})
	})
	s := core.New(f, core.Options{}).Start(ctx, core.RunInput{}).Wait()
	wantReason(t, s, core.SettleAborted)
	if f.count(core.StepCallError) != 0 {
		t.Fatal("call.error dispatched after cancel")
	}
}
