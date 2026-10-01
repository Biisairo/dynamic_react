package terminal

import (
	"strings"
	"testing"
)

func newScreen(w int) (*screen, *vscreen) {
	v := newVScreen(w)
	return &screen{out: v, width: func() int { return v.w }}, v
}

func input(prompt, text string, col int) view {
	return view{lines: []string{prompt + text}, col: len([]rune(prompt)) + col}
}

func checkCursor(t *testing.T, v *vscreen, r, c int) {
	t.Helper()
	if gr, gc := v.cursor(); gr != r || gc != c {
		t.Fatalf("cursor (%d,%d) want (%d,%d)\n%s", gr, gc, r, c, v.text())
	}
}

// TM-FR-19, 20: 덧붙이는 출력 위, 입력 줄 아래. 꼬리는 이어 붙는다.
func TestScreenTailAndInput(t *testing.T) {
	s, v := newScreen(20)
	s.draw(input("> ", "ab", 2))
	if v.text() != "> ab" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 0, 4)
	s.print("hello\nwor")
	if v.text() != "hello\nwor\n> ab" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 2, 4)
	s.print("ld\n")
	if v.text() != "hello\nworld\n> ab" {
		t.Fatalf("%q", v.text())
	}
	s.draw(view{status: "● run", lines: []string{"> x", "  y"}, line: 1, col: 3})
	if v.text() != "hello\nworld\n● run\n> x\n  y" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 4, 3)
	s.print("more\n")
	if v.text() != "hello\nworld\nmore\n● run\n> x\n  y" {
		t.Fatalf("after print %q", v.text())
	}
	s.draw(input("> ", "", 0))
	if v.text() != "hello\nworld\nmore\n> " && v.text() != "hello\nworld\nmore\n>" {
		t.Fatalf("shrunk %q", v.text())
	}
	checkCursor(t, v, 3, 2)
}

// TM-FR-20: 넓은 글자는 2열, 행 끝에 걸리면 다음 행.
func TestScreenWideWrap(t *testing.T) {
	s, v := newScreen(5)
	s.draw(input("> ", "가나다", 3))
	if v.text() != "> 가\n나다" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 1, 4)
	s.draw(input("> ", "가나다", 1))
	checkCursor(t, v, 0, 4)
	s.print("x\n")
	if v.text() != "x\n> 가\n나다" {
		t.Fatalf("%q", v.text())
	}
}

// TM-FR-20: 마지막 줄이 폭에 꼭 맞으면 커서는 다음 행 처음.
func TestScreenExactWidth(t *testing.T) {
	s, v := newScreen(4)
	s.draw(input("> ", "ab", 2))
	checkCursor(t, v, 1, 0)
	s.print("z\n")
	if v.text() != "z\n> ab" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 2, 0)
	s.draw(input("> ", "a", 1))
	if v.text() != "z\n> a" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 1, 3)
}

// TM-FR-20: 긴 꼬리가 여러 행이어도 지우고 다시 그린다.
func TestScreenLongTail(t *testing.T) {
	s, v := newScreen(4)
	s.draw(input("> ", "", 0))
	s.print("abcdefghij")
	if v.text() != "abcd\nefgh\nij\n>" {
		t.Fatalf("%q", v.text())
	}
	s.print("k\n")
	if v.text() != "abcd\nefgh\nijk\n>" {
		t.Fatalf("%q", v.text())
	}
	s.erase()
	if v.text() != "abcd\nefgh\nijk" {
		t.Fatalf("erase %q", v.text())
	}
	checkCursor(t, v, 3, 0)
}

// TM-FR-20: 꼬리에 든 SGR 은 폭에 세지 않는다. 출력의 \n 은 \r\n 으로.
func TestScreenSGRWidth(t *testing.T) {
	s, v := newScreen(6)
	s.draw(input("> ", "", 0))
	s.print("\x1b[2mabcdef\x1b[0m")
	if v.text() != "abcdef\n>" {
		t.Fatalf("%q", v.text())
	}
	s.print("\n")
	if v.text() != "abcdef\n>" || v.sgr != 4 {
		t.Fatalf("%q sgr %d", v.text(), v.sgr)
	}
}

// TM-FR-06: 폭이 바뀌면 다음 그리기부터 새 폭.
func TestScreenResize(t *testing.T) {
	s, v := newScreen(10)
	s.draw(input("> ", "abcdef", 6))
	checkCursor(t, v, 0, 8)
	v.w = 5
	s.draw(input("> ", "abcdef", 6))
	if !strings.HasSuffix(v.text(), "> abc\ndef") {
		t.Fatalf("%q", v.text())
	}
	r, _ := v.cursor()
	checkCursor(t, v, r, 3)
}

// TM-FR-20: 표시 폭.
func TestRuneWidth(t *testing.T) {
	for _, c := range []struct {
		r rune
		w int
	}{{'a', 1}, {'가', 2}, {'Ａ', 2}, {'中', 2}, {'\x07', 0}, {'é', 1}, {'│', 1}} {
		if got := runeWidth(c.r); got != c.w {
			t.Errorf("%q: %d want %d", c.r, got, c.w)
		}
	}
}

// TM-FR-03: erase 는 꼬리를 확정하고 영역을 지운다. 폭이 없으면 80.
func TestScreenEraseTail(t *testing.T) {
	s, v := newScreen(20)
	s.draw(view{status: "st", lines: []string{"> x"}, col: 3})
	s.print("partial")
	s.erase()
	if v.text() != "partial" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 1, 0)
	if (&screen{}).cols() != 80 || (&screen{width: func() int { return 0 }}).cols() != 80 {
		t.Fatal("default width")
	}
}

// TM-FR-20: 커서 1열, SGR 이 든 꼬리의 줄 넘김.
func TestScreenCursorAndSGRWrap(t *testing.T) {
	s, v := newScreen(4)
	s.draw(view{lines: []string{"ab"}, col: 1})
	checkCursor(t, v, 0, 1)
	s.print("\x1b[2mabcdef\x1b[0m")
	if v.text() != "abcd\nef\nab" {
		t.Fatalf("%q", v.text())
	}
	s.print("\n")
	if v.text() != "abcd\nef\nab" {
		t.Fatalf("%q", v.text())
	}
	checkCursor(t, v, 2, 1)
}
