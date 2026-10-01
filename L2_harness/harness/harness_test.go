package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

const cfg = `{"model":{"provider":"p","id":"m"},"maxOutputTokens":1000}`

type fakeModel struct {
	mu     sync.Mutex
	reqs   []contract.Request
	script func(n int, req contract.Request) []contract.StreamEvent
}

func (m *fakeModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	m.mu.Lock()
	n := len(m.reqs)
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	evs := m.script(n, req)
	return func(yield func(contract.StreamEvent, error) bool) {
		for _, e := range evs {
			if !yield(e, nil) {
				return
			}
		}
	}
}

func (m *fakeModel) Lookup(context.Context, string, string) (contract.ModelInfo, bool) {
	return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "x"}, ContextWindow: 100000, MaxOutputTokens: 500}, true
}

func (m *fakeModel) requests() []contract.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]contract.Request(nil), m.reqs...)
}

func text(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

func user(id, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{text(s)}}
}

func reply(id string, stop contract.StopKind, blocks ...contract.Block) []contract.StreamEvent {
	m := contract.Message{ID: id, Role: contract.RoleAssistant, Timestamp: ts, Content: blocks,
		Usage: &contract.Usage{Input: 10, Output: 1}, StopReason: &contract.StopReason{Kind: stop}}
	return []contract.StreamEvent{{Kind: contract.EventStart}, {Kind: contract.EventTextDelta, Delta: "x"}, {Kind: contract.EventDone, Message: &m}}
}

func call(id, name, args string) contract.Block {
	return contract.Block{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}}
}

func ids() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprint("id", n)
	}
}

func options(e *envtest.Env, m *fakeModel) harness.Options {
	return harness.Options{Env: e, Model: m, SessionDir: "/h/sessions", Now: func() time.Time { return ts }, NewID: ids()}
}

func wait(t *testing.T, r *core.Run) core.Settle {
	t.Helper()
	done := make(chan core.Settle, 1)
	go func() { done <- r.Wait() }()
	select {
	case s := <-done:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("run did not settle")
		return core.Settle{}
	}
}

func start(t *testing.T, a *harness.Agent, sid string, in ...contract.Message) (core.Settle, string) {
	t.Helper()
	r, id, err := a.Start(context.Background(), harness.RunRequest{SessionID: sid, Input: in, Config: json.RawMessage(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	return wait(t, r), id
}

// HN-FR-26
func TestNewValidation(t *testing.T) {
	ctx := context.Background()
	e, m := envtest.New("/w", "/h"), &fakeModel{}
	for name, o := range map[string]harness.Options{
		"no env":       {Model: m, SessionDir: "/s"},
		"no model":     {Env: e, SessionDir: "/s"},
		"no dir":       {Env: e, Model: m},
		"relative dir": {Env: e, Model: m, SessionDir: "s"},
	} {
		if _, err := harness.New(ctx, o); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	e.FailOn("/w/AGENTS.md", errors.New("io"))
	if _, err := harness.New(ctx, options(e, m)); err == nil {
		t.Fatal("prompt env error ignored")
	}
}

// HN-FR-26, 29
func TestAssembly(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/AGENTS.md", "Project rule X.")
	e.SetFile("/h/.claude/skills/s/SKILL.md", "---\nname: myskill\ndescription: does things\n---\n")
	o := options(e, &fakeModel{})
	o.ToolSet = func(def []sets.Tool) []sets.Tool {
		return append(def,
			sets.Tool{Spec: contract.ToolSpec{Name: "mytool", Description: "My tool.\nMore detail.", InputSchema: json.RawMessage(`{"type":"object"}`)}},
			sets.Tool{Spec: contract.ToolSpec{Name: "read", Description: "replaced", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	}
	a, err := harness.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(a.Tools().Names(), ","); got != "read,bash,edit,write,grep,find,ls,mytool" {
		t.Fatalf("tools %s", got)
	}
	if tl, _ := a.Tools().Get("read"); tl.Spec.Description != "replaced" {
		t.Fatal("read not replaced")
	}
	sys := a.System()
	if len(sys) != 1 {
		t.Fatalf("system %+v", sys)
	}
	for _, want := range []string{"- read: Read file contents", "- mytool: My tool.\n", "Project rule X.", "myskill", "<cwd>\n/w\n</cwd>"} {
		if !strings.Contains(sys[0].Text, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}

// HN-FR-27
func TestStartChecks(t *testing.T) {
	m := &fakeModel{script: func(int, contract.Request) []contract.StreamEvent { return reply("a", contract.StopStop, text("ok")) }}
	a, _ := harness.New(context.Background(), options(envtest.New("/w", "/h"), m))
	ctx := context.Background()
	if _, _, err := a.Start(ctx, harness.RunRequest{Config: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("bad config accepted")
	}
	if _, _, err := a.Start(ctx, harness.RunRequest{SessionID: "../x", Config: json.RawMessage(cfg)}); err == nil {
		t.Fatal("bad session id accepted")
	}
	s, id := start(t, a, "", user("u1", "hi"))
	if s.Reason != core.SettleCompleted || !regexp.MustCompile(`^20260930-120000-[0-9a-f]{8}$`).MatchString(id) {
		t.Fatalf("settle %+v id %q", s, id)
	}
}

type counter struct {
	mu     sync.Mutex
	stream int
}

func (c *counter) Observe(ev core.ObserverEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ev.Kind == core.ObsStreamEvent {
		c.stream++
	}
}

// HN-NFR-02: read → edit → bash 라운드 후 종료
func TestToolRounds(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.txt", "hello\n")
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		c, _ := e.File("/w/a.txt")
		io.WriteString(out, c)
		return env.ExecResult{}, nil
	})
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch n {
		case 0:
			return reply("a0", contract.StopToolUse, call("c0", "read", `{"path":"a.txt"}`))
		case 1:
			return reply("a1", contract.StopToolUse, call("c1", "edit", `{"path":"a.txt","edits":[{"oldText":"hello","newText":"bye"}]}`))
		case 2:
			return reply("a2", contract.StopToolUse, call("c2", "bash", `{"command":"cat a.txt"}`))
		}
		return reply("a3", contract.StopStop, text("done"))
	}}
	obs := &counter{}
	o := options(e, m)
	o.Observer = obs
	a, err := harness.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := start(t, a, "s1", user("u1", "go"))
	if s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	reqs := m.requests()
	if len(reqs) != 4 {
		t.Fatalf("calls %d", len(reqs))
	}
	result := func(r contract.Request) string {
		last := r.Messages[len(r.Messages)-1]
		return last.Result.Content[0].Text
	}
	if result(reqs[1]) != "hello\n" || !strings.Contains(result(reqs[2]), "+bye") || result(reqs[3]) != "bye\n" {
		t.Fatalf("results %q | %q | %q", result(reqs[1]), result(reqs[2]), result(reqs[3]))
	}
	if c, _ := e.File("/w/a.txt"); c != "bye\n" {
		t.Fatalf("file %q", c)
	}
	raw, _ := e.File("/h/sessions/s1.jsonl")
	if strings.Count(raw, `"type":"message"`) != 8 { // u1, a0, r0, a1, r1, a2, r2, a3
		t.Fatalf("session file:\n%s", raw)
	}
	if obs.stream == 0 {
		t.Fatal("observer got no stream events")
	}
}

// HN-NFR-02: 새 Agent 로 세션 재개
func TestResume(t *testing.T) {
	e := envtest.New("/w", "/h")
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return reply(fmt.Sprint("a", n), contract.StopStop, text("ok"))
	}}
	a1, _ := harness.New(context.Background(), options(e, m))
	start(t, a1, "s1", user("u1", "first"))
	a2, _ := harness.New(context.Background(), options(e, m))
	if s, _ := start(t, a2, "s1", user("u2", "second")); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	var got []string
	for _, msg := range m.requests()[1].Messages {
		got = append(got, msg.ID)
	}
	if strings.Join(got, ",") != "u1,a0,u2" {
		t.Fatalf("resumed request %v", got)
	}
}

// HN-NFR-02: overflow → 실제 요약기로 압축 → 재호출
func TestOverflowWithSummarizer(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch {
		case n == 0:
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}}}
		case len(req.System) == 1 && req.System[0].Text == summary.SystemPrompt:
			return reply("sum", contract.StopStop, text("## Goal\nsummarized"))
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	a, _ := harness.New(context.Background(), options(envtest.New("/w", "/h"), m))
	long := func(id string) contract.Message { return user(id, strings.Repeat("abcd", 50)) }
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{long("u1"), long("u2"), long("u3")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"compaction":{"keepRecentTokens":10}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("settle %+v", s)
	}
	reqs := m.requests()
	if len(reqs) != 3 || reqs[1].Options.MaxOutputTokens != 500 {
		t.Fatalf("calls %d", len(reqs))
	}
	first := reqs[2].Messages[0].Content[0].Text
	if !strings.Contains(first, "## Goal\nsummarized") || len(reqs[2].Messages) != 2 {
		t.Fatalf("compacted request %+v", reqs[2].Messages)
	}
}

// HN-FR-28
func TestConcurrentRuns(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return reply(fmt.Sprint("a", n), contract.StopStop, text("ok"))
	}}
	a, _ := harness.New(context.Background(), options(envtest.New("/w", "/h"), m))
	var wg sync.WaitGroup
	for _, sid := range []string{"s1", "s2"} {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			if s, _ := start(t, a, sid, user("u", "x")); s.Reason != core.SettleCompleted {
				t.Errorf("%s %+v", sid, s)
			}
		}(sid)
	}
	wg.Wait()
}

// HN-NFR-04, HN-C1
func TestImports(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}:{{join .Imports \" \"}}", "./...").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	forbidden := map[string]bool{"os": true, "os/exec": true, "net": true, "net/http": true, "syscall": true, "io/ioutil": true, "path/filepath": true}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, _ := strings.Cut(line, ":")
		for _, imp := range strings.Fields(imports) {
			if forbidden[imp] {
				t.Errorf("%s imports %s", pkg, imp)
			}
			if strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") && !strings.HasPrefix(imp, "github.com/Biisairo/dynamic_react/") && !strings.HasPrefix(imp, "golang.org/x/text/") {
				t.Errorf("%s imports external %s", pkg, imp)
			}
			if strings.Contains(imp, "/adapters") {
				t.Errorf("%s imports adapters", pkg)
			}
		}
	}
}
