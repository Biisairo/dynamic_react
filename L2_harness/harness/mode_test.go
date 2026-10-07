package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

// withModes 는 x(켬)·y(끔)·z(끔) 태그 세트와 모드 m(Push y, Off x)·n(Push x z)을 등록한다.
func withModes(extra ...harness.SetEntry) func(*harness.Options) {
	return func(o *harness.Options) {
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			out := append(harness.DefaultSets(d),
				harness.SetEntry{Set: tagSet("x"), On: true},
				harness.SetEntry{Set: tagSet("y"), On: false},
				harness.SetEntry{Set: tagSet("z"), On: false})
			return append(out, extra...)
		}
		o.Modes = []harness.Mode{
			{Name: "m", Description: "mode m", Push: []string{"y"}, Off: []string{"x"}},
			{Name: "n", Description: "mode n", Push: []string{"x", "z"}},
		}
	}
}

func status(t *testing.T, a *harness.Agent, sid string) harness.ModeStatus {
	t.Helper()
	st, err := a.ModeStatus(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func request(a *harness.Agent, sid, to string, o harness.ModeOrigin) error {
	return a.RequestMode(context.Background(), sid, harness.ModeRequest{To: to, Origin: o})
}

// HN-FR-55
func TestModeValidation(t *testing.T) {
	for name, modes := range map[string][]harness.Mode{
		"empty name":   {{Name: "", Push: []string{"y"}}},
		"duplicate":    {{Name: "m"}, {Name: "m"}},
		"unknown push": {{Name: "m", Push: []string{"ghost"}}},
		"unknown off":  {{Name: "m", Off: []string{"ghost"}}},
		"push twice":   {{Name: "m", Push: []string{"y", "y"}}},
		"push and off": {{Name: "m", Push: []string{"y"}, Off: []string{"y"}}},
		"bind fails":   {{Name: "m", Off: []string{"require"}}},
	} {
		o := options(envtest.New("/w", "/h"), textModel())
		withModes()(&o)
		o.Modes = modes
		if _, err := harness.New(context.Background(), o); err == nil || !strings.Contains(err.Error(), "harness:") {
			t.Errorf("%s: %v", name, err)
		}
	}
	o := options(envtest.New("/w", "/h"), textModel())
	o.Sets = func(d sets.Deps) []harness.SetEntry {
		return append(harness.DefaultSets(d), harness.SetEntry{Set: tagSet(harness.ModeNamespace), On: false})
	}
	if _, err := harness.New(context.Background(), o); err == nil {
		t.Error("reserved set name accepted")
	}
}

// HN-FR-56, 65: run 밖 예약 → 다음 Start 첫 요청부터 모드 배치, 이탈 → 기본 배치
func TestModeLayoutAtStart(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, withModes())
	if got := a.Modes(); len(got) != 2 || got[1].Name != "n" {
		t.Fatalf("Modes %+v", got)
	}
	start(t, a, "s1", user("u1", "hi"))
	for _, step := range []struct{ to, want string }{{"m", "y"}, {"n", "x,z"}, {"", "x"}} {
		if err := request(a, "s1", step.to, harness.ModeUser); err != nil {
			t.Fatal(err)
		}
		start(t, a, "s1", user("u", "hi"))
		reqs := m.requests()
		if got := tags(reqs[len(reqs)-1]); got != step.want {
			t.Fatalf("mode %q: tags %q, want %q", step.to, got, step.want)
		}
	}
	if got := tags(m.requests()[0]); got != "x" {
		t.Fatalf("default tags %q", got)
	}
}

// HN-FR-66~70: 모델이 mode 도구를 부른 턴은 그대로, 다음 turn.start 부터 모드 배치
func TestModeToolSwitchesNextTurn(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c0", "mode", `{"name":"m","arg":"goal text"}`))
		}
		return reply("a1", contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		withModes()(o)
		o.OnEvent = rec.on
	})
	start(t, a, "s1", user("u1", "hi"))
	reqs := m.requests()
	if len(reqs) != 2 || tags(reqs[0]) != "x" || tags(reqs[1]) != "y" {
		t.Fatalf("tags %q %q", tags(reqs[0]), tags(reqs[1]))
	}
	var result string
	for _, msg := range reqs[1].Messages {
		if msg.Result != nil && msg.Result.CallID == "c0" {
			result = msg.Result.Content[0].Text
		}
	}
	if result != `Mode "m" will apply from the next turn.` {
		t.Fatalf("tool result %q", result)
	}
	st := status(t, a, "s1")
	if st.Active != "m" || st.Arg != "goal text" || st.Pending != nil {
		t.Fatalf("status %+v", st)
	}
	var modes []harness.Event
	for _, ev := range rec.all() {
		if ev.Kind == harness.EventMode {
			modes = append(modes, ev)
		}
	}
	if len(modes) != 1 || modes[0].RunID == "" || modes[0].SessionID != "s1" ||
		*modes[0].Mode != (harness.ModeChange{From: "", To: "m", Arg: "goal text", Origin: harness.ModeModel}) {
		t.Fatalf("mode events %+v", modes)
	}
}

// HN-FR-68: 도구 spec, Modes 비면 도구 없음, ToolSet 으로 제거
func TestModeToolSpec(t *testing.T) {
	find := func(req contract.Request) *contract.ToolSpec {
		for i := range req.Tools {
			if req.Tools[i].Name == "mode" {
				return &req.Tools[i]
			}
		}
		return nil
	}
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, withModes())
	start(t, a, "s1", user("u1", "hi"))
	spec := find(m.requests()[0])
	if spec == nil || !strings.Contains(spec.Description, "- m: mode m") || !strings.Contains(spec.Description, "- n: mode n") ||
		!strings.Contains(string(spec.InputSchema), `"enum":["m","n"]`) {
		t.Fatalf("spec %+v", spec)
	}
	m = textModel()
	a = newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {})
	start(t, a, "s1", user("u1", "hi"))
	if find(m.requests()[0]) != nil {
		t.Fatal("mode tool without modes")
	}
	m = textModel()
	a = newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		withModes()(o)
		o.ToolSet = func(def []sets.Tool) []sets.Tool {
			var out []sets.Tool
			for _, t := range def {
				if t.Spec.Name != "mode" {
					out = append(out, t)
				}
			}
			return out
		}
	})
	start(t, a, "s1", user("u1", "hi"))
	if find(m.requests()[0]) != nil {
		t.Fatal("ToolSet could not remove mode")
	}
}

// exitSet 은 turn.finish 에서 아래 층이 settle 을 내면 모드 이탈을 요청한다 (판단기).
func exitSet(d sets.Deps) inject.FunctionSet {
	return inject.FunctionSet{Name: "judge",
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			sigs, err := ctx.Next()
			if err != nil {
				return sigs, err
			}
			for _, s := range sigs {
				if s.Kind() == core.SigSettle {
					if err := sets.ModesOf(d).Exit(ctx.Context(), ctx.Flow().SessionID(), "judged done"); err != nil {
						return nil, err
					}
				}
			}
			return sigs, nil
		}}
}

// HN-FR-62, 71: 세트 이탈 → 예약(set) → 다음 run 은 기본 배치. Current 는 활성 모드
func TestModeExitBySet(t *testing.T) {
	m := textModel()
	var cur sets.ModeState
	o := options(envtest.New("/w", "/h"), m)
	withModes()(&o)
	inner := o.Sets
	o.Sets = func(d sets.Deps) []harness.SetEntry {
		probe := inject.FunctionSet{Name: "probe",
			ContextMaintain: func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
				cur, _ = sets.ModesOf(d).Current(ctx.Context(), ctx.Flow().SessionID())
				return ctx.Next()
			}}
		return append(inner(d), harness.SetEntry{Set: exitSet(d), On: false}, harness.SetEntry{Set: probe, On: true})
	}
	o.Modes[0].Push = []string{"y", "judge"}
	a, err := harness.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RequestMode(context.Background(), "s1", harness.ModeRequest{To: "m", Arg: "A", Origin: harness.ModeUser}); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u1", "hi"))
	if cur != (sets.ModeState{Active: "m", Arg: "A"}) {
		t.Fatalf("Current in run %+v", cur)
	}
	st := status(t, a, "s1")
	if st.Active != "m" || st.Pending == nil || st.Pending.To != "" || st.Pending.Origin != harness.ModeSet || st.Pending.Reason != "judged done" {
		t.Fatalf("status after judge %+v", st)
	}
	start(t, a, "s1", user("u2", "again"))
	reqs := m.requests()
	if got := tags(reqs[len(reqs)-1]); got != "x" {
		t.Fatalf("after exit tags %q", got)
	}
	if st := status(t, a, "s1"); st.Active != "" || st.Pending != nil {
		t.Fatalf("status after exit %+v", st)
	}
}

// HN-FR-61~63: 권한 표
func TestModePermissions(t *testing.T) {
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), withModes())
	denied := func(err error) bool { return errors.Is(err, harness.ErrModeDenied) }
	if err := request(a, "s1", "", harness.ModeModel); !denied(err) {
		t.Fatalf("model exit: %v", err)
	}
	if err := request(a, "s1", "", harness.ModeSet); !denied(err) {
		t.Fatalf("set exit in default: %v", err)
	}
	if err := request(a, "s1", "m", harness.ModeSet); !denied(err) {
		t.Fatalf("set enter: %v", err)
	}
	if err := request(a, "s1", "ghost", harness.ModeUser); !errors.Is(err, harness.ErrUnknownMode) {
		t.Fatalf("unknown: %v", err)
	}
	if err := request(a, "s1", "m", "robot"); err == nil {
		t.Fatal("unknown origin accepted")
	}
	if err := request(a, "", "m", harness.ModeUser); err == nil {
		t.Fatal("empty session accepted")
	}
	if err := request(a, "s1", "m", harness.ModeModel); err != nil {
		t.Fatalf("model enter: %v", err)
	}
	if err := request(a, "s1", "n", harness.ModeModel); !denied(err) {
		t.Fatalf("model enter with pending: %v", err)
	}
	if err := request(a, "s1", "", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	if st := status(t, a, "s1"); st.Pending != nil || st.Active != "" {
		t.Fatalf("user exit in default should cancel pending: %+v", st)
	}
	if err := request(a, "s1", "n", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u1", "hi"))
	if st := status(t, a, "s1"); st.Active != "n" || st.Pending != nil {
		t.Fatalf("applied %+v", st)
	}
	if err := request(a, "s1", "m", harness.ModeModel); !denied(err) {
		t.Fatalf("model enter in mode: %v", err)
	}
	if err := request(a, "s1", "n", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	if st := status(t, a, "s1"); st.Pending != nil {
		t.Fatalf("same-state request should be a no-op: %+v", st)
	}
	if err := request(a, "s1", "m", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	if err := request(a, "s1", "", harness.ModeSet); err != nil {
		t.Fatal(err)
	}
	if st := status(t, a, "s1"); st.Pending == nil || st.Pending.To != "m" || st.Pending.Origin != harness.ModeUser {
		t.Fatalf("user pending must win over set exit: %+v", st)
	}
	if err := request(a, "s1", "", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	if st := status(t, a, "s1"); st.Pending == nil || st.Pending.To != "" || st.Pending.Origin != harness.ModeUser {
		t.Fatalf("user exit in mode: %+v", st)
	}
}

// HN-FR-59, 60: 재시작 후 복원, 등록되지 않은 모드 → 기본·이벤트
func TestModePersistence(t *testing.T) {
	e := envtest.New("/w", "/h")
	a := newAgent(t, e, textModel(), withModes())
	if err := a.RequestMode(context.Background(), "s1", harness.ModeRequest{To: "m", Arg: "A", Origin: harness.ModeUser}); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u1", "hi"))
	if err := request(a, "s1", "n", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	b := newAgent(t, e, textModel(), withModes())
	st := status(t, b, "s1")
	if st.Active != "m" || st.Arg != "A" || st.Pending == nil || st.Pending.To != "n" {
		t.Fatalf("restored %+v", st)
	}
	rec := &recorder{}
	c := newAgent(t, e, textModel(), func(o *harness.Options) {
		withModes()(o)
		o.Modes = o.Modes[1:]
		o.OnEvent = rec.on
	})
	st = status(t, c, "s1")
	if st.Active != "" || st.Pending == nil || st.Pending.To != "n" {
		t.Fatalf("unknown active should reset to default: %+v", st)
	}
	evs := rec.all()
	if len(evs) != 1 || evs[0].Kind != harness.EventMode || evs[0].Mode.From != "m" || evs[0].Mode.Reason != "unknown mode m" {
		t.Fatalf("events %+v", evs)
	}
	if st := status(t, newAgent(t, e, textModel(), func(o *harness.Options) { withModes()(o); o.Modes = o.Modes[1:] }), "s1"); st.Active != "" {
		t.Fatalf("fix not saved: %+v", st)
	}
}

// counterSet 은 turn.start 마다 상태 count 를 1 늘리고 그 값을 seen 에 남긴다 (모든 배치에 켜짐).
func counterSet(mu *sync.Mutex, seen *[]int) inject.FunctionSet {
	return inject.FunctionSet{Name: "counter",
		TurnStart: func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
			var n int
			if v, ok := ctx.State().Get("count"); ok {
				json.Unmarshal(v, &n)
			}
			n++
			if err := ctx.State().Set("count", n); err != nil {
				return nil, nil, err
			}
			mu.Lock()
			*seen = append(*seen, n)
			mu.Unlock()
			return ctx.Next()
		}}
}

// HN-FR-57, 66: 턴 경계에서 바꿔도 세트 상태가 이어진다 (공유 RunTable)
func TestModeSwitchKeepsSetState(t *testing.T) {
	var mu sync.Mutex
	var seen []int
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c0", "mode", `{"name":"m"}`))
		}
		return reply("a1", contract.StopStop, text("done"))
	}}
	a := newAgent(t, envtest.New("/w", "/h"), m, withModes(harness.SetEntry{Set: counterSet(&mu, &seen), On: true}))
	start(t, a, "s1", user("u1", "hi"))
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("counter across switch %v", seen)
	}
	if got := tags(m.requests()[1]); got != "y" {
		t.Fatalf("switched tags %q", got)
	}
}

// failState 는 harness.mode 네임스페이스 저장만 실패시킨다.
type failState struct {
	harness.Store
	fail bool
}

func (f *failState) SaveState(ctx context.Context, sid, ns string, d inject.StateDelta) error {
	if f.fail && ns == harness.ModeNamespace {
		return errors.New("disk full")
	}
	return f.Store.SaveState(ctx, sid, ns, d)
}

// HN-FR-59, 64: 저장 실패 → 에러·상태 그대로
func TestModeSaveFailure(t *testing.T) {
	fs := &failState{}
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), func(o *harness.Options) {
		withModes()(o)
		o.Store = func(def harness.Store) harness.Store { fs.Store = def; return fs }
	})
	fs.fail = true
	if err := request(a, "s1", "m", harness.ModeUser); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("save failure: %v", err)
	}
	if st := status(t, a, "s1"); st.Pending != nil {
		t.Fatalf("status changed: %+v", st)
	}
	fs.fail = false
	if err := request(a, "s1", "m", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	fs.fail = true
	_, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")}, Config: json.RawMessage(cfg)})
	if err == nil {
		t.Fatal("Start ignored apply failure")
	}
	if st := status(t, a, "s1"); st.Active != "" || st.Pending == nil {
		t.Fatalf("apply failure changed status: %+v", st)
	}
}

// HN-FR-72
func TestComplete(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {})
	sys := []contract.Block{text("judge")}
	conf := json.RawMessage(`{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"p","id":"small"}}}`)
	got, err := a.Complete(context.Background(), conf, "judge", sys, []contract.Message{user("u1", "q")})
	if err != nil || got.Content[0].Text != "ok" {
		t.Fatalf("Complete %+v %v", got, err)
	}
	if _, err := a.Complete(context.Background(), conf, "other", nil, []contract.Message{user("u1", "q")}); err != nil {
		t.Fatal(err)
	}
	reqs := m.requests()
	if reqs[0].Model.ID != "small" || reqs[1].Model.ID != "m" || len(reqs[0].Tools) != 0 ||
		reqs[0].Options.MaxOutputTokens != 500 || len(reqs[0].System) != 1 || reqs[0].System[0].Text != "judge" {
		t.Fatalf("requests %+v", reqs)
	}
	if _, err := a.Complete(context.Background(), json.RawMessage(`{}`), "judge", nil, nil); err == nil {
		t.Fatal("bad config accepted")
	}
}
