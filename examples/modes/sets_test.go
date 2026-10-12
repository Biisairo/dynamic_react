package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

const testCfg = `{"model":{"provider":"p","id":"m"}}`

// fakeModel 은 판정 요청(System 에 judgeMarker)과 작업 요청을 나눠 각자의 대본으로 답한다.
type fakeModel struct {
	mu     sync.Mutex
	work   []contract.Request
	judges []contract.Request
	worker func(n int, req contract.Request) contract.Message
	judge  func(n int) string
}

func (m *fakeModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	m.mu.Lock()
	var msg contract.Message
	if len(req.System) > 0 && strings.Contains(req.System[0].Text, judgeMarker) {
		n := len(m.judges)
		m.judges = append(m.judges, req)
		msg = assistant(m.judge(n))
	} else {
		n := len(m.work)
		m.work = append(m.work, req)
		msg = m.worker(n, req)
	}
	m.mu.Unlock()
	return func(yield func(contract.StreamEvent, error) bool) {
		yield(contract.StreamEvent{Kind: contract.EventDone, Message: &msg}, nil)
	}
}

func (m *fakeModel) Lookup(context.Context, string, string) (contract.ModelInfo, bool) {
	return contract.ModelInfo{}, false
}

func (m *fakeModel) snapshot() ([]contract.Request, []contract.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]contract.Request(nil), m.work...), append([]contract.Request(nil), m.judges...)
}

func assistant(s string, calls ...contract.Block) contract.Message {
	stop := contract.StopStop
	if len(calls) > 0 {
		stop = contract.StopToolUse
	}
	return contract.Message{ID: fmt.Sprint("a", time.Now().UnixNano()), Role: contract.RoleAssistant, Timestamp: time.Now(),
		Content: append([]contract.Block{{Kind: contract.BlockText, Text: s}}, calls...), Usage: &contract.Usage{},
		StopReason: &contract.StopReason{Kind: stop}}
}

func toolCall(id, name, args string) contract.Block {
	return contract.Block{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}}
}

func say(s string) func(int, contract.Request) contract.Message {
	return func(int, contract.Request) contract.Message { return assistant(s) }
}

func verdicts(vs ...string) func(int) string {
	return func(n int) string { return vs[min(n, len(vs)-1)] }
}

func has(req contract.Request, text string) bool {
	for _, m := range req.Messages {
		blocks := m.Content
		if m.Result != nil {
			blocks = append(blocks, m.Result.Content...)
		}
		for _, b := range blocks {
			if strings.Contains(b.Text, text) {
				return true
			}
		}
	}
	return false
}

func toolNames(req contract.Request) string {
	var out []string
	for _, t := range req.Tools {
		out = append(out, t.Name)
	}
	return strings.Join(out, ",")
}

type modeEvents struct {
	mu     sync.Mutex
	evs    []harness.ModeChange
	prefix []harness.PrefixBreak
}

func (r *modeEvents) on(ev harness.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev.Kind {
	case harness.EventMode:
		r.evs = append(r.evs, *ev.Mode)
	case harness.EventPrefixBreak:
		r.prefix = append(r.prefix, *ev.Prefix)
	}
}

func newHarness(t *testing.T, m *fakeModel, c config) (*harness.Agent, *modeEvents) {
	t.Helper()
	rec := &modeEvents{}
	a, err := harness.New(context.Background(), harness.Options{Env: envtest.New("/w", "/h"), Model: m, SessionDir: "/h/s",
		Sets: c.sets, Modes: c.modes(), OnEvent: rec.on})
	if err != nil {
		t.Fatal(err)
	}
	return a, rec
}

func enter(t *testing.T, a *harness.Agent, mode, arg string) {
	t.Helper()
	if err := a.RequestMode(context.Background(), "s1", harness.ModeRequest{To: mode, Arg: arg, Origin: harness.ModeUser}); err != nil {
		t.Fatal(err)
	}
}

func runOnce(t *testing.T, a *harness.Agent, text string) core.Settle {
	t.Helper()
	in := contract.Message{ID: fmt.Sprint("u", time.Now().UnixNano()), Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{in}, Config: json.RawMessage(testCfg)})
	if err != nil {
		t.Fatal(err)
	}
	return r.Wait()
}

func active(t *testing.T, a *harness.Agent) harness.ModeStatus {
	t.Helper()
	st, err := a.ModeStatus(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// MX-FR-01~04: 목표 주입 → continue → continuation → done → 이탈 → 다음 run 은 기본
func TestGoalLoop(t *testing.T) {
	m := &fakeModel{worker: say("working"), judge: verdicts(`{"verdict":"continue","reason":"not yet"}`, `{"verdict":"done","reason":"tests pass"}`)}
	a, rec := newHarness(t, m, config{goalTurns: 20})
	enter(t, a, "goal", "make tests pass")
	if s := runOnce(t, a, "go"); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	work, judges := m.snapshot()
	if len(work) != 2 || len(judges) != 2 {
		t.Fatalf("work %d judges %d", len(work), len(judges))
	}
	if !has(work[0], "[goal mode] Goal: make tests pass") || has(work[0], "[Continuing toward your standing goal]") {
		t.Fatal("first request lacks goal or has a continuation")
	}
	if !has(work[1], "[Continuing toward your standing goal]\nGoal: make tests pass\nJudge: not yet") {
		t.Fatal("continuation missing")
	}
	if !has(judges[0], "Goal:\nmake tests pass\n\nAgent's most recent response:\nworking") {
		t.Fatalf("judge prompt %+v", judges[0].Messages)
	}
	st := active(t, a)
	if st.Pending == nil || st.Pending.To != "" || st.Pending.Reason != "goal achieved: tests pass" {
		t.Fatalf("status %+v", st)
	}
	runOnce(t, a, "next")
	work, _ = m.snapshot()
	if has(work[2], "[goal mode]") {
		t.Fatal("goal still applied after exit")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.evs) != 2 || rec.evs[0].To != "goal" || rec.evs[1].From != "goal" || rec.evs[1].Origin != harness.ModeSet {
		t.Fatalf("mode events %+v", rec.evs)
	}
}

// MX-FR-02, 03: blocked → 이탈, 깨진 JSON → continue, 예산 → 이탈
func TestGoalStops(t *testing.T) {
	m := &fakeModel{worker: say("cannot"), judge: verdicts(`{"verdict":"blocked","reason":"no access"}`)}
	a, _ := newHarness(t, m, config{goalTurns: 20})
	enter(t, a, "goal", "G")
	runOnce(t, a, "go")
	if st := active(t, a); st.Pending == nil || st.Pending.Reason != "goal blocked: no access" {
		t.Fatalf("blocked %+v", st)
	}

	m = &fakeModel{worker: say("hmm"), judge: verdicts("not json at all")}
	a, _ = newHarness(t, m, config{goalTurns: 3})
	enter(t, a, "goal", "G")
	runOnce(t, a, "go")
	work, judges := m.snapshot()
	if len(work) != 3 || len(judges) != 3 {
		t.Fatalf("work %d judges %d", len(work), len(judges))
	}
	if st := active(t, a); st.Pending == nil || st.Pending.Reason != "goal turn budget 3/3" {
		t.Fatalf("budget %+v", st)
	}
}

// MX-FR-08: -check 가 실패하는 동안 판정 없이 verify 보고로 계속
func TestGoalCheckFirst(t *testing.T) {
	m := &fakeModel{judge: verdicts(`{"verdict":"done","reason":"ok"}`),
		worker: func(n int, req contract.Request) contract.Message {
			if n == 0 {
				return assistant("writing", toolCall("c1", "write", `{"path":"/w/a.txt","content":"a"}`))
			}
			return assistant("finished")
		}}
	var mu sync.Mutex
	checks := 0
	check := func(context.Context) (sets.CheckResult, error) {
		mu.Lock()
		defer mu.Unlock()
		checks++
		return sets.CheckResult{OK: checks > 1, Report: "FAIL x"}, nil
	}
	a, _ := newHarness(t, m, config{goalTurns: 20, check: check})
	enter(t, a, "goal", "G")
	runOnce(t, a, "go")
	work, judges := m.snapshot()
	if checks != 2 || len(judges) != 1 || len(work) != 3 || !has(work[2], "Verification failed (attempt 1 of 3)") {
		t.Fatalf("checks %d judges %d work %d", checks, len(judges), len(work))
	}
	if !has(judges[0], notesHeader+"\n- go") || has(judges[0], "Verification failed") {
		t.Fatalf("notes must hold user input only: %+v", judges[0].Messages)
	}
}

func steer(text string) contract.Message {
	return contract.Message{ID: "steer-" + text, Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

// MX-FR-02·03·11 (R38): 판정 턴에 사용자 입력이 있으면 판정을 미루고, 그 입력을 처리한 턴 뒤에 판정한다.
// 판정에는 진행 중 사용자 메시지가 들어가고 continuation 은 빠진다.
func TestGoalDefersToUserInput(t *testing.T) {
	runs := make(chan *core.Run, 1)
	m := &fakeModel{judge: verdicts(`{"verdict":"continue","reason":"more"}`, `{"verdict":"done","reason":"ok"}`),
		worker: func(n int, req contract.Request) contract.Message {
			if n == 0 {
				(<-runs).Input(steer("also update the README"))
				return assistant("first")
			}
			return assistant(fmt.Sprint("answer ", n))
		}}
	a, _ := newHarness(t, m, config{goalTurns: 20})
	enter(t, a, "goal", "G")
	in := contract.Message{ID: "u1", Role: contract.RoleUser, Timestamp: time.Now(), Content: []contract.Block{{Kind: contract.BlockText, Text: "go"}}}
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{in}, Config: json.RawMessage(testCfg)})
	if err != nil {
		t.Fatal(err)
	}
	runs <- r
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	work, judges := m.snapshot()
	if len(work) != 3 || len(judges) != 2 || !has(work[1], "also update the README") {
		t.Fatalf("work %d judges %d", len(work), len(judges))
	}
	if !has(judges[0], "Agent's most recent response:\nanswer 1") || !has(judges[0], notesHeader+"\n- go\n- also update the README") {
		t.Fatalf("first judge %+v", judges[0].Messages)
	}
	if has(judges[1], "[Continuing toward your standing goal]") || !has(judges[1], "- also update the README") {
		t.Fatalf("second judge %+v", judges[1].Messages)
	}
}

// MX-FR-11: Arg 가 바뀌면 모은 메시지를 비운다. 목표가 없으면 모으지 않는다. user 역할·텍스트가 있는 것만 모은다.
func TestGoalNotesPerArg(t *testing.T) {
	m := &fakeModel{worker: say("w"), judge: verdicts(`{"verdict":"done","reason":"ok"}`)}
	a, _ := newHarness(t, m, config{goalTurns: 20})
	runOnce(t, a, "before goal")
	enter(t, a, "goal", "A")
	img := contract.Message{ID: "img", Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}}}
	sys := contract.Message{ID: "sys", Role: contract.RoleSystem, Timestamp: time.Now(), Content: []contract.Block{{Kind: contract.BlockText, Text: "system note"}}}
	first := contract.Message{ID: "first", Role: contract.RoleUser, Timestamp: time.Now(), Content: []contract.Block{{Kind: contract.BlockText, Text: "first"}}}
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{img, sys, first}, Config: json.RawMessage(testCfg)})
	if err != nil {
		t.Fatal(err)
	}
	r.Wait()
	enter(t, a, "goal", "B")
	runOnce(t, a, "second")
	_, judges := m.snapshot()
	if len(judges) != 2 || has(judges[0], "before goal") || has(judges[0], "system note") || !has(judges[0], notesHeader+"\n- first") {
		t.Fatalf("judges %d %+v", len(judges), judges)
	}
	if has(judges[1], "- first") || !has(judges[1], notesHeader+"\n- second") {
		t.Fatalf("second goal %+v", judges[1].Messages)
	}
}

// MX-FR-03: 메시지당 500자(넘으면 …), 줄 합 2000 rune, 오래된 것부터 뺌, 없으면 빈 문자열.
func TestNotesBlock(t *testing.T) {
	if got := notesBlock(nil); got != "" {
		t.Fatalf("empty %q", got)
	}
	long := strings.Repeat("가", 600)
	got := notesBlock([]string{"old", long})
	if want := "\n\n" + notesHeader + "\n- old\n- " + strings.Repeat("가", 500) + "…"; got != want {
		t.Fatalf("cut %q", got)
	}
	exact := strings.Repeat("x", 500)
	msgs := []string{"oldest", exact, exact, exact, strings.Repeat("y", 489)} // 줄 502·502·502·491 = 1997
	got = notesBlock(msgs)
	if strings.Contains(got, "oldest") || strings.Count(got, "\n- ") != 4 || strings.Contains(got, "…") {
		t.Fatalf("budget %q", got)
	}
	if got := notesBlock([]string{"a", exact, exact, exact, exact}); strings.Contains(got, "- a") || strings.Count(got, "\n- ") != 3 {
		t.Fatalf("older lines go once one does not fit: %q", got) // 1506 + 502 > 2000
	}
	if got := notesBlock([]string{"b", exact, exact, exact, strings.Repeat("y", 489)}); !strings.Contains(got, "\n- b\n") {
		t.Fatalf("fits at 2000: %q", got) // 3 + 1997 = 2000
	}
	if got := notesBlock([]string{"a", exact, exact, exact, strings.Repeat("y", 492)}); strings.Contains(got, "\n- a\n") {
		t.Fatalf("over 2000: %q", got) // 3 + 2000
	}
}

// MX-FR-05~07: 지시에 막힌 도구, 쓰기 도구 차단, 도구 목록 유지(R29), 이탈 → 다음 run 은 기본
func TestPlanMode(t *testing.T) {
	m := &fakeModel{judge: verdicts(`{}`),
		worker: func(n int, req contract.Request) contract.Message {
			if n == 1 {
				return assistant("try", toolCall("c1", "write", `{"path":"/w/a.txt","content":"a"}`))
			}
			return assistant("1. do it")
		}}
	a, rec := newHarness(t, m, config{goalTurns: 20})
	runOnce(t, a, "hi")
	enter(t, a, "plan", "add login")
	runOnce(t, a, "go")
	if st := active(t, a); st.Pending == nil || st.Pending.Reason != "plan done" {
		t.Fatalf("plan status %+v", st)
	}
	runOnce(t, a, "now do it")
	work, _ := m.snapshot()
	if len(work) != 4 {
		t.Fatalf("requests %d", len(work))
	}
	tools := toolNames(work[0])
	if !strings.Contains(tools, "write") || !strings.Contains(tools, "edit") || !strings.Contains(tools, "bash") ||
		toolNames(work[1]) != tools || toolNames(work[2]) != tools || toolNames(work[3]) != tools {
		t.Fatalf("tools %s / %s / %s / %s", tools, toolNames(work[1]), toolNames(work[2]), toolNames(work[3]))
	}
	if !has(work[1], "[plan mode] Plan only") || !has(work[1], "Blocked tools in this mode: bash, edit, write.") ||
		!has(work[1], "Task: add login") || !has(work[2], "blocked in plan mode") {
		t.Fatal("plan instruction or block missing")
	}
	if has(work[0], "[plan mode]") || has(work[3], "[plan mode]") {
		t.Fatal("plan applied outside the mode")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, p := range rec.prefix {
		if p.Part == harness.PrefixTools || p.Part == harness.PrefixSystem {
			t.Fatalf("prefix break %+v", p)
		}
	}
}
