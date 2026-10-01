package terminal

import "strings"

// editor 는 입력 줄 편집기다 (TM-FR-11). 문자는 rune 단위.
type editor struct {
	buf   []rune
	cur   int
	hist  [][]rune
	hi    int // 기록 위치. len(hist) 이면 쓰던 입력
	draft []rune
}

func (e *editor) insert(s string) {
	rs := []rune(s)
	e.buf = append(e.buf[:e.cur], append(rs, e.buf[e.cur:]...)...)
	e.cur += len(rs)
}

func (e *editor) newline() { e.insert("\n") }

func (e *editor) backspace() {
	if e.cur > 0 {
		e.buf = append(e.buf[:e.cur-1], e.buf[e.cur:]...)
		e.cur--
	}
}

func (e *editor) del() {
	if e.cur < len(e.buf) {
		e.buf = append(e.buf[:e.cur], e.buf[e.cur+1:]...)
	}
}

func (e *editor) left() { e.cur = max(0, e.cur-1) }

func (e *editor) right() { e.cur = min(len(e.buf), e.cur+1) }

func (e *editor) home() {
	for e.cur > 0 && e.buf[e.cur-1] != '\n' {
		e.cur--
	}
}

func (e *editor) end() {
	for e.cur < len(e.buf) && e.buf[e.cur] != '\n' {
		e.cur++
	}
}

func (e *editor) up() {
	if e.hi == 0 {
		return
	}
	if e.hi == len(e.hist) {
		e.draft = append([]rune(nil), e.buf...)
	}
	e.hi--
	e.set(e.hist[e.hi])
}

func (e *editor) down() {
	if e.hi >= len(e.hist) {
		return
	}
	e.hi++
	if e.hi == len(e.hist) {
		e.set(e.draft)
		return
	}
	e.set(e.hist[e.hi])
}

func (e *editor) set(rs []rune) {
	e.buf = append([]rune(nil), rs...)
	e.cur = len(e.buf)
}

func (e *editor) text() string { return string(e.buf) }

func (e *editor) empty() bool { return len(e.buf) == 0 }

func (e *editor) clear() {
	e.buf, e.cur = nil, 0
}

// submit 은 입력을 돌려주고 비운다. 공백뿐이 아니면 기록에 더한다.
func (e *editor) submit() string {
	s := e.text()
	if strings.TrimSpace(s) != "" {
		e.hist = append(e.hist, append([]rune(nil), e.buf...))
	}
	e.hi = len(e.hist)
	e.clear()
	return s
}

// layout 은 줄들과 커서의 (줄, rune 열)이다.
func (e *editor) layout() ([]string, int, int) {
	before := string(e.buf[:e.cur])
	line := strings.Count(before, "\n")
	col := len([]rune(before[strings.LastIndex(before, "\n")+1:]))
	return strings.Split(e.text(), "\n"), line, col
}
