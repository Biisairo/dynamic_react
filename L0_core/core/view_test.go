package core_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

func ids(ms []contract.Message) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return strings.Join(out, ",")
}

// CORE-FR-12, 28: 다른 goroutine 의 Input 은 다음 dispatch 의 View 에 보이고, 진행 중 dispatch 의 View 는 바뀌지 않는다.
func TestInputAndSnapshot(t *testing.T) {
	runCh := make(chan *core.Run, 1)
	var during, after int
	f := newFake().
		on(core.StepContextMaintain, func(c call) (core.Result, error) {
			r := <-runCh
			r.Input(userMsg("x1", "hey"))
			during = len(c.View.Inbox())
			return out(nil)
		}).
		on(core.StepContextInject, func(c call) (core.Result, error) {
			after = len(c.View.Inbox())
			return out([]contract.Message(nil))
		})
	r := core.New(f, core.Options{}).Start(context.Background(), core.RunInput{})
	runCh <- r
	r.Wait()
	if during != 0 || after != 1 {
		t.Fatalf("inbox during=%d after=%d", during, after)
	}
}

func TestInputAfterSettleIsSafe(t *testing.T) {
	r := core.New(newFake(), core.Options{}).Start(context.Background(), core.RunInput{})
	r.Wait()
	r.Input(userMsg("late", "x"))
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatal("second Wait differs")
	}
}

// CORE-FR-17a 대신 IN-FR-17 의 기반: View 가 돌려준 슬라이스를 고쳐도 다른 dispatch 에 영향이 없다.
func TestViewReturnsCopies(t *testing.T) {
	f := newFake().
		on(core.StepRunStart, func(call) (core.Result, error) {
			return out(core.RunInit{History: []contract.Message{userMsg("h1", "old")}})
		}).
		on(core.StepTurnStart, func(c call) (core.Result, error) {
			h := c.View.History()
			h[0].ID = "mutated"
			tr := c.View.Transcript()
			tr[0].ID = "mutated"
			return out([]contract.Message(nil))
		})
	run(t, f)
	v := f.callsOf(core.StepContextMaintain)[0].View
	if v.History()[0].ID != "h1" || v.Transcript()[0].ID != "h1" {
		t.Fatal("view slice shared")
	}
}

// CORE-FR-29, 31a: run 기록 누적 시점.
func TestTranscript(t *testing.T) {
	calls := []contract.ToolCall{tc("c1", "read", `{}`)}
	f := withTools(newFake(), calls...).
		on(core.StepRunStart, func(call) (core.Result, error) {
			return out(core.RunInit{History: []contract.Message{userMsg("h1", "old")}})
		}).
		on(core.StepTurnStart, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out([]contract.Message{userMsg("u1", "go")})
			}
			return out([]contract.Message(nil))
		}).
		on(core.StepModelCall, func(c call) (core.Result, error) {
			switch c.N {
			case 0:
				return out(streamOf(toolResponse("bad")).cs)
			case 1:
				return out(streamOf(toolResponse("a1", calls...)).cs)
			}
			return out(streamOf(textResponse("a2")).cs)
		}).
		on(core.StepResponseClassify, func(c call) (core.Result, error) {
			if c.N == 0 {
				return out(nil, sig(core.SigRestartTurn))
			}
			return out(nil)
		})
	run(t, f)
	at := func(s core.Step, n int) string { return ids(f.callsOf(s)[n].View.Transcript()) }
	checks := []struct {
		step core.Step
		n    int
		want string
	}{
		{core.StepTurnStart, 0, "h1"},
		{core.StepContextMaintain, 0, "h1,u1"},
		{core.StepResponseCommit, 0, "h1,u1"}, // 턴1 호출 bad 는 classify 에서 restart → 미누적
		{core.StepToolsPlan, 0, "h1,u1,a1"},
		{core.StepToolsCommit, 0, "h1,u1,a1"},
		{core.StepTurnFinish, 0, "h1,u1,a1,tr-c1"},
		{core.StepRunSettled, 0, "h1,u1,a1,tr-c1,a2"},
	}
	for _, c := range checks {
		if got := at(c.step, c.n); got != c.want {
			t.Errorf("transcript at %s#%d = %s, want %s", c.step, c.n, got, c.want)
		}
	}
	if got := ids(f.callsOf(core.StepContextMaintain)[0].View.TurnAppended()); got != "u1" {
		t.Errorf("turn appended %s", got)
	}
	if got := ids(f.callsOf(core.StepContextMaintain)[1].View.TurnAppended()); got != "" {
		t.Errorf("turn appended not reset: %s", got)
	}
	if got := ids(f.callsOf(core.StepRunSettled)[0].View.History()); got != "h1" {
		t.Errorf("history %s", got)
	}
}

// CORE-FR-29: 나머지 사실들.
func TestViewFacts(t *testing.T) {
	calls := []contract.ToolCall{tc("c1", "read", `{}`)}
	proj := []contract.Message{userMsg("p1", "x")}
	eph := []contract.Message{userMsg("e1", "x")}
	f := withTools(newFake(), calls...).
		on(core.StepContextInject, func(call) (core.Result, error) { return out(eph) }).
		on(core.StepContextProject, func(call) (core.Result, error) { return out(proj) }).
		on(core.StepRequestFinalize, func(c call) (core.Result, error) {
			r := c.Input.(contract.Request)
			r.Options.MaxOutputTokens = 77
			return out(r)
		})
	cfg := json.RawMessage(`{"k":1}`)
	r := core.New(f, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s9", Config: cfg})
	r.Wait()

	v := f.callsOf(core.StepToolAfter)[0].View
	if v.SessionID() != "s9" || string(v.Config()) != `{"k":1}` || v.RunID() == "" {
		t.Errorf("ids %q %s %q", v.SessionID(), v.Config(), v.RunID())
	}
	if v.Step() != core.StepToolAfter || v.Unit() != core.UnitTool || v.Seq() <= 0 {
		t.Errorf("step %s unit %s seq %d", v.Step(), v.Unit(), v.Seq())
	}
	if ids(v.Ephemeral()) != "e1" || ids(v.Projection()) != "p1" {
		t.Errorf("ephemeral %s projection %s", ids(v.Ephemeral()), ids(v.Projection()))
	}
	if req, ok := v.Request(); !ok || req.Options.MaxOutputTokens != 77 || req.Model != modelInfo().Ref {
		t.Errorf("request %+v %v", req, ok)
	}
	if m, ok := v.Model(); !ok || m.Ref != modelInfo().Ref {
		t.Errorf("model %+v", m)
	}
	if p, ok := v.Plan(); !ok || len(p.Groups) != 1 {
		t.Errorf("plan %+v", p)
	}
	if cs := v.ToolCalls(); len(cs) != 1 || cs[0].ID != "c1" {
		t.Errorf("tool calls %+v", cs)
	}
	if v.Elapsed() <= 0 {
		t.Error("elapsed")
	}
	if rs := f.callsOf(core.StepToolsCommit)[0].View.ToolResults(); len(rs) != 1 || rs[0].CallID != "c1" {
		t.Errorf("tool results %+v", rs)
	}
	// 새 턴에서 턴 범위 사실은 초기화된다.
	v2 := f.callsOf(core.StepContextMaintain)[1].View
	if _, ok := v2.Response(); ok {
		t.Error("response not reset")
	}
	if _, ok := v2.Plan(); ok {
		t.Error("plan not reset")
	}
	if len(v2.ToolCalls()) != 0 || len(v2.Projection()) != 0 {
		t.Error("turn facts not reset")
	}
	// 첫 dispatch 는 아무것도 없다.
	v0 := f.callsOf(core.StepRunStart)[0].View
	if _, ok := v0.Request(); ok {
		t.Error("request before build")
	}
	if _, ok := v0.Model(); ok {
		t.Error("model before select")
	}
}

// CORE-FR-30, 31
func TestSignalRecordsAndCount(t *testing.T) {
	f := newFake().
		on(core.StepModelCall, func(c call) (core.Result, error) {
			if c.N < 2 {
				return out(rawStream(item{ev: errEvent(contract.ClassRateLimit)}).cs)
			}
			return defaultResult(c)
		}).
		on(core.StepCallError, func(c call) (core.Result, error) {
			return core.Result{Signals: []core.Signal{{Kind: core.SigRecall, Reason: "rate_limit"}}, Origin: "resilient"}, nil
		}).
		on(core.StepContextMaintain, func(c call) (core.Result, error) {
			return out(nil, core.Signal{Kind: core.SigRewrite, Edit: edit("e1"), Reason: "compact"})
		})
	run(t, f)
	v := f.callsOf(core.StepRunSettled)[0].View
	if n := v.SignalCount(core.SigRecall, "rate_limit"); n != 2 {
		t.Errorf("recall rate_limit = %d", n)
	}
	if n := v.SignalCount(core.SigRecall, ""); n != 2 {
		t.Errorf("recall any = %d", n)
	}
	if n := v.SignalCount(core.SigRecall, "overflow"); n != 0 {
		t.Errorf("recall overflow = %d", n)
	}
	if n := v.SignalCount(core.SigRewrite, "compact"); n != 1 {
		t.Errorf("rewrite = %d", n)
	}
	var rec *core.SignalRecord
	for _, r := range v.Signals() {
		if r.Step == core.StepCallError {
			rec = &r
			break
		}
	}
	if rec == nil || rec.Origin != "resilient" || rec.Unit != core.UnitCall || rec.Seq <= 0 || len(rec.Results) != 1 {
		t.Fatalf("record %+v", rec)
	}
	if es := v.Edits(); len(es) != 1 || es[0].ID != "e1" {
		t.Fatalf("edits %+v", es)
	}
}

func TestElapsedGrows(t *testing.T) {
	f := newFake().on(core.StepContextMaintain, func(call) (core.Result, error) {
		time.Sleep(10 * time.Millisecond)
		return out(nil)
	})
	run(t, f)
	if f.callsOf(core.StepContextInject)[0].View.Elapsed() < 10*time.Millisecond {
		t.Fatal("elapsed not measured from run start")
	}
}

// CORE-NFR-01
func TestImportsOnlyStdlibAndContract(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		first := strings.SplitN(p, "/", 2)[0]
		if !strings.Contains(first, ".") {
			continue
		}
		if p != "github.com/Biisairo/dynamic_react/L0_core/core" && p != "github.com/Biisairo/dynamic_react/L0_core/contract" {
			t.Errorf("forbidden dependency %s", p)
		}
	}
}
