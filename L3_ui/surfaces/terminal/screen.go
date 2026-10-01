package terminal

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

// view 는 다시 그리는 영역 중 꼬리 아래 부분이다 (TM-FR-20).
type view struct {
	status string   // 비면 줄을 생략
	lines  []string // 입력 영역의 표시 줄 (프롬프트·들여쓰기 포함)
	line   int      // 커서 줄
	col    int      // 커서의 rune 열 (lines[line] 기준)
}

// screen 은 덧붙이는 출력과 맨 아래 다시 그리는 영역을 관리한다 (TM-FR-19, 20). 잠금은 호출자가 한다.
type screen struct {
	out   io.Writer
	width func() int
	tail  string // 줄바꿈으로 끝나지 않은 출력 꼬리
	v     view
	up    int // 커서 행 − 영역 첫 행
	drawn bool
}

func (s *screen) cols() int {
	if s.width == nil {
		return 80
	}
	if w := s.width(); w >= 1 {
		return w
	}
	return 80
}

// print 는 text 를 덧붙인다. 줄바꿈으로 끝난 부분은 영역 위로 확정하고 나머지는 꼬리가 된다.
func (s *screen) print(text string) {
	s.clear()
	all := s.tail + text
	i := strings.LastIndexByte(all, '\n')
	io.WriteString(s.out, strings.ReplaceAll(all[:i+1], "\n", "\r\n"))
	s.tail = all[i+1:]
	s.paint()
}

func (s *screen) draw(v view) {
	s.clear()
	s.v = v
	s.paint()
}

// erase 는 영역을 지우고 꼬리를 확정해 커서를 새 줄 처음에 둔다 (TM-FR-03).
func (s *screen) erase() {
	s.clear()
	if s.tail != "" {
		io.WriteString(s.out, s.tail+"\r\n")
		s.tail = ""
	}
}

func (s *screen) clear() {
	if !s.drawn {
		return
	}
	io.WriteString(s.out, "\r")
	if s.up > 0 {
		fmt.Fprintf(s.out, "\x1b[%dA", s.up)
	}
	io.WriteString(s.out, "\x1b[J")
	s.drawn, s.up = false, 0
}

func (s *screen) paint() {
	w := s.cols()
	var rows []string
	if s.tail != "" {
		rows = append(rows, s.tail)
	}
	if s.v.status != "" {
		rows = append(rows, s.v.status)
	}
	first := len(rows) // 입력 영역 첫 줄의 인덱스
	rows = append(rows, s.v.lines...)
	row, curRow, curCol := 0, 0, 0
	for i, r := range rows {
		if i == first+s.v.line {
			pr, pc := position(r, s.v.col, w)
			curRow, curCol = row+pr, pc
		}
		n, end := layout(r, w)
		io.WriteString(s.out, r)
		if i < len(rows)-1 {
			io.WriteString(s.out, "\r\n")
			row += n
			continue
		}
		row += n - 1
		if end == w {
			// 마지막 열에서 대기 중인 커서를 다음 행으로 넘긴다.
			io.WriteString(s.out, " \r\x1b[K")
			row++
		}
	}
	io.WriteString(s.out, "\r")
	if row > curRow {
		fmt.Fprintf(s.out, "\x1b[%dA", row-curRow)
	}
	if curCol > 0 {
		fmt.Fprintf(s.out, "\x1b[%dC", curCol)
	}
	s.up, s.drawn = curRow, true
}

// layout 은 한 줄(줄바꿈 없음)이 차지하는 행 수와 마지막 행의 끝 열이다. 끝 열 = w 이면 줄 넘김 대기.
func layout(line string, w int) (int, int) {
	rows, col := 1, 0
	forEachRune(line, func(r rune) {
		cw := runeWidth(r)
		if cw == 0 {
			return
		}
		if col+cw > w {
			rows++
			col = 0
		}
		col += cw
	})
	return rows, col
}

// position 은 줄의 n 번째 rune 앞(커서)의 (행, 열)이다.
func position(line string, n, w int) (int, int) {
	row, col, i := 0, 0, 0
	forEachRune(line, func(r rune) {
		if i >= n {
			return
		}
		i++
		cw := runeWidth(r)
		if cw == 0 {
			return
		}
		if col+cw > w {
			row++
			col = 0
		}
		col += cw
	})
	if col == w {
		return row + 1, 0
	}
	return row, col
}

// forEachRune 은 SGR 시퀀스(ESC [ … m)를 건너뛰고 rune 마다 f 를 부른다.
func forEachRune(s string, f func(rune)) {
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		f(r)
		i += n
	}
}

// runeWidth 는 표시 폭이다: East Asian Wide·Fullwidth = 2, 제어 문자 = 0, 나머지 1 (TM-FR-20).
func runeWidth(r rune) int {
	if unicode.IsControl(r) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}
