package terminal

import "testing"

// show 는 커서 위치에 | 를 넣은 문자열이다.
func show(e *editor) string {
	return string(e.buf[:e.cur]) + "|" + string(e.buf[e.cur:])
}

// TM-FR-11: 삽입·삭제·이동 (rune 단위).
func TestEditorBasic(t *testing.T) {
	var e editor
	e.insert("한글ab")
	e.left()
	e.left()
	e.insert("X")
	if got := show(&e); got != "한글X|ab" {
		t.Fatal(got)
	}
	e.backspace()
	e.backspace()
	if got := show(&e); got != "한|ab" {
		t.Fatal(got)
	}
	e.del()
	if got := show(&e); got != "한|b" {
		t.Fatal(got)
	}
	e.right()
	e.right()
	e.del()
	if got := show(&e); got != "한b|" {
		t.Fatal(got)
	}
	e.home()
	e.backspace()
	e.left()
	if got := show(&e); got != "|한b" {
		t.Fatal(got)
	}
	e.end()
	if got := show(&e); got != "한b|" {
		t.Fatal(got)
	}
	if e.empty() {
		t.Fatal("not empty")
	}
	e.clear()
	if !e.empty() || show(&e) != "|" {
		t.Fatal(show(&e))
	}
}

// TM-FR-11: 여러 줄 — Home·End 는 현재 줄 기준, lines·cursor.
func TestEditorMultiline(t *testing.T) {
	var e editor
	e.insert("ab\ncd")
	e.newline()
	e.insert("ef")
	e.left()
	e.home()
	if got := show(&e); got != "ab\ncd\n|ef" {
		t.Fatal(got)
	}
	e.left()
	e.home()
	if got := show(&e); got != "ab\n|cd\nef" {
		t.Fatal(got)
	}
	e.end()
	if got := show(&e); got != "ab\ncd|\nef" {
		t.Fatal(got)
	}
	lines, line, col := e.layout()
	if len(lines) != 3 || lines[1] != "cd" || line != 1 || col != 2 {
		t.Fatalf("layout %q %d %d", lines, line, col)
	}
}

// TM-FR-11: 기록 — ↑ 는 최신부터, 끝에서 ↓ 는 쓰던 입력. 빈 입력은 기록하지 않음.
func TestEditorHistory(t *testing.T) {
	var e editor
	e.insert("one")
	if got := e.submit(); got != "one" || !e.empty() {
		t.Fatal(got)
	}
	e.insert("   ")
	e.submit()
	e.insert("two")
	e.submit()
	e.insert("draft")
	e.up()
	if got := show(&e); got != "two|" {
		t.Fatal(got)
	}
	e.up()
	e.up()
	if got := show(&e); got != "one|" {
		t.Fatal(got)
	}
	e.down()
	if got := show(&e); got != "two|" {
		t.Fatal(got)
	}
	e.down()
	if got := show(&e); got != "draft|" {
		t.Fatal(got)
	}
	e.down()
	if got := show(&e); got != "draft|" {
		t.Fatal(got)
	}
	var empty editor
	empty.up()
	empty.down()
	if show(&empty) != "|" {
		t.Fatal(show(&empty))
	}
}
