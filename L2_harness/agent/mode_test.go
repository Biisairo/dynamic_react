package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

const modeRule = "MODE-RULE-9"

// withMode 는 모드 g(Push "g": context.inject 로 modeRule 을 붙이는 세트)를 등록한다.
func withMode(o *agent.Options) {
	o.Sets = func(d sets.Deps) []harness.SetEntry {
		g := inject.FunctionSet{Name: "g",
			ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
				msgs, sigs, err := ctx.Next()
				m := contract.Message{ID: "g-" + ctx.Flow().RunID(), Role: contract.RoleUser, Timestamp: time.Now(),
					Content: []contract.Block{{Kind: contract.BlockText, Text: modeRule}}}
				return append(msgs, m), sigs, err
			}}
		return append(harness.DefaultSets(d), harness.SetEntry{Set: g, On: false})
	}
	o.Modes = []harness.Mode{{Name: "g", Description: "mode g", Push: []string{"g"}}}
}

// AG-FR-47, 50
func TestCompleteAux(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("verdict"), textReply("plain"))
	f.writeConfig(t, strings.Replace(testConfig(l.URL, ""), `"id":"m"}}`, `"id":"m"},"aux":{"judge":{"provider":"test","id":"small"}}}`, 1))
	a, err := agent.New(context.Background(), f.opts())
	if err != nil {
		t.Fatal(err)
	}
	sys := []contract.Block{{Kind: contract.BlockText, Text: "judge rules"}}
	msgs := user("is it done?")
	got, err := a.Complete(context.Background(), "judge", sys, msgs)
	if err != nil || got.Content[0].Text != "verdict" {
		t.Fatalf("Complete %+v %v", got, err)
	}
	if _, err := a.Complete(context.Background(), "other", nil, msgs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(l.body(0), `"model":"small"`) || !strings.Contains(l.body(0), "judge rules") || strings.Contains(l.body(0), `"tools"`) {
		t.Fatalf("aux request %.300s", l.body(0))
	}
	if !strings.Contains(l.body(1), `"model":"m"`) {
		t.Fatalf("fallback request %.300s", l.body(1))
	}
	f.writeConfig(t, strings.Replace(testConfig(l.URL, ""), `"id":"m"}}`, `"id":"m"},"aux":{"judge":{"provider":""}}}`, 1))
	if _, err := agent.New(context.Background(), f.opts()); err == nil {
		t.Fatal("bad aux accepted")
	}
}

// AG-FR-48
func TestModesValidation(t *testing.T) {
	f := setup(t)
	f.writeConfig(t, testConfig("http://127.0.0.1:1", ""))
	o := f.opts()
	withMode(&o)
	o.Modes = append(o.Modes, harness.Mode{Name: "bad", Push: []string{"ghost"}})
	if _, err := agent.New(context.Background(), o); err == nil || !strings.HasPrefix(err.Error(), "agent: ") {
		t.Fatalf("mode error: %v", err)
	}
}

// AG-FR-49: EnterMode → 다음 Start 부터, 재시작 뒤에도 유지, ExitMode → 기본
func TestEnterExitMode(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("one"), textReply("two"), textReply("three"))
	a := newAgent(t, f, testConfig(l.URL, ""), withMode)
	if got := a.Modes(); len(got) != 1 || got[0].Name != "g" {
		t.Fatalf("Modes %+v", got)
	}
	runOnce(t, a, "s1", "first")
	if err := a.EnterMode(context.Background(), "s1", "g", "the goal"); err != nil {
		t.Fatal(err)
	}
	runOnce(t, a, "s1", "second")
	if strings.Contains(l.body(0), modeRule) || !strings.Contains(l.body(1), modeRule) {
		t.Fatal("mode not applied from the next Start")
	}
	b := newAgent(t, f, testConfig(l.URL, ""), withMode)
	st, err := b.ModeStatus(context.Background(), "s1")
	if err != nil || st.Active != "g" || st.Arg != "the goal" {
		t.Fatalf("restored %+v %v", st, err)
	}
	if err := b.ExitMode(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	runOnce(t, b, "s1", "third")
	if strings.Contains(l.body(2), modeRule) {
		t.Fatal("mode still applied after ExitMode")
	}
}

// AG-FR-49, 51: 실행 중 ExitMode → 다음 turn.start 에서 기본, mode 도구는 Approver 를 거친다
func TestModeWhileRunning(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("mode", `{"name":"g"}`), toolReply("read", `{"path":"x"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	var a *agent.Agent
	o := f.opts()
	withMode(&o)
	ap := &approver{decide: func(ctx context.Context, r agent.ApprovalRequest) (agent.Decision, error) {
		if r.Call.Name == "read" {
			if err := a.ExitMode(ctx, r.SessionID); err != nil {
				t.Error(err)
			}
		}
		return agent.Decision{Kind: "allow"}, nil
	}}
	o.Approver = ap
	var err error
	if a, err = agent.New(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if s, _ := runOnce(t, a, "s1", "go"); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	asked := ap.asked()
	if len(asked) != 2 || asked[0].Call.Name != "mode" || asked[1].Call.Name != "read" {
		t.Fatalf("asked %+v", asked)
	}
	if strings.Contains(l.body(0), modeRule) || !strings.Contains(l.body(1), modeRule) || strings.Contains(l.body(2), modeRule) {
		t.Fatal("turn-boundary switching wrong")
	}

	f2 := setup(t)
	l2 := newLLM(t, toolReply("mode", `{"name":"g"}`), textReply("done"))
	d := newAgent(t, f2, testConfig(l2.URL, `,"permissions":{"deny":["mode"]}`), withMode)
	runOnce(t, d, "s1", "go")
	if !strings.Contains(l2.body(1), "denied by rule mode") || strings.Contains(l2.body(1), modeRule) {
		t.Fatalf("deny rule: %.400s", l2.body(1))
	}
}
