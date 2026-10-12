package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

const goalRule = "GOAL-RULE-3"

func withGoalMode(o *agent.Options) {
	o.Sets = func(d sets.Deps) []harness.SetEntry {
		g := inject.FunctionSet{Name: "goal",
			ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
				msgs, sigs, err := ctx.Next()
				m := contract.Message{ID: "g-" + ctx.Flow().RunID(), Role: contract.RoleUser, Timestamp: time.Now(),
					Content: []contract.Block{{Kind: contract.BlockText, Text: goalRule}}}
				return append(msgs, m), sigs, err
			}}
		return append(harness.DefaultSets(d), harness.SetEntry{Set: g, On: false})
	}
	o.Modes = []harness.Mode{
		{Name: "goal", Description: "keep going until done", Push: []string{"goal"}},
		{Name: "plan", Description: "plan only"},
	}
}

// TM-FR-59
func TestModeLine(t *testing.T) {
	term := New(Options{Width: func() int { return 80 }})
	got := term.modeLine(harness.ModeChange{From: "", To: "goal", Origin: harness.ModeUser})
	if got != "── 모드 기본 → goal (user)" {
		t.Fatalf("%q", got)
	}
	got = term.modeLine(harness.ModeChange{From: "goal", To: "", Origin: harness.ModeSet, Reason: "goal achieved"})
	if got != "── 모드 goal → 기본 (set): goal achieved" {
		t.Fatalf("%q", got)
	}
}

// TM-FR-57~59
func TestRunModes(t *testing.T) {
	e := newEnv(t)
	e.mod = withGoalMode
	l := newLLM(t, textReply("one"), textReply("two"), toolReply("call_1", "ls", `{}`), textReply("three"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, ">") })
	h.typ("/modes\r")
	h.wait("  goal  keep going until done\n  plan  plan only\n  (지금: 기본 · 예약: 없음)")
	h.typ("/mode goal x\r")
	h.wait("✗ 첫 메시지를 보낸 뒤에 쓸 수 있습니다")
	h.typ("hi\r")
	h.wait("── completed")
	h.typ("/mode goal 테스트를 통과시켜\r")
	h.wait("── 모드 예약: goal (다음 메시지부터)")
	h.typ("/modes\r")
	h.wait("(지금: 기본 · 예약: goal)")
	h.typ("next\r")
	h.wait("── 모드 기본 → goal (user)")
	h.waitCount("── completed", 2)
	if !strings.Contains(l.body(1), goalRule) || strings.Contains(l.body(0), goalRule) {
		t.Fatal("goal mode not applied to the second message only")
	}
	h.typ("/modes\r")
	h.wait("* goal  keep going until done")
	h.wait(`(지금: goal "테스트를 통과시켜" · 예약: 없음)`)
	h.typ("/mode ghost\r")
	h.wait("✗ harness: unknown mode")
	h.typ("/mode\r")
	h.wait("✗ 사용법: /mode <name> [arg] | /mode off")

	// R36: 실행 중 /mode off 는 같은 run 의 다음 턴부터, /mode <name> 은 거부
	gate := l.block()
	h.typ("again\r")
	waitUntil(t, func() bool { return l.count() == 3 }) // 첫 turn.start 가 지나 goal 배치로 요청이 나간 뒤
	h.typ("/mode plan\r")
	h.wait("✗ 실행 중에는 모드를 바꿀 수 없습니다")
	h.typ("/mode off\r")
	h.wait("── 모드 예약: 기본 (다음 턴부터)")
	gate <- struct{}{}
	gate <- struct{}{}
	h.wait("ls 승인 필요")
	h.typ("y")
	h.wait("── 모드 goal → 기본 (user)")
	h.waitCount("── completed", 3)
	if l.count() != 4 || !strings.Contains(l.body(2), goalRule) || strings.Contains(l.body(3), goalRule) {
		t.Fatalf("off not applied at the next turn: %d requests", l.count())
	}
	h.idle()
	h.typ("/mode off\r")
	h.wait("── 모드 예약: 기본 (다음 메시지부터)")
	h.typ("/help\r")
	h.wait("/modes · /mode <name> [arg] · /mode off")
}

// TM-FR-57: 모드가 없으면 한 줄
func TestRunNoModes(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, ">") })
	h.typ("/modes\r")
	h.wait("── 등록된 모드 없음")
}
