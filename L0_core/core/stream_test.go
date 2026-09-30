package core_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// CORE-FR-15, 17, 43: 이벤트마다 10, 순서대로, 종료 이벤트 포함.
func TestStreamEventsInOrder(t *testing.T) {
	final := textResponse("a1")
	f := newFake().on(core.StepModelCall, func(call) (core.Result, error) {
		return out(streamOf(final, textDelta(0, "a"), textDelta(0, "b"), textDelta(0, "c")).cs)
	})
	run(t, f)
	evs := f.callsOf(core.StepStreamEvent)
	want := []string{"start", "a", "b", "c", "done"}
	if len(evs) != len(want) {
		t.Fatalf("stream.event dispatched %d times", len(evs))
	}
	for i, c := range evs {
		e := c.Input.(contract.StreamEvent)
		got := string(e.Kind)
		if e.Kind == contract.EventTextDelta {
			got = e.Delta
		}
		if got != want[i] {
			t.Errorf("event %d = %s, want %s", i, got, want[i])
		}
		if v, ok := c.View.Event(); !ok || v.Kind != e.Kind {
			t.Errorf("View.Event() at %d = %v %v", i, v.Kind, ok)
		}
	}
	end := f.callsOf(core.StepStreamEnd)
	if len(end) != 1 || end[0].Input.(contract.Message).ID != "a1" {
		t.Fatalf("stream.end input %+v", end)
	}
	if resp, ok := f.callsOf(core.StepResponseClassify)[0].View.Response(); !ok || resp.ID != "a1" {
		t.Fatal("View.Response() missing after stream.end")
	}
}

// CORE-FR-15: 이전 이벤트의 10 이 끝나야 다음 이벤트를 꺼낸다.
func TestStreamBackpressure(t *testing.T) {
	var s stream
	f := newFake().
		on(core.StepModelCall, func(call) (core.Result, error) {
			s = streamOf(textResponse("a1"), textDelta(0, "a"), textDelta(0, "b"))
			return out(s.cs)
		}).
		on(core.StepStreamEvent, func(c call) (core.Result, error) {
			if got, want := int(s.pulled.Load()), c.N+1; got != want {
				t.Errorf("event %d: pulled %d before dispatch returned", c.N, got)
			}
			return out(nil)
		})
	run(t, f)
}

func TestStreamErrorPaths(t *testing.T) {
	boom := errors.New("socket closed")
	cases := []struct {
		name      string
		h         handler
		class     contract.ErrorClass
		msg       string
		streamEvs int
	}{
		{"error event", func(call) (core.Result, error) {
			return out(rawStream(item{ev: contract.StreamEvent{Kind: contract.EventStart}}, item{ev: errEvent(contract.ClassOverflow)}).cs)
		}, contract.ClassOverflow, "overflow", 2},
		{"model.call go error", func(call) (core.Result, error) { return core.Result{}, boom }, contract.ClassUnknown, "socket closed", 0},
		{"iterator error", func(call) (core.Result, error) {
			return out(rawStream(item{ev: contract.StreamEvent{Kind: contract.EventStart}}, item{err: boom}).cs)
		}, contract.ClassUnknown, "socket closed", 1},
		{"no terminal", func(call) (core.Result, error) {
			return out(rawStream(item{ev: contract.StreamEvent{Kind: contract.EventStart}}).cs)
		}, contract.ClassUnknown, "stream ended without terminal event", 1},
		{"iterator panic", func(call) (core.Result, error) {
			return out(core.CallStream{Events: func(yield func(contract.StreamEvent, error) bool) { panic("bad adapter") }})
		}, contract.ClassUnknown, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake().on(core.StepModelCall, c.h)
			s := run(t, f)
			wantReason(t, s, core.SettleFailed)
			ce := f.callsOf(core.StepCallError)
			if len(ce) != 1 {
				t.Fatalf("call.error dispatched %d times", len(ce))
			}
			in := ce[0].Input.(contract.CallError)
			if in.Class != c.class || (c.msg != "" && in.Message != c.msg) {
				t.Fatalf("call.error input %+v", in)
			}
			if v, ok := ce[0].View.CallError(); !ok || v.Class != c.class {
				t.Fatal("View.CallError() missing")
			}
			if n := f.count(core.StepStreamEvent); n != c.streamEvs {
				t.Fatalf("stream.event dispatched %d times, want %d", n, c.streamEvs)
			}
			if f.count(core.StepStreamEnd) != 0 {
				t.Fatal("stream.end dispatched on error")
			}
		})
	}
}

func TestEventsAfterTerminalNotPulled(t *testing.T) {
	var s stream
	final := textResponse("a1")
	f := newFake().on(core.StepModelCall, func(call) (core.Result, error) {
		s = rawStream(item{ev: contract.StreamEvent{Kind: contract.EventDone, Message: &final}}, item{ev: textDelta(0, "late")})
		return out(s.cs)
	})
	run(t, f)
	if n := s.pulled.Load(); n != 1 {
		t.Fatalf("pulled %d events", n)
	}
	if f.count(core.StepStreamEvent) != 1 {
		t.Fatal("event after terminal dispatched")
	}
}

// CORE-FR-16, 27: 3번째 이벤트에서 restart_turn → 4번째를 꺼내지 않고 9 의 context 취소.
func TestStreamInterrupt(t *testing.T) {
	var s stream
	var callCtx []call
	f := newFake().
		on(core.StepModelCall, func(c call) (core.Result, error) {
			callCtx = append(callCtx, c)
			if c.N == 0 {
				s = streamOf(textResponse("a1"), textDelta(0, "a"), textDelta(0, "b"), textDelta(0, "c"))
				return out(s.cs)
			}
			return defaultResult(c)
		}).
		on(core.StepStreamEvent, func(c call) (core.Result, error) {
			if c.N == 2 {
				return out(nil, sig(core.SigRestartTurn))
			}
			return out(nil)
		})
	wantReason(t, run(t, f), core.SettleCompleted)
	if n := s.pulled.Load(); n != 3 {
		t.Fatalf("pulled %d events, want 3", n)
	}
	if callCtx[0].Ctx.Err() == nil {
		t.Fatal("model.call context not canceled")
	}
	steps := f.steps()
	idx := 0
	for i, st := range steps {
		if st == core.StepStreamEvent {
			idx++
			if idx == 3 && steps[i+1] != core.StepTurnStart {
				t.Fatalf("after interrupt: %s", steps[i+1])
			}
		}
	}
	if f.count(core.StepTurnStart) != 2 {
		t.Fatal("turn not restarted")
	}
}

type live struct {
	mu   sync.Mutex
	sent []string
	err  error
}

func (l *live) Send(m contract.Message) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	l.sent = append(l.sent, m.ID)
	return nil
}

// CORE-FR-18
func TestInjectInputDuringStream(t *testing.T) {
	nudge := userMsg("n1", "stop that")
	cases := []struct {
		name   string
		live   *live
		result string
		inbox  int
	}{
		{"live ok", &live{}, core.ResultDeliveredLive, 0},
		{"no live", nil, core.ResultDeferred, 1},
		{"live error", &live{err: errors.New("closed")}, core.ResultDeferred, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake().
				on(core.StepModelCall, func(call) (core.Result, error) {
					s := streamOf(textResponse("a1"), textDelta(0, "a"))
					if c.live != nil {
						s.cs.Live = c.live
					}
					return out(s.cs)
				}).
				on(core.StepStreamEvent, func(cl call) (core.Result, error) {
					if cl.N == 1 {
						return out(nil, core.Signal{Kind: core.SigInjectInput, Message: &nudge, Reason: "steer"})
					}
					return out(nil)
				})
			run(t, f)
			v := f.callsOf(core.StepStreamEnd)[0].View
			if got := len(v.Inbox()); got != c.inbox {
				t.Fatalf("inbox %d, want %d", got, c.inbox)
			}
			var rec *core.SignalRecord
			for _, r := range v.Signals() {
				if r.Step == core.StepStreamEvent && len(r.Signals) > 0 && r.Signals[0].Kind == core.SigInjectInput {
					rec = &r
				}
			}
			if rec == nil || len(rec.Results) != 1 || rec.Results[0] != c.result {
				t.Fatalf("signal record %+v", rec)
			}
			if c.live != nil && c.live.err == nil && len(c.live.sent) != 1 {
				t.Fatal("live not sent")
			}
		})
	}
}

// CORE-FR-19
func TestInjectInputAfterStream(t *testing.T) {
	nudge := userMsg("n1", "continue")
	f := newFake().
		on(core.StepTurnFinish, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(nil, core.Signal{Kind: core.SigInjectInput, Message: &nudge}, sig(core.SigContinueTurn))
			}
			return out(nil, sig(core.SigSettle))
		}).
		on(core.StepRunBeforeSettle, func(c call) (core.Result, error) {
			m := userMsg("n2", "more")
			return out(nil, core.Signal{Kind: core.SigInjectInput, Message: &m})
		})
	run(t, f)
	if in := f.callsOf(core.StepTurnStart)[1].View.Inbox(); len(in) != 1 || in[0].ID != "n1" {
		t.Fatalf("inbox at 2nd turn %v", in)
	}
	if in := f.callsOf(core.StepRunSettled)[0].View.Inbox(); len(in) != 2 || in[1].ID != "n2" {
		t.Fatalf("inbox at settle %v", in)
	}
}

// CORE-FR-46
func TestDraftAssembly(t *testing.T) {
	c1 := tc("c1", "read", `{"path":"a"}`)
	f := newFake().on(core.StepModelCall, func(call) (core.Result, error) {
		return out(streamOf(toolResponse("a1", c1),
			contract.StreamEvent{Kind: contract.EventThinkingDelta, Index: 0, Delta: "hm"},
			textDelta(1, "he"), textDelta(1, "llo"),
			contract.StreamEvent{Kind: contract.EventToolCallStart, Index: 2, ToolCall: &contract.ToolCall{ID: "c1", Name: "read"}},
			contract.StreamEvent{Kind: contract.EventToolCallDelta, Index: 2, Delta: `{"pa`},
			contract.StreamEvent{Kind: contract.EventToolCallDelta, Index: 2, Delta: `th":"a"}`},
		).cs)
	})
	run(t, f)
	evs := f.callsOf(core.StepStreamEvent)
	if _, ok := evs[0].View.Draft(); ok {
		t.Fatal("draft present before any content")
	}
	d, ok := evs[5].View.Draft() // 첫 toolcall_delta 시점 (현재 이벤트까지 반영)
	if !ok {
		t.Fatal("no draft")
	}
	if d.Role != contract.RoleAssistant || len(d.Content) != 3 {
		t.Fatalf("draft %+v", d)
	}
	if d.Content[0].Kind != contract.BlockThinking || d.Content[0].Text != "hm" {
		t.Errorf("block 0 %+v", d.Content[0])
	}
	if d.Content[1].Kind != contract.BlockText || d.Content[1].Text != "hello" {
		t.Errorf("block 1 %+v", d.Content[1])
	}
	if b := d.Content[2]; b.Kind != contract.BlockToolCall || b.ToolCall.ID != "c1" || string(b.ToolCall.Args) != `{"pa` {
		t.Errorf("block 2 %+v %s", b, b.ToolCall.Args)
	}
	last, _ := evs[len(evs)-1].View.Draft()
	if string(last.Content[2].ToolCall.Args) != `{"path":"a"}` {
		t.Errorf("args %s", last.Content[2].ToolCall.Args)
	}
	var obj map[string]string
	if json.Unmarshal(last.Content[2].ToolCall.Args, &obj) != nil {
		t.Error("assembled args not JSON")
	}
}

func TestDraftEnd(t *testing.T) {
	c1 := tc("c1", "read", `{}`)
	f := newFake().on(core.StepModelCall, func(c call) (core.Result, error) {
		if c.N == 0 {
			return out(streamOf(toolResponse("a1", c1),
				contract.StreamEvent{Kind: contract.EventToolCallStart, Index: 0, ToolCall: &contract.ToolCall{ID: "c1", Name: "read"}},
				contract.StreamEvent{Kind: contract.EventToolCallEnd, Index: 0, ToolCall: &c1},
			).cs)
		}
		return defaultResult(c)
	}).on(core.StepStreamEnd, func(c call) (core.Result, error) {
		if c.N == 0 {
			return out(core.StreamEndOut{Response: c.Input.(contract.Message)}, sig(core.SigRecall))
		}
		return defaultResult(c)
	})
	run(t, f)
	evs := f.callsOf(core.StepStreamEvent)
	d, _ := evs[3].View.Draft() // done 이벤트 시점
	if string(d.Content[0].ToolCall.Args) != `{}` {
		t.Fatalf("toolcall_end not applied: %+v", d.Content[0].ToolCall)
	}
	if _, ok := evs[4].View.Draft(); ok {
		t.Fatal("draft not reset on recall")
	}
}
