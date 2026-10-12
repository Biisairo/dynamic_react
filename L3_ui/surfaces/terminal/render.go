package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

const (
	sgrBold   = "1"
	sgrDim    = "2"
	sgrRed    = "31"
	sgrGreen  = "32"
	sgrYellow = "33"

	maxToolLines = 20 // TM-FR-27
	argLines     = 5  // TM-FR-35
)

// callOut 은 호출 하나의 bash 출력 상태다 (TM-FR-27).
type callOut struct {
	lines   int  // 찍은 줄 수 (줄바꿈 기준)
	midLine bool // 찍은 마지막 줄이 줄바꿈으로 끝나지 않았는지
	dropped int  // 버린 줄바꿈 수
	tail    bool // 버린 출력의 마지막이 줄바꿈으로 끝나지 않았는지
}

// paint 는 줄마다 SGR 로 감싼다 (같은 줄 안에서 닫음, TM-FR-22).
func (t *Terminal) paint(code, s string) string {
	if !t.o.Color {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		if p != "" {
			parts[i] = "\x1b[" + code + "m" + p + "\x1b[0m"
		}
	}
	return strings.Join(parts, "\n")
}

// sanitize 는 표시할 수 없는 제어 문자를 버리고 탭을 공백 4칸으로 바꾼다.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r != '\n' && (r < 0x20 || r == 0x7f) {
			return -1
		}
		return r
	}, s)
}

// truncate 는 표시 폭이 w 를 넘으면 잘라 끝에 … 를 붙인다.
func truncate(s string, w int) string {
	if displayWidth(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		cw := runeWidth(r)
		if used+cw > w-1 {
			break
		}
		b.WriteRune(r)
		used += cw
	}
	return b.String() + "…"
}

func displayWidth(s string) int {
	n := 0
	forEachRune(s, func(r rune) { n += runeWidth(r) })
	return n
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// newline 은 출력이 줄 중간이면 줄바꿈을 넣는다.
func (t *Terminal) newline() {
	if t.scr.tail != "" {
		t.scr.print("\n")
	}
}

// line 은 줄을 열고 한 줄을 덧붙인다.
func (t *Terminal) line(s string) {
	t.newline()
	t.scr.print(s + "\n")
}

// event 는 이벤트 하나를 그린다 (TM-FR-23~33). 잠금은 호출자가 잡는다.
func (t *Terminal) event(ev harness.Event) {
	switch ev.Kind {
	case harness.EventMessageUpdate:
		if ev.Delta != nil {
			t.delta(ev.Delta)
		}
	case harness.EventMessageEnd:
		t.newline()
		t.block = ""
		if ev.Message != nil && ev.Message.Usage != nil {
			u := ev.Message.Usage
			t.tokIn += u.Input + u.CacheRead + u.CacheWrite
			t.tokOut += u.Output
			t.tokCache += u.CacheRead
		}
	case harness.EventInput:
		if ev.Message != nil {
			t.input(*ev.Message)
		}
	case harness.EventToolStart:
		if ev.Call != nil {
			t.line(t.toolLine(*ev.Call))
		}
	case harness.EventToolUpdate:
		if ev.Call != nil {
			t.toolOutput(ev.Call.ID, ev.Output)
		}
	case harness.EventToolEnd:
		if ev.Call != nil {
			t.toolEnd(ev.Call.ID, ev.Result)
		}
	case harness.EventApproval:
		if ev.Approval != nil {
			t.line(t.approvalLine(*ev.Approval))
		}
	case harness.EventRetry:
		msg := ""
		if ev.Error != nil {
			msg = ev.Error.Message
		}
		t.line(fmt.Sprintf("%s 재시도 %d회째 — %.1fs 후 (%s)", t.paint(sgrYellow, "↻"), ev.Attempt, ev.Delay.Seconds(), sanitize(msg)))
	case harness.EventCompaction:
		t.line(t.compactionLine(ev))
	case harness.EventMode:
		if ev.Mode != nil {
			t.line(t.paint(sgrDim, t.modeLine(*ev.Mode)))
		}
	case harness.EventPrefixBreak:
		if ev.Prefix != nil && !ev.Prefix.Expected {
			t.line(t.prefixLine(*ev.Prefix))
		}
	case harness.EventRunEnd:
		t.newline()
		t.line(t.runEndLine(ev.Settle))
		t.tokIn, t.tokOut, t.tokCache, t.followUps = 0, 0, 0, 0
	}
}

func (t *Terminal) delta(d *contract.StreamEvent) {
	var kind string
	switch d.Kind {
	case contract.EventTextDelta:
		kind = "text"
	case contract.EventThinkingDelta:
		kind = "thinking"
	default:
		return
	}
	if t.block != kind {
		t.newline()
	}
	t.block = kind
	s := sanitize(d.Delta)
	if kind == "thinking" {
		s = t.paint(sgrDim, s)
	}
	t.scr.print(s)
}

// argSummary 는 command → path → 인자 JSON 이다 (TM-FR-26).
func argSummary(args json.RawMessage) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) == nil {
		for _, k := range []string{"command", "path"} {
			var s string
			if json.Unmarshal(m[k], &s) == nil && s != "" {
				return s
			}
		}
	}
	var b bytes.Buffer
	if json.Compact(&b, args) != nil {
		return string(args)
	}
	return b.String()
}

func (t *Terminal) toolLine(c contract.ToolCall) string {
	return truncate("▸ "+c.Name+"  "+sanitize(firstLine(argSummary(c.Args))), t.scr.cols())
}

func (t *Terminal) toolOutput(id, chunk string) {
	st := t.calls[id]
	if st == nil {
		st = &callOut{}
		t.calls[id] = st
	}
	s := sanitize(chunk)
	var out strings.Builder
	for s != "" {
		if st.lines >= maxToolLines {
			st.dropped += strings.Count(s, "\n")
			st.tail = !strings.HasSuffix(s, "\n")
			break
		}
		seg, rest, nl := strings.Cut(s, "\n")
		if seg != "" {
			if !st.midLine {
				out.WriteString("  ")
			}
			out.WriteString(t.paint(sgrDim, seg))
			st.midLine = true
		}
		if nl {
			if !st.midLine {
				out.WriteString("  ")
			}
			out.WriteString("\n")
			st.lines++
			st.midLine = false
		}
		s = rest
	}
	t.scr.print(out.String())
}

func resultText(r *contract.ToolResult) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, b := range r.Content {
		if b.Kind == contract.BlockText {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
}

func (t *Terminal) toolEnd(id string, r *contract.ToolResult) {
	st := t.calls[id]
	delete(t.calls, id)
	t.newline()
	if st != nil {
		n := st.dropped
		if st.tail {
			n++
		}
		if n > 0 {
			t.line(t.paint(sgrDim, fmt.Sprintf("  … +%d줄 생략", n)))
		}
	}
	t.line(t.statusLine(r))
}

// statusLine 은 도구 결과의 한 줄이다 (TM-FR-28).
func (t *Terminal) statusLine(r *contract.ToolResult) string {
	text := resultText(r)
	var s string
	switch {
	case r == nil || r.Status == contract.StatusOK:
		s = t.paint(sgrGreen, "✓")
	case r.Status == contract.StatusSkipped || r.Status == contract.StatusError && strings.HasPrefix(text, "skipped:"):
		s = "⤼ skipped"
	case r.Status == contract.StatusAborted:
		s = "■ aborted"
	default:
		s = t.paint(sgrRed, "✗") + " error: " + sanitize(firstLine(text))
	}
	return truncate(s, t.scr.cols())
}

func (t *Terminal) approvalLine(a harness.Approval) string {
	switch a.Decision {
	case "allow":
		return t.paint(sgrGreen, "✓") + " 이번만 허용"
	case "allow_session":
		return t.paint(sgrGreen, "✓") + " 이 세션 동안 허용"
	case "rule_deny":
		return t.paint(sgrRed, "✗") + " 규칙 거부: " + a.Rule
	}
	return t.paint(sgrRed, "✗") + " 거부: " + sanitize(a.Reason)
}

func (t *Terminal) compactionLine(ev harness.Event) string {
	switch {
	case ev.Phase == "start":
		return "⋯ 컨텍스트 압축 중"
	case ev.Err != nil:
		return t.paint(sgrRed, "✗") + " 압축 실패: " + sanitize(ev.Err.Error())
	}
	return t.paint(sgrGreen, "✓") + " 압축 완료"
}

// prefixLine 은 prefix_break 줄이다 (TM-FR-60).
func (t *Terminal) prefixLine(p harness.PrefixBreak) string {
	if p.Part == harness.PrefixModel {
		return t.paint(sgrDim, truncate("· 캐시 새로 시작: 모델 변경 → "+sanitize(p.Model.Provider+"/"+p.Model.ID), t.scr.cols()))
	}
	s := fmt.Sprintf("⚠ 캐시 접두 깨짐: %s 변경 (0/%d 메시지 유지)", p.Part, p.Total)
	if p.Part == harness.PrefixMessages {
		s = fmt.Sprintf("⚠ 캐시 접두 깨짐: 메시지 #%d 변경, 편집 없음 (%d/%d 유지)", p.At, p.At, p.Total)
	}
	return t.paint(sgrYellow, "⚠") + strings.TrimPrefix(truncate(s, t.scr.cols()), "⚠")
}

func (t *Terminal) runEndLine(s *core.Settle) string {
	if s == nil {
		return "──"
	}
	if s.Reason == core.SettleAborted {
		return "── 중단됨"
	}
	in := formatTokens(t.tokIn)
	if t.tokIn > 0 {
		in += " (캐시 " + percent(t.tokCache, t.tokIn) + ")"
	}
	line := fmt.Sprintf("── %s · 입력 %s / 출력 %s 토큰", s.Reason, in, formatTokens(t.tokOut))
	if (s.Reason == core.SettleFailed || s.Reason == core.SettleGuard) && s.Err != nil {
		line += " · " + sanitize(s.Err.Error())
	}
	return line
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// input 은 이 Terminal 이 보내지 않은 입력 메시지를 그린다 (TM-FR-54).
func (t *Terminal) input(m contract.Message) {
	if t.sent[m.ID] {
		delete(t.sent, m.ID)
		return
	}
	var parts []string
	for _, b := range m.Content {
		switch b.Kind {
		case contract.BlockText:
			parts = append(parts, b.Text)
		case contract.BlockImage:
			parts = append(parts, "[image]")
		}
	}
	lines := strings.Split(sanitize(strings.Join(parts, "\n")), "\n")
	shown := lines[:min(len(lines), maxToolLines)]
	t.line(t.paint(sgrYellow, "»") + " " + strings.Join(shown, "\n  "))
	if n := len(lines) - len(shown); n > 0 {
		t.line(t.paint(sgrDim, fmt.Sprintf("  … +%d줄 생략", n)))
	}
}

// userLine 은 user 입력 줄이다 (TM-FR-23).
func (t *Terminal) userLine(text, suffix string) string {
	lines := strings.Split(sanitize(text), "\n")
	return t.paint(sgrBold, ">") + " " + strings.Join(lines, "\n  ") + suffix
}

// message 는 History 의 메시지 하나를 그린다 (TM-FR-41).
func (t *Terminal) message(m contract.Message) {
	switch m.Role {
	case contract.RoleUser:
		var parts []string
		for _, b := range m.Content {
			switch b.Kind {
			case contract.BlockText:
				parts = append(parts, b.Text)
			case contract.BlockImage:
				parts = append(parts, "[image]")
			}
		}
		t.line(t.userLine(strings.Join(parts, "\n"), ""))
	case contract.RoleAssistant:
		block := ""
		for _, b := range m.Content {
			if block != "" && block != string(b.Kind) {
				t.newline()
			}
			block = string(b.Kind)
			switch b.Kind {
			case contract.BlockText:
				t.scr.print(sanitize(b.Text))
			case contract.BlockThinking:
				t.scr.print(t.paint(sgrDim, sanitize(b.Text)))
			case contract.BlockToolCall:
				if b.ToolCall != nil {
					t.line(t.toolLine(*b.ToolCall))
				}
			case contract.BlockImage:
				t.scr.print("[image]")
			}
		}
		t.newline()
	case contract.RoleToolResult:
		t.line(t.statusLine(m.Result))
	}
}

// approvalView 는 승인 프롬프트의 줄들이다 (TM-FR-35).
func (t *Terminal) approvalView(a *approval) ([]string, int) {
	rule := "(규칙: " + a.req.Rule + ")"
	if a.req.Rule == "" {
		rule = "(규칙 없음 — 기본 묻기)"
	}
	lines := []string{t.paint(sgrYellow, "⚠") + " " + a.req.Call.Name + " 승인 필요  " + rule}
	target := a.req.Target
	if target == "" {
		target = argSummary(a.req.Call.Args)
	}
	for _, l := range strings.Split(sanitize(target), "\n") {
		lines = append(lines, "  "+l)
	}
	var m map[string]json.RawMessage
	json.Unmarshal(a.req.Call.Args, &m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var v string
		if k == "command" || k == "path" || json.Unmarshal(m[k], &v) != nil {
			continue
		}
		vl := strings.Split(sanitize(v), "\n")
		for i, l := range vl[:min(len(vl), argLines)] {
			if i == 0 {
				lines = append(lines, "  "+k+": "+l)
			} else {
				lines = append(lines, "    "+l)
			}
		}
		if len(vl) > argLines {
			lines = append(lines, "  …")
		}
	}
	if !a.reasoning {
		return append(lines, "  [y] 이번만  [a] 이 세션 동안  [n] 거부"), -1
	}
	prompt := "  거부 사유 (Enter 로 확정): "
	return append(lines, prompt+t.reason.text()), len([]rune(prompt)) + t.reason.cur
}
