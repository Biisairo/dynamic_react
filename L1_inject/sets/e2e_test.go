package sets_test

import (
	"context"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

func injectFacts(proj []contract.Message, resp *contract.Message, req *contract.Request, model *contract.ModelInfo) injecttest.Facts {
	return injecttest.Facts{Transcript: proj, Projection: proj, Response: resp, Request: req, Model: model}
}

// ST-NFR-02: 도구 2라운드 후 종료
func TestTwoToolRounds(t *testing.T) {
	var runs []string
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		switch n {
		case 0:
			return doneEv(assistant("a1", contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c1", "echo", `{"s":"one"}`)))
		case 1:
			return doneEv(assistant("a2", contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c2", "echo", `{"s":"two"}`)))
		}
		return doneEv(assistant("a3", contract.Usage{Output: 1}, contract.StopStop, text("done")))
	}
	d := deps(m, echoTool(&runs))
	s := agent(t, d, true, cfgJSON, user("u1", "go"))
	if s.Reason != core.SettleCompleted || len(runs) != 2 || runs[1] != "two" {
		t.Fatalf("settle %s runs %v", s.Reason, runs)
	}
	reqs := m.requests()
	last := reqs[2].Messages
	if len(last) != 5 || last[4].Result == nil || last[4].Result.Content[0].Text != "echo:two" {
		t.Fatalf("third request %+v", last)
	}
	if reqs[0].System[0].Text != "sys" || reqs[0].Model.ID != "m" || len(reqs[0].Tools) != 1 {
		t.Fatalf("request %+v", reqs[0])
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	if len(sess.Messages) != 6 { // u1, a1, r1, a2, r2, a3
		t.Fatalf("stored %d", len(sess.Messages))
	}
}

// ST-NFR-02: 세션 재개 시 짝 복구·편집 복원
func TestResume(t *testing.T) {
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return doneEv(assistant("b", contract.Usage{Output: 1}, contract.StopStop, text("ok")))
	}
	d := deps(m)
	ctx := context.Background()
	d.Store.AppendMessages(ctx, "s1", []contract.Message{
		user("u0", "old"),
		assistant("a0", contract.Usage{}, contract.StopToolUse, toolCall("c0", "echo", `{"s":"x"}`)),
	})
	d.Store.AppendEdits(ctx, "s1", []contract.ContextEdit{{ID: "e0", Kind: contract.EditHide, Timestamp: ts, Target: "u0"}})
	s := agent(t, d, true, cfgJSON, user("u1", "again"))
	if s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	msgs := m.requests()[0].Messages
	// u0 는 숨김, a0 뒤에 aborted 결과, 그리고 u1
	if len(msgs) != 3 || msgs[0].ID != "a0" || msgs[1].Result == nil || msgs[1].Result.Status != contract.StatusAborted || msgs[2].ID != "u1" {
		t.Fatalf("resumed request %+v", msgs)
	}
	if err := m.requests()[0].Validate(); err != nil {
		t.Fatal(err)
	}
	sess, _ := d.Store.Load(ctx, "s1")
	if len(sess.Edits) != 1 {
		t.Fatalf("restored edit stored again: %d", len(sess.Edits))
	}
}

func TestEmptyFirstTurnSettles(t *testing.T) {
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent { t.Fatal("model called"); return nil }
	if s := agent(t, deps(m), false, cfgJSON); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
}

func TestBadConfigFails(t *testing.T) {
	if s := agent(t, deps(infoW(0)), false, `{}`, user("u1", "x")); s.Reason != core.SettleFailed {
		t.Fatalf("settle %+v", s)
	}
}

// 설정 없이 Store 를 비워 둬도 메모리 Store 로 동작한다 (ST-FR-34).
func TestNilStoreAndTools(t *testing.T) {
	m := infoW(0)
	m.script = func(n int, req contract.Request) []contract.StreamEvent {
		return doneEv(assistant("a", contract.Usage{Output: 1}, contract.StopStop, text("ok")))
	}
	d := deps(m)
	d.Store, d.Tools = nil, nil
	if s := agent(t, d, true, cfgJSON, user("u1", "x")); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
}
