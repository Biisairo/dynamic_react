package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

type tc struct{ name, args string }

// loopModel 은 n 번째 턴에 turns[n] 의 호출을 낸다 (빈 목록이면 텍스트 답). 끝나면 마지막 것을 반복.
func loopModel(turns ...[]tc) *fakeModel {
	return &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		calls := turns[min(n, len(turns)-1)]
		id := fmt.Sprintf("a%d", n)
		if len(calls) == 0 {
			return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopStop, text("done")))
		}
		var blocks []contract.Block
		for i, c := range calls {
			blocks = append(blocks, toolCall(fmt.Sprintf("c%d-%d", n, i), c.name, c.args))
		}
		return doneEv(assistant(id, contract.Usage{Output: 1}, contract.StopToolUse, blocks...))
	}}
}

func turns(n int, calls ...tc) [][]tc {
	out := make([][]tc, n)
	for i := range out {
		out[i] = calls
	}
	return out
}

// sayTool 은 인자 r 을 결과 텍스트로 돌려준다. name 이 "bad" 면 error 결과, "mute" 면 내용 없음.
func sayTool(name string) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`)},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult {
			var a struct{ R string }
			json.Unmarshal(c.Args, &a)
			switch name {
			case "bad":
				return contract.ToolResult{CallID: c.ID, Status: contract.StatusError, Content: []contract.Block{text("bad:" + a.R)}}
			case "mute":
				return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK}
			case "two":
				return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text("a" + a.R), text("b")}}
			}
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text(a.R)}}
		},
	}
}

func repeatDeps(m *fakeModel) sets.Deps {
	return deps(m, echoTool(nil), sayTool("say"), sayTool("bad"), sayTool("mute"), sayTool("wait"), sayTool("two"))
}

// repeatRun 은 stack(+resilient) + below + repeat 로 run 하나를 돌린다.
func repeatRun(t *testing.T, d sets.Deps, o sets.RepeatOptions, below ...inject.FunctionSet) core.Settle {
	t.Helper()
	r, err := sets.Repeat(d, o)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := inject.BindWith(inject.BindOptions{StateStore: stateStoreOf(d.Store)}, stack(d, true, append(below, r)...)...)
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

const redirectHead = "Repeated tool calls: you made the same tool calls "
const redirectTail = "\nDo not make these calls again with the same arguments. Use different arguments, choose another tool, or finish with what you have."

// redirects 는 요청 메시지 중 지시 메시지의 수다.
func redirects(req contract.Request) int {
	n := 0
	for _, m := range req.Messages {
		if m.Role == contract.RoleUser && len(m.Content) == 1 && strings.HasPrefix(m.Content[0].Text, redirectHead) {
			n++
		}
	}
	return n
}

// ST-FR-66·67·68: 기본값 5·8. 5턴째 뒤에 지시가 한 번 들어가고 8턴째에 run 이 실패한다.
func TestRepeatWarnStop(t *testing.T) {
	m := loopModel(turns(1, tc{"echo", `{"s":"x"}`})...)
	d := repeatDeps(m)
	s := repeatRun(t, d, sets.RepeatOptions{})
	reqs := m.requests()
	if s.Reason != core.SettleFailed || !errors.Is(s.Err, sets.ErrRepeat) || len(reqs) != 8 {
		t.Fatalf("settle %s err %v reqs %d", s.Reason, s.Err, len(reqs))
	}
	if !strings.Contains(s.Err.Error(), "repeat: same tool calls repeated: 8 turns in a row") {
		t.Fatalf("err %v", s.Err)
	}
	want := redirectHead + "5 turns in a row with identical arguments.\n\n- echo {\"s\":\"x\"}\n  last result: echo:x\n" + redirectTail
	if got := lastText(reqs[5]); got != want {
		t.Fatalf("redirect %q", got)
	}
	if redirects(reqs[4]) != 0 || redirects(reqs[7]) != 1 {
		t.Fatalf("redirects before %d after %d", redirects(reqs[4]), redirects(reqs[7]))
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	found := false
	for _, msg := range sess.Messages {
		if msg.Role == contract.RoleUser && len(msg.Content) == 1 && msg.Content[0].Text == want && strings.HasPrefix(msg.ID, "id") && msg.Timestamp.Equal(ts) {
			found = true
		}
	}
	if !found {
		t.Fatal("redirect not stored")
	}
}

// ST-FR-67: Warn 미만이면 아무것도 하지 않는다.
func TestRepeatBelowWarn(t *testing.T) {
	m := loopModel(append(turns(4, tc{"echo", `{"s":"x"}`}), nil)...)
	s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{})
	reqs := m.requests()
	if s.Reason != core.SettleCompleted || len(reqs) != 5 || redirects(reqs[4]) != 0 {
		t.Fatalf("settle %s %v reqs %d", s.Reason, s.Err, len(reqs))
	}
}

// ST-FR-65·66: 지문과 연속 수 (Warn 2, Stop 3).
func TestRepeatFingerprint(t *testing.T) {
	x, y := tc{"echo", `{"s":"x"}`}, tc{"echo", `{"s":"y"}`}
	for _, c := range []struct {
		name   string
		turns  [][]tc
		exempt []string
		failed bool
	}{
		{"key order and spaces", [][]tc{{{"say", `{"a":1,"r":"q"}`}}, {{"say", `{ "r" : "q", "a" : 1 }`}}, {{"say", `{"r":"q","a":1}`}}}, nil, true},
		{"different values", [][]tc{{x}, {y}, {x}, {y}, nil}, nil, false},
		{"number notation", [][]tc{{{"say", `{"n":1}`}}, {{"say", `{"n":1.0}`}}, {{"say", `{"n":1}`}}, nil}, nil, false},
		{"order in turn", [][]tc{{x, y}, {y, x}, {x, y}}, nil, true},
		{"different name", [][]tc{{{"say", `{"s":"x"}`}}, {x}, {{"say", `{"s":"x"}`}}, nil}, nil, false},
		{"subset", [][]tc{{x, y}, {x}, {x, y}, nil}, nil, false},
		{"error results count", turns(3, tc{"bad", `{"r":"z"}`}), nil, true},
		{"exempt only", append(turns(4, tc{"wait", `{}`}), nil), []string{"wait"}, false},
		{"exempt mixed", turns(3, tc{"wait", `{}`}, x), []string{"wait"}, true},
		{"exempt args in fingerprint", [][]tc{{{"wait", `{"r":"a"}`}, x}, {{"wait", `{"r":"b"}`}, x}, {{"wait", `{"r":"a"}`}, x}, nil}, []string{"wait"}, false},
	} {
		m := loopModel(c.turns...)
		s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 2, Stop: 3, Exempt: c.exempt})
		reqs := m.requests()
		if c.failed {
			if s.Reason != core.SettleFailed || !errors.Is(s.Err, sets.ErrRepeat) || len(reqs) != 3 || redirects(reqs[2]) != 1 {
				t.Errorf("%s: settle %s err %v reqs %d", c.name, s.Reason, s.Err, len(reqs))
			}
			continue
		}
		if s.Reason != core.SettleCompleted || redirects(reqs[len(reqs)-1]) != 0 {
			t.Errorf("%s: settle %s err %v reqs %d redirects %d", c.name, s.Reason, s.Err, len(reqs), redirects(reqs[len(reqs)-1]))
		}
	}
}

// ST-FR-68: 지시에는 면제가 아닌 호출만, 이번 턴의 호출 순서로 적는다.
func TestRepeatRedirectCalls(t *testing.T) {
	m := loopModel(append(turns(2, tc{"say", `{"r":"one"}`}, tc{"wait", `{}`}, tc{"echo", `{"s":"x"}`}), nil)...)
	s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 2, Stop: 3, Exempt: []string{"wait"}})
	reqs := m.requests()
	want := redirectHead + "2 turns in a row with identical arguments.\n\n- say {\"r\":\"one\"}\n  last result: one\n- echo {\"s\":\"x\"}\n  last result: echo:x\n" + redirectTail
	if s.Reason != core.SettleCompleted || len(reqs) != 3 || lastText(reqs[2]) != want {
		t.Fatalf("settle %s %v reqs %d redirect %q", s.Reason, s.Err, len(reqs), lastText(reqs[len(reqs)-1]))
	}
}

// ST-FR-68: 요약 — 공백 압축, 인자 400자·결과 200자(rune), 넘으면 "…", 결과 없음.
func TestRepeatSummary(t *testing.T) {
	long := strings.Repeat("가", 450)
	for _, c := range []struct {
		name, tool, args, wantArgs, wantResult string
	}{
		{"spaces", "say", `{"r":"a  \n\t b "}`, `{"r":"a \n\t b "}`, "a b"},
		{"result 200", "say", `{"r":"` + strings.Repeat("나", 200) + `"}`, `{"r":"` + strings.Repeat("나", 200) + `"}`, strings.Repeat("나", 200)},
		{"long", "say", `{"r":"` + long + `"}`, string([]rune(`{"r":"` + long)[:400]) + "…", strings.Repeat("가", 200) + "…"},
		{"no text", "mute", `{}`, `{}`, "(no text result)"},
		{"two blocks", "two", `{}`, `{}`, "a b"},
		{"blank text", "say", `{"r":"  "}`, `{"r":" "}`, "(no text result)"},
	} {
		m := loopModel(append(turns(2, tc{c.tool, c.args}), nil)...)
		repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 2, Stop: 3})
		reqs := m.requests()
		want := redirectHead + "2 turns in a row with identical arguments.\n\n- " + c.tool + " " + c.wantArgs + "\n  last result: " + c.wantResult + "\n" + redirectTail
		if len(reqs) != 3 || lastText(reqs[2]) != want {
			t.Errorf("%s: reqs %d redirect %q", c.name, len(reqs), lastText(reqs[len(reqs)-1]))
		}
	}
}

// ST-FR-66: 호출 없는 턴은 연속 수를 0 으로 되돌린다.
func TestRepeatResetOnNoCalls(t *testing.T) {
	x := tc{"echo", `{"s":"x"}`}
	m := loopModel([][]tc{{x}, nil, {x}, nil}...)
	continued := false
	lower := inject.FunctionSet{Name: "lower", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		sigs, err := ctx.Next()
		if err == nil && len(ctx.Tools().Calls()) == 0 && !continued {
			continued = true
			return []inject.TurnFinishSignal{ctx.ContinueTurn()}, nil
		}
		return sigs, err
	}}
	s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 2, Stop: 3}, lower)
	reqs := m.requests()
	if s.Reason != core.SettleCompleted || len(reqs) != 4 || redirects(reqs[3]) != 0 {
		t.Fatalf("settle %s %v reqs %d", s.Reason, s.Err, len(reqs))
	}
}

// ST-FR-64: 아래 층의 신호를 그대로 두고, 아래 층 에러는 그대로 돌려준다.
func TestRepeatNext(t *testing.T) {
	m := loopModel(turns(1, tc{"echo", `{"s":"x"}`})...)
	settle := inject.FunctionSet{Name: "lower", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	}}
	if s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 2, Stop: 3}, settle); s.Reason != core.SettleCompleted || len(m.requests()) != 1 {
		t.Fatalf("settle %s reqs %d", s.Reason, len(m.requests()))
	}
	m = loopModel(turns(1, tc{"echo", `{"s":"x"}`})...)
	stop := inject.FunctionSet{Name: "lower", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return []inject.TurnFinishSignal{ctx.Fail(errors.New("lower stop"))}, nil
	}}
	if s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 1, Stop: 2}, stop); s.Reason != core.SettleFailed || errors.Is(s.Err, sets.ErrRepeat) || !strings.Contains(s.Err.Error(), "lower stop") || len(m.requests()) != 1 {
		t.Fatalf("lower fail on warn: %s %v reqs %d", s.Reason, s.Err, len(m.requests()))
	}
	boom := inject.FunctionSet{Name: "lower", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		return nil, errors.New("next boom")
	}}
	if s := repeatRun(t, repeatDeps(loopModel(nil)), sets.RepeatOptions{}, boom); s.Reason != core.SettleFailed || !strings.Contains(s.Err.Error(), "next boom") {
		t.Fatalf("next error: %s %v", s.Reason, s.Err)
	}
}

// ST-FR-69: 상태는 run 마다 초기화된다 (같은 세션, 같은 Store).
func TestRepeatStatePerRun(t *testing.T) {
	x := tc{"echo", `{"s":"x"}`}
	d := repeatDeps(loopModel(turns(1, x)...))
	if s := repeatRun(t, d, sets.RepeatOptions{Warn: 2, Stop: 3}); s.Reason != core.SettleFailed {
		t.Fatalf("run1 %s", s.Reason)
	}
	m := loopModel([][]tc{{x}, {x}, nil}...)
	d.Model = m
	s := repeatRun(t, d, sets.RepeatOptions{Warn: 2, Stop: 3})
	reqs := m.requests()
	if s.Reason != core.SettleCompleted || len(reqs) != 3 || !strings.HasPrefix(lastText(reqs[2]), redirectHead+"2 turns") {
		t.Fatalf("run2 %s %v reqs %d last %q", s.Reason, s.Err, len(reqs), lastText(reqs[len(reqs)-1]))
	}
}

// 3.2d: 기본값과 옵션 검사, 채운 step.
func TestRepeatOptions(t *testing.T) {
	for _, o := range []sets.RepeatOptions{{Warn: 3, Stop: 3}, {Warn: 9}, {Stop: 4}, {Warn: 4, Stop: 2}} {
		if _, err := sets.Repeat(deps(scripted("x")), o); err == nil || err.Error() != "sets: repeat: Warn must be less than Stop" {
			t.Errorf("%+v: err %v", o, err)
		}
	}
	for _, o := range []sets.RepeatOptions{{}, {Warn: 4}, {Stop: 6}, {Warn: -1, Stop: -1}} {
		if _, err := sets.Repeat(deps(scripted("x")), o); err != nil {
			t.Errorf("%+v: err %v", o, err)
		}
	}
	r, err := sets.Repeat(deps(scripted("x")), sets.RepeatOptions{})
	if err != nil || r.Name != "repeat" || r.Description == "" || r.TurnFinish == nil || r.TurnStart != nil || r.ToolBefore != nil {
		t.Fatalf("set %+v %v", r.Name, err)
	}
	m := loopModel(turns(1, tc{"echo", `{"s":"x"}`})...)
	if s := repeatRun(t, repeatDeps(m), sets.RepeatOptions{Warn: 1, Stop: 2}); s.Reason != core.SettleFailed || len(m.requests()) != 2 || !strings.HasPrefix(lastText(m.requests()[1]), redirectHead+"1 turns") {
		t.Fatalf("warn 1: %s reqs %d", s.Reason, len(m.requests()))
	}
}
