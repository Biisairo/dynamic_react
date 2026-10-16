package agent_test

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
)

type approver struct {
	mu     sync.Mutex
	reqs   []agent.ApprovalRequest
	decide func(ctx context.Context, r agent.ApprovalRequest) (agent.Decision, error)
}

func (a *approver) Approve(ctx context.Context, r agent.ApprovalRequest) (agent.Decision, error) {
	a.mu.Lock()
	a.reqs = append(a.reqs, r)
	a.mu.Unlock()
	return a.decide(ctx, r)
}

func (a *approver) asked() []agent.ApprovalRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agent.ApprovalRequest(nil), a.reqs...)
}

type events struct {
	mu  sync.Mutex
	evs []harness.Event
}

func (e *events) on(ev harness.Event) {
	e.mu.Lock()
	e.evs = append(e.evs, ev)
	e.mu.Unlock()
}

func (e *events) of(kind harness.EventKind) []harness.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []harness.Event
	for _, ev := range e.evs {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func start(t *testing.T, f *fixture, o agent.Options, sid, prompt string) *agent.Run {
	t.Helper()
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Start(context.Background(), sid, user(prompt))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// AG-FR-18, 20: Approver 거부는 사유와 함께 모델에 가고, allow 규칙은 묻지 않는다.
func TestApprovalDeny(t *testing.T) {
	f := setup(t)
	os.WriteFile(filepath.Join(f.cwd, "a.txt"), []byte("A\n"), 0o644)
	l := newLLM(t, toolReply("read", `{"path":"a.txt"}`), toolReply("bash", `{"command":"echo x > b.txt"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, `,"permissions":{"allow":["read"]}`))
	ap := &approver{decide: func(context.Context, agent.ApprovalRequest) (agent.Decision, error) {
		return agent.Decision{Kind: "deny", Reason: "not now"}, nil
	}}
	ev := &events{}
	o := f.opts()
	o.Approver, o.OnEvent = ap, ev.on
	r := start(t, f, o, "", "go")
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	reqs := ap.asked()
	if len(reqs) != 1 || reqs[0].Call.Name != "bash" || reqs[0].Target != "echo x > b.txt" || reqs[0].Rule != "" || reqs[0].SessionID != r.SessionID() || reqs[0].RunID == "" {
		t.Fatalf("asked %+v", reqs)
	}
	if !strings.Contains(l.body(2), "denied: not now") {
		t.Fatalf("deny not sent: %.400s", l.body(2))
	}
	if _, err := os.Stat(filepath.Join(f.cwd, "b.txt")); err == nil {
		t.Fatal("denied command ran")
	}
	if a := ev.of(harness.EventApproval); len(a) != 1 || a[0].Approval.Decision != "deny" || a[0].Approval.Reason != "not now" || a[0].Call.Name != "bash" {
		t.Fatalf("approval events %+v", a)
	}
}

// AG-FR-18: Approver 가 없으면 묻지 않고 실행한다.
func TestNoApprover(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"echo x > b.txt"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	r := start(t, f, f.opts(), "", "go")
	r.Wait()
	if _, err := os.Stat(filepath.Join(f.cwd, "b.txt")); err != nil {
		t.Fatal("command did not run without approver")
	}
}

// AG-FR-19: allow_session 은 같은 세션의 같은 대상만 다시 묻지 않는다.
func TestAllowSession(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"echo 1"}`), toolReply("bash", `{"command":"echo 1"}`), toolReply("bash", `{"command":"echo 2"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	ap := &approver{decide: func(context.Context, agent.ApprovalRequest) (agent.Decision, error) {
		return agent.Decision{Kind: "allow_session"}, nil
	}}
	o := f.opts()
	o.Approver = ap
	a, _ := agent.New(context.Background(), o)
	r, _ := a.Start(context.Background(), "s1", user("go"))
	r.Wait()
	if got := ap.asked(); len(got) != 2 || got[0].Target != "echo 1" || got[1].Target != "echo 2" {
		t.Fatalf("asked %+v", got)
	}
	l2 := newLLM(t, toolReply("bash", `{"command":"echo 1"}`), textReply("done"))
	f.writeConfig(t, testConfig(l2.URL, ""))
	a2, _ := agent.New(context.Background(), o)
	r2, _ := a2.Start(context.Background(), "s2", user("go"))
	r2.Wait()
	if got := ap.asked(); len(got) != 3 {
		t.Fatalf("other session not asked: %+v", got)
	}
}

// AG-FR-18: Approver error → 거부, ctx 취소 → aborted.
func TestApproverErrors(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"echo 1"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	o := f.opts()
	o.Approver = &approver{decide: func(context.Context, agent.ApprovalRequest) (agent.Decision, error) {
		return agent.Decision{}, errors.New("ui gone")
	}}
	start(t, f, o, "", "go").Wait()
	if !strings.Contains(l.body(1), "denied: ui gone") {
		t.Fatalf("%.300s", l.body(1))
	}
	l2 := newLLM(t, toolReply("bash", `{"command":"echo 1"}`), textReply("done"))
	f.writeConfig(t, testConfig(l2.URL, ""))
	asked := make(chan struct{})
	o.Approver = &approver{decide: func(ctx context.Context, _ agent.ApprovalRequest) (agent.Decision, error) {
		close(asked)
		<-ctx.Done()
		return agent.Decision{}, ctx.Err()
	}}
	r := start(t, f, o, "", "go")
	<-asked
	r.Abort()
	if s := r.Wait(); s.Reason != core.SettleAborted {
		t.Fatalf("%+v", s)
	}
}

// AG-FR-12, 20: 세 출처, deny 규칙 이벤트, 프로젝트 파일 검사.
func TestPermissionSources(t *testing.T) {
	f := setup(t)
	l := newLLM(t, toolReply("bash", `{"command":"echo x > b.txt"}`), toolReply("read", `{"path":"b.txt"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	os.MkdirAll(filepath.Join(f.cwd, ".dynamic_react"), 0o755)
	project := filepath.Join(f.cwd, ".dynamic_react", "config.json")
	os.WriteFile(project, []byte(`{"permissions":{"allow":["bash(echo *)"]}}`), 0o644)
	ap := &approver{decide: func(context.Context, agent.ApprovalRequest) (agent.Decision, error) {
		return agent.Decision{Kind: "allow"}, nil
	}}
	ev := &events{}
	o := f.opts()
	o.Approver, o.OnEvent = ap, ev.on
	o.Permissions = agent.Permissions{Deny: []string{"read"}}
	start(t, f, o, "", "go").Wait()
	if len(ap.asked()) != 0 {
		t.Fatalf("project allow ignored: %+v", ap.asked())
	}
	if !strings.Contains(l.body(2), "denied by rule read") {
		t.Fatalf("profile deny ignored: %.300s", l.body(2))
	}
	if a := ev.of(harness.EventApproval); len(a) != 1 || a[0].Approval.Decision != "rule_deny" || a[0].Approval.Rule != "read" {
		t.Fatalf("approval events %+v", a)
	}
	os.WriteFile(project, []byte(`{"permissions":{"allow":["bash"]},"agent":{}}`), 0o644)
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("project file with other keys accepted")
	}
	os.Remove(project)
	f.writeConfig(t, testConfig(l.URL, `,"permissions":{"allow":["bash("]}`))
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("bad rule accepted")
	}
	f.writeConfig(t, testConfig(l.URL, `,"permissions":{"maybe":[]}`))
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("unknown permissions key accepted")
	}
}

func toolNames(t *testing.T, body string) []string {
	t.Helper()
	var req struct {
		Tools []struct {
			Function struct{ Name string }
		}
	}
	json.Unmarshal([]byte(body), &req)
	var names []string
	for _, tl := range req.Tools {
		names = append(names, tl.Function.Name)
	}
	return names
}

// AG-FR-21
func TestToolsLimit(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, testConfig(l.URL, ""))
	o := f.opts()
	o.Tools = []string{"read", "bash"}
	start(t, f, o, "", "go").Wait()
	if got := strings.Join(toolNames(t, l.body(0)), ","); got != "read,bash" {
		t.Fatalf("tools %s", got)
	}
	cfg := strings.Replace(testConfig(l.URL, ""), `"agent":{`, `"agent":{"tools":["bash","edit"],`, 1)
	f.writeConfig(t, cfg)
	start(t, f, o, "", "go").Wait()
	if got := strings.Join(toolNames(t, l.body(1)), ","); got != "bash" {
		t.Fatalf("intersection %s", got)
	}
	o.Tools = []string{"write"}
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("empty intersection accepted")
	}
	o.Tools = []string{"read", "nope"}
	if _, err := agent.New(context.Background(), o); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

// AG-FR-21: 설정 agent 가 null 이면 {} 로 본다 (LINT-REPORT #1).
func TestToolsLimitNullAgent(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, strings.Replace(testConfig(l.URL, ""), `"agent":{"model":{"provider":"test","id":"m"}}`, `"agent":null`, 1))
	o := f.opts()
	o.Tools = []string{"read"}
	if _, err := agent.New(context.Background(), o); err != nil {
		t.Fatal(err)
	}
}

func multiTool(calls ...[2]string) string {
	var parts []string
	for i, c := range calls {
		q, _ := json.Marshal(c[1])
		parts = append(parts, `{"index":`+string(rune('0'+i))+`,"id":"call_`+string(rune('a'+i))+`","type":"function","function":{"name":"`+c[0]+`","arguments":`+string(q)+`}}`)
	}
	return sse(`{"id":"cm","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[` + strings.Join(parts, ",") + `]},"finish_reason":"tool_calls"}]}`)
}

// AG-FR-24: Steer 뒤 같은 턴의 남은 도구는 skipped, 다음 턴은 정상.
func TestSteer(t *testing.T) {
	f := setup(t)
	os.WriteFile(filepath.Join(f.cwd, "a.txt"), []byte("CONTENT-A\n"), 0o644)
	l := newLLM(t, multiTool([2]string{"bash", `{"command":"sleep 0.4"}`}, [2]string{"read", `{"path":"a.txt"}`}),
		toolReply("read", `{"path":"a.txt"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, ""))
	started := make(chan struct{}, 1)
	o := f.opts()
	o.OnEvent = func(ev harness.Event) {
		if ev.Kind == harness.EventToolStart && ev.Call.Name == "bash" {
			started <- struct{}{}
		}
	}
	r := start(t, f, o, "", "go")
	<-started
	if err := r.Steer(user("change of plan")[0]); err != nil {
		t.Fatal(err)
	}
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	b1 := l.body(1)
	if !strings.Contains(b1, "skipped: the user sent a new message") || strings.Contains(b1, "CONTENT-A") || !strings.Contains(b1, "change of plan") {
		t.Fatalf("turn 1 request: %.600s", b1)
	}
	if !strings.Contains(l.body(2), "CONTENT-A") {
		t.Fatalf("next turn tool skipped: %.600s", l.body(2))
	}
}

// AG-FR-25
func TestFollowUp(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("first answer"), textReply("second answer"))
	// 첫 응답을 붙잡아 두어 FollowUp 이 run 종료보다 먼저 들어가게 한다.
	arrived, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(arrived)
			<-release
		}
		l.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.writeConfig(t, testConfig(srv.URL, ""))
	ev := &events{}
	o := f.opts()
	o.OnEvent = ev.on
	r := start(t, f, o, "", "go")
	<-arrived
	if err := r.FollowUp(user("and then")[0]); err != nil {
		t.Fatal(err)
	}
	close(release)
	if s := r.Wait(); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	if b := l.body(1); !strings.Contains(b, "first answer") || !strings.Contains(b, "and then") {
		t.Fatalf("follow-up run: %.400s", b)
	}
	if n := len(ev.of(harness.EventRunEnd)); n != 2 {
		t.Fatalf("run_end %d", n)
	}
	if r.Steer(user("x")[0]) != agent.ErrRunDone || r.FollowUp(user("x")[0]) != agent.ErrRunDone {
		t.Fatal("finished run accepted input")
	}
}

func serve(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}
}

// blockingLLM 은 release 가 닫힐 때까지 스트림을 붙잡는다 (첫 조각은 보낸다).
func blockingLLM(t *testing.T, then http.HandlerFunc) (*httptest.Server, chan struct{}, *atomic.Int32) {
	release := make(chan struct{})
	n := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		n.Add(1)
		select {
		case <-release:
			then(w, r)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return srv, release, n
}

// AG-FR-23, 25: Abort·실패면 대기 중인 FollowUp 을 버린다.
func TestAbortAndFailureDropFollowUps(t *testing.T) {
	f := setup(t)
	srv, release, n := blockingLLM(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad"}}`, http.StatusBadRequest)
	})
	f.writeConfig(t, testConfig(srv.URL, ""))
	r := start(t, f, f.opts(), "", "go")
	r.FollowUp(user("later")[0])
	for n.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	r.Abort()
	if s := r.Wait(); s.Reason != core.SettleAborted {
		t.Fatalf("abort %+v", s)
	}
	r = start(t, f, f.opts(), "", "go")
	r.FollowUp(user("later")[0])
	close(release)
	if s := r.Wait(); s.Reason != core.SettleFailed {
		t.Fatalf("failed %+v", s)
	}
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 2 {
		t.Fatalf("requests %d (follow-ups must be dropped)", n.Load())
	}
}

// AG-FR-22
func TestSessionBusy(t *testing.T) {
	f := setup(t)
	srv, release, _ := blockingLLM(t, serve(textReply("ok")))
	f.writeConfig(t, testConfig(srv.URL, ""))
	a, err := agent.New(context.Background(), f.opts())
	if err != nil {
		t.Fatal(err)
	}
	r1, err := a.Start(context.Background(), "s1", user("go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Start(context.Background(), "s1", user("again")); err != agent.ErrSessionBusy {
		t.Fatalf("busy: %v", err)
	}
	r2, err := a.Start(context.Background(), "s2", user("other"))
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	r1.Wait()
	r2.Wait()
	r3, err := a.Start(context.Background(), "s1", user("after"))
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	r3.Wait()
}

type panicObserver struct{}

func (panicObserver) Observe(core.ObserverEvent) { panic("observer bug") }

// AG-FR-26: 콜백 panic 은 그 run 만 failed.
func TestCallbackPanics(t *testing.T) {
	f := setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "bad-prompt") && !strings.Contains(string(b), `"role":"tool"`) {
			serve(toolReply("bash", `{"command":"echo 1"}`))(w, r)
			return
		}
		time.Sleep(50 * time.Millisecond)
		serve(textReply("fine"))(w, r)
	}))
	t.Cleanup(srv.Close)
	f.writeConfig(t, testConfig(srv.URL, ""))
	cases := map[string]func(*agent.Options){
		"on_event": func(o *agent.Options) {
			o.OnEvent = func(ev harness.Event) {
				if ev.Kind == harness.EventToolStart {
					panic("surface bug")
				}
			}
		},
		"approver": func(o *agent.Options) {
			o.Approver = &approver{decide: func(context.Context, agent.ApprovalRequest) (agent.Decision, error) { panic("approver bug") }}
		},
		"observer": func(o *agent.Options) { o.Observer = panicObserver{} },
	}
	for name, mod := range cases {
		t.Run(name, func(t *testing.T) {
			o := f.opts()
			mod(&o)
			a, err := agent.New(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			bad, _ := a.Start(context.Background(), "", user("bad-prompt"))
			if name == "observer" {
				if s := bad.Wait(); s.Reason != core.SettleFailed || !errors.Is(s.Err, core.ErrPanic) {
					t.Fatalf("observer: %+v", s)
				}
				return
			}
			good, _ := a.Start(context.Background(), "", user("good-prompt"))
			if s := bad.Wait(); s.Reason != core.SettleFailed {
				t.Fatalf("bad: %+v", s)
			}
			if s := good.Wait(); s.Reason != core.SettleCompleted {
				t.Fatalf("good: %+v", s)
			}
		})
	}
}

// AG-FR-28, 29
func TestSessions(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, testConfig(l.URL, ""))
	a, _ := agent.New(context.Background(), f.opts())
	if list, err := a.Sessions(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("empty dir: %v %v", list, err)
	}
	long := strings.Repeat("가", 100)
	r1, _ := a.Start(context.Background(), "s-old", user(long))
	r1.Wait()
	time.Sleep(20 * time.Millisecond)
	key := agent.SessionKey("slack/team/channel")
	if session.CheckID(key) != nil {
		t.Fatalf("key %q fails CheckID", key)
	}
	r2, _ := a.Start(context.Background(), key, user("newer"))
	r2.Wait()
	os.WriteFile(filepath.Join(a.SessionDir(), "junk.jsonl"), []byte("not json\n{\n"), 0o644)
	list, err := a.Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != key || list[1].ID != "s-old" {
		t.Fatalf("list %+v", list)
	}
	if list[1].Title != strings.Repeat("가", 80) || list[1].Messages != 2 || list[0].Title != "newer" || !list[0].Updated.After(list[1].Updated) {
		t.Fatalf("info %+v", list)
	}
}
