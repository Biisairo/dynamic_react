package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
)

// 자동 압축은 끈 설정 — 수동 압축은 Enabled 와 무관해야 한다 (HN-FR-36).
const compactCfg = `{"model":{"provider":"p","id":"m"},"compaction":{"enabled":false,"keepRecentTokens":10}}`

func isSummary(req contract.Request) bool {
	return len(req.System) == 1 && req.System[0].Text == summary.SystemPrompt
}

// compactModel 은 요약 요청에는 sumReply, 그 밖에는 "ok" 로 답한다.
func compactModel(sumReply func() []contract.StreamEvent) *fakeModel {
	return &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if isSummary(req) {
			return sumReply()
		}
		return reply("a"+string(rune('0'+n)), contract.StopStop, text("ok"))
	}}
}

func okSummary() []contract.StreamEvent {
	return reply("sum", contract.StopStop, text("## Goal\nsummarized"))
}

func longRun(t *testing.T, a *harness.Agent, sid string) {
	t.Helper()
	long := func(id string) contract.Message { return user(id, strings.Repeat("abcd", 50)) }
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: sid,
		Input: []contract.Message{long("u1"), long("u2"), long("u3")}, Config: json.RawMessage(compactCfg)})
	if err != nil {
		t.Fatal(err)
	}
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
}

func editLines(t *testing.T, e *envtest.Env, sid string) int {
	t.Helper()
	data, err := e.ReadFile(context.Background(), "/h/sessions/"+sid+".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), `"type":"edit"`)
}

// HN-FR-36: 긴 세션 → 편집 저장, 다음 run 은 요약을 보고, 편집 줄은 중복되지 않는다.
func TestCompactManual(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(okSummary)
	rec := &recorder{}
	a := newAgent(t, e, m, func(o *harness.Options) { o.OnEvent = rec.on })
	longRun(t, a, "s1")
	if n := editLines(t, e, "s1"); n != 0 {
		t.Fatalf("auto compaction ran: %d edits", n)
	}
	ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg))
	if err != nil || !ok {
		t.Fatalf("compact ok=%v err=%v", ok, err)
	}
	if n := editLines(t, e, "s1"); n != 1 {
		t.Fatalf("edit lines %d", n)
	}
	var phases []string
	for _, ev := range rec.all() {
		if ev.Kind == harness.EventCompaction {
			if ev.RunID != "" || ev.SessionID != "s1" || ev.Err != nil {
				t.Fatalf("compaction event %+v", ev)
			}
			phases = append(phases, ev.Phase)
		}
	}
	if strings.Join(phases, ",") != "start,end" {
		t.Fatalf("phases %v", phases)
	}
	reqs := m.requests()
	sum := reqs[len(reqs)-1]
	if !isSummary(sum) || sum.Options.MaxOutputTokens != 500 {
		t.Fatalf("summary request %+v", sum.Options)
	}

	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1",
		Input: []contract.Message{user("u4", "next")}, Config: json.RawMessage(compactCfg)})
	if err != nil {
		t.Fatal(err)
	}
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	reqs = m.requests()
	next := reqs[len(reqs)-1]
	if !strings.Contains(next.Messages[0].Content[0].Text, "## Goal\nsummarized") {
		t.Fatalf("next request does not start with summary: %+v", next.Messages[0])
	}
	if n := editLines(t, e, "s1"); n != 1 {
		t.Fatalf("edit duplicated: %d", n)
	}
}

// HN-FR-36: 압축할 구간이 없으면 false, 요약기를 부르지 않는다.
func TestCompactNothing(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(okSummary)
	a := newAgent(t, e, m, func(*harness.Options) {})
	if ok, err := a.Compact(context.Background(), "none", json.RawMessage(compactCfg)); ok || err != nil {
		t.Fatalf("empty session ok=%v err=%v", ok, err)
	}
	start(t, a, "short", user("u1", "hi"))
	if ok, err := a.Compact(context.Background(), "short", json.RawMessage(cfg)); ok || err != nil {
		t.Fatalf("short session ok=%v err=%v", ok, err)
	}
	for _, r := range m.requests() {
		if isSummary(r) {
			t.Fatal("summarizer called")
		}
	}
}

// HN-FR-36: 요약 실패 → 에러, 저장 없음. end 이벤트에 에러.
func TestCompactSummaryFails(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(func() []contract.StreamEvent {
		return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassInvalidRequest, Message: "bad"}}}
	})
	rec := &recorder{}
	a := newAgent(t, e, m, func(o *harness.Options) { o.OnEvent = rec.on })
	longRun(t, a, "s1")
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); ok || err == nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if n := editLines(t, e, "s1"); n != 0 {
		t.Fatalf("edit saved: %d", n)
	}
	evs := rec.all()
	if last := evs[len(evs)-1]; last.Kind != harness.EventCompaction || last.Phase != "end" || last.Err == nil {
		t.Fatalf("last event %+v", last)
	}
}

// HN-FR-36: 잘못된 설정·ID 는 에러.
func TestCompactValidation(t *testing.T) {
	a := newAgent(t, envtest.New("/w", "/h"), compactModel(okSummary), func(*harness.Options) {})
	if _, err := a.Compact(context.Background(), "s1", json.RawMessage(`{`)); err == nil {
		t.Fatal("bad config accepted")
	}
	if _, err := a.Compact(context.Background(), "s1", json.RawMessage(`{}`)); err == nil {
		t.Fatal("config without model accepted")
	}
	if _, err := a.Compact(context.Background(), "../x", json.RawMessage(compactCfg)); err == nil {
		t.Fatal("bad id accepted")
	}
}

// HN-FR-36: OnEvent panic(start 또는 end) → ErrPanic 을 감싼 에러, 저장 없음.
func TestCompactOnEventPanic(t *testing.T) {
	for _, phase := range []string{"start", "end"} {
		e := envtest.New("/w", "/h")
		armed := false
		a := newAgent(t, e, compactModel(okSummary), func(o *harness.Options) {
			o.OnEvent = func(ev harness.Event) {
				if armed && ev.Kind == harness.EventCompaction && ev.Phase == phase {
					panic("boom")
				}
			}
		})
		longRun(t, a, "s1")
		armed = true
		if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); ok || !errors.Is(err, core.ErrPanic) {
			t.Fatalf("%s: ok=%v err=%v", phase, ok, err)
		}
		if n := editLines(t, e, "s1"); n != 0 {
			t.Fatalf("%s: edit saved", phase)
		}
	}
}

// HN-FR-36: 두 번째 압축은 투영(앞 요약 포함)을 쓴다 — 앞 요약이 Previous 로 요약 요청에 들어간다.
func TestCompactUsesProjection(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(okSummary)
	a := newAgent(t, e, m, func(*harness.Options) {})
	longRun(t, a, "s1")
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); !ok || err != nil {
		t.Fatalf("first ok=%v err=%v", ok, err)
	}
	longRun(t, a, "s1")
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); !ok || err != nil {
		t.Fatalf("second ok=%v err=%v", ok, err)
	}
	reqs := m.requests()
	sum := reqs[len(reqs)-1]
	var body strings.Builder
	for _, msg := range sum.Messages {
		for _, b := range msg.Content {
			body.WriteString(b.Text)
		}
	}
	if !isSummary(sum) || !strings.Contains(body.String(), "summarized") {
		t.Fatalf("second summary request lacks previous summary: %q", body.String())
	}
}

// HN-FR-36: 설정 reserveTokens 가 요약 한도에 쓰인다 (min(0.8×1000, …) = 800).
func TestCompactReserveFromConfig(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(okSummary)
	a := newAgent(t, e, m, func(o *harness.Options) { o.Model = unknownModel{m} })
	longRun(t, a, "s1")
	c := `{"model":{"provider":"p","id":"m"},"compaction":{"reserveTokens":1000,"keepRecentTokens":10}}`
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(c)); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	reqs := m.requests()
	if got := reqs[len(reqs)-1].Options.MaxOutputTokens; got != 800 {
		t.Fatalf("max tokens %d", got)
	}
}

type unknownModel struct{ *fakeModel }

func (unknownModel) Lookup(context.Context, string, string) (contract.ModelInfo, bool) {
	return contract.ModelInfo{}, false
}

// HN-FR-36: 모르는 모델 → Ref 만 (ContextWindow 0 → reserve 16384, 한도 13107). NewID nil → 무작위 ID.
func TestCompactUnknownModel(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := compactModel(okSummary)
	a := newAgent(t, e, m, func(o *harness.Options) { o.Model = unknownModel{m}; o.NewID = nil })
	longRun(t, a, "s1")
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); !ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	reqs := m.requests()
	sum := reqs[len(reqs)-1]
	if sum.Model.Provider != "p" || sum.Model.ID != "m" || sum.Options.MaxOutputTokens != 13107 {
		t.Fatalf("summary request model %+v options %+v", sum.Model, sum.Options)
	}
	data, _ := e.ReadFile(context.Background(), "/h/sessions/s1.jsonl")
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var l struct {
		Edit struct {
			ID       string
			Messages []struct{ ID string }
		}
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &l); err != nil {
		t.Fatal(err)
	}
	hex := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex.MatchString(l.Edit.ID) || len(l.Edit.Messages) != 1 || !hex.MatchString(l.Edit.Messages[0].ID) || l.Edit.ID == l.Edit.Messages[0].ID {
		t.Fatalf("ids %+v", l.Edit)
	}
}

// HN-FR-36: 깨진 세션 파일 → 에러.
func TestCompactBrokenSession(t *testing.T) {
	e := envtest.New("/w", "/h")
	if err := e.WriteFile(context.Background(), "/h/sessions/s1.jsonl", []byte("{bad\n{}\n")); err != nil {
		t.Fatal(err)
	}
	a := newAgent(t, e, compactModel(okSummary), func(*harness.Options) {})
	if _, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); err == nil {
		t.Fatal("broken session accepted")
	}
}

// HN-FR-36: 저장 실패 → 에러 (요약 뒤 파일 쓰기가 실패하도록 요약 응답 시점에 고장을 건다).
func TestCompactAppendFails(t *testing.T) {
	e := envtest.New("/w", "/h")
	boom := errors.New("disk full")
	m := compactModel(func() []contract.StreamEvent {
		e.FailOn("/h/sessions/s1.jsonl", boom)
		return okSummary()
	})
	a := newAgent(t, e, m, func(*harness.Options) {})
	longRun(t, a, "s1")
	if ok, err := a.Compact(context.Background(), "s1", json.RawMessage(compactCfg)); ok || !errors.Is(err, boom) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
