package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func newTestTerminal(w int, color bool) (*Terminal, *vscreen) {
	v := newVScreen(w)
	t := New(Options{Out: v, Width: func() int { return v.w }, Color: color})
	t.redraw()
	return t, v
}

// body 는 대기 상태 입력 줄(">")을 뺀 출력이다.
func body(v *vscreen) string {
	return strings.TrimSuffix(strings.TrimSuffix(v.text(), ">"), "\n")
}

func textDelta(s string) harness.Event {
	return harness.Event{Kind: harness.EventMessageUpdate, Delta: &contract.StreamEvent{Kind: contract.EventTextDelta, Delta: s}}
}

func thinkDelta(s string) harness.Event {
	return harness.Event{Kind: harness.EventMessageUpdate, Delta: &contract.StreamEvent{Kind: contract.EventThinkingDelta, Delta: s}}
}

func msgEnd(u *contract.Usage) harness.Event {
	return harness.Event{Kind: harness.EventMessageEnd, Message: &contract.Message{Role: contract.RoleAssistant, Usage: u}}
}

func toolCall(id, name, args string) *contract.ToolCall {
	return &contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func toolEnd(c *contract.ToolCall, st contract.ToolStatus, text string) harness.Event {
	r := &contract.ToolResult{CallID: c.ID, Status: st}
	if text != "" {
		r.Content = []contract.Block{{Kind: contract.BlockText, Text: text}}
	}
	return harness.Event{Kind: harness.EventToolEnd, Call: c, Result: r}
}

func runEnd(r core.SettleReason, err error) harness.Event {
	return harness.Event{Kind: harness.EventRunEnd, Settle: &core.Settle{Reason: r, Err: err}}
}

func emit(t *Terminal, evs ...harness.Event) {
	for _, ev := range evs {
		t.event(ev)
	}
}

// TM-FR-24, 25, 33: 텍스트 스트리밍, text↔thinking 줄바꿈, message_end 줄바꿈.
func TestRenderText(t *testing.T) {
	term, v := newTestTerminal(40, false)
	emit(term, harness.Event{Kind: harness.EventMessageStart}, thinkDelta("thi"), thinkDelta("nk"),
		textDelta("Hel"), textDelta("lo\nWor"), textDelta("ld"))
	if body(v) != "think\nHello\nWorld" {
		t.Fatalf("%q", v.text())
	}
	emit(term, msgEnd(nil), harness.Event{Kind: harness.EventTurnEnd}, textDelta("next\n"), thinkDelta("t2"))
	if body(v) != "think\nHello\nWorld\nnext\nt2" {
		t.Fatalf("%q", v.text())
	}
	if v.sgr != 0 {
		t.Fatalf("color off but %d SGR", v.sgr)
	}
}

// TM-FR-22: Color 면 thinking 은 흐림 SGR.
func TestRenderColor(t *testing.T) {
	term, v := newTestTerminal(40, true)
	emit(term, thinkDelta("a\nb"), textDelta("c"))
	if body(v) != "a\nb\nc" || v.sgr < 4 {
		t.Fatalf("%q sgr %d", v.text(), v.sgr)
	}
}

// TM-FR-26: tool_start 줄 — command → path → 인자 JSON, 첫 줄, 폭으로 자름.
func TestRenderToolStart(t *testing.T) {
	term, v := newTestTerminal(24, false)
	emit(term,
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("1", "bash", `{"command":"ls -la\necho"}`)},
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("2", "read", `{"path":"/a/b","offset":1}`)},
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("3", "grep", `{"pattern": "x"}`)},
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("4", "bash", `{"command":"`+strings.Repeat("y", 40)+`"}`)},
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("5", "x", `{"command":"","path":"/p"}`)},
		harness.Event{Kind: harness.EventToolStart, Call: toolCall("6", "x", `{"path":"`+strings.Repeat("z", 19)+`"}`)},
	)
	want := "▸ bash  ls -la\n▸ read  /a/b\n▸ grep  {\"pattern\":\"x\"}\n▸ bash  " + strings.Repeat("y", 15) + "…\n▸ x  /p\n▸ x  " + strings.Repeat("z", 19)
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}
}

// TM-FR-27, 28: bash 출력 들여쓰기·20줄 상한·생략 줄 수, 꼬리 줄바꿈, 제어 문자 정제.
func TestRenderToolOutput(t *testing.T) {
	term, v := newTestTerminal(40, false)
	c := toolCall("1", "bash", `{"command":"x"}`)
	emit(term, harness.Event{Kind: harness.EventToolStart, Call: c})
	var out strings.Builder
	for i := 1; i <= 23; i++ {
		fmt.Fprintf(&out, "l%d\n", i)
	}
	all := out.String() + "tail"
	for i := 0; i < len(all); i += 7 {
		emit(term, harness.Event{Kind: harness.EventToolUpdate, Call: c, Output: all[i:min(i+7, len(all))]})
	}
	emit(term, toolEnd(c, contract.StatusOK, "ignored body"))
	lines := strings.Split(body(v), "\n")
	if len(lines) != 23 || lines[1] != "  l1" || lines[20] != "  l20" || lines[21] != "  … +4줄 생략" || lines[22] != "✓" {
		t.Fatalf("%q", lines)
	}

	term, v = newTestTerminal(40, false)
	c2 := toolCall("2", "bash", `{"command":"x"}`)
	emit(term, harness.Event{Kind: harness.EventToolUpdate, Call: c2, Output: "\x1b[31mred\x1b[0m\r\n\tb"},
		toolEnd(c2, contract.StatusError, "boom\nmore"))
	if body(v) != "  [31mred[0m\n      b\n✗ error: boom" {
		t.Fatalf("%q", body(v))
	}
}

// TM-FR-28: Status 별 줄.
func TestRenderToolEnd(t *testing.T) {
	term, v := newTestTerminal(30, false)
	c := toolCall("1", "read", `{"path":"/x"}`)
	emit(term, toolEnd(c, contract.StatusOK, ""), toolEnd(c, contract.StatusError, "skipped: the user sent a new message"),
		toolEnd(c, contract.StatusSkipped, ""), toolEnd(c, contract.StatusAborted, ""), toolEnd(c, contract.StatusError, ""),
		toolEnd(c, contract.StatusError, strings.Repeat("e", 40)))
	want := "✓\n⤼ skipped\n⤼ skipped\n■ aborted\n✗ error:\n✗ error: " + strings.Repeat("e", 20) + "…"
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}
}

// TM-FR-29~31: approval·retry·compaction 줄.
func TestRenderNotices(t *testing.T) {
	term, v := newTestTerminal(60, false)
	c := toolCall("1", "bash", `{"command":"x"}`)
	ap := func(d, rule, reason string) harness.Event {
		return harness.Event{Kind: harness.EventApproval, Call: c, Approval: &harness.Approval{Decision: d, Rule: rule, Reason: reason}}
	}
	emit(term, ap("allow", "", ""), ap("allow_session", "", ""), ap("deny", "", "nope"), ap("rule_deny", "bash(rm *)", ""),
		harness.Event{Kind: harness.EventRetry, Attempt: 2, Delay: 3 * time.Second, Error: &contract.CallError{Message: "rate"}},
		harness.Event{Kind: harness.EventRetry, Attempt: 1, Delay: 1240 * time.Millisecond, Error: &contract.CallError{Message: "x"}},
		harness.Event{Kind: harness.EventCompaction, Phase: "start"},
		harness.Event{Kind: harness.EventCompaction, Phase: "end"},
		harness.Event{Kind: harness.EventCompaction, Phase: "end", Err: errors.New("bad")})
	want := "✓ 이번만 허용\n✓ 이 세션 동안 허용\n✗ 거부: nope\n✗ 규칙 거부: bash(rm *)\n" +
		"↻ 재시도 2회째 — 3.0s 후 (rate)\n↻ 재시도 1회째 — 1.2s 후 (x)\n⋯ 컨텍스트 압축 중\n✓ 압축 완료\n✗ 압축 실패: bad"
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}
}

// TM-FR-25, 32: run_end 토큰 합계·형식·초기화, 중단, 실패, 열린 줄 닫기.
func TestRenderRunEnd(t *testing.T) {
	term, v := newTestTerminal(80, false)
	emit(term, textDelta("hi"), msgEnd(&contract.Usage{Input: 1000, CacheRead: 200, CacheWrite: 100, Output: 999}),
		msgEnd(&contract.Usage{Input: 11111, Output: 1}), runEnd(core.SettleCompleted, nil),
		msgEnd(&contract.Usage{Input: 999, Output: 1000}), runEnd(core.SettleStopped, nil),
		textDelta("partial"), runEnd(core.SettleAborted, nil),
		runEnd(core.SettleFailed, errors.New("boom")), runEnd(core.SettleGuard, errors.New("limit")))
	want := "hi\n── completed · 입력 12.4k / 출력 1.0k 토큰\n── stopped · 입력 999 / 출력 1.0k 토큰\npartial\n── 중단됨\n" +
		"── failed · 입력 0 / 출력 0 토큰 · boom\n── guard · 입력 0 / 출력 0 토큰 · limit"
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}
}

func TestFormatTokens(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1.0k", 1049: "1.0k", 1050: "1.1k", 12345: "12.3k", 1234567: "1234.6k"} {
		if got := formatTokens(n); got != want {
			t.Errorf("%d: %q want %q", n, got, want)
		}
	}
}

// TM-FR-41: History 메시지 — user(이미지), assistant 블록 순서, tool_result 상태 줄.
func TestRenderHistoryMessage(t *testing.T) {
	term, v := newTestTerminal(60, false)
	img := contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png"}}
	term.message(contract.Message{Role: contract.RoleUser, Content: []contract.Block{{Kind: contract.BlockText, Text: "look\nhere"}, img}})
	term.message(contract.Message{Role: contract.RoleAssistant, Content: []contract.Block{
		{Kind: contract.BlockThinking, Text: "hmm"}, {Kind: contract.BlockText, Text: "answer"},
		{Kind: contract.BlockToolCall, ToolCall: toolCall("1", "bash", `{"command":"ls"}`)}, img,
		{Kind: contract.BlockToolCall}}})
	term.message(contract.Message{Role: contract.RoleToolResult, Result: &contract.ToolResult{Status: contract.StatusError,
		Content: []contract.Block{{Kind: contract.BlockText, Text: "bad"}}}})
	term.message(contract.Message{Role: contract.RoleSystem})
	want := "> look\n  here\n  [image]\nhmm\nanswer\n▸ bash  ls\n[image]\n✗ error: bad"
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}
}

// TM-FR-22, 24: 빈 줄에는 SGR 을 쓰지 않는다, DEL 은 버린다.
func TestRenderSanitizeAndEmptyPaint(t *testing.T) {
	term, v := newTestTerminal(40, true)
	emit(term, thinkDelta("\n\n"))
	if v.sgr != 0 {
		t.Fatalf("SGR for empty lines: %d", v.sgr)
	}
	if got := sanitize("a\x7fb\x00c"); got != "abc" {
		t.Fatalf("%q", got)
	}
}

// TM-FR-35: Target 이 비면 인자 요약, 문자열 인자 5줄은 생략 표시 없음, 사유 입력 커서.
func TestApprovalView(t *testing.T) {
	term, _ := newTestTerminal(80, false)
	args := `{"pattern":"p1\np2\np3\np4\np5","n":3}`
	a := &approval{req: agent.ApprovalRequest{Call: contract.ToolCall{Name: "grep", Args: json.RawMessage(args)}, Rule: "grep"}}
	lines, col := term.approvalView(a)
	want := "⚠ grep 승인 필요  (규칙: grep)\n  {\"pattern\":\"p1\\np2\\np3\\np4\\np5\",\"n\":3}\n  pattern: p1\n    p2\n    p3\n    p4\n    p5\n  [y] 이번만  [a] 이 세션 동안  [n] 거부"
	if strings.Join(lines, "\n") != want || col != -1 {
		t.Fatalf("%q\nwant %q", strings.Join(lines, "\n"), want)
	}
	a.reasoning = true
	term.reason.insert("abc")
	term.reason.left()
	lines, col = term.approvalView(a)
	if last := lines[len(lines)-1]; last != "  거부 사유 (Enter 로 확정): abc" || col != len([]rune("  거부 사유 (Enter 로 확정): "))+2 {
		t.Fatalf("%q %d", last, col)
	}
}

// TM-FR-20, 21: 입력 줄 탭은 공백 4칸(커서도), 선택 프롬프트 커서, FollowUp 0 이면 +N 없음.
func TestView(t *testing.T) {
	term, _ := newTestTerminal(80, false)
	term.ed.insert("\tx")
	term.ed.left()
	if v := term.view(); v.lines[0] != ">     x" || v.col != 6 {
		t.Fatalf("%+v", v)
	}
	term.ed.clear()
	term.st = stPick
	term.ed.insert("1")
	if v := term.view(); v.lines[0] != promptPick+"1" || v.col != len([]rune(promptPick))+1 {
		t.Fatalf("%+v", v)
	}
	term.st = stRunning
	if v := term.view(); v.status != statusRun {
		t.Fatalf("%q", v.status)
	}
	term.followUps = 2
	if v := term.view(); v.status != "+2 "+statusRun {
		t.Fatalf("%q", v.status)
	}
}
