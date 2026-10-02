package terminal

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// showSets 는 TM-FR-55 다.
func (t *Terminal) showSets() {
	list := t.a.Sets()
	t.update(func() {
		for _, l := range t.setLines(list) {
			t.line(l)
		}
	})
}

func (t *Terminal) setLines(list []harness.SetState) []string {
	var out []string
	for i, s := range list {
		state := "on"
		if !s.On {
			state = "off"
		}
		line := truncate(fmt.Sprintf("%d. %s %s  %s", i+1, state, sanitize(s.Name), sanitize(firstLine(s.Description))), t.scr.cols())
		if !s.On {
			line = t.paint(sgrDim, line)
		}
		out = append(out, line)
	}
	return append(out, "  (다음 run 부터 적용 · /set <name> on|off|to <n>)")
}

// setCommand 는 TM-FR-56 이다.
func (t *Terminal) setCommand(args []string) {
	usage := func() { t.say(t.paint(sgrRed, "✗") + " 사용법: /set <name> on|off|to <n>") }
	layout := t.a.Sets()
	n := 0
	switch {
	case len(args) == 2 && (args[1] == "on" || args[1] == "off"):
	case len(args) == 3 && args[1] == "to":
		v, err := strconv.Atoi(args[2])
		if err != nil || v < 1 || v > len(layout) {
			usage()
			return
		}
		n = v
	default:
		usage()
		return
	}
	i := slices.IndexFunc(layout, func(s harness.SetState) bool { return s.Name == args[0] })
	name := sanitize(args[0])
	if i < 0 {
		t.say(t.paint(sgrRed, "✗") + " 없는 세트: " + name + " (/sets)")
		return
	}
	var done string
	switch {
	case n > 0:
		s := layout[i]
		layout = slices.Insert(slices.Delete(layout, i, i+1), n-1, s)
		done = fmt.Sprintf("→ %d번", n)
	case args[1] == "on":
		layout[i].On, done = true, "켬"
	default:
		layout[i].On, done = false, "끔"
	}
	if err := t.a.ArrangeSets(t.ctx, layout); err != nil {
		t.fail(err)
		return
	}
	t.say("── 세트 " + name + " " + done + " (다음 run 부터, 저장됨)")
}
