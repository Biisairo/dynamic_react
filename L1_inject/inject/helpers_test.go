package inject_test

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func userMsg(id, text string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Content: []contract.Block{{Kind: contract.BlockText, Text: text}}, Timestamp: ts}
}

func reply(id string, calls ...contract.ToolCall) contract.Message {
	m := contract.Message{
		ID: id, Role: contract.RoleAssistant, Timestamp: ts,
		Content:    []contract.Block{{Kind: contract.BlockText, Text: "ok"}},
		Usage:      &contract.Usage{Input: 10, Output: 2},
		StopReason: &contract.StopReason{Kind: contract.StopStop},
	}
	for i := range calls {
		m.Content = append(m.Content, contract.Block{Kind: contract.BlockToolCall, ToolCall: &calls[i]})
	}
	return m
}

func streamOf(m contract.Message) core.CallStream {
	return core.CallStream{Events: iter.Seq2[contract.StreamEvent, error](func(yield func(contract.StreamEvent, error) bool) {
		yield(contract.StreamEvent{Kind: contract.EventDone, Message: &m}, nil)
	})}
}

// workingSet 은 22 step 을 모두 채운, 도구 없이 한 턴 뒤 끝나는 세트다.
func workingSet(name string) inject.FunctionSet {
	return inject.FunctionSet{
		Name: name,
		RunStart: func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
			return core.RunInit{}, nil, nil
		},
		TurnStart: func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
			return []contract.Message{userMsg("u1", "hi")}, nil, nil
		},
		ContextMaintain: func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) { return nil, nil },
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			return nil, nil, nil
		},
		ContextProject: func(ctx *inject.ContextProjectCtx) ([]contract.Message, []inject.ContextProjectSignal, error) {
			return ctx.Conv().Transcript(), nil, nil
		},
		RequestBuild: func(ctx *inject.RequestBuildCtx) (contract.Request, []inject.RequestBuildSignal, error) {
			return contract.Request{Messages: ctx.Conv().Projection()}, nil, nil
		},
		RequestFinalize: func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
			return req, nil, nil
		},
		ModelSelect: func(ctx *inject.ModelSelectCtx, req contract.Request) (contract.ModelInfo, []inject.ModelSelectSignal, error) {
			return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "fake"}}, nil, nil
		},
		ModelCall: func(ctx *inject.ModelCallCtx, req contract.Request) (core.CallStream, []inject.ModelCallSignal, error) {
			return streamOf(reply("a1")), nil, nil
		},
		StreamEvent: func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
			return nil, nil
		},
		StreamEnd: func(ctx *inject.StreamEndCtx, msg contract.Message) (core.StreamEndOut, []inject.StreamEndSignal, error) {
			return core.StreamEndOut{Response: msg}, nil, nil
		},
		CallError: func(ctx *inject.CallErrorCtx, ce contract.CallError) ([]inject.CallErrorSignal, error) {
			return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
		},
		ResponseClassify: func(ctx *inject.ResponseClassifyCtx) ([]inject.ResponseClassifySignal, error) { return nil, nil },
		ResponseCommit:   func(ctx *inject.ResponseCommitCtx) ([]inject.ResponseCommitSignal, error) { return nil, nil },
		ToolsPlan: func(ctx *inject.ToolsPlanCtx, calls []contract.ToolCall) (core.Plan, []inject.ToolsPlanSignal, error) {
			var p core.Plan
			for _, c := range calls {
				p.Groups = append(p.Groups, core.Group{Calls: []contract.ToolCall{c}})
			}
			return p, nil, nil
		},
		ToolBefore: func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			return core.BeforeOut{Call: call}, nil, nil
		},
		ToolExecute: func(ctx *inject.ToolExecuteCtx, call contract.ToolCall) (contract.ToolResult, []inject.ToolExecuteSignal, error) {
			return contract.ToolResult{CallID: call.ID, Status: contract.StatusOK}, nil, nil
		},
		ToolAfter: func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			return res, nil, nil
		},
		ToolsCommit: func(ctx *inject.ToolsCommitCtx, calls []contract.ToolCall, results []contract.ToolResult) ([]contract.Message, []inject.ToolsCommitSignal, error) {
			out := make([]contract.Message, len(results))
			for i := range results {
				r := results[i]
				out[i] = contract.Message{ID: "tr-" + r.CallID, Role: contract.RoleToolResult, Result: &r, Timestamp: ts}
			}
			return out, nil, nil
		},
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			if len(ctx.Tools().Results()) > 0 {
				return []inject.TurnFinishSignal{ctx.ContinueTurn()}, nil
			}
			return []inject.TurnFinishSignal{ctx.Settle()}, nil
		},
		RunBeforeSettle: func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) { return nil, nil },
		RunSettled:      func(ctx *inject.RunSettledCtx, s core.Settle) error { return nil },
	}
}

func mustBind(t *testing.T, sets ...inject.FunctionSet) *inject.Dispatcher {
	t.Helper()
	d, err := inject.Bind(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func runCore(t *testing.T, d core.Dispatcher) core.Settle {
	t.Helper()
	done := make(chan core.Settle, 1)
	go func() {
		done <- core.New(d, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1", Config: json.RawMessage(`{"a":1}`)}).Wait()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("run did not settle")
		return core.Settle{}
	}
}

// dispatch 는 set 의 step 하나를 가짜 View 로 dispatch 한다.
func dispatch(t *testing.T, set inject.FunctionSet, step core.Step, input any, f injecttest.Facts) (core.Result, error) {
	t.Helper()
	return injecttest.Dispatch(context.Background(), set, step, input, f.View())
}

func facts(step core.Step) injecttest.Facts {
	return injecttest.Facts{RunID: "r1", SessionID: "s1", Step: step, Seq: 1}
}
