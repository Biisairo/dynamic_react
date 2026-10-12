package harness_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

func replyU(id string, u *contract.Usage, stop contract.StopKind, blocks ...contract.Block) []contract.StreamEvent {
	m := contract.Message{ID: id, Role: contract.RoleAssistant, Timestamp: ts, Content: blocks,
		Usage: u, StopReason: &contract.StopReason{Kind: stop}}
	return []contract.StreamEvent{{Kind: contract.EventStart}, {Kind: contract.EventDone, Message: &m}}
}

// usageModel 은 요청마다 us[i] 의 Usage 로 답한다. 마지막 것만 끝내고 나머지는 t1 을 부른다. run 마다 처음부터.
func usageModel(us ...*contract.Usage) *fakeModel {
	return &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		i := n % len(us)
		if i == len(us)-1 {
			return replyU(fmt.Sprint("a", n), us[i], contract.StopStop, text("done"))
		}
		return replyU(fmt.Sprint("a", n), us[i], contract.StopToolUse, call(fmt.Sprint("c", n), "t1", `{}`))
	}}
}

func stats(t *testing.T, a *harness.Agent, sid string) harness.Stats {
	t.Helper()
	st, ok := a.Stats(sid)
	if !ok {
		t.Fatalf("no stats for %s", sid)
	}
	return st
}

var threeCalls = []*contract.Usage{
	{Input: 100, Output: 5},
	{Input: 20, CacheRead: 100, Output: 5}, // prompt 120, 상한 100
	{Input: 10, CacheRead: 200, Output: 7}, // prompt 210, 상한 120 → 자름
}

// HN-FR-79~83: 한 run, 다음 run, 다른 세션, 기록 없음.
func TestStatsRuns(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, usageModel(threeCalls...), rec)
	if _, ok := a.Stats("s1"); ok {
		t.Fatal("stats before any request")
	}
	start(t, a, "s1", user("u1", "go"))
	st := stats(t, a, "s1")
	want := harness.UsageTotals{Requests: 3, Prompt: 430, CacheRead: 300, Ceiling: 220, CeilingRead: 220}
	if st.LastRun != want || st.Session != want {
		t.Fatalf("run1 %+v", st)
	}
	if st.Model.Provider != "p" || st.Model.ID != "m" || st.Context != 217 || st.Window != 100000 || st.Threshold != 100000-16384 {
		t.Fatalf("context %+v", st)
	}
	start(t, a, "s1", user("u2", "again"))
	st = stats(t, a, "s1")
	// run2 첫 요청의 상한 = run1 마지막 prompt 210, CacheRead 0
	run2 := harness.UsageTotals{Requests: 3, Prompt: 430, CacheRead: 300, Ceiling: 210 + 100 + 120, CeilingRead: 0 + 100 + 120}
	if st.LastRun != run2 {
		t.Fatalf("run2 last %+v", st.LastRun)
	}
	if s := st.Session; s.Requests != 6 || s.Prompt != 860 || s.Ceiling != 220+430 || s.CeilingRead != 220+220 {
		t.Fatalf("run2 session %+v", s)
	}
	start(t, a, "s2", user("u3", "other"))
	if st := stats(t, a, "s2"); st.Session != want {
		t.Fatalf("s2 %+v", st.Session)
	}
}

// HN-FR-82: 설정의 reserve, 압축 꺼짐.
func TestStatsThreshold(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, usageModel(&contract.Usage{Input: 10, Output: 1}), rec)
	run := func(sid, cfg string) harness.Stats {
		r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: sid, Input: []contract.Message{user("u-"+sid, "hi")}, Config: json.RawMessage(cfg)})
		if err != nil {
			t.Fatal(err)
		}
		wait(t, r)
		return stats(t, a, sid)
	}
	if st := run("s1", `{"model":{"provider":"p","id":"m"},"compaction":{"reserveTokens":5000}}`); st.Threshold != 95000 {
		t.Fatalf("reserve %+v", st)
	}
	if st := run("s2", `{"model":{"provider":"p","id":"m"},"compaction":{"enabled":false}}`); st.Threshold != 0 || st.Window != 100000 {
		t.Fatalf("disabled %+v", st)
	}
}

// HN-FR-82: 압축이 켜져 있어도 요약기가 없거나 Window 가 0 이면 0.
func TestStatsThresholdOff(t *testing.T) {
	a := newAgent(t, envtest.New("/w", "/h"), usageModel(&contract.Usage{Input: 10, Output: 1}), func(o *harness.Options) {
		o.OnEvent = (&recorder{}).on
		o.Summarizer = func(sets.Summarizer) sets.Summarizer { return nil }
	})
	start(t, a, "s1", user("u1", "hi"))
	if st := stats(t, a, "s1"); st.Threshold != 0 || st.Window != 100000 {
		t.Fatalf("no summarizer %+v", st)
	}
	unknown := inject.FunctionSet{Name: "route", Description: "test route",
		ModelSelect: func(ctx *inject.ModelSelectCtx, req contract.Request) (contract.ModelInfo, []inject.ModelSelectSignal, error) {
			return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "x"}, MaxOutputTokens: 500}, nil, nil
		}}
	b := prefixAgent(t, usageModel(&contract.Usage{Input: 10, Output: 1}), &recorder{}, harness.SetEntry{Set: unknown, On: true})
	start(t, b, "s1", user("u1", "hi"))
	if st := stats(t, b, "s1"); st.Threshold != 0 || st.Window != 0 {
		t.Fatalf("no window %+v", st)
	}
}

// HN-FR-81: 모델 A → B → A. B 첫 요청은 상한 제외, A 로 돌아오면 A 의 직전 prompt 가 상한.
func TestStatsModelSwitch(t *testing.T) {
	var calls atomic.Int32
	pick := inject.FunctionSet{Name: "route", Description: "test route",
		ModelSelect: func(ctx *inject.ModelSelectCtx, req contract.Request) (contract.ModelInfo, []inject.ModelSelectSignal, error) {
			id := "A"
			if calls.Add(1) == 2 {
				id = "B"
			}
			return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: id, API: "x"}, ContextWindow: 50000, MaxOutputTokens: 500}, nil, nil
		}}
	rec := &recorder{}
	a := prefixAgent(t, usageModel(threeCalls...), rec, harness.SetEntry{Set: pick, On: true})
	start(t, a, "s1", user("u1", "go"))
	st := stats(t, a, "s1")
	// 1: A 첫 요청(제외), 2: B 첫 요청(제외), 3: A, 상한 = A 의 직전 100
	if st.LastRun.Ceiling != 100 || st.LastRun.CeilingRead != 100 || st.Model.ID != "A" || st.Window != 50000 {
		t.Fatalf("%+v", st)
	}
}

// HN-FR-79: Usage 가 없는 응답은 세지 않는다.
func TestStatsNoUsage(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, usageModel(nil), rec)
	start(t, a, "s1", user("u1", "go"))
	if _, ok := a.Stats("s1"); ok {
		t.Fatal("counted a response without usage")
	}
}

// HN-FR-83: 동시 run·조회 (-race).
func TestStatsConcurrent(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, usageModel(&contract.Usage{Input: 10, Output: 1}), rec)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		sid := fmt.Sprint("s", i)
		go func() { defer wg.Done(); start(t, a, sid, user("u"+sid, "go")) }()
		go func() { defer wg.Done(); a.Stats(sid) }()
	}
	wg.Wait()
	for i := range 4 {
		if st := stats(t, a, fmt.Sprint("s", i)); st.Session.Requests != 1 {
			t.Fatalf("s%d %+v", i, st)
		}
	}
}
