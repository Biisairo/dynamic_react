package harness_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
)

// toolLoop 은 tools 턴 동안 t1 을 부르고 그다음 끝낸다. 요청 수로 세므로 run 마다 새로 만든다.
func toolLoop(tools int) *fakeModel {
	return &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n%(tools+1) < tools {
			return reply(fmt.Sprint("a", n), contract.StopToolUse, call(fmt.Sprint("c", n), "t1", `{}`))
		}
		return reply(fmt.Sprint("a", n), contract.StopStop, text("done"))
	}}
}

// finalizeSet 은 아래 층의 확정 요청을 fn 으로 고친다. n 은 이 세트가 불린 횟수(0부터)다.
func finalizeSet(name string, fn func(n int, req contract.Request) contract.Request) inject.FunctionSet {
	var calls atomic.Int32
	return inject.FunctionSet{Name: name, Description: "test " + name,
		RequestFinalize: func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
			req, sigs, err := ctx.Next(req)
			if err != nil {
				return req, sigs, err
			}
			return fn(int(calls.Add(1)-1), req), sigs, nil
		}}
}

func dropTool(name string) func(int, contract.Request) contract.Request {
	return func(_ int, req contract.Request) contract.Request {
		var out []contract.ToolSpec
		for _, t := range req.Tools {
			if t.Name != name {
				out = append(out, t)
			}
		}
		req.Tools = out
		return req
	}
}

// prefixAgent 는 t1 도구, 매 턴 꼬리 메시지(tagSet x), extra 세트, OnEvent 를 가진 Agent 다.
func prefixAgent(t *testing.T, m *fakeModel, rec *recorder, extra ...harness.SetEntry) *harness.Agent {
	t.Helper()
	return newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.ToolSet = func(def []sets.Tool) []sets.Tool { return append(def, objTool("t1", "T1.")) }
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			return append(append(harness.DefaultSets(d), harness.SetEntry{Set: tagSet("x"), On: true}), extra...)
		}
	})
}

func breaks(rec *recorder) []harness.PrefixBreak {
	var out []harness.PrefixBreak
	for _, ev := range rec.all() {
		if ev.Kind == harness.EventPrefixBreak {
			out = append(out, *ev.Prefix)
		}
	}
	return out
}

// HN-FR-75 ②④: 덧붙기만 하면(꼬리는 바뀌어도) 이벤트가 없다. 세션 첫 요청도 없다. 다음 run 도 이어진다.
func TestPrefixAppendOnly(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(2), rec)
	start(t, a, "s1", user("u1", "go"))
	start(t, a, "s1", user("u2", "again"))
	if b := breaks(rec); len(b) != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75 ③: tools 가 바뀌면 Part tools. 이벤트는 그 호출의 message_start 보다 먼저.
func TestPrefixTools(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(1), rec, harness.SetEntry{Set: finalizeSet("drop", func(turn int, req contract.Request) contract.Request {
		if turn >= 1 {
			return dropTool("t1")(turn, req)
		}
		return req
	}), On: true})
	start(t, a, "s1", user("u1", "go"))
	b := breaks(rec)
	if len(b) != 1 || b[0].Part != harness.PrefixTools || b[0].At != -1 || b[0].Total != 1 ||
		b[0].Expected || len(b[0].Edits) != 0 || b[0].Model.Provider != "p" || b[0].Model.ID != "m" {
		t.Fatalf("breaks %+v", b)
	}
	kinds := strings.Join(rec.kinds(), ",")
	if !strings.Contains(kinds, "turn_end,input,prefix_break,message_start") && !strings.Contains(kinds, "turn_end,prefix_break,message_start") {
		t.Fatalf("order %s", kinds)
	}
}

// HN-FR-75 ③: system 이 바뀌면 Part system.
func TestPrefixSystem(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(1), rec, harness.SetEntry{Set: finalizeSet("sys", func(turn int, req contract.Request) contract.Request {
		if turn >= 1 {
			req.System = append(req.System, text("extra"))
		}
		return req
	}), On: true})
	start(t, a, "s1", user("u1", "go"))
	if b := breaks(rec); len(b) != 1 || b[0].Part != harness.PrefixSystem || b[0].At != -1 || b[0].Expected {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75 ④: 편집 없이 저장된 메시지가 바뀌면 Part messages, Expected false.
func TestPrefixMessages(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(2), rec, harness.SetEntry{Set: finalizeSet("edit", func(turn int, req contract.Request) contract.Request {
		if turn >= 2 {
			msgs := append([]contract.Message(nil), req.Messages...)
			msgs[1].Content = []contract.Block{text("rewritten")}
			req.Messages = msgs
		}
		return req
	}), On: true})
	start(t, a, "s1", user("u1", "go"))
	// 턴1 의 안정 구간 = u1, a0, r0 (꼬리 tag 제외)
	if b := breaks(rec); len(b) != 1 || b[0].Part != harness.PrefixMessages || b[0].At != 1 || b[0].Total != 3 ||
		b[0].Expected || len(b[0].Edits) != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-74: provider 에 보내지 않는 필드(ID·Timestamp)만 다르면 같다.
func TestPrefixIgnoresIDs(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(2), rec, harness.SetEntry{Set: finalizeSet("pre", func(turn int, req contract.Request) contract.Request {
		m := user(fmt.Sprint("pre-", turn), "same")
		req.Messages = append([]contract.Message{m}, req.Messages...)
		return req
	}), On: true})
	start(t, a, "s1", user("u1", "go"))
	if b := breaks(rec); len(b) != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75 ④: 압축(replace_range)으로 바뀌면 Expected true, Edits 에 그 종류.
func TestPrefixCompaction(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch {
		case n == 0:
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}}}
		case len(req.System) == 1 && req.System[0].Text == summary.SystemPrompt:
			return reply("sum", contract.StopStop, text("## Goal\nsummarized"))
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	long := func(id string) contract.Message { return user(id, strings.Repeat("abcd", 50)) }
	r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{long("u1"), long("u2"), long("u3")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"compaction":{"keepRecentTokens":10}}`)})
	wait(t, r)
	b := breaks(rec)
	if len(b) != 1 || b[0].Part != harness.PrefixMessages || !b[0].Expected || len(b[0].Edits) != 1 ||
		b[0].Edits[0] != contract.EditReplaceRange || b[0].Total != 3 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-77: 같은 요청을 다시 보내면(recall) 이벤트가 없다.
func TestPrefixRecall(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n < 2 {
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassRateLimit, Message: "slow down"}}}
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) { o.OnEvent = rec.on })
	r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"retry":{"baseDelayMs":1,"maxDelayMs":2}}`)})
	wait(t, r)
	if len(m.requests()) != 3 {
		t.Fatalf("requests %d", len(m.requests()))
	}
	if b := breaks(rec); len(b) != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75 ①: 모델 A → B → A. B 첫 요청에 Part model, A 로 돌아오면 덧붙기뿐이라 없음.
func TestPrefixModelSwitch(t *testing.T) {
	var calls atomic.Int32
	pick := inject.FunctionSet{Name: "route", Description: "test route",
		ModelSelect: func(ctx *inject.ModelSelectCtx, req contract.Request) (contract.ModelInfo, []inject.ModelSelectSignal, error) {
			id := "A"
			if calls.Add(1) == 2 {
				id = "B"
			}
			return contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: id, API: "x"}, ContextWindow: 100000, MaxOutputTokens: 500}, nil, nil
		}}
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(2), rec, harness.SetEntry{Set: pick, On: true})
	start(t, a, "s1", user("u1", "go"))
	b := breaks(rec)
	if len(b) != 1 || b[0].Part != harness.PrefixModel || b[0].Model.ID != "B" || b[0].At != -1 || b[0].Total != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75·76: 모드 진입·이탈이 tools 를 바꾸면 진입 턴과 이탈 뒤 첫 요청에 Part tools.
func TestPrefixModeSwitch(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return reply("a0", contract.StopToolUse, call("c0", "mode", `{"name":"m"}`))
		}
		return reply(fmt.Sprint("a", n), contract.StopStop, text("done"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			return append(harness.DefaultSets(d), harness.SetEntry{Set: finalizeSet("drop", dropTool("write")), On: false})
		}
		o.Modes = []harness.Mode{{Name: "m", Description: "mode m", Push: []string{"drop"}}}
	})
	start(t, a, "s1", user("u1", "go"))
	if err := request(a, "s1", "", harness.ModeUser); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u2", "again"))
	b := breaks(rec)
	if len(b) != 2 || b[0].Part != harness.PrefixTools || b[1].Part != harness.PrefixTools {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-76: run 사이 Arrange 로 tools 를 바꾸는 세트를 켜면 다음 run 첫 요청에 Part tools.
func TestPrefixArrange(t *testing.T) {
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			return append(harness.DefaultSets(d), harness.SetEntry{Set: finalizeSet("drop", dropTool("write")), On: false})
		}
	})
	start(t, a, "s1", user("u1", "go"))
	if err := a.Arrange(layoutOf("+base +require +cache +guard +truncate +resilient +drop")); err != nil {
		t.Fatal(err)
	}
	start(t, a, "s1", user("u2", "again"))
	start(t, a, "s2", user("u3", "other session"))
	if b := breaks(rec); len(b) != 1 || b[0].Part != harness.PrefixTools || b[0].Total != 1 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-76: 동시 run 에 안전하다 (-race).
func TestPrefixConcurrent(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(1), rec)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start(t, a, fmt.Sprint("s", i), user(fmt.Sprint("u", i), "go"))
		}()
	}
	wg.Wait()
	if b := breaks(rec); len(b) != 0 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-74 ③: 도구 결과의 내용만 바뀌어도 Part messages.
func TestPrefixToolResultChange(t *testing.T) {
	rec := &recorder{}
	a := prefixAgent(t, toolLoop(2), rec, harness.SetEntry{Set: finalizeSet("res", func(n int, req contract.Request) contract.Request {
		if n >= 2 {
			msgs := append([]contract.Message(nil), req.Messages...)
			r := *msgs[2].Result
			r.Content = []contract.Block{text("rewritten result")}
			msgs[2].Result = &r
			req.Messages = msgs
		}
		return req
	}), On: true})
	start(t, a, "s1", user("u1", "go"))
	if b := breaks(rec); len(b) != 1 || b[0].Part != harness.PrefixMessages || b[0].At != 2 {
		t.Fatalf("breaks %+v", b)
	}
}

// HN-FR-75: 편집이 있어도 tools 가 바뀌면 Expected 는 false.
func TestPrefixToolsWithEdit(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		switch {
		case n == 0:
			return []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}}}
		case len(req.System) == 1 && req.System[0].Text == summary.SystemPrompt:
			return reply("sum", contract.StopStop, text("## Goal\nsummarized"))
		}
		return reply("a", contract.StopStop, text("ok"))
	}}
	rec := &recorder{}
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.OnEvent = rec.on
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			return append(harness.DefaultSets(d), harness.SetEntry{Set: finalizeSet("drop", func(n int, req contract.Request) contract.Request {
				if n >= 1 {
					return dropTool("write")(n, req)
				}
				return req
			}), On: true})
		}
	})
	long := func(id string) contract.Message { return user(id, strings.Repeat("abcd", 50)) }
	r, _, _ := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{long("u1"), long("u2"), long("u3")},
		Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"compaction":{"keepRecentTokens":10}}`)})
	wait(t, r)
	b := breaks(rec)
	if len(b) != 1 || b[0].Part != harness.PrefixTools || len(b[0].Edits) != 1 || b[0].Expected {
		t.Fatalf("breaks %+v", b)
	}
}
