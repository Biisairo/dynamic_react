package core_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// startAt 은 index 번째 stream.event 에서 start_tool(call) 을 낸다.
func startAt(index int, c contract.ToolCall) handler {
	return func(cl call) (core.Result, error) {
		if cl.View.TurnIndex() == 1 && cl.View.CallIndex() == 1 && cl.N == index {
			return out(nil, core.Signal{Kind: core.SigStartTool, Call: &c})
		}
		return out(nil)
	}
}

func specFake(respCall contract.ToolCall, started contract.ToolCall) *fake {
	return withTools(newFake(), respCall).on(core.StepStreamEvent, startAt(0, started))
}

// CORE-FR-25, 26: 재사용.
func TestSpeculativeReuse(t *testing.T) {
	c := tc("a", "read", `{"p":1}`)
	f := specFake(c, tc("a", "read", `{ "p" : 1 }`)) // 공백만 다름 → 같은 digest
	wantReason(t, run(t, f), core.SettleCompleted)
	if f.count(core.StepToolBefore) != 1 || f.count(core.StepToolExecute) != 1 {
		t.Fatalf("before/execute = %d/%d, want 1/1", f.count(core.StepToolBefore), f.count(core.StepToolExecute))
	}
	// 16 이 선행 실행 쪽 인자(공백 포함)를 받았으면 재사용된 것이다.
	if got := string(f.callsOf(core.StepToolBefore)[0].Input.(contract.ToolCall).Args); got != `{ "p" : 1 }` {
		t.Fatalf("tool.before args %s: not the speculative dispatch", got)
	}
	after := f.callsOf(core.StepToolAfter)
	if len(after) != 1 || after[0].Input.(core.ToolAfterInput).Result.Content[0].Text != "ran read" {
		t.Fatalf("tool.after %+v", after)
	}
	var rec *core.SignalRecord
	for _, r := range after[0].View.Signals() {
		if len(r.Signals) > 0 && r.Signals[0].Kind == core.SigStartTool {
			rec = &r
		}
	}
	if rec == nil || rec.Results[0] != core.ResultApplied {
		t.Fatalf("start_tool record %+v", rec)
	}
}

func TestSpeculativeView(t *testing.T) {
	c := tc("a", "read", `{}`)
	release := make(chan struct{})
	f := specFake(c, c).
		on(core.StepToolExecute, func(cl call) (core.Result, error) {
			<-release
			return out(okResult("a", "x"))
		}).
		on(core.StepStreamEnd, func(cl call) (core.Result, error) {
			if cl.N > 0 {
				return defaultResult(cl)
			}
			sp := cl.View.Speculative()
			if len(sp) != 1 || sp[0].CallID != "a" || sp[0].ArgsDigest != c.ArgsDigest() || sp[0].Done {
				t.Errorf("speculative at stream.end %+v", sp)
			}
			close(release)
			return defaultResult(cl)
		})
	run(t, f)
}

// CORE-FR-26: Discard, 인자 변경 → 핸들 취소 후 정상 실행.
func TestSpeculativeNotReused(t *testing.T) {
	orig := tc("a", "read", `{"p":1}`)
	cases := []struct {
		name    string
		resp    contract.ToolCall
		discard bool
	}{
		{"discard", orig, true},
		{"args changed", tc("a", "read", `{"p":2}`), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := specFake(c.resp, orig).on(core.StepToolExecute, func(cl call) (core.Result, error) {
				if cl.N == 0 {
					<-cl.Ctx.Done()
				}
				return out(okResult("a", "x"))
			})
			if c.discard {
				f.on(core.StepStreamEnd, func(cl call) (core.Result, error) {
					return out(core.StreamEndOut{Response: cl.Input.(contract.Message), Discard: []string{"a"}})
				})
			}
			wantReason(t, run(t, f), core.SettleCompleted)
			execs := f.callsOf(core.StepToolExecute)
			if len(execs) != 2 {
				t.Fatalf("execute = %d, want 2 (speculative + normal)", len(execs))
			}
			if execs[0].Ctx.Err() == nil {
				t.Fatal("speculative handle not canceled")
			}
			if got := string(execs[1].Input.(contract.ToolCall).Args); got != string(c.resp.Args) {
				t.Fatalf("normal execution args %s", got)
			}
		})
	}
}

// CORE-FR-27: recall 등 전이 시 핸들 취소.
func TestSpeculativeCanceledOnTransition(t *testing.T) {
	for _, k := range []core.SignalKind{core.SigRecall, core.SigRestartTurn, core.SigStop} {
		t.Run(string(k), func(t *testing.T) {
			c := tc("a", "read", `{}`)
			var execCtx atomic.Value
			f := newFake().
				on(core.StepStreamEvent, func(cl call) (core.Result, error) {
					if cl.N == 0 {
						return out(nil, core.Signal{Kind: core.SigStartTool, Call: &c})
					}
					if cl.N == 1 {
						time.Sleep(20 * time.Millisecond)
						return out(nil, sig(k))
					}
					return out(nil)
				}).
				on(core.StepToolExecute, func(cl call) (core.Result, error) {
					execCtx.Store(cl.Ctx)
					<-cl.Ctx.Done()
					return out(okResult("a", "x"))
				})
			run(t, f)
			v := execCtx.Load()
			if v == nil {
				t.Fatal("speculative execute not started")
			}
			if v.(interface{ Err() error }).Err() == nil {
				t.Fatal("handle not canceled")
			}
		})
	}
}

// CORE-FR-45
func TestSpeculativeFailure(t *testing.T) {
	c := tc("a", "read", `{}`)
	failing := func(f *fake) *fake {
		return f.on(core.StepToolExecute, func(cl call) (core.Result, error) {
			if cl.N == 0 {
				return core.Result{}, errors.New("spec boom")
			}
			return defaultResult(cl)
		})
	}
	t.Run("reused", func(t *testing.T) {
		f := failing(specFake(c, c))
		s := run(t, f)
		wantReason(t, s, core.SettleFailed)
	})
	t.Run("discarded", func(t *testing.T) {
		f := failing(specFake(c, c)).on(core.StepStreamEnd, func(cl call) (core.Result, error) {
			return out(core.StreamEndOut{Response: cl.Input.(contract.Message), Discard: []string{"a"}})
		})
		wantReason(t, run(t, f), core.SettleCompleted)
	})
}

func TestSpeculativeDuplicate(t *testing.T) {
	c := tc("a", "read", `{}`)
	f := withTools(newFake(), c).on(core.StepStreamEvent, func(cl call) (core.Result, error) {
		if cl.View.TurnIndex() == 1 && cl.N < 2 {
			return out(nil, core.Signal{Kind: core.SigStartTool, Call: &c})
		}
		return out(nil)
	})
	run(t, f)
	if f.count(core.StepToolExecute) != 1 {
		t.Fatalf("execute = %d", f.count(core.StepToolExecute))
	}
	var results []string
	for _, r := range f.callsOf(core.StepToolsCommit)[0].View.Signals() {
		if len(r.Signals) > 0 && r.Signals[0].Kind == core.SigStartTool {
			results = append(results, r.Results[0])
		}
	}
	if len(results) != 2 || results[0] != core.ResultApplied || results[1] != core.ResultDuplicate {
		t.Fatalf("results %v", results)
	}
}

// 선행 실행 16 의 ShortCircuit 도 재사용된다.
func TestSpeculativeShortCircuit(t *testing.T) {
	c := tc("a", "read", `{}`)
	blocked := contract.ToolResult{CallID: "a", Status: contract.StatusError}
	f := specFake(c, c).on(core.StepToolBefore, func(cl call) (core.Result, error) {
		return out(core.BeforeOut{Call: cl.Input.(contract.ToolCall), ShortCircuit: &blocked})
	})
	run(t, f)
	if f.count(core.StepToolExecute) != 0 || f.count(core.StepToolBefore) != 1 {
		t.Fatal("short circuit not reused")
	}
	if r := f.callsOf(core.StepToolAfter)[0].Input.(core.ToolAfterInput).Result; r.Status != contract.StatusError {
		t.Fatalf("result %+v", r)
	}
}
