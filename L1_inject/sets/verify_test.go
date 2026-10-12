package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// fakeCheck 은 호출마다 results 를 차례로 돌려준다 (끝나면 마지막 것을 반복).
type fakeCheck struct {
	mu      sync.Mutex
	calls   int
	results []sets.CheckResult
	err     error
}

func (f *fakeCheck) check(ctx context.Context) (sets.CheckResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return sets.CheckResult{}, f.err
	}
	return f.results[min(f.calls, len(f.results))-1], nil
}

func (f *fakeCheck) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// writeTool 은 인자 {"fail":true} 면 error 결과를 돌려준다.
func writeTool() sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "write", Description: "write", InputSchema: json.RawMessage(`{"type":"object"}`)},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult {
			var a struct{ Fail bool }
			json.Unmarshal(c.Args, &a)
			if a.Fail {
				return contract.ToolResult{CallID: c.ID, Status: contract.StatusError, Content: []contract.Block{text("nope")}}
			}
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text("wrote")}}
		},
	}
}

// scripted 는 턴마다 replies[n] 을 답한다. "write"·"write!"(실패)·"echo" 는 그 도구 호출, 그 밖은 텍스트.
func scripted(replies ...string) *fakeModel {
	return &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		r := replies[min(n, len(replies)-1)]
		id := fmt.Sprintf("a%d", n)
		switch r {
		case "write":
			return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c"+id, "write", `{}`)))
		case "write!":
			return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c"+id, "write", `{"fail":true}`)))
		case "echo":
			return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c"+id, "echo", `{"s":"x"}`)))
		}
		return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopStop, text(r)))
	}}
}

// verifyRun 은 stack(+resilient) + below + verify 로 run 하나를 돌린다.
func verifyRun(t *testing.T, d sets.Deps, o sets.VerifyOptions, below ...inject.FunctionSet) core.Settle {
	t.Helper()
	v, err := sets.Verify(d, o)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := inject.BindWith(inject.BindOptions{StateStore: stateStoreOf(d.Store)}, stack(d, true, append(below, v)...)...)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan core.Settle, 1)
	go func() {
		done <- core.New(disp, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1",
			Input: []contract.Message{user(d.NewID(), "go")}, Config: json.RawMessage(cfgJSON)}).Wait()
	}()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("run did not settle")
		return core.Settle{}
	}
}

func lastText(req contract.Request) string {
	m := req.Messages[len(req.Messages)-1]
	if m.Role != contract.RoleUser || len(m.Content) != 1 {
		return fmt.Sprintf("<%s>", m.Role)
	}
	return m.Content[0].Text
}

func fail(report string) sets.CheckResult { return sets.CheckResult{Report: report} }

var pass = sets.CheckResult{OK: true, Report: "ok"}

// ST-FR-46·47: 변경 도구 없이 끝나면 검증하지 않는다. 변경 뒤 끝나면 검증하고 OK 면 끝난다.
func TestVerifyWhenChanged(t *testing.T) {
	for _, c := range []struct {
		name    string
		replies []string
		tools   []string
		checks  int
		reqs    int
	}{
		{"text only", []string{"hi"}, nil, 0, 1},
		{"write then done", []string{"write", "done"}, nil, 1, 2},
		{"write error not counted", []string{"write!", "done"}, nil, 0, 2},
		{"tool not in Tools", []string{"echo", "done"}, nil, 0, 2},
		{"custom Tools", []string{"echo", "done"}, []string{"echo"}, 1, 2},
		{"empty Tools", []string{"write", "done"}, []string{}, 0, 2},
	} {
		m := scripted(c.replies...)
		chk := &fakeCheck{results: []sets.CheckResult{pass}}
		s := verifyRun(t, deps(m, writeTool(), echoTool(nil)), sets.VerifyOptions{Check: chk.check, Tools: c.tools})
		if s.Reason != core.SettleCompleted || chk.count() != c.checks || len(m.requests()) != c.reqs {
			t.Errorf("%s: settle %s err %v checks %d reqs %d", c.name, s.Reason, s.Err, chk.count(), len(m.requests()))
		}
	}
}

// ST-FR-48·49: 실패 보고가 다음 요청의 user 메시지로 들어가고 세션에 저장된다.
func TestVerifyRetry(t *testing.T) {
	m := scripted("write", "done", "write", "done")
	chk := &fakeCheck{results: []sets.CheckResult{fail("FAIL a\nline2"), pass}}
	d := deps(m, writeTool())
	s := verifyRun(t, d, sets.VerifyOptions{Check: chk.check})
	reqs := m.requests()
	if s.Reason != core.SettleCompleted || chk.count() != 2 || len(reqs) != 4 {
		t.Fatalf("settle %s %v checks %d reqs %d", s.Reason, s.Err, chk.count(), len(reqs))
	}
	want := "Verification failed (attempt 1 of 3). Fix the problems below, then finish.\n\nFAIL a\nline2"
	if got := lastText(reqs[2]); got != want {
		t.Fatalf("report message %q", got)
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	found := false
	for _, msg := range sess.Messages {
		if msg.Role == contract.RoleUser && len(msg.Content) == 1 && msg.Content[0].Text == want && msg.ID != "" && msg.Timestamp.Equal(ts) {
			found = true
		}
	}
	if !found {
		t.Fatal("report message not stored")
	}
}

// ST-FR-47 ②: 실패 뒤 변경 없이 끝내도 다시 검증한다.
func TestVerifyRecheckAfterFailure(t *testing.T) {
	m := scripted("write", "done", "still done")
	chk := &fakeCheck{results: []sets.CheckResult{fail("bad"), pass}}
	s := verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check})
	if s.Reason != core.SettleCompleted || chk.count() != 2 || len(m.requests()) != 3 {
		t.Fatalf("settle %s checks %d reqs %d", s.Reason, chk.count(), len(m.requests()))
	}
}

// ST-FR-48: MaxAttempts 번째 실패는 run 실패.
func TestVerifyMaxAttempts(t *testing.T) {
	m := scripted("write", "done")
	chk := &fakeCheck{results: []sets.CheckResult{fail("\n\n  first line  \nmore")}}
	s := verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check, MaxAttempts: 2})
	if s.Reason != core.SettleFailed || !errors.Is(s.Err, sets.ErrVerifyFailed) || chk.count() != 2 {
		t.Fatalf("settle %s err %v checks %d", s.Reason, s.Err, chk.count())
	}
	if !strings.Contains(s.Err.Error(), "verify: checks still failing after 2 attempts: first line") {
		t.Fatalf("err %v", s.Err)
	}
	if got := lastText(m.requests()[2]); !strings.HasPrefix(got, "Verification failed (attempt 1 of 2).") {
		t.Fatalf("first report %q", got)
	}
	m = scripted("write", "done")
	chk = &fakeCheck{results: []sets.CheckResult{fail("x")}}
	if s := verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check}); s.Reason != core.SettleFailed || chk.count() != 3 {
		t.Fatalf("default 3: %s %d", s.Reason, chk.count())
	}
}

// ST-FR-49: 보고는 끝 2000줄, 비면 (no output).
func TestVerifyReport(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 2500; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	for _, c := range []struct {
		report, want string
	}{
		{"", "(no output)"},
		{b.String(), "[earlier output truncated]\nline501\n"},
		{strings.Repeat("x", 60*1024), "[earlier output truncated]\n" + strings.Repeat("x", 50*1024)},
	} {
		m := scripted("write", "done", "write", "done")
		chk := &fakeCheck{results: []sets.CheckResult{fail(c.report), pass}}
		verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check})
		got := strings.TrimPrefix(lastText(m.requests()[2]), "Verification failed (attempt 1 of 3). Fix the problems below, then finish.\n\n")
		if !strings.HasPrefix(got, c.want) || (c.report == b.String() && !strings.HasSuffix(got, "\nline2500")) || len(got) > 50*1024+40 {
			t.Errorf("report %.60q... len %d", got, len(got))
		}
	}
}

// ST-FR-48: Check error → run 실패.
func TestVerifyCheckError(t *testing.T) {
	chk := &fakeCheck{err: errors.New("boom")}
	s := verifyRun(t, deps(scripted("write", "done"), writeTool()), sets.VerifyOptions{Check: chk.check})
	if s.Reason != core.SettleFailed || !strings.Contains(s.Err.Error(), "verify: check: boom") || errors.Is(s.Err, sets.ErrVerifyFailed) {
		t.Fatalf("settle %s err %v", s.Reason, s.Err)
	}
}

// ST-FR-46: 아래 층을 바꿔 쌓으면 settle 을 내는 시점이 그 층을 따른다. 아래 층 에러는 그대로.
func TestVerifyNext(t *testing.T) {
	m := scripted("write", "unused")
	chk := &fakeCheck{results: []sets.CheckResult{pass}}
	next := func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	}
	s := verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check}, inject.FunctionSet{Name: "lower", TurnFinish: next})
	if s.Reason != core.SettleCompleted || chk.count() != 1 || len(m.requests()) != 1 {
		t.Fatalf("settle %s checks %d reqs %d", s.Reason, chk.count(), len(m.requests()))
	}
	boom := func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return nil, errors.New("next boom")
	}
	if s := verifyRun(t, deps(scripted("hi"), writeTool()), sets.VerifyOptions{Check: chk.check}, inject.FunctionSet{Name: "lower", TurnFinish: boom}); s.Reason != core.SettleFailed || !strings.Contains(s.Err.Error(), "next boom") {
		t.Fatalf("next error: %s %v", s.Reason, s.Err)
	}
}

// ST-FR-50: 상태는 run 마다 초기화된다 (같은 세션, 같은 Store).
func TestVerifyStatePerRun(t *testing.T) {
	d := deps(scripted("write", "done", "done"), writeTool())
	chk := &fakeCheck{results: []sets.CheckResult{fail("x")}}
	if s := verifyRun(t, d, sets.VerifyOptions{Check: chk.check, MaxAttempts: 2}); s.Reason != core.SettleFailed {
		t.Fatalf("run1 %s", s.Reason)
	}
	m := scripted("hi")
	d.Model = m
	if s := verifyRun(t, d, sets.VerifyOptions{Check: chk.check, MaxAttempts: 2}); s.Reason != core.SettleCompleted || chk.count() != 2 {
		t.Fatalf("run2 text only: %s checks %d", s.Reason, chk.count())
	}
	m = scripted("write", "done", "write", "done")
	d.Model = m
	chk.results = []sets.CheckResult{fail("x"), fail("x"), fail("y"), pass}
	if s := verifyRun(t, d, sets.VerifyOptions{Check: chk.check, MaxAttempts: 2}); s.Reason != core.SettleCompleted {
		t.Fatalf("run3 %s %v", s.Reason, s.Err)
	}
	if got := lastText(m.requests()[2]); !strings.HasPrefix(got, "Verification failed (attempt 1 of 2).") {
		t.Fatalf("run3 report %q", got)
	}
}

func TestVerifyNeedsCheck(t *testing.T) {
	if _, err := sets.Verify(deps(scripted("x")), sets.VerifyOptions{}); err == nil || err.Error() != "sets: verify: Check is required" {
		t.Fatalf("err %v", err)
	}
	v, err := sets.Verify(deps(scripted("x")), sets.VerifyOptions{Check: (&fakeCheck{}).check})
	if err != nil || v.Name != "verify" || v.TurnFinish == nil || v.TurnStart != nil || v.RunBeforeSettle != nil {
		t.Fatalf("set %+v %v", v.Name, err)
	}
}

// ST-FR-47·50: 통과한 검증 뒤 run 이 이어지면(입력함 → resilient 21 continue) 그 뒤 변경이 없으면 다시 검증하지 않는다.
func TestVerifyCheckedAfterPass(t *testing.T) {
	m := scripted("write", "done", "ok")
	d := deps(m, writeTool())
	var run *core.Run
	ready := make(chan struct{})
	calls := 0
	check := func(ctx context.Context) (sets.CheckResult, error) {
		<-ready
		calls++
		if calls == 1 {
			run.Input(user("u-late", "one more thing"))
		}
		return pass, nil
	}
	v, err := sets.Verify(d, sets.VerifyOptions{Check: check})
	if err != nil {
		t.Fatal(err)
	}
	disp, err := inject.BindWith(inject.BindOptions{StateStore: stateStoreOf(d.Store)}, stack(d, true, v)...)
	if err != nil {
		t.Fatal(err)
	}
	run = core.New(disp, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1",
		Input: []contract.Message{user("u1", "go")}, Config: json.RawMessage(cfgJSON)})
	close(ready)
	s := run.Wait()
	if s.Reason != core.SettleCompleted || calls != 1 || len(m.requests()) != 3 {
		t.Fatalf("settle %s %v checks %d reqs %d", s.Reason, s.Err, calls, len(m.requests()))
	}
}

// ST-FR-49: 50KB 경계와 UTF-8 경계, ST-FR-48: 빈 보고의 첫 줄.
func TestVerifyReportBoundary(t *testing.T) {
	for _, c := range []struct {
		report, want string
	}{
		{strings.Repeat("x", 50*1024), strings.Repeat("x", 50*1024)},
		{strings.Repeat("x", 50*1024+1), "[earlier output truncated]\n" + strings.Repeat("x", 50*1024)},
		{strings.Repeat("가", 20000), "[earlier output truncated]\n" + strings.Repeat("가", 50*1024/3)},
		{strings.Repeat(strings.Repeat("y", 999)+"\n", 100), "[earlier output truncated]\n" + strings.TrimSuffix(strings.Repeat(strings.Repeat("y", 999)+"\n", 51), "\n")},
	} {
		m := scripted("write", "done", "write", "done")
		chk := &fakeCheck{results: []sets.CheckResult{fail(c.report), pass}}
		verifyRun(t, deps(m, writeTool()), sets.VerifyOptions{Check: chk.check})
		if got := strings.TrimPrefix(lastText(m.requests()[2]), "Verification failed (attempt 1 of 3). Fix the problems below, then finish.\n\n"); got != c.want {
			t.Errorf("len %d: got len %d prefix %.40q", len(c.report), len(got), got)
		}
	}
	chk := &fakeCheck{results: []sets.CheckResult{fail(" \n")}}
	s := verifyRun(t, deps(scripted("write", "done"), writeTool()), sets.VerifyOptions{Check: chk.check, MaxAttempts: 1})
	if s.Reason != core.SettleFailed || !strings.HasSuffix(s.Err.Error(), "after 1 attempts: ") {
		t.Fatalf("empty report: %s %v", s.Reason, s.Err)
	}
}
