package harness_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
)

// recorder 는 이벤트를 모으고 동시 호출을 검사한다.
type recorder struct {
	mu       sync.Mutex
	evs      []harness.Event
	inflight atomic.Int32
	overlap  atomic.Bool
}

func (r *recorder) on(ev harness.Event) {
	if r.inflight.Add(1) > 1 {
		r.overlap.Store(true)
	}
	time.Sleep(time.Millisecond)
	r.mu.Lock()
	r.evs = append(r.evs, ev)
	r.mu.Unlock()
	r.inflight.Add(-1)
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.evs {
		out = append(out, string(e.Kind))
	}
	return out
}

func (r *recorder) all() []harness.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]harness.Event(nil), r.evs...)
}

func streamed(id string, stop contract.StopKind, deltas []string, blocks ...contract.Block) []contract.StreamEvent {
	m := contract.Message{ID: id, Role: contract.RoleAssistant, Timestamp: ts, Content: blocks,
		Usage: &contract.Usage{Input: 10, Output: 1}, StopReason: &contract.StopReason{Kind: stop}}
	evs := []contract.StreamEvent{{Kind: contract.EventStart}}
	for _, d := range deltas {
		evs = append(evs, contract.StreamEvent{Kind: contract.EventTextDelta, Delta: d})
	}
	return append(evs, contract.StreamEvent{Kind: contract.EventDone, Message: &m})
}

func newAgent(t *testing.T, e *envtest.Env, m *fakeModel, mod func(*harness.Options)) *harness.Agent {
	t.Helper()
	o := options(e, m)
	mod(&o)
	a, err := harness.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// HN-FR-31, 32: 텍스트 턴 — message_start → update* (누적본) → end → turn_end → run_end.
func TestEventsTextTurn(t *testing.T) {
	m := &fakeModel{script: func(int, contract.Request) []contract.StreamEvent {
		return streamed("a", contract.StopStop, []string{"he", "llo"}, text("hello"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	if s, _ := start(t, a, "s1", user("u1", "hi")); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	if got := strings.Join(rec.kinds(), ","); got != "input,message_start,message_update,message_update,message_end,turn_end,run_end" {
		t.Fatalf("kinds %s", got)
	}
	evs := rec.all()
	for _, e := range evs {
		if e.RunID == "" || e.SessionID != "s1" {
			t.Fatalf("ids %+v", e)
		}
	}
	if evs[0].Message == nil || evs[0].Message.ID != "u1" {
		t.Fatalf("input %+v", evs[0])
	}
	evs = evs[1:]
	partial := func(e harness.Event) string { return e.Message.Content[0].Text }
	if evs[1].Delta.Delta != "he" || partial(evs[1]) != "he" || partial(evs[2]) != "hello" {
		t.Fatalf("updates %+v %+v", evs[1], evs[2])
	}
	if evs[3].Message.ID != "a" || evs[3].Message.Usage == nil || evs[4].Message.ID != "a" {
		t.Fatalf("end/turn %+v %+v", evs[3], evs[4])
	}
	if evs[5].Settle == nil || evs[5].Settle.Reason != core.SettleCompleted {
		t.Fatalf("run_end %+v", evs[5])
	}
}

// HN-FR-30, 31, 32: Gate, 도구 이벤트, 병렬 도구에서도 직렬 호출.
func TestGateAndToolEvents(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.txt", "A\n")
	e.SetFile("/w/b.txt", "B\n")
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c1", "read", `{"path":"a.txt"}`), call("c2", "read", `{"path":"b.txt"}`),
				call("c3", "bash", `{"command":"rm -rf /"}`), call("c4", "nope", `{}`))
		}
		return reply("a1", contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	var gateMu sync.Mutex
	var gated []harness.GateInput
	a := newAgent(t, e, m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Gate = func(ctx context.Context, in harness.GateInput) *contract.ToolResult {
			gateMu.Lock()
			gated = append(gated, in)
			gateMu.Unlock()
			if in.Call.Name == "bash" {
				return &contract.ToolResult{Status: contract.StatusError, Content: []contract.Block{text("blocked")}}
			}
			return nil
		}
	})
	if s, _ := start(t, a, "s1", user("u1", "go")); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	if rec.overlap.Load() {
		t.Fatal("OnEvent called concurrently")
	}
	gateMu.Lock()
	if len(gated) != 3 {
		t.Fatalf("gate calls %+v (unknown tool must not reach the gate)", gated)
	}
	for _, g := range gated {
		if g.RunID == "" || g.SessionID != "s1" || g.TurnIndex != gated[0].TurnIndex || g.InboxConsumed != 0 {
			t.Fatalf("gate input %+v", g)
		}
	}
	gateMu.Unlock()
	sent := map[string]contract.ToolResult{}
	for _, msg := range m.requests()[1].Messages {
		if msg.Result != nil {
			sent[msg.Result.CallID] = *msg.Result
		}
	}
	if r := sent["c3"]; r.Status != contract.StatusError || r.Content[0].Text != "blocked" {
		t.Fatalf("gate result not sent: %+v", r)
	}
	starts, ends := map[string]int{}, map[string]int{}
	for _, ev := range rec.all() {
		switch ev.Kind {
		case harness.EventToolStart:
			if ends[ev.Call.ID] > 0 {
				t.Fatalf("tool_start after tool_end for %s", ev.Call.ID)
			}
			starts[ev.Call.ID]++
		case harness.EventToolEnd:
			if starts[ev.Call.ID] != 1 {
				t.Fatalf("tool_end without tool_start for %s", ev.Call.ID)
			}
			ends[ev.Call.ID]++
			if got := sent[ev.Call.ID]; got.Status != ev.Result.Status || got.Content[0].Text != ev.Result.Content[0].Text {
				t.Fatalf("tool_end %s differs from sent result: %+v vs %+v", ev.Call.ID, ev.Result, got)
			}
		}
	}
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		if starts[id] != 1 || ends[id] != 1 {
			t.Fatalf("%s: starts %d ends %d", id, starts[id], ends[id])
		}
	}
	kinds := rec.kinds()
	if kinds[len(kinds)-1] != "run_end" || strings.Count(strings.Join(kinds, ","), "turn_end") != 2 {
		t.Fatalf("kinds %v", kinds)
	}
}

// HN-FR-32: 스트림 에러면 message_end 없이 run_end.
func TestEventsStreamError(t *testing.T) {
	m := &fakeModel{script: func(int, contract.Request) []contract.StreamEvent {
		return []contract.StreamEvent{{Kind: contract.EventStart}, {Kind: contract.EventTextDelta, Delta: "x"},
			{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassInvalidRequest, Message: "bad"}}}
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	if s, _ := start(t, a, "s1", user("u1", "hi")); s.Reason != core.SettleFailed {
		t.Fatalf("%+v", s)
	}
	if got := strings.Join(rec.kinds(), ","); got != "input,message_start,message_update,run_end" {
		t.Fatalf("kinds %s", got)
	}
}

// HN-FR-31: retry 는 대기 전에 Error·Attempt·Delay 와 함께.
func TestEventsRetry(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n < 2 {
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassRateLimit, Message: "slow down"}}}
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"retry":{"baseDelayMs":20,"maxDelayMs":50}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	var retries []harness.Event
	for _, e := range rec.all() {
		if e.Kind == harness.EventRetry {
			retries = append(retries, e)
		}
	}
	if len(retries) != 2 || retries[0].Attempt != 1 || retries[1].Attempt != 2 || retries[0].Error.Class != contract.ClassRateLimit ||
		retries[0].Delay != 20*time.Millisecond || retries[1].Delay != 40*time.Millisecond {
		t.Fatalf("retries %+v", retries)
	}
}

// HN-FR-31: 압축 start/end.
func TestEventsCompaction(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch {
		case n == 0:
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}}}
		case len(req.System) == 1 && req.System[0].Text == summary.SystemPrompt:
			return reply("sum", contract.StopStop, text("## Goal\nsummarized"))
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	long := func(id string) contract.Message { return user(id, strings.Repeat("abcd", 50)) }
	r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{long("u1"), long("u2"), long("u3")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"compaction":{"keepRecentTokens":10}}`)})
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	var phases []string
	for _, e := range rec.all() {
		if e.Kind == harness.EventCompaction {
			phases = append(phases, e.Phase)
			if e.Err != nil {
				t.Fatalf("compaction err %v", e.Err)
			}
		}
	}
	if strings.Join(phases, ",") != "start,end" {
		t.Fatalf("phases %v", phases)
	}
}

// HN-FR-33: bash 출력이 tool_update 로.
func TestEventsToolUpdate(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		io.WriteString(out, "one\n")
		io.WriteString(out, "two\n")
		return env.ExecResult{}, nil
	})
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c1", "bash", `{"command":"x"}`))
		}
		return reply("a1", contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	a := newAgent(t, e, m, func(o *harness.Options) { o.OnEvent = rec.on })
	start(t, a, "s1", user("u1", "go"))
	var seq, out []string
	for _, ev := range rec.all() {
		if strings.HasPrefix(string(ev.Kind), "tool_") {
			seq = append(seq, string(ev.Kind))
		}
		if ev.Kind == harness.EventToolUpdate {
			if ev.Call == nil || ev.Call.ID != "c1" {
				t.Fatalf("update call %+v", ev.Call)
			}
			out = append(out, ev.Output)
		}
	}
	if strings.Join(seq, ",") != "tool_start,tool_update,tool_update,tool_end" || strings.Join(out, "") != "one\ntwo\n" {
		t.Fatalf("seq %v out %q", seq, out)
	}
}

// HN-FR-34: 콜백 panic → failed.
func TestOnEventPanic(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.txt", "A\n")
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return reply("a0", contract.StopToolUse, call("c1", "read", `{"path":"a.txt"}`))
	}}
	a := newAgent(t, e, m, func(o *harness.Options) {
		o.OnEvent = func(ev harness.Event) {
			if ev.Kind == harness.EventToolStart {
				panic("surface bug")
			}
		}
	})
	if s, _ := start(t, a, "s1", user("u1", "go")); s.Reason != core.SettleFailed || !strings.Contains(s.Err.Error(), "surface bug") {
		t.Fatalf("%+v", s)
	}
}

// HN-FR-34: 동시에 도는 두 run 의 이벤트도 한 번에 하나씩.
func TestOnEventSerializedAcrossRuns(t *testing.T) {
	m := &fakeModel{script: func(int, contract.Request) []contract.StreamEvent {
		return streamed("a", contract.StopStop, []string{"a", "b", "c", "d"}, text("abcd"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	var wg sync.WaitGroup
	for _, sid := range []string{"s1", "s2", "s3"} {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: sid, Input: []contract.Message{user("u-"+sid, "hi")}, Config: json.RawMessage(cfg)})
			r.Wait()
		}(sid)
	}
	wg.Wait()
	if rec.overlap.Load() {
		t.Fatal("OnEvent called concurrently across runs")
	}
}

// HN-FR-31: retry 의 Attempt 는 턴마다 1부터.
func TestRetryAttemptPerTurn(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.txt", "A\n")
	rate := []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassRateLimit, Message: "slow"}}}
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch n {
		case 0, 2:
			return rate
		case 1:
			return reply("a1", contract.StopToolUse, call("c1", "read", `{"path":"a.txt"}`))
		}
		return reply("a3", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, e, m, func(o *harness.Options) { o.OnEvent = rec.on })
	r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"retry":{"baseDelayMs":1,"maxDelayMs":2}}`)})
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	var attempts []int
	for _, ev := range rec.all() {
		if ev.Kind == harness.EventRetry {
			attempts = append(attempts, ev.Attempt)
		}
	}
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 1 {
		t.Fatalf("attempts %v", attempts)
	}
}

// HN-FR-31: tool_end 는 tool.after 의 절단 뒤 결과다.
func TestToolEndIsTruncated(t *testing.T) {
	big := sets.Tool{Spec: contract.ToolSpec{Name: "big", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult {
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text(strings.Repeat("line\n", 5000))}}
		}}
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c1", "big", `{}`))
		}
		return reply("a1", contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.ToolSet = func(def []sets.Tool) []sets.Tool { return append(def, big) }
	})
	start(t, a, "s1", user("u1", "go"))
	var sent string
	for _, msg := range m.requests()[1].Messages {
		if msg.Result != nil {
			sent = msg.Result.Content[0].Text
		}
	}
	for _, ev := range rec.all() {
		if ev.Kind == harness.EventToolEnd {
			if got := ev.Result.Content[0].Text; got != sent || len(got) >= 5*5000 {
				t.Fatalf("tool_end %d bytes, sent %d bytes", len(got), len(sent))
			}
			return
		}
	}
	t.Fatal("no tool_end")
}

// HN-FR-27: RunRequest.Observer 는 그 run 에만 쓰인다.
func TestRunObserver(t *testing.T) {
	m := &fakeModel{script: func(int, contract.Request) []contract.StreamEvent { return reply("a", contract.StopStop, text("ok")) }}
	shared, own := &counter{}, &counter{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.Observer = shared })
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")},
		Config: json.RawMessage(cfg), Observer: own})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, r)
	if own.stream == 0 || shared.stream != 0 {
		t.Fatalf("own %d shared %d", own.stream, shared.stream)
	}
	start(t, a, "s2", user("u2", "hi"))
	if shared.stream == 0 {
		t.Fatal("Options.Observer not used without RunRequest.Observer")
	}
}

// HN-FR-35
func TestEmit(t *testing.T) {
	rec := &recorder{}
	m := &fakeModel{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	a.Emit(harness.Event{Kind: harness.EventApproval, RunID: "r"})
	if k := rec.kinds(); len(k) != 1 || k[0] != "approval" {
		t.Fatalf("%v", k)
	}
	newAgent(t, envtest.New("/w", "/h"), m, func(*harness.Options) {}).Emit(harness.Event{Kind: harness.EventApproval})
}

// HN-FR-31 input: turn.start 가 돌려준 메시지마다, 세트의 inject_input 포함.
func TestEventsInput(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return reply(fmt.Sprintf("a%d", n), contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	nudge := func(d sets.Deps) inject.FunctionSet {
		base := sets.BaseTurnFinish(d)
		var calls atomic.Int32
		return inject.FunctionSet{Name: "nudge", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			if calls.Add(1) > 1 {
				return base(ctx)
			}
			return []inject.TurnFinishSignal{ctx.InjectInput(user("n1", "check again")), ctx.ContinueTurn()}, nil
		}}
	}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Sets = func(d sets.Deps) []inject.FunctionSet { return append(harness.DefaultSets(d), nudge(d)) }
	})
	if s, _ := start(t, a, "s1", user("u1", "hi"), user("u2", "there")); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	var got []string
	for _, e := range rec.all() {
		switch e.Kind {
		case harness.EventInput:
			got = append(got, "input:"+e.Message.ID)
		case harness.EventMessageStart, harness.EventTurnEnd, harness.EventRunEnd:
			got = append(got, string(e.Kind))
		}
	}
	if strings.Join(got, ",") != "input:u1,input:u2,message_start,turn_end,input:n1,message_start,turn_end,run_end" {
		t.Fatalf("order %v", got)
	}
}
