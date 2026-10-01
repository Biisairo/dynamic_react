package terminal

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

// maxChoices 는 선택 화면에 그리는 일치 항목 수다 (TM-FR-49).
const maxChoices = 10

const (
	promptModel    = "모델: "
	promptProvider = "provider: "
	statusModel    = "모델 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소"
	statusProvider = "provider 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소"
	statusKey      = "키 입력 · Enter 저장 · Esc 취소"
	noModels       = "  (쓸 수 있는 모델 없음 — /login)"
	noModelMatch   = "  (일치하는 모델 없음)"
	noProvider     = "  (일치하는 provider 없음)"
	savedNote      = " (저장됨)"
)

type chooseKind int

const (
	chModel chooseKind = iota
	chLogin
	chLogout
)

type choice struct{ label, note string }

// chooser 는 필터 + 하이라이트 선택 상태다 (TM-FR-49·50, 52). 필터는 t.ed 다.
type chooser struct {
	kind  chooseKind
	items []choice
	sel   int // matches 안의 하이라이트
	top   int // 보이는 창의 첫 항목
}

func (c *chooser) matches(filter string) []choice {
	words := strings.Fields(strings.ToLower(filter))
	var out []choice
	for _, it := range c.items {
		label := strings.ToLower(it.label)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(label, w)
		}
		if ok {
			out = append(out, it)
		}
	}
	return out
}

// move 는 하이라이트를 d 만큼 옮긴다 (끝에서 멈춤, 창을 따라 옮김).
func (c *chooser) move(d, n int) {
	c.sel = max(0, min(n-1, c.sel+d))
	switch {
	case c.sel < c.top:
		c.top = c.sel
	case c.sel >= c.top+maxChoices:
		c.top = c.sel - maxChoices + 1
	}
}

// chooseLines 는 상태 줄과 입력 줄 사이의 목록이다. 잠금은 호출자가 잡는다.
func (t *Terminal) chooseLines() []string {
	c := t.ch
	ms := c.matches(t.ed.text())
	switch {
	case len(c.items) == 0 && c.kind == chModel:
		return []string{noModels}
	case len(ms) == 0 && c.kind == chModel:
		return []string{noModelMatch}
	case len(ms) == 0:
		return []string{noProvider}
	}
	end := min(len(ms), c.top+maxChoices)
	var out []string
	for i := c.top; i < end; i++ {
		line := "  " + ms[i].label
		if i == c.sel {
			line = "▶ " + ms[i].label
		}
		if ms[i].note != "" && displayWidth(line+ms[i].note) <= t.scr.cols() {
			line += t.paint(sgrDim, ms[i].note)
		} else {
			line = truncate(line+ms[i].note, t.scr.cols())
		}
		out = append(out, line)
	}
	if n := len(ms) - (end - c.top); n > 0 {
		out = append(out, fmt.Sprintf("  … +%d개", n))
	}
	return out
}

func (t *Terminal) choose(c *chooser) {
	t.update(func() {
		t.ch, t.st = c, stChoose
		t.ed.clear()
	})
}

// chooseModel 은 모델 선택 상태로 들어간다 (TM-FR-49).
func (t *Terminal) chooseModel() {
	c := &chooser{kind: chModel}
	for _, m := range t.a.Models() {
		c.items = append(c.items, choice{label: m.Provider + "/" + m.ID})
	}
	t.choose(c)
}

// chooseProvider 는 /login·/logout 의 provider 선택 상태다 (TM-FR-52). 항목이 없으면 false.
func (t *Terminal) chooseProvider(kind chooseKind) bool {
	c := &chooser{kind: kind}
	for _, p := range t.a.Providers() {
		if kind == chLogin && !p.RequiresKey || kind == chLogout && !p.HasKey {
			continue
		}
		it := choice{label: p.Name}
		if p.HasKey {
			it.note = savedNote
		}
		c.items = append(c.items, it)
	}
	if len(c.items) == 0 {
		return false
	}
	t.choose(c)
	return true
}

func (t *Terminal) idle() {
	t.update(func() {
		t.st, t.ch, t.keyFor = stIdle, nil, ""
		t.ed.clear()
		t.secret.clear()
	})
}

func (t *Terminal) chooseKey(k key) {
	c := t.ch
	switch k.kind {
	case keyEsc, keyCtrlC:
		t.idle()
	case keyUp, keyDown:
		d := 1
		if k.kind == keyUp {
			d = -1
		}
		t.update(func() { c.move(d, len(c.matches(t.ed.text()))) })
	case keyEnter:
		ms := c.matches(t.ed.text())
		if len(ms) == 0 {
			return
		}
		label := ms[c.sel].label
		switch c.kind {
		case chModel:
			t.setModel(label)
		case chLogin:
			t.enterKey(label)
		case chLogout:
			t.idle()
			t.removeKey(label)
		}
	default:
		t.update(func() {
			t.edit(&t.ed, k, false)
			c.sel, c.top = 0, 0
		})
	}
}

// setModel 은 SetModel 결과를 알린다. 성공하면 대기로 (TM-FR-47a, 50).
func (t *Terminal) setModel(ref string) {
	if err := t.a.SetModel(t.ctx, ref); err != nil {
		t.fail(err)
		return
	}
	t.idle()
	t.say("── 모델 " + sanitize(ref) + " (저장됨)")
}

func (t *Terminal) removeKey(provider string) {
	if err := t.a.RemoveKey(t.ctx, provider); err != nil {
		t.fail(err)
		return
	}
	t.say("── 키 삭제됨 " + sanitize(provider))
}

// enterKey 는 provider 의 키 입력 상태로 들어간다 (TM-FR-53).
func (t *Terminal) enterKey(provider string) {
	t.update(func() {
		t.st, t.ch, t.keyFor = stKey, nil, provider
		t.ed.clear()
		t.secret.clear()
	})
}

// secretKey 는 키 입력 상태의 키다. 입력은 덧붙이기 출력·기록에 남기지 않는다 (TM-FR-53, TM-C2).
func (t *Terminal) secretKey(k key) {
	switch k.kind {
	case keyEsc, keyCtrlC:
		t.idle()
	case keyEnter:
		text := t.secret.text()
		if text == "" {
			return
		}
		provider := t.keyFor
		if err := t.a.SetKey(t.ctx, provider, text); err != nil {
			t.update(func() { t.secret.clear() })
			t.fail(err)
			return
		}
		t.idle()
		t.say("── 키 저장됨 " + sanitize(provider))
	default:
		t.update(func() { t.edit(&t.secret, k, false) })
	}
}

// checkModel 은 시작 시 모델 문제를 알린다 (TM-FR-48). 한 번만 한다.
func (t *Terminal) checkModel() {
	if t.modelChecked {
		return
	}
	t.modelChecked = true
	ref, err := t.a.Model()
	if err == nil {
		return
	}
	msg := err.Error()
	if errors.Is(err, agent.ErrNoModel) {
		msg = "모델이 설정되지 않았습니다"
	}
	t.say(t.paint(sgrYellow, "⚠") + " " + sanitize(msg))
	if errors.Is(err, agent.ErrNoAPIKey) {
		provider, _, _ := strings.Cut(ref, "/")
		t.enterKey(provider)
		return
	}
	t.chooseModel()
}

// modelHint 는 Start 에러 뒤에 붙일 안내다 (TM-FR-16).
func (t *Terminal) modelHint() string {
	_, err := t.a.Model()
	switch {
	case err == nil:
		return ""
	case errors.Is(err, agent.ErrNoAPIKey):
		return " (/login)"
	}
	return " (/model)"
}

// providerKnown 은 /login <provider> 의 검사다 (TM-FR-52).
func (t *Terminal) providerKnown(name string) bool {
	for _, p := range t.a.Providers() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// keyCommand 는 /login·/logout 이다 (TM-FR-52).
func (t *Terminal) keyCommand(name string, args []string) {
	switch {
	case t.st != stIdle:
		t.say(t.paint(sgrRed, "✗") + " 실행 중에는 키를 바꿀 수 없습니다")
	case name == "/login" && len(args) > 0 && !t.providerKnown(args[0]):
		t.say(t.paint(sgrRed, "✗") + " unknown provider " + fmt.Sprintf("%q", sanitize(args[0])))
	case name == "/login" && len(args) > 0:
		t.enterKey(args[0])
	case name == "/login":
		t.chooseProvider(chLogin)
	case len(args) > 0:
		t.removeKey(args[0])
	case !t.chooseProvider(chLogout):
		t.say(t.paint(sgrRed, "✗") + " 저장된 키가 없습니다")
	}
}
