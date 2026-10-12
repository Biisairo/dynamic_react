package terminal

import (
	"fmt"
	"strings"

	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func modeName(name string) string {
	if name == "" {
		return "기본"
	}
	return sanitize(name)
}

// showModes 는 TM-FR-57 이다.
func (t *Terminal) showModes() {
	modes := t.a.Modes()
	if len(modes) == 0 {
		t.say("── 등록된 모드 없음")
		return
	}
	var st harness.ModeStatus
	if t.sid != "" {
		s, err := t.a.ModeStatus(t.ctx, t.sid)
		if err != nil {
			t.fail(err)
			return
		}
		st = s
	}
	t.update(func() {
		for _, m := range modes {
			mark := " "
			if m.Name == st.Active {
				mark = "*"
			}
			t.line(truncate(fmt.Sprintf("%s %s  %s", mark, sanitize(m.Name), sanitize(firstLine(m.Description))), t.scr.cols()))
		}
		now := modeName(st.Active)
		if st.Active != "" && st.Arg != "" {
			arg := []rune(sanitize(firstLine(st.Arg)))
			if len(arg) > 40 {
				arg = arg[:40]
			}
			now += fmt.Sprintf(" %q", string(arg))
		}
		pending := "없음"
		if st.Pending != nil {
			pending = modeName(st.Pending.To)
		}
		t.line(fmt.Sprintf("  (지금: %s · 예약: %s)", now, pending))
	})
}

// modeCommand 는 TM-FR-58 이다. text 는 명령 줄 원문이다.
func (t *Terminal) modeCommand(text string, args []string) {
	if len(args) == 0 {
		t.say(t.paint(sgrRed, "✗") + " 사용법: /mode <name> [arg] | /mode off")
		return
	}
	running := t.st == stRunning
	if t.st != stIdle && !(running && args[0] == "off") {
		t.say(t.paint(sgrRed, "✗") + " 실행 중에는 모드를 바꿀 수 없습니다")
		return
	}
	if t.sid == "" {
		t.say(t.paint(sgrRed, "✗") + " 첫 메시지를 보낸 뒤에 쓸 수 있습니다")
		return
	}
	var err error
	to := args[0]
	if to == "off" {
		to = ""
		err = t.a.ExitMode(t.ctx, t.sid)
	} else {
		rest := strings.TrimSpace(text)
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "/mode"))
		arg := strings.TrimSpace(strings.TrimPrefix(rest, to))
		err = t.a.EnterMode(t.ctx, t.sid, to, arg)
	}
	if err != nil {
		t.fail(err)
		return
	}
	when := "다음 메시지부터"
	if running {
		when = "다음 턴부터"
	}
	t.say("── 모드 예약: " + modeName(to) + " (" + when + ")")
}

// modeLine 은 TM-FR-59 이다.
func (t *Terminal) modeLine(c harness.ModeChange) string {
	s := fmt.Sprintf("── 모드 %s → %s (%s)", modeName(c.From), modeName(c.To), c.Origin)
	if c.Reason != "" {
		s += ": " + sanitize(c.Reason)
	}
	return s
}
