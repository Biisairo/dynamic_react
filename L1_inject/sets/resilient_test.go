package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

const wide = `{"model":{"provider":"p","id":"m"},"maxOutputTokens":1000,"compaction":{"keepRecentTokens":10}}`

func infoW(w int) *fakeModel {
	return &fakeModel{info: map[string]contract.ModelInfo{"p/m": {Ref: contract.ModelRef{Provider: "p", ID: "m"}, ContextWindow: w, MaxOutputTokens: 500}}}
}

func long(id string, n int) contract.Message { return user(id, strings.Repeat("abcd", n)) }

// 종단: overflow → 압축 → 재호출 (ST-NFR-02)
func TestOverflowCompactRecall(t *testing.T) {
	m := infoW(100000)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return errEv(contract.CallError{Class: contract.ClassOverflow, Message: "prompt is too long"})
		}
		return doneEv(assistant("a", contract.Usage{Input: 10, Output: 1}, contract.StopStop, text("ok")))
	}
	sum := &fakeSummarizer{}
	d := deps(m)
	d.Summarizer = sum
	s := agent(t, d, true, wide, long("u1", 50), long("u2", 50), long("u3", 50))
	if s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	reqs := m.requests()
	if len(reqs) != 2 || len(sum.reqs) != 1 {
		t.Fatalf("calls %d summaries %d", len(reqs), len(sum.reqs))
	}
	if first := reqs[1].Messages[0]; !strings.HasPrefix(first.Content[0].Text, "summary of") && !strings.Contains(first.Content[0].Text, "summary of") {
		t.Fatalf("second request not compacted: %+v", reqs[1].Messages)
	}
	if len(reqs[1].Messages) >= len(reqs[0].Messages) {
		t.Fatal("not shorter")
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	if len(sess.Edits) != 1 || sess.Edits[0].Kind != contract.EditReplaceRange {
		t.Fatalf("edit not stored %+v", sess.Edits)
	}
}

func TestOverflowOnlyOncePerTurn(t *testing.T) {
	m := infoW(100000)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return errEv(contract.CallError{Class: contract.ClassOverflow})
	}
	d := deps(m)
	d.Summarizer = &fakeSummarizer{}
	s := agent(t, d, true, wide, long("u1", 50), long("u2", 50), long("u3", 50))
	if s.Reason != core.SettleFailed || len(m.requests()) != 2 {
		t.Fatalf("settle %s calls %d", s.Reason, len(m.requests()))
	}
}

func TestOverflowWithoutSummarizer(t *testing.T) {
	m := infoW(100000)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return errEv(contract.CallError{Class: contract.ClassOverflow})
	}
	if s := agent(t, deps(m), true, wide, user("u1", "x")); s.Reason != core.SettleFailed || len(m.requests()) != 1 {
		t.Fatalf("settle %s", s.Reason)
	}
}

// ST-FR-44: overflow 복구 표시는 같은 run·같은 턴에만 적용된다.
func TestOverflowRecoveredMarkScope(t *testing.T) {
	for _, c := range []struct {
		name string
		mark string
		want string
	}{
		{"same run and turn", `{"run":"r1","turn":1}`, "[fail]"},
		{"other turn", `{"run":"r1","turn":2}`, "[rewrite rebuild]"},
		{"other run", `{"run":"r0","turn":1}`, "[rewrite rebuild]"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			store := sets.NewMemoryStore()
			store.SaveState(ctx, "s1", "resilient", inject.StateDelta{Set: map[string]json.RawMessage{"recovered": json.RawMessage(c.mark)}})
			d := deps(infoW(100000))
			d.Summarizer = &fakeSummarizer{}
			set := injecttest.Fill(inject.FunctionSet{Name: "resilient", CallError: sets.ResilientCallError(d),
				RunStart: func(*inject.RunStartCtx, core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
					return core.RunInit{}, nil, nil
				}})
			disp, err := inject.BindWith(inject.BindOptions{StateStore: sets.StateStoreOf(store)}, set)
			if err != nil {
				t.Fatal(err)
			}
			f := injecttest.Facts{RunID: "r1", SessionID: "s1", Config: json.RawMessage(wide), Step: core.StepRunStart}
			if _, err := disp.DispatchSource(ctx, core.StepRunStart, core.RunInput{SessionID: "s1"}, f.View()); err != nil {
				t.Fatal(err)
			}
			f.Step, f.TurnIndex = core.StepCallError, 1
			f.Transcript = []contract.Message{long("u1", 50), long("u2", 50), long("u3", 50)}
			f.Model = &contract.ModelInfo{ContextWindow: 100000}
			res, err := disp.DispatchSource(ctx, core.StepCallError, contract.CallError{Class: contract.ClassOverflow}, f.View())
			if err != nil {
				t.Fatal(err)
			}
			if kinds(res.Signals) != c.want {
				t.Fatalf("signals %s", kinds(res.Signals))
			}
		})
	}
}

// 종단: 429 → 대기 → 재시도, 소진, RetryAfter 상한
func TestRetry(t *testing.T) {
	var waits []time.Duration
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		if n < 2 {
			return errEv(contract.CallError{Class: contract.ClassRateLimit, RetryAfter: time.Duration(n*90) * time.Second})
		}
		return doneEv(assistant("a", contract.Usage{Output: 1}, contract.StopStop, text("ok")))
	}
	d := deps(m)
	d.Sleep = func(ctx context.Context, dd time.Duration) error { waits = append(waits, dd); return nil }
	s := agent(t, d, true, cfgJSON, user("u1", "x"))
	if s.Reason != core.SettleCompleted || len(m.requests()) != 3 {
		t.Fatalf("settle %s calls %d", s.Reason, len(m.requests()))
	}
	// n=0: RetryAfter 0 → baseDelay 2s; n=1: RetryAfter 90s → 상한 60s
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 60*time.Second {
		t.Fatalf("waits %v", waits)
	}
}

func TestRetryExhaustedAndBackoff(t *testing.T) {
	var waits []time.Duration
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return errEv(contract.CallError{Class: contract.ClassTransientNetwork})
	}
	d := deps(m)
	d.Sleep = func(ctx context.Context, dd time.Duration) error { waits = append(waits, dd); return nil }
	s := agent(t, d, true, cfgJSON, user("u1", "x"))
	if s.Reason != core.SettleFailed || len(m.requests()) != 4 {
		t.Fatalf("settle %s calls %d", s.Reason, len(m.requests()))
	}
	if len(waits) != 3 || waits[0] != 2*time.Second || waits[1] != 4*time.Second || waits[2] != 8*time.Second {
		t.Fatalf("backoff %v", waits)
	}
}

func TestNonRetryableAndCanceledWait(t *testing.T) {
	for _, class := range []contract.ErrorClass{contract.ClassAuth, contract.ClassInvalidRequest, contract.ClassPayloadTooLarge, contract.ClassPermanent, contract.ClassUnknown} {
		m := infoW(0)
		m.script = func(n int, req contract.Request) []contract.StreamEvent {
			return errEv(contract.CallError{Class: class})
		}
		if s := agent(t, deps(m), true, cfgJSON, user("u1", "x")); s.Reason != core.SettleFailed || len(m.requests()) != 1 {
			t.Errorf("%s: %s calls %d", class, s.Reason, len(m.requests()))
		}
	}
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return errEv(contract.CallError{Class: contract.ClassOverloaded})
	}
	d := deps(m)
	d.Sleep = func(ctx context.Context, dd time.Duration) error { return context.Canceled }
	if s := agent(t, d, true, cfgJSON, user("u1", "x")); s.Reason != core.SettleFailed || len(m.requests()) != 1 {
		t.Fatalf("canceled wait: %s", s.Reason)
	}
}

// 13 classify: length 회복, silent overflow
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		msg  contract.Message
		want string
	}{
		{"recoverable length", assistant("a", contract.Usage{Output: 10}, contract.StopLength), "[rewrite rebuild]"},
		{"length at limit", assistant("a", contract.Usage{Output: 1000}, contract.StopLength), "[]"},
		{"silent overflow", assistant("a", contract.Usage{Input: 900, CacheRead: 200, Output: 1}, contract.StopStop), "[rewrite rebuild]"},
		{"normal", assistant("a", contract.Usage{Input: 10, Output: 1}, contract.StopStop), "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := deps(infoW(1000))
			d.Summarizer = &fakeSummarizer{}
			set := inject.FunctionSet{Name: "resilient", ResponseClassify: sets.ResilientResponseClassify(d)}
			req := contract.Request{Options: contract.Options{MaxOutputTokens: 1000}}
			model := contract.ModelInfo{ContextWindow: 1000}
			proj := []contract.Message{long("u1", 100), long("u2", 100), long("u3", 100)}
			f := injectFacts(proj, &c.msg, &req, &model)
			f.Config = json.RawMessage(wide)
			res, err := step(t, set, core.StepResponseClassify, nil, f)
			if err != nil {
				t.Fatal(err)
			}
			if kinds(res.Signals) != c.want {
				t.Fatalf("signals %s", kinds(res.Signals))
			}
		})
	}
}

// 3 maintain: 임계치 경계 (ST-FR-05)
func TestMaintainThreshold(t *testing.T) {
	// W=100000 → reserve = max(16384, 15000) = 16384 → 임계 83616
	for _, c := range []struct {
		used int
		want string
	}{{83616, "[]"}, {83617, "[rewrite next]"}} {
		d := deps(infoW(100000))
		d.Summarizer = &fakeSummarizer{}
		set := inject.FunctionSet{Name: "resilient", ContextMaintain: sets.ResilientContextMaintain(d)}
		a := assistant("a", contract.Usage{Input: c.used - 1, Output: 1}, contract.StopStop, text("x"))
		tr := []contract.Message{long("u1", 100), long("u2", 100), a}
		model := contract.ModelInfo{ContextWindow: 100000}
		f := injectFacts(tr, nil, nil, &model)
		f.Config = json.RawMessage(wide)
		res, err := step(t, set, core.StepContextMaintain, nil, f)
		if err != nil {
			t.Fatal(err)
		}
		if kinds(res.Signals) != c.want {
			t.Errorf("used %d: %s", c.used, kinds(res.Signals))
		}
	}
	// W=0, Summarizer 없음 → 아무것도 안 함
	d := deps(infoW(0))
	set := inject.FunctionSet{Name: "resilient", ContextMaintain: sets.ResilientContextMaintain(d)}
	res, _ := step(t, set, core.StepContextMaintain, nil, injectFacts(nil, nil, nil, &contract.ModelInfo{}))
	if len(res.Signals) != 0 {
		t.Fatal("W=0 acted")
	}
}

// ST-FR-06: 무효 압축 2회 연속 → blocked
func TestInvalidCompactionBlocks(t *testing.T) {
	m := infoW(20000) // reserve 16384 → 임계 3616
	calls := 0
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		calls++
		stop := contract.StopToolUse
		blocks := []contract.Block{toolCall(string(rune('a'+n)), "echo", `{"s":"x"}`)}
		if n >= 4 {
			stop, blocks = contract.StopStop, []contract.Block{text("done")}
		}
		a := assistant(string(rune('A'+n)), contract.Usage{Input: 9000, Output: 1}, stop, blocks...)
		a.Timestamp = ts.Add(time.Duration(n+1) * time.Hour) // 편집보다 뒤의 응답 → usage 신뢰 (ST-FR-22)
		return doneEv(a)
	}
	sum := &fakeSummarizer{}
	d := deps(m, echoTool(nil))
	d.Summarizer = sum
	var tick time.Duration
	d.Now = func() time.Time { tick += time.Second; return ts.Add(tick) }
	s := agent(t, d, true, `{"model":{"provider":"p","id":"m"},"maxOutputTokens":100,"compaction":{"keepRecentTokens":1}}`,
		long("u1", 50), long("u2", 50), long("u3", 50))
	if s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	if len(sum.reqs) != 2 {
		t.Fatalf("summaries %d, want 2 then blocked", len(sum.reqs))
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	if string(sess.State["resilient"].Values["compaction.blocked"]) != "true" {
		t.Fatalf("blocked flag %+v", sess.State["resilient"])
	}
}

// ST-FR-23, 24, 25, 27
func TestCompact(t *testing.T) {
	sum := &fakeSummarizer{}
	proj := []contract.Message{
		user("u1", strings.Repeat("abcd", 10)),
		assistant("a1", contract.Usage{}, contract.StopToolUse, toolCall("c1", "echo", `{}`)),
		resultMsg("r1", "c1", strings.Repeat("abcd", 10)),
		user("u2", strings.Repeat("abcd", 10)),
	}
	// keepRecent 12 → 끝에서 u2(10) + r1(10) 에서 넘음 → r1 은 절단점 불가 → a1 에서 자름
	edit, ok, err := sets.Compact(context.Background(), sum, contract.ModelRef{Provider: "p", ID: "m"}, proj, 12, 100000, 64000, ts, "e1", "s1")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if edit.From != "u1" || edit.To != "u1" || edit.Kind != contract.EditReplaceRange || len(edit.Messages) != 1 || edit.Validate() != nil {
		t.Fatalf("edit %+v", edit)
	}
	if r := sum.reqs[0]; len(r.Messages) != 1 || r.MaxTokens != 16384 || r.Previous != "" {
		t.Fatalf("summary request %+v", r)
	}
	// 요약 한도: min(0.8*reserve, max output, 16384)
	sets.Compact(context.Background(), sum, contract.ModelRef{}, proj, 12, 1000, 500, ts, "e2", "s2")
	if sum.reqs[1].MaxTokens != 500 {
		t.Fatalf("max tokens %d", sum.reqs[1].MaxTokens)
	}
	sets.Compact(context.Background(), sum, contract.ModelRef{}, proj, 12, 1000, 0, ts, "e3", "s3")
	if sum.reqs[2].MaxTokens != 800 {
		t.Fatalf("max tokens %d", sum.reqs[2].MaxTokens)
	}
	// 이전 요약이 구간 앞에 있으면 Previous
	withPrev := append([]contract.Message{edit.Messages[0]}, proj...)
	sets.Compact(context.Background(), sum, contract.ModelRef{}, withPrev, 12, 100000, 0, ts, "e4", "s4")
	if !strings.Contains(sum.reqs[3].Previous, "summary of") {
		t.Fatalf("previous %q", sum.reqs[3].Previous)
	}
	// 절단 구간이 비면 압축 안 함
	if _, ok, _ := sets.Compact(context.Background(), sum, contract.ModelRef{}, proj, 100000, 100000, 0, ts, "e5", "s5"); ok {
		t.Fatal("compacted empty range")
	}
	// 실패
	if _, _, err := sets.Compact(context.Background(), &fakeSummarizer{err: errors.New("x")}, contract.ModelRef{}, proj, 12, 100000, 0, ts, "e6", "s6"); err == nil {
		t.Fatal("summarizer error ignored")
	}
}

// 21 follow-up
func TestFollowUp(t *testing.T) {
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return doneEv(assistant(string(rune('a'+n)), contract.Usage{Output: 1}, contract.StopStop, text("ok")))
	}
	d := deps(m)
	runCh := make(chan *core.Run, 1)
	var once bool
	hook := sets.Base(d)
	orig := hook.StreamEnd
	wrapped := inject.FunctionSet{Name: "hook", StreamEnd: func(ctx *inject.StreamEndCtx, msg contract.Message) (core.StreamEndOut, []inject.StreamEndSignal, error) {
		if !once {
			once = true
			(<-runCh).Input(user("f1", "follow up"))
		}
		return orig(ctx, msg)
	}}
	disp, _ := inject.BindWith(inject.BindOptions{StateStore: sets.StateStoreOf(d.Store)}, sets.Base(d), sets.Resilient(d), wrapped)
	r := core.New(disp, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1", Input: []contract.Message{user("u1", "x")}, Config: json.RawMessage(cfgJSON)})
	runCh <- r
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	reqs := m.requests()
	if len(reqs) != 2 || reqs[1].Messages[len(reqs[1].Messages)-1].ID != "f1" {
		t.Fatalf("follow-up not delivered: %d", len(reqs))
	}
}
