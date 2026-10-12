package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

func layoutString(l []harness.SetState) string {
	var out []string
	for _, s := range l {
		mark := "-"
		if s.On {
			mark = "+"
		}
		out = append(out, mark+s.Name)
	}
	return strings.Join(out, " ")
}

func layoutOf(spec string) []harness.SetState {
	var out []harness.SetState
	for _, f := range strings.Fields(spec) {
		out = append(out, harness.SetState{Name: f[1:], On: f[0] == '+'})
	}
	return out
}

// tagSet 은 context.inject 에서 아래 층의 메시지 뒤에 자기 이름의 메시지를 붙인다.
func tagSet(name string) inject.FunctionSet {
	return inject.FunctionSet{Name: name, Description: "tag " + name,
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			msgs, sigs, err := ctx.Next()
			return append(msgs, user("tag-"+name+"-"+ctx.Flow().RunID(), "tag:"+name)), sigs, err
		}}
}

func tags(req contract.Request) string {
	var out []string
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if t, ok := strings.CutPrefix(b.Text, "tag:"); ok {
				out = append(out, t)
			}
		}
	}
	return strings.Join(out, ",")
}

func withTags(o *harness.Options) {
	o.Sets = func(d sets.Deps) []harness.SetEntry {
		return append(harness.DefaultSets(d), harness.SetEntry{Set: tagSet("x"), On: true}, harness.SetEntry{Set: tagSet("y"), On: false})
	}
}

const defaultLayout = "+base +require +cache +guard +truncate +resilient +repeat"

// HN-FR-51: 기본 배치 = 등록 순서·켜짐, Description 은 세트의 것
func TestSetsDefaultLayout(t *testing.T) {
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), withTags)
	got := a.Sets()
	if layoutString(got) != defaultLayout+" +x -y" {
		t.Fatalf("layout %s", layoutString(got))
	}
	if got[0].Description == "" || got[7].Description != "tag x" {
		t.Fatalf("descriptions %+v", got)
	}
	got[0].On = false
	if !a.Sets()[0].On {
		t.Fatal("Sets() exposes internal state")
	}
}

// HN-FR-52: Layout 반영 — 앞으로, 모르는 이름·중복 무시, 빠진 등록 세트는 뒤에 등록 순서·기본 켜짐
func TestSetsInitialLayout(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		withTags(o)
		o.Layout = layoutOf("+y -truncate +ghost -x +y +truncate")
	})
	if got := layoutString(a.Sets()); got != "+y -truncate -x +base +require +cache +guard +resilient +repeat" {
		t.Fatalf("layout %s", got)
	}
	start(t, a, "s1", user("u1", "hi"))
	// y 는 require 아래라서 require 의 context.inject(Next 없음)가 덮는다
	if got := tags(m.requests()[0]); got != "" {
		t.Fatalf("tags %q", got)
	}
	o := options(envtest.New("/w", "/h"), textModel())
	o.Layout = layoutOf("-require")
	if _, err := harness.New(context.Background(), o); err == nil || !strings.Contains(err.Error(), "harness: sets:") {
		t.Fatalf("empty step accepted: %v", err)
	}
}

// HN-FR-53: guard 를 끄면 다음 run 에서 없는 도구가 base 17 의 결과가 되고 Gate 가 불린다. 이벤트는 유지.
func TestArrangeGuardOff(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n%2 == 0 {
			return reply(fmt.Sprint("a", n), contract.StopToolUse, call(fmt.Sprint("c", n), "nope", `{}`))
		}
		return reply(fmt.Sprint("a", n), contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	var mu sync.Mutex
	var gated []string
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Gate = func(_ context.Context, in harness.GateInput) *contract.ToolResult {
			mu.Lock()
			gated = append(gated, in.Call.ID)
			mu.Unlock()
			return nil
		}
	})
	result := func(req contract.Request) string {
		for _, msg := range req.Messages {
			if msg.Result != nil {
				return msg.Result.Content[0].Text
			}
		}
		return ""
	}
	start(t, a, "s1", user("u1", "go"))
	if err := a.Arrange(layoutOf("+base +require +cache -guard +truncate +resilient +repeat")); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s2", user("u2", "go"))
	reqs := m.requests()
	if len(reqs) != 4 || result(reqs[1]) != "unknown tool: nope" || result(reqs[3]) != "unknown tool: nope" {
		t.Fatalf("results %q %q", result(reqs[1]), result(reqs[3]))
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(gated, ",") != "c2" {
		t.Fatalf("gated %v", gated)
	}
	kinds := strings.Join(rec.kinds(), ",")
	if strings.Count(kinds, "tool_start") != 2 || strings.Count(kinds, "tool_end") != 2 || strings.Count(kinds, "run_end") != 2 {
		t.Fatalf("events %s", kinds)
	}
	if got := layoutString(a.Sets()); got != "+base +require +cache -guard +truncate +resilient +repeat" {
		t.Fatalf("layout %s", got)
	}
}

// HN-FR-53: 같은 step 을 채운 두 세트의 순서를 바꾸면 다음 run 에 반영된다.
func TestArrangeOrder(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, withTags)
	if err := a.Arrange(layoutOf(defaultLayout + " +x +y")); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u1", "hi"))
	if err := a.Arrange(layoutOf(defaultLayout + " +y +x")); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s2", user("u2", "hi"))
	reqs := m.requests()
	if tags(reqs[0]) != "x,y" || tags(reqs[1]) != "y,x" {
		t.Fatalf("tags %q %q", tags(reqs[0]), tags(reqs[1]))
	}
}

// HN-FR-53: 실행 중 Arrange — 그 run 은 옛 구성, 다음 run 은 새 구성
func TestArrangeDuringRun(t *testing.T) {
	gate := make(chan struct{})
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			<-gate
			return reply("a0", contract.StopToolUse, call("c0", "nope", `{}`))
		}
		return reply(fmt.Sprint("a", n), contract.StopStop, text("ok"))
	}}
	a := newAgent(t, envtest.New("/w", "/h"), m, withTags)
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")}, Config: json.RawMessage(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Arrange(layoutOf(defaultLayout + " -x +y")); err != nil {
		t.Fatal(err)
	}
	close(gate)
	if s := wait(t, r); s.Reason != core.SettleCompleted {
		t.Fatalf("%+v", s)
	}
	start(t, a, "s2", user("u2", "hi"))
	reqs := m.requests()
	if len(reqs) != 3 || tags(reqs[1]) != "x" || tags(reqs[2]) != "y" {
		t.Fatalf("tags %d %q %q", len(reqs), tags(reqs[1]), tags(reqs[2]))
	}
}

// HN-FR-53: 빠짐·중복·모르는 이름 → ErrSetLayout, Bind 실패 → 그 에러. 배치는 그대로.
func TestArrangeRejects(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, withTags)
	before := layoutString(a.Sets())
	for name, l := range map[string]string{
		"missing":          defaultLayout + " +x",
		"duplicate":        defaultLayout + " +x +y +x",
		"dup, same length": defaultLayout + " +x +x",
		"unknown":          defaultLayout + " +x +y +ghost",
		"empty":            "",
	} {
		if err := a.Arrange(layoutOf(l)); !errors.Is(err, harness.ErrSetLayout) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if harness.ErrSetLayout.Error() != "harness: invalid set layout" {
		t.Fatalf("message %q", harness.ErrSetLayout)
	}
	err := a.Arrange(layoutOf("+base -require +cache +guard +truncate +resilient +repeat +x +y"))
	if err == nil || errors.Is(err, harness.ErrSetLayout) || !strings.Contains(err.Error(), "harness: sets:") {
		t.Fatalf("bind failure: %v", err)
	}
	if got := layoutString(a.Sets()); got != before {
		t.Fatalf("layout changed: %s", got)
	}
	start(t, a, "s1", user("u1", "hi"))
	if tags(m.requests()[0]) != "x" {
		t.Fatal("old layout not used")
	}
}

// HN-FR-54: 동시 Arrange·Start·Sets (-race)
func TestArrangeConcurrent(t *testing.T) {
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), withTags)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			on := "+"
			if i%2 == 0 {
				on = "-"
			}
			if err := a.Arrange(layoutOf(defaultLayout + " " + on + "x +y")); err != nil {
				t.Error(err)
			}
			_ = a.Sets()
			r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: fmt.Sprint("s", i), Input: []contract.Message{user("u", "hi")}, Config: json.RawMessage(cfg)})
			if err != nil {
				t.Error(err)
				return
			}
			if s := wait(t, r); s.Reason != core.SettleCompleted {
				t.Errorf("%+v", s)
			}
		}()
	}
	wg.Wait()
}
