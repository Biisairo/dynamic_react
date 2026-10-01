package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func msgLine(id string, role contract.Role, text string) string {
	m := contract.Message{ID: id, Role: role, Timestamp: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
	b, _ := json.Marshal(map[string]any{"type": "message", "message": m})
	return string(b) + "\n"
}

func editLine(e contract.ContextEdit) string {
	e.Timestamp = time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC)
	b, _ := json.Marshal(map[string]any{"type": "edit", "edit": e})
	return string(b) + "\n"
}

func newAgent(t *testing.T, f *fixture, cfg string, mod func(*agent.Options)) *agent.Agent {
	t.Helper()
	f.writeConfig(t, cfg)
	o := f.opts()
	if mod != nil {
		mod(&o)
	}
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func writeSession(t *testing.T, a *agent.Agent, sid, content string) {
	t.Helper()
	os.MkdirAll(a.SessionDir(), 0o755)
	if err := os.WriteFile(filepath.Join(a.SessionDir(), sid+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ids(ms []contract.Message) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return strings.Join(out, ",")
}

// AG-FR-30: 편집 적용 뒤 투영에서 원본 메시지만, 요약은 빼고 Omitted 로.
func TestHistory(t *testing.T) {
	f := setup(t)
	a := newAgent(t, f, testConfig("http://127.0.0.1:1", ""), nil)
	sum := contract.Message{ID: "sum", Role: contract.RoleUser, Timestamp: time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC),
		Content: []contract.Block{{Kind: contract.BlockText, Text: "summary"}}}
	writeSession(t, a, "s1",
		msgLine("m1", contract.RoleUser, "one")+msgLine("m2", contract.RoleAssistant, "two")+
			msgLine("m3", contract.RoleUser, "three")+msgLine("m4", contract.RoleAssistant, "four")+
			msgLine("m5", contract.RoleUser, "five")+
			editLine(contract.ContextEdit{ID: "e1", Kind: contract.EditReplaceRange, From: "m1", To: "m2", Messages: []contract.Message{sum}})+
			editLine(contract.ContextEdit{ID: "e2", Kind: contract.EditHide, Target: "m4"}))
	h, err := a.History(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if ids(h.Messages) != "m3,m5" || h.Omitted != 3 {
		t.Fatalf("history %s omitted %d", ids(h.Messages), h.Omitted)
	}
	if h.Messages[0].Content[0].Text != "three" {
		t.Fatalf("content %+v", h.Messages[0])
	}

	writeSession(t, a, "plain", msgLine("p1", contract.RoleUser, "a")+msgLine("p2", contract.RoleAssistant, "b"))
	if h, err := a.History(context.Background(), "plain"); err != nil || ids(h.Messages) != "p1,p2" || h.Omitted != 0 {
		t.Fatalf("plain %+v %v", h, err)
	}
	if h, err := a.History(context.Background(), "missing"); err != nil || len(h.Messages) != 0 || h.Omitted != 0 {
		t.Fatalf("missing %+v %v", h, err)
	}
	writeSession(t, a, "broken", "not json\n{}\n")
	if _, err := a.History(context.Background(), "broken"); err == nil {
		t.Fatal("broken session accepted")
	}
	if _, err := a.History(context.Background(), "../x"); err == nil {
		t.Fatal("bad id accepted")
	}
}

// AG-FR-30: 실행 중인 세션도 읽는다 (잠금 없음).
func TestHistoryWhileRunning(t *testing.T) {
	f := setup(t)
	srv, release, n := blockingLLM(t, serve(textReply("ok")))
	a := newAgent(t, f, testConfig(srv.URL, ""), nil)
	writeSession(t, a, "s1", msgLine("p1", contract.RoleUser, "earlier")+msgLine("p2", contract.RoleAssistant, "reply"))
	r, err := a.Start(context.Background(), "s1", user("go"))
	if err != nil {
		t.Fatal(err)
	}
	for n.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	h, err := a.History(context.Background(), "s1")
	if err != nil || !strings.HasPrefix(ids(h.Messages), "p1,p2") {
		t.Fatalf("history while running %+v %v", h, err)
	}
	close(release)
	r.Wait()
}

// compactConfig 는 자동 압축을 끄고 keepRecentTokens 를 작게 둔 설정이다.
func compactConfig(url string) string {
	return `{"agent":{"model":{"provider":"test","id":"m"},"compaction":{"enabled":false,"keepRecentTokens":10}},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `","apiKeyEnv":""}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}}`
}

func editCount(t *testing.T, a *agent.Agent, sid string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(a.SessionDir(), sid+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), `"type":"edit"`)
}

type compactEvents struct {
	mu     sync.Mutex
	phases []string
}

func (c *compactEvents) on(ev harness.Event) {
	if ev.Kind == harness.EventCompaction {
		c.mu.Lock()
		c.phases = append(c.phases, ev.Phase+":"+ev.SessionID)
		c.mu.Unlock()
	}
}

// AG-FR-31: 긴 세션 → 압축·편집 하나·History 에 요약 없음·다음 run 은 요약을 봄·중복 없음.
func TestCompact(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	ev := &compactEvents{}
	a := newAgent(t, f, compactConfig(l.URL), func(o *agent.Options) { o.OnEvent = ev.on })
	for _, p := range []string{"first", "second", "third"} {
		runOnce(t, a, "s1", p+strings.Repeat(" abcd", 50))
	}
	ok, err := a.Compact(context.Background(), "s1")
	if err != nil || !ok {
		t.Fatalf("compact ok=%v err=%v", ok, err)
	}
	if n := editCount(t, a, "s1"); n != 1 {
		t.Fatalf("edits %d", n)
	}
	if strings.Join(ev.phases, ",") != "start:s1,end:s1" {
		t.Fatalf("events %v", ev.phases)
	}
	h, err := a.History(context.Background(), "s1")
	if err != nil || h.Omitted == 0 || len(h.Messages)+h.Omitted != 6 {
		t.Fatalf("history %+v %v", h, err)
	}
	for _, m := range h.Messages {
		if strings.Contains(m.Content[0].Text, "compacted") {
			t.Fatalf("summary in history: %+v", m)
		}
	}
	l.mu.Lock()
	before := len(l.bodies)
	l.mu.Unlock()
	runOnce(t, a, "s1", "next")
	if !strings.Contains(l.body(before), "compacted into the following summary") {
		t.Fatalf("next request lacks summary: %s", l.body(before))
	}
	if n := editCount(t, a, "s1"); n != 1 {
		t.Fatalf("edit duplicated: %d", n)
	}
}

// AG-FR-31: 짧은 세션 → false, 빈 ID → 에러, 실행 중 → ErrSessionBusy, 끝나면 잠금이 풀림.
func TestCompactEdges(t *testing.T) {
	f := setup(t)
	srv, release, n := blockingLLM(t, serve(textReply("ok")))
	a := newAgent(t, f, compactConfig(srv.URL), nil)
	if _, err := a.Compact(context.Background(), ""); err == nil {
		t.Fatal("empty id accepted")
	}
	r, err := a.Start(context.Background(), "s1", user("hi"))
	if err != nil {
		t.Fatal(err)
	}
	for n.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := a.Compact(context.Background(), "s1"); !errors.Is(err, agent.ErrSessionBusy) {
		t.Fatalf("busy: %v", err)
	}
	close(release)
	r.Wait()
	if ok, err := a.Compact(context.Background(), "s1"); ok || err != nil {
		t.Fatalf("short ok=%v err=%v", ok, err)
	}
	if r, err := a.Start(context.Background(), "s1", user("again")); err != nil {
		t.Fatalf("lock not released: %v", err)
	} else {
		r.Wait()
	}
}
