package inject_test

import (
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

func richFacts() injecttest.Facts {
	call := contract.ToolCall{ID: "c1", Name: "read", Args: []byte(`{"p":1}`)}
	resp := reply("a2", call)
	req := contract.Request{Messages: []contract.Message{userMsg("u1", "x")}}
	return injecttest.Facts{
		RunID: "r1", SessionID: "s1", Config: []byte(`{"k":1}`), Step: core.StepTurnFinish, Seq: 9,
		TurnIndex: 2, CallIndex: 1, Elapsed: time.Second,
		History:      []contract.Message{userMsg("h1", "old"), reply("h2")},
		Transcript:   []contract.Message{userMsg("h1", "old"), reply("h2"), userMsg("u1", "x"), reply("a1")},
		TurnAppended: []contract.Message{userMsg("u1", "x")},
		Ephemeral:    []contract.Message{userMsg("e1", "x")},
		Projection:   []contract.Message{userMsg("p1", "x")},
		Inbox:        []contract.Message{userMsg("i1", "x")},
		Request:      &req,
		Model:        &contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "a"}, ContextWindow: 100},
		Response:     &resp,
		CallError:    &contract.CallError{Class: contract.ClassOverflow},
		Plan:         &core.Plan{Groups: []core.Group{{Calls: []contract.ToolCall{call}}}},
		ToolCalls:    []contract.ToolCall{call},
		ToolResults:  []contract.ToolResult{{CallID: "c1", Status: contract.StatusOK}},
		Edits:        []contract.ContextEdit{{ID: "e1", Kind: contract.EditHide, Timestamp: ts, Target: "h1"}},
		Signals:      []core.SignalRecord{{Seq: 3, Step: core.StepCallError, Signals: []core.Signal{{Kind: core.SigRecall, Reason: "rate"}}}},
		Speculative:  []core.SpeculativeState{{CallID: "c1", Done: true}},
	}
}

func onFinish(t *testing.T, f injecttest.Facts, fn func(ctx *inject.TurnFinishCtx)) {
	t.Helper()
	set := inject.FunctionSet{Name: "q", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		fn(ctx)
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	}}
	if _, err := dispatch(t, set, core.StepTurnFinish, nil, f); err != nil {
		t.Fatal(err)
	}
}

// 3.4 그룹 메서드가 View 의 사실을 그대로 전달한다.
func TestQueriesPassFacts(t *testing.T) {
	onFinish(t, richFacts(), func(ctx *inject.TurnFinishCtx) {
		fl := ctx.Flow()
		if fl.Step() != core.StepTurnFinish || fl.Seq() != 9 || fl.RunID() != "r1" || fl.SessionID() != "s1" ||
			fl.TurnIndex() != 2 || fl.CallIndex() != 1 || fl.Elapsed() != time.Second {
			t.Error("flow")
		}
		if fl.Count(core.SigRecall, "rate") != 1 || fl.Count(core.SigRecall, "") != 1 || fl.Count(core.SigStop, "") != 0 {
			t.Error("count")
		}
		if len(fl.Signals()) != 1 {
			t.Error("signals")
		}
		cv := ctx.Conv()
		if len(cv.History()) != 2 || len(cv.Transcript()) != 4 || cv.TurnAppended()[0].ID != "u1" ||
			cv.Ephemeral()[0].ID != "e1" || cv.Projection()[0].ID != "p1" || cv.Inbox()[0].ID != "i1" || cv.Edits()[0].ID != "e1" {
			t.Error("conv")
		}
		if len(ctx.Request().Current().Messages) != 1 || ctx.Request().Model().ContextWindow != 100 {
			t.Error("request")
		}
		out := ctx.Output()
		if out.Response().ID != "a2" || out.CallError().Class != contract.ClassOverflow {
			t.Error("output")
		}
		tl := ctx.Tools()
		if len(tl.Plan().Groups) != 1 || tl.Calls()[0].ID != "c1" || tl.Results()[0].CallID != "c1" || !tl.Speculative()[0].Done {
			t.Error("tools")
		}
	})
}

func TestStreamQueries(t *testing.T) {
	f := richFacts()
	f.Step = core.StepStreamEvent
	f.Event = &contract.StreamEvent{Kind: contract.EventTextDelta, Delta: "x"}
	f.Draft = &contract.Message{Role: contract.RoleAssistant, Content: []contract.Block{{Kind: contract.BlockText, Text: "x"}}}
	set := inject.FunctionSet{Name: "q", StreamEvent: func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
		if ctx.Stream().Event().Delta != "x" || ctx.Stream().Draft().Content[0].Text != "x" {
			t.Error("stream")
		}
		return nil, nil
	}}
	dispatch(t, set, core.StepStreamEvent, *f.Event, f)
}

// 없는 값은 zero value.
func TestQueriesZeroWhenAbsent(t *testing.T) {
	onFinish(t, facts(core.StepTurnFinish), func(ctx *inject.TurnFinishCtx) {
		if ctx.Request().Current().Messages != nil || ctx.Request().Model().ContextWindow != 0 ||
			ctx.Output().Response().ID != "" || ctx.Output().CallError().Class != "" ||
			ctx.Output().LastUsage() != (contract.Usage{}) || len(ctx.Tools().Plan().Groups) != 0 {
			t.Error("not zero")
		}
	})
}

// IN-FR-46: LastUsage = 응답의 usage, 없으면 이 run 에서 추가된 마지막 assistant.
func TestLastUsage(t *testing.T) {
	f := richFacts()
	f.Response.Usage = &contract.Usage{Input: 77}
	onFinish(t, f, func(ctx *inject.TurnFinishCtx) {
		if ctx.Output().LastUsage().Input != 77 {
			t.Error("response usage not used")
		}
	})
	f = richFacts()
	f.Response = nil
	f.Transcript[3].Usage = &contract.Usage{Input: 5}
	onFinish(t, f, func(ctx *inject.TurnFinishCtx) {
		if ctx.Output().LastUsage().Input != 5 {
			t.Error("transcript usage not used")
		}
	})
	f = richFacts()
	f.Response = nil
	f.Transcript = f.History // 이 run 에서 추가된 응답 없음 → History 의 usage 는 보지 않는다
	onFinish(t, f, func(ctx *inject.TurnFinishCtx) {
		if ctx.Output().LastUsage() != (contract.Usage{}) {
			t.Error("history usage leaked")
		}
	})
}

// IN-FR-17, 48: 결과를 고쳐도 같은 dispatch 의 다음 조회가 바뀌지 않는다.
func TestQueryResultsAreDeepCopies(t *testing.T) {
	onFinish(t, richFacts(), func(ctx *inject.TurnFinishCtx) {
		h := ctx.Conv().History()
		h[0].Content[0].Text = "mutated"
		h[1].Usage.Input = 999
		if ctx.Conv().History()[0].Content[0].Text != "old" || ctx.Conv().History()[1].Usage.Input == 999 {
			t.Error("history shared")
		}
		r := ctx.Output().Response()
		r.Content[1].ToolCall.Args[0] = 'X'
		if string(ctx.Output().Response().Content[1].ToolCall.Args) != `{"p":1}` {
			t.Error("response shared")
		}
		p := ctx.Tools().Plan()
		p.Groups[0].Calls[0].ID = "zz"
		if ctx.Tools().Plan().Groups[0].Calls[0].ID != "c1" {
			t.Error("plan shared")
		}
		req := ctx.Request().Current()
		req.Messages[0].ID = "zz"
		if ctx.Request().Current().Messages[0].ID != "u1" {
			t.Error("request shared")
		}
		cfg := ctx.Env().Config()
		cfg[0] = 'X'
		if string(ctx.Env().Config()) != `{"k":1}` {
			t.Error("config shared")
		}
	})
}

// 신호 payload 는 넘긴 값을 그대로 담는다.
func TestSignalPayloads(t *testing.T) {
	msg := userMsg("n1", "nudge")
	res, _ := finishWith(func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.InjectInput(msg), ctx.ContinueTurn()}, nil
	})
	if res.Signals[0].Message.ID != "n1" {
		t.Fatal("inject_input payload")
	}
	set := inject.FunctionSet{Name: "p", ToolsPlan: func(ctx *inject.ToolsPlanCtx, calls []contract.ToolCall) (core.Plan, []inject.ToolsPlanSignal, error) {
		return core.Plan{}, []inject.ToolsPlanSignal{ctx.SkipTools(nil)}, nil
	}}
	r, err := dispatch(t, set, core.StepToolsPlan, []contract.ToolCall{}, facts(core.StepToolsPlan))
	if err != nil || r.Signals[0].Results == nil {
		t.Fatalf("skip_tools(nil) should carry empty results: %+v %v", r.Signals, err)
	}
	call := contract.ToolCall{ID: "c1", Name: "t", Args: []byte(`{}`)}
	ev := inject.FunctionSet{Name: "e", StreamEvent: func(ctx *inject.StreamEventCtx, e contract.StreamEvent) ([]inject.StreamEventSignal, error) {
		return []inject.StreamEventSignal{ctx.StartTool(call), ctx.Recall().WithReason("r")}, nil
	}}
	r, _ = dispatch(t, ev, core.StepStreamEvent, contract.StreamEvent{Kind: contract.EventStart}, facts(core.StepStreamEvent))
	if r.Signals[0].Call.ID != "c1" || r.Signals[1].Kind != core.SigRecall || r.Signals[1].Reason != "r" {
		t.Fatalf("signals %+v", r.Signals)
	}
}
