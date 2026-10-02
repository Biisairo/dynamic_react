package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

// --- 가짜 모델 서버 (Chat Completions 호환) ---

type llm struct {
	*httptest.Server
	mu      sync.Mutex
	bodies  []string
	heads   []http.Header
	replies []string
	gate    chan struct{} // nil 이 아니면 요청마다 하나씩 받아야 답한다
}

func newLLM(t *testing.T, replies ...string) *llm {
	l := &llm{replies: replies}
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		l.mu.Lock()
		n := len(l.bodies)
		l.bodies = append(l.bodies, string(b))
		l.heads = append(l.heads, r.Header.Clone())
		gate := l.gate
		l.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, l.replies[min(n, len(l.replies)-1)])
	}))
	t.Cleanup(l.Close)
	return l
}

func (l *llm) block() chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gate = make(chan struct{}, 16)
	return l.gate
}

func (l *llm) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.bodies)
}

func (l *llm) body(i int) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i < 0 {
		i += len(l.bodies)
	}
	if i < 0 || i >= len(l.bodies) {
		return ""
	}
	return l.bodies[i]
}

func (l *llm) header(i int) http.Header {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i < 0 || i >= len(l.heads) {
		return http.Header{}
	}
	return l.heads[i]
}

func sse(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: " + c + "\n\n")
	}
	return b.String() + "data: [DONE]\n\n"
}

func textReply(s string) string {
	q, _ := json.Marshal(s)
	return sse(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":` + string(q) +
		`},"finish_reason":"stop"}],"usage":{"prompt_tokens":1500,"completion_tokens":20}}`)
}

func toolReply(id, name, args string) string {
	q, _ := json.Marshal(args)
	return sse(`{"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + string(q) + `}}]},"finish_reason":"tool_calls"}]}`)
}

// --- 화면·입력 ---

// syncScreen 은 잠금을 건 가상 화면 + 원본 바이트 기록이다.
type syncScreen struct {
	mu  sync.Mutex
	v   *vscreen
	raw strings.Builder
}

func (s *syncScreen) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw.Write(b)
	return s.v.Write(b)
}

func (s *syncScreen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.v.text()
}

func (s *syncScreen) bytes() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

type harnessT struct {
	t    *testing.T
	term *Terminal
	scr  *syncScreen
	in   *io.PipeWriter
	a    *agent.Agent
	done chan struct{} // Run 이 돌아오면 닫힌다
	err  error
	cfg  string
}

type env struct {
	home, cwd string
}

func newEnv(t *testing.T) *env {
	e := &env{home: t.TempDir(), cwd: t.TempDir()}
	t.Setenv("HOME", e.home)
	return e
}

// config 는 가짜 서버를 provider "test" 로 등록한 설정이다. agentExtra 는 "agent" 안에 붙는다.
func config(url, agentExtra, extra string) string {
	return `{"agent":{"model":{"provider":"test","id":"m"}` + agentExtra + `},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000}}` + extra + `}`
}

func (e *env) agent(t *testing.T, cfg string, term *Terminal) *agent.Agent {
	t.Helper()
	p := filepath.Join(e.home, ".dynamic_react", "config.json")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	o := agent.Options{Cwd: e.cwd, NoRemoteCatalog: true}
	if term != nil {
		o.Approver, o.OnEvent = term.Approver(), term.OnEvent
	}
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e *env) start(t *testing.T, ctx context.Context, cfg string, s Start) *harnessT {
	t.Helper()
	r, w := io.Pipe()
	scr := &syncScreen{v: newVScreen(100)}
	term := New(Options{In: r, Out: scr, Width: func() int { return 100 }, Now: time.Now, EscTimeout: 10 * time.Millisecond})
	h := &harnessT{t: t, term: term, scr: scr, in: w, done: make(chan struct{}), cfg: cfg}
	h.a = e.agent(t, cfg, term)
	go func() {
		h.err = term.Run(ctx, h.a, s)
		close(h.done)
	}()
	t.Cleanup(func() {
		w.Close()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
		}
	})
	return h
}

func (h *harnessT) typ(s string) {
	h.t.Helper()
	if _, err := h.in.Write([]byte(s)); err != nil {
		h.t.Fatal(err)
	}
}

// wait 는 화면에 want 가 나타날 때까지 기다린다.
func (h *harnessT) wait(want string) {
	h.t.Helper()
	h.waitFn(want, func(s string) bool { return strings.Contains(s, want) })
}

func (h *harnessT) waitFn(desc string, ok func(string) bool) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok(h.scr.text()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("screen never matched %q:\n%s", desc, h.scr.text())
}

func (h *harnessT) waitCount(want string, n int) {
	h.t.Helper()
	h.waitFn(want, func(s string) bool { return strings.Count(s, want) >= n })
}

func (h *harnessT) exit() error {
	h.t.Helper()
	select {
	case <-h.done:
		return h.err
	case <-time.After(10 * time.Second):
		h.t.Fatalf("Run did not return:\n%s", h.scr.text())
		return nil
	}
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition never met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// --- 테스트 ---

// TM-FR-07, 08, 14~16, 25, 32: 기본 턴과 /quit, 켜기·해제 시퀀스.
func TestRunTurnAndQuit(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("hello there"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("hi\r")
	h.wait("── completed · 입력 1.5k / 출력 20 토큰")
	s := h.scr.text()
	if !strings.Contains(s, "> hi\n── 세션 ") || !strings.Contains(s, "hello there") || strings.Contains(s, "»") {
		t.Fatalf("%s", s)
	}
	h.typ("/quit\r")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
	raw := h.scr.bytes()
	if !strings.HasPrefix(raw, seqPasteOn+seqQuery) || !strings.HasSuffix(raw, seqPasteOff) || strings.Contains(raw, seqModifyOn) {
		t.Fatalf("sequences %q … %q", raw[:min(30, len(raw))], raw[max(0, len(raw)-30):])
	}
}

// TM-FR-07, 08: kitty 응답 / DA 만.
func TestRunNegotiation(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("\x1b[?1u\x1b[?62;22c\x1b[13;2ux")
	h.wait(">\n  x")
	h.typ("\x1b[99;5u\x1b[99;5u\x1b[99;5u")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
	if raw := h.scr.bytes(); !strings.HasSuffix(raw, seqKittyOff+seqPasteOff) || strings.Contains(raw, seqModifyOn) {
		t.Fatalf("kitty %q", raw[max(0, len(raw)-30):])
	}

	h = e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("\x1b[?62c")
	waitUntil(t, func() bool { return strings.Contains(h.scr.bytes(), seqModifyOn) })
	h.typ("\x04")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
	if raw := h.scr.bytes(); !strings.HasSuffix(raw, seqModifyOff+seqPasteOff) {
		t.Fatalf("modify %q", raw[max(0, len(raw)-30):])
	}

	// DA 뒤에 온 kitty 응답은 무시한다.
	h = e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("\x1b[?62c")
	waitUntil(t, func() bool { return strings.Contains(h.scr.bytes(), seqModifyOn) })
	h.typ("\x1b[?1u\x04")
	h.exit()
	if raw := h.scr.bytes(); strings.Contains(raw, seqKittyOff) {
		t.Fatal("late kitty response accepted")
	}

	// 시간 초과 뒤의 DA 는 modifyOtherKeys 를 켜지 않는다.
	old := negotiateTimeout
	negotiateTimeout = 0
	defer func() { negotiateTimeout = old }()
	h = e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("\x1b[?62cx")
	h.wait("> x")
	h.typ("\x7f\x04")
	h.exit()
	if raw := h.scr.bytes(); strings.Contains(raw, seqModifyOn) || strings.Contains(raw, seqModifyOff) {
		t.Fatal("late DA enabled modifyOtherKeys")
	}
}

// TM-FR-12, 14: Ctrl+C 지우기·두 번 종료, Ctrl+D, EOF.
func TestRunExitKeys(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	cfg := config(l.URL, "", "")
	h := e.start(t, context.Background(), cfg, Start{})
	h.typ("abc")
	h.wait("> abc")
	h.typ("\x03")
	h.waitFn("cleared", func(s string) bool { return !strings.Contains(s, "abc") })
	h.typ("\x03")
	h.wait(exitHint)
	h.typ("x")
	h.waitFn("hint gone", func(s string) bool { return !strings.Contains(s, exitHint) && strings.Contains(s, "> x") })
	h.typ("\x7f\x03\x03")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}

	h = e.start(t, context.Background(), cfg, Start{})
	h.typ("a\x04")
	h.wait("> a")
	h.typ("\x7f\x04")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}

	h = e.start(t, context.Background(), cfg, Start{})
	h.in.Close()
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
}

// TM-FR-13: 실행 중 Esc·Ctrl+C = Abort (입력 줄 유지), 빈 줄 Ctrl+D = Abort 후 종료.
func TestRunAbortKeys(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	l.block()
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait(statusRun)
	h.typ("keep\x1b")
	h.wait("── 중단됨")
	h.waitFn("idle with input kept", func(s string) bool { return !strings.Contains(s, statusRun) && strings.HasSuffix(s, "> keep") })
	h.typ("\r")
	h.wait(statusRun)
	h.typ("\x03")
	h.waitCount("── 중단됨", 2)
	h.typ("again\r")
	h.wait(statusRun)
	h.typ("\x04")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.scr.text(), "── 중단됨") {
		t.Fatal(h.scr.text())
	}
}

// TM-FR-03: ctx 취소 → Abort·정리 후 ctx.Err().
func TestRunContextCancel(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	l.block()
	ctx, cancel := context.WithCancel(context.Background())
	h := e.start(t, ctx, config(l.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait(statusRun)
	h.typ("half")
	h.wait("> half")
	cancel()
	if err := h.exit(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if s := h.scr.text(); strings.Contains(s, statusRun) || strings.Contains(s, "> half") {
		t.Fatalf("region not erased:\n%s", s)
	}
	if !strings.HasSuffix(h.scr.bytes(), seqPasteOff) {
		t.Fatal("paste mode not restored")
	}
}

// TM-FR-34~36: 승인 y / a / n+사유 / Esc.
func TestRunApproval(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, toolReply("call_1", "bash", `{"command":"echo one"}`), textReply("done"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("draft")
	h.wait("> draft")
	h.typ("\x7f\x7f\x7f\x7f\x7fgo\r")
	h.wait("⚠ bash 승인 필요  (규칙 없음 — 기본 묻기)")
	h.wait("  echo one\n  [y] 이번만  [a] 이 세션 동안  [n] 거부")
	h.typ("q")
	h.typ("y")
	h.wait("── completed")
	if s := h.scr.text(); !strings.Contains(s, "✓ 이번만 허용") || !strings.Contains(s, "  one") || strings.Contains(s, "[y] 이번만") {
		t.Fatal(s)
	}

	l2 := newLLM(t, toolReply("call_1", "bash", `{"command":"echo two"}`), toolReply("call_2", "bash", `{"command":"echo two"}`), textReply("done"))
	h = e.start(t, context.Background(), config(l2.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait("승인 필요")
	h.typ("A")
	h.wait("── completed")
	if s := h.scr.text(); strings.Count(s, "이 세션 동안 허용") != 1 || strings.Count(s, "▸ bash") != 2 {
		t.Fatal(s)
	}

	l3 := newLLM(t, toolReply("call_1", "bash", `{"command":"rm x"}`), textReply("done"))
	h = e.start(t, context.Background(), config(l3.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait("승인 필요")
	h.typ("n")
	h.wait("거부 사유 (Enter 로 확정):")
	h.typ("no\x1b[200~ wa\ny\x1b[201~\r")
	h.wait("── completed")
	if !strings.Contains(h.scr.text(), "✗ 거부: no wa y") || !strings.Contains(l3.body(1), "denied: no wa y") {
		t.Fatalf("%s\n%s", h.scr.text(), l3.body(1))
	}

	l4 := newLLM(t, toolReply("call_1", "bash", `{"command":"x"}`), textReply("done"))
	h = e.start(t, context.Background(), config(l4.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait("승인 필요")
	h.typ("n\x03")
	h.wait("── 중단됨")

	l5 := newLLM(t, toolReply("call_1", "bash", `{"command":"x"}`), textReply("done"))
	h = e.start(t, context.Background(), config(l5.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait("승인 필요")
	h.typ("n\r")
	h.wait("✗ 거부: user denied")
}

// TM-FR-35: Target 이 없으면 인자 요약, command·path 외의 문자열 인자는 앞 5줄.
func TestRunApprovalArgs(t *testing.T) {
	e := newEnv(t)
	content := "l1\nl2\nl3\nl4\nl5\nl6"
	args, _ := json.Marshal(map[string]string{"path": "f.txt", "content": content})
	l := newLLM(t, toolReply("call_1", "write", string(args)), textReply("done"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("go\r")
	h.wait("⚠ write 승인 필요")
	h.wait("  content: l1\n    l2\n    l3\n    l4\n    l5\n  …\n  [y]")
	if !strings.Contains(h.scr.text(), "  "+filepath.Join(e.cwd, "f.txt")) {
		t.Fatal(h.scr.text())
	}
	h.typ("y")
	h.wait("── completed")
}

// TM-FR-13, 17, 21: 실행 중 Enter = Steer (남은 도구 skipped), Alt+Enter = FollowUp (+N, 연쇄 run).
func TestRunSteerFollowUp(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, toolReply("call_1", "bash", `{"command":"echo a"}`), textReply("after steer"), textReply("follow"))
	gate := l.block()
	cfg := config(l.URL, "", `,"permissions":{"allow":["bash"]}`)
	h := e.start(t, context.Background(), cfg, Start{})
	h.typ("go\r")
	h.wait(statusRun)
	waitUntil(t, func() bool { return l.count() == 1 })
	h.typ("change\r")
	h.wait("> change (steer)")
	h.typ("later\x1b\r")
	h.wait("> later (follow-up)")
	h.wait("+1 " + statusRun)
	gate <- struct{}{}
	gate <- struct{}{}
	gate <- struct{}{}
	h.waitCount("── completed", 2)
	s := h.scr.text()
	if !strings.Contains(s, "⤼ skipped") || !strings.Contains(s, "after steer") || !strings.Contains(s, "follow") || strings.Contains(s, "»") {
		t.Fatal(s)
	}
	if !strings.Contains(l.body(1), "change") || !strings.Contains(l.body(2), "later") {
		t.Fatalf("bodies\n%s\n%s", l.body(1), l.body(2))
	}
	h.waitFn("idle", func(s string) bool { return !strings.Contains(s, statusRun) })
}

// TM-FR-15, 47: // 이스케이프, 알 수 없는 명령, /help.
func TestRunSlash(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("/foo bar\r")
	h.wait("✗ 알 수 없는 명령: /foo (/help)")
	h.typ("/help\r")
	h.wait("명령: /help")
	h.typ("   \r")
	h.typ("//x\r")
	h.wait("── completed")
	if l.count() != 1 || !strings.Contains(l.body(0), `"content":"/x"`) {
		t.Fatalf("%d %s", l.count(), l.body(0))
	}
	if !strings.Contains(h.scr.text(), "> /x\n") {
		t.Fatal(h.scr.text())
	}
}

// seed 는 sessionDir 에 세션 두 개(first → second 순)를 만든다.
func seed(t *testing.T, e *env, url string) (string, string) {
	t.Helper()
	a := e.agent(t, config(url, "", ""), nil)
	ids := []string{}
	for _, p := range []string{"first", "second"} {
		r, err := a.Start(context.Background(), "", []contract.Message{newMessage(p, time.Now())})
		if err != nil {
			t.Fatal(err)
		}
		r.Wait()
		ids = append(ids, r.SessionID())
		time.Sleep(20 * time.Millisecond)
	}
	return ids[0], ids[1]
}

// TM-FR-38, 41: Continue → 최근 세션 헤더 + History, 이어서 같은 세션으로 Start.
func TestRunContinue(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("reply"))
	_, second := seed(t, e, l.URL)
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{Continue: true})
	h.wait("── 세션 " + second + " · second · 2 메시지\n> second\nreply\n")
	h.typ("more\r")
	h.wait("── completed")
	if strings.Contains(h.scr.text(), "── 세션 "+second+"\n") || !strings.Contains(l.body(-1), "second") {
		t.Fatalf("%s\n%s", h.scr.text(), l.body(-1))
	}
}

// TM-FR-38: Session 지정 — 있는 세션은 이어가고, 없는 ID 는 헤더 없이 그 ID 로.
func TestRunSessionFlag(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("reply"))
	first, _ := seed(t, e, l.URL)
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{Session: first})
	h.wait("── 세션 " + first + " · first")
	h.typ("/quit\r")
	h.exit()

	h = e.start(t, context.Background(), config(l.URL, "", ""), Start{Session: "fresh-id"})
	h.typ("x\r")
	h.wait("── completed")
	if strings.Contains(h.scr.text(), "── 세션") {
		t.Fatal(h.scr.text())
	}
	if _, err := os.Stat(filepath.Join(h.a.SessionDir(), "fresh-id.jsonl")); err != nil {
		t.Fatal(err)
	}
}

// TM-FR-39, 40: Pick — 목록, 잘못된 번호, 번호 선택, Enter 새 세션, Esc 새 세션.
func TestRunPick(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("reply"))
	first, second := seed(t, e, l.URL)
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{Pick: true})
	h.wait("1  방금  2msg  second\n2  방금  2msg  first")
	h.wait(strings.TrimSpace(promptPick))
	h.typ("9\r")
	h.wait("✗ 잘못된 번호")
	h.typ("0\r")
	h.waitCount("✗ 잘못된 번호", 2)
	h.typ("x\x7f2\r")
	h.wait("── 세션 " + first + " · first")
	h.waitFn("prompt back", func(s string) bool { return strings.HasSuffix(s, "\n>") })

	h = e.start(t, context.Background(), config(l.URL, "", ""), Start{Pick: true})
	h.wait(strings.TrimSpace(promptPick))
	h.typ("\r")
	h.wait("── 새 세션")
	h.typ("/resume\r")
	h.wait(strings.TrimSpace(promptPick))
	h.typ("\x1b")
	h.waitCount("── 새 세션", 2)
	h.typ("/resume 1\r")
	h.wait("── 세션 " + second)
	h.typ("/resume " + first + "\r")
	h.wait("── 세션 " + first)
	h.typ("/resume nosuch\r")
	h.wait("✗ 없는 세션: nosuch")
	h.typ("/resume 7\r")
	h.wait("✗ 잘못된 번호")
	h.typ("/resume 0\r")
	h.waitCount("✗ 잘못된 번호", 2)
	h.typ("/sessions\r")
	h.waitCount("1  방금  2msg  second", 3)
	h.typ("/new\r")
	h.waitCount("── 새 세션", 3)
}

// TM-FR-39: 빈 목록, 제목 없음, 상대 시각.
func TestListLines(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	term := New(Options{Out: io.Discard, Width: func() int { return 60 }, Now: func() time.Time { return now }})
	if got := term.listLines(nil); len(got) != 1 || got[0] != "(세션 없음)" {
		t.Fatal(got)
	}
	list := []agent.SessionInfo{
		{ID: "a", Updated: now.Add(-30 * time.Second), Messages: 1},
		{ID: "b", Title: "t", Updated: now.Add(-5 * time.Minute), Messages: 2},
		{ID: "c", Title: "t", Updated: now.Add(-3 * time.Hour), Messages: 3},
		{ID: "d", Title: "t", Updated: now.Add(-2 * 24 * time.Hour), Messages: 4},
		{ID: "e", Title: strings.Repeat("긴", 40), Updated: time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local), Messages: 5},
	}
	got := strings.Join(term.listLines(list), "\n")
	want := "1  방금  1msg  (제목 없음)\n2  5분 전  2msg  t\n3  3시간 전  3msg  t\n4  2일 전  4msg  t\n5  2026-09-01  5msg  " + strings.Repeat("긴", 19) + "…"
	if got != want {
		t.Fatalf("%q\nwant %q", got, want)
	}
}

// TM-FR-41: 압축된 세션 → "이전 N개 메시지 압축됨", 요약 본문은 그리지 않음.
func TestRunCompactedHistory(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	cfg := config(l.URL, `,"compaction":{"enabled":false,"keepRecentTokens":10}`, "")
	a := e.agent(t, cfg, nil)
	for _, p := range []string{"one", "two", "three"} {
		r, _ := a.Start(context.Background(), "s1", []contract.Message{newMessage(p+strings.Repeat(" abcd", 50), time.Now())})
		r.Wait()
	}
	if ok, err := a.Compact(context.Background(), "s1"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	h := e.start(t, context.Background(), cfg, Start{Continue: true})
	h.wait("⋯ 이전 2개 메시지 압축됨\n> two")
	if strings.Contains(h.scr.text(), "compacted") {
		t.Fatal(h.scr.text())
	}
}

// TM-FR-42, 45: /compact — 세션 없음, 짧은 세션, 긴 세션, 실행 중 거부, 취소.
func TestRunCompact(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	cfg := config(l.URL, `,"compaction":{"enabled":false,"keepRecentTokens":10}`, "")
	h := e.start(t, context.Background(), cfg, Start{})
	h.typ("/compact\r")
	h.wait("✗ 압축할 세션이 없습니다")
	h.typ("hi\r")
	h.wait("── completed")
	h.waitFn("idle", func(s string) bool { return !strings.Contains(s, statusRun) })
	h.typ("/compact\r")
	h.wait("압축할 구간이 없습니다")
	for i, p := range []string{"a", "b"} {
		h.typ(p + strings.Repeat(" abcd", 50) + "\r")
		h.waitCount("── completed", i+2)
	}
	h.waitFn("idle", func(s string) bool { return !strings.Contains(s, statusRun) })
	h.typ("/compact\r")
	h.wait("⋯ 컨텍스트 압축 중\n✓ 압축 완료")

	gate := l.block()
	h.typ("run\r")
	h.wait(statusRun)
	h.typ("/compact\r")
	h.wait("✗ 실행 중에는 압축할 수 없습니다")
	h.typ("/new\r")
	h.wait("✗ 실행 중에는 세션을 바꿀 수 없습니다")
	gate <- struct{}{}
	h.waitCount("── completed", 4)
	h.waitFn("idle", func(s string) bool { return !strings.Contains(s, statusRun) })
	h.typ("/compact\r")
	h.wait(statusComp)
	h.typ("x")
	h.typ("\x1b")
	h.wait("■ 압축 취소")
	h.typ("/compact\r")
	h.wait(statusComp)
	h.typ("\x03")
	h.waitCount("■ 압축 취소", 2)
}

// TM-FR-02, 04, 05, 06: Start 배타, Run 두 번, Run 밖 콜백, Resize.
func TestRunMisuse(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	var out strings.Builder
	term := New(Options{In: strings.NewReader(""), Out: &out})
	a := e.agent(t, config(l.URL, "", ""), term)
	if err := term.Run(context.Background(), a, Start{Continue: true, Pick: true}); err == nil {
		t.Fatal("exclusive flags accepted")
	}
	term.OnEvent(harness.Event{Kind: harness.EventRunEnd})
	if out.Len() != 0 {
		t.Fatalf("OnEvent outside Run wrote %q", out.String())
	}
	d, err := term.Approver().Approve(context.Background(), agent.ApprovalRequest{})
	if err != nil || d.Kind != "deny" || d.Reason != "terminal not running" {
		t.Fatal(d, err)
	}
	if err := term.Run(context.Background(), a, Start{}); err != nil {
		t.Fatal(err)
	}
	if err := term.Run(context.Background(), a, Start{}); err == nil {
		t.Fatal("second Run accepted")
	}
	term.Resize()
}

// TM-FR-01: TTY 가 아니면 ErrNotTerminal.
func TestOpenNotTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := Open(f); !errors.Is(err, ErrNotTerminal) {
		t.Fatal(err)
	}
	if IsTerminal(int(f.Fd())) {
		t.Fatal("file is a terminal")
	}
}

// TM-FR-11: 입력 줄 편집 키와 기록이 실제 입력으로 간다.
func TestRunEditing(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.typ("bc\x1b[D\x1b[DXa\x1b[3~\x1b[H(\x1b[F)\x1b[C\x1b[13;2uz\r")
	h.wait("── completed")
	if !strings.Contains(l.body(0), `"content":"(Xac)\nz"`) {
		t.Fatalf("%s", l.body(0))
	}
	h.typ("\x1b[A\x1b[A\x1b[B\x1b[B")
	h.waitFn("history back to empty", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("\x1b[A")
	h.wait("> (Xac)\n  z")
	h.typ("\x05\x01\x01\x7f!\r")
	h.waitCount("── completed", 2)
	if !strings.Contains(l.body(-1), `"content":"(Xac)!z"`) {
		t.Fatalf("%s", l.body(-1))
	}
}

// TM-FR-16: Start 에러는 ✗ 줄 (상태 대기 유지).
func TestRunStartError(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{Session: "bad id!"})
	h.typ("hi\r")
	h.wait("✗ ")
	h.waitFn("prompt back", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	if strings.Contains(h.scr.text(), "(/model)") || strings.Contains(h.scr.text(), "(/login)") {
		t.Fatal(h.scr.text())
	}
	h.typ("/quit\r")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
}

// TM-FR-38, 44: Sessions 에러는 ✗ 줄 (sessionDir 이 파일).
func TestRunSessionsError(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	file := filepath.Join(e.home, "notdir")
	os.WriteFile(file, []byte("x"), 0o644)
	cfg := strings.Replace(config(l.URL, "", ""), "{", `{"sessionDir":"`+file+`",`, 1)
	for _, s := range []Start{{Continue: true}, {Pick: true}, {Session: "x"}} {
		h := e.start(t, context.Background(), cfg, s)
		h.wait("✗ ")
		h.typ("/sessions\r")
		h.waitCount("✗ ", 2)
		h.typ("/resume 1\r")
		h.waitCount("✗ ", 3)
		h.typ("/quit\r")
		if err := h.exit(); err != nil {
			t.Fatal(err)
		}
	}
}

// TM-FR-03, 45: 압축 중 입력 EOF → 압축 취소 후 종료, ctx 취소 → ctx.Err().
func TestRunQuitWhileCompacting(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	cfg := config(l.URL, `,"compaction":{"enabled":false,"keepRecentTokens":10}`, "")
	for _, viaCtx := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		h := e.start(t, ctx, cfg, Start{})
		l.mu.Lock()
		l.gate = nil
		l.mu.Unlock()
		for i, p := range []string{"a", "b"} {
			h.typ(p + strings.Repeat(" abcd", 50) + "\r")
			h.waitCount("── completed", i+1)
		}
		h.waitFn("idle", func(s string) bool { return !strings.Contains(s, statusRun) })
		l.block()
		h.typ("/compact\r")
		h.wait(statusComp)
		if viaCtx {
			cancel()
			if err := h.exit(); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		} else {
			h.in.Close()
			if err := h.exit(); err != nil {
				t.Fatal(err)
			}
		}
		cancel()
	}
}

// TM-FR-39: 목록은 최대 20개.
func TestRunSessionsCap(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	dir := h.a.SessionDir()
	os.MkdirAll(dir, 0o755)
	for i := 0; i < 21; i++ {
		m := contract.Message{ID: "m", Role: contract.RoleUser, Timestamp: time.Now().Add(-time.Duration(i) * time.Minute),
			Content: []contract.Block{{Kind: contract.BlockText, Text: "t"}}}
		b, _ := json.Marshal(map[string]any{"type": "message", "message": m})
		os.WriteFile(filepath.Join(dir, "s"+strings.Repeat("x", i)+".jsonl"), append(b, '\n'), 0o644)
	}
	h.typ("/sessions\r")
	h.wait("\n20  ")
	if strings.Contains(h.scr.text(), "\n21  ") {
		t.Fatal(h.scr.text())
	}
}
