package terminal

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

type keyKind int

const (
	keyRune keyKind = iota
	keyPaste
	keyEnter
	keyNewline // Shift+Enter, Ctrl+J
	keyAltEnter
	keyEsc
	keyCtrlC
	keyCtrlD
	keyBackspace
	keyDelete
	keyLeft
	keyRight
	keyUp
	keyDown
	keyHome // Home, Ctrl+A
	keyEnd  // End, Ctrl+E
	keyKittyFlags
	keyDA
)

var keyNames = map[keyKind]string{
	keyEnter: "enter", keyNewline: "newline", keyAltEnter: "alt-enter", keyEsc: "esc",
	keyCtrlC: "ctrl-c", keyCtrlD: "ctrl-d", keyBackspace: "backspace", keyDelete: "delete",
	keyLeft: "left", keyRight: "right", keyUp: "up", keyDown: "down", keyHome: "home", keyEnd: "end", keyDA: "da",
}

type key struct {
	kind keyKind
	r    rune   // keyRune
	text string // keyPaste
	n    int    // keyKittyFlags
}

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// parser 는 바이트를 키로 바꾼다 (TM-FR-09, 10). 쪼개진 시퀀스·UTF-8 은 다음 feed 까지 들고 있다.
type parser struct {
	buf   []byte
	paste *bytes.Buffer // 붙여넣기 중이면 nil 이 아님
}

func (p *parser) feed(b []byte) []key {
	p.buf = append(p.buf, b...)
	var out []key
	for len(p.buf) > 0 {
		if p.paste != nil {
			i := bytes.Index(p.buf, []byte(pasteEnd))
			if i < 0 {
				// 끝 표시가 쪼개져 있을 수 있으니 그 길이만큼은 남긴다.
				keep := min(len(p.buf), len(pasteEnd)-1)
				p.paste.Write(p.buf[:len(p.buf)-keep])
				p.buf = p.buf[len(p.buf)-keep:]
				return out
			}
			p.paste.Write(p.buf[:i])
			out = append(out, key{kind: keyPaste, text: cleanPaste(p.paste.String())})
			p.paste = nil
			p.buf = p.buf[i+len(pasteEnd):]
			continue
		}
		k, n := p.next()
		if n == 0 {
			break
		}
		p.buf = p.buf[n:]
		if k != nil {
			out = append(out, *k)
		}
	}
	return out
}

// loneEsc 는 아직 해석하지 못한 바이트가 ESC 하나뿐인지다 (호출자가 EscTimeout 뒤 flush).
func (p *parser) loneEsc() bool { return p.paste == nil && len(p.buf) == 1 && p.buf[0] == 0x1b }

// flush 는 단독 ESC 를 Esc 키로 확정한다. 다른 미완성 시퀀스는 그대로 둔다.
func (p *parser) flush() []key {
	if !p.loneEsc() {
		return nil
	}
	p.buf = p.buf[:0]
	return []key{{kind: keyEsc}}
}

// next 는 buf 앞의 키 하나와 소비한 바이트 수다. 미완성이면 n = 0, 무시할 바이트면 k = nil.
func (p *parser) next() (*key, int) {
	b := p.buf
	switch c := b[0]; {
	case c == 0x1b:
		return p.escape()
	case c < 0x20 || c == 0x7f:
		return control(c), 1
	}
	if !utf8.FullRune(b) {
		return nil, 0
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError {
		return nil, n
	}
	return &key{kind: keyRune, r: r}, n
}

func control(c byte) *key {
	switch c {
	case '\r':
		return &key{kind: keyEnter}
	case '\n':
		return &key{kind: keyNewline}
	case '\t':
		return &key{kind: keyRune, r: '\t'}
	case 0x03:
		return &key{kind: keyCtrlC}
	case 0x04:
		return &key{kind: keyCtrlD}
	case 0x01:
		return &key{kind: keyHome}
	case 0x05:
		return &key{kind: keyEnd}
	case 0x7f, 0x08:
		return &key{kind: keyBackspace}
	}
	return nil
}

func (p *parser) escape() (*key, int) {
	b := p.buf
	if len(b) < 2 {
		return nil, 0
	}
	switch b[1] {
	case '\r':
		return &key{kind: keyAltEnter}, 2
	case 0x1b:
		return &key{kind: keyEsc}, 1
	case 'O':
		if len(b) < 3 {
			return nil, 0
		}
		return ss3(b[2]), 3
	case '[':
		for i := 2; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				if string(b[:i+1]) == pasteStart {
					p.paste = &bytes.Buffer{}
					return nil, i + 1
				}
				return csi(string(b[2:i]), b[i]), i + 1
			}
		}
		return nil, 0
	}
	// Alt+글자 등: ESC 와 다음 바이트를 버린다.
	return nil, 2
}

func ss3(c byte) *key {
	if k, ok := arrows[c]; ok {
		return &key{kind: k}
	}
	return nil
}

var arrows = map[byte]keyKind{'A': keyUp, 'B': keyDown, 'C': keyRight, 'D': keyLeft, 'H': keyHome, 'F': keyEnd}

func csi(params string, final byte) *key {
	if strings.HasPrefix(params, "?") {
		switch final {
		case 'u':
			n, err := strconv.Atoi(params[1:])
			if err != nil {
				return nil
			}
			return &key{kind: keyKittyFlags, n: n}
		case 'c':
			return &key{kind: keyDA}
		}
		return nil
	}
	nums := numbers(params)
	if k, ok := arrows[final]; ok {
		return &key{kind: k}
	}
	switch final {
	case '~':
		if len(nums) == 3 && nums[0] == 27 {
			return mapCode(nums[2], nums[1])
		}
		if len(nums) == 1 {
			switch nums[0] {
			case 3:
				return &key{kind: keyDelete}
			case 1, 7:
				return &key{kind: keyHome}
			case 4, 8:
				return &key{kind: keyEnd}
			}
		}
	case 'u':
		if len(nums) == 1 {
			return mapCode(nums[0], 1)
		}
		if len(nums) == 2 {
			return mapCode(nums[0], nums[1])
		}
	}
	return nil
}

// numbers 는 ; 로 나눈 파라미터다. 각 칸의 : 뒤(대체 키)는 버린다. 숫자가 아니면 nil.
func numbers(params string) []int {
	if params == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Split(params, ";") {
		f, _, _ = strings.Cut(f, ":")
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// mapCode 는 kitty·modifyOtherKeys 의 (코드, 수식자)다. 수식자 = 1 + shift(1)|alt(2)|ctrl(4).
func mapCode(code, mods int) *key {
	switch {
	case code == 13 && mods == 1:
		return &key{kind: keyEnter}
	case code == 13 && mods == 2:
		return &key{kind: keyNewline}
	case code == 13 && mods == 3:
		return &key{kind: keyAltEnter}
	case code == 27 && mods == 1:
		return &key{kind: keyEsc}
	case code == 127 && mods == 1:
		return &key{kind: keyBackspace}
	case mods == 5:
		switch code {
		case 'c':
			return &key{kind: keyCtrlC}
		case 'd':
			return &key{kind: keyCtrlD}
		case 'a':
			return &key{kind: keyHome}
		case 'e':
			return &key{kind: keyEnd}
		case 'j':
			return &key{kind: keyNewline}
		}
	case (mods == 1 || mods == 2) && code >= 0x20 && code != 0x7f && utf8.ValidRune(rune(code)):
		return &key{kind: keyRune, r: rune(code)}
	}
	return nil
}

// cleanPaste 는 \r\n·\r 을 \n 으로 바꾸고 \n·\t 외의 제어 바이트를 버린다.
func cleanPaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
