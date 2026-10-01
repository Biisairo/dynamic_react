package terminal

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// vscreen 은 이 모듈이 쓰는 시퀀스만 해석하는 가상 터미널이다 (TM 4. 검증).
// 자동 줄 넘김: 마지막 열에 쓰면 커서는 그 자리에 머물고(대기), 다음 글자가 다음 행으로 간다.
// 넓은 글자가 행 끝에 걸리면 통째로 다음 행으로 간다.
type vscreen struct {
	w    int
	rows [][]rune // 칸마다 rune, 넓은 글자의 둘째 칸은 0
	r, c int      // c == w 이면 줄 넘김 대기
	seqs []string // 해석한 CSI (SGR 제외)
	sgr  int
	pend []byte
}

func newVScreen(w int) *vscreen { return &vscreen{w: w, rows: [][]rune{nil}} }

func (v *vscreen) Write(b []byte) (int, error) {
	data := append(v.pend, b...)
	v.pend = nil
	for len(data) > 0 {
		if data[0] == 0x1b {
			n := v.escape(data)
			if n == 0 {
				v.pend = append([]byte(nil), data...)
				break
			}
			data = data[n:]
			continue
		}
		r, n := utf8.DecodeRune(data)
		if r == utf8.RuneError && !utf8.FullRune(data) {
			v.pend = append([]byte(nil), data...)
			break
		}
		data = data[n:]
		switch r {
		case '\r':
			v.c = 0
		case '\n':
			v.r++
			if v.c == v.w {
				v.c = v.w - 1
			}
			v.ensure()
		default:
			v.put(r)
		}
	}
	return len(b), nil
}

func (v *vscreen) ensure() {
	for len(v.rows) <= v.r {
		v.rows = append(v.rows, nil)
	}
}

func (v *vscreen) put(r rune) {
	w := runeWidth(r)
	if w == 0 {
		return
	}
	if v.c+w > v.w {
		v.r++
		v.c = 0
		v.ensure()
	}
	row := v.rows[v.r]
	for len(row) < v.c+w {
		row = append(row, ' ')
	}
	row[v.c] = r
	if w == 2 {
		row[v.c+1] = 0
	}
	v.rows[v.r] = row
	v.c += w
}

// escape 는 data 앞의 시퀀스를 해석하고 길이를 돌려준다. 미완성이면 0.
func (v *vscreen) escape(data []byte) int {
	if len(data) < 2 {
		return 0
	}
	if data[1] != '[' {
		return 2
	}
	for i := 2; i < len(data); i++ {
		if data[i] >= 0x40 && data[i] <= 0x7e {
			v.csi(string(data[2:i]), data[i])
			return i + 1
		}
	}
	return 0
}

func (v *vscreen) csi(params string, final byte) {
	n := 1
	if x, err := strconv.Atoi(params); err == nil {
		n = x
	}
	switch {
	case final == 'm':
		v.sgr++
		return
	case final == 'A' && params != "" && params[0] >= '0' && params[0] <= '9' || final == 'A' && params == "":
		if v.c == v.w {
			v.c = v.w - 1
		}
		v.r = max(0, v.r-n)
	case final == 'C':
		v.c = min(v.c+n, v.w-1)
	case final == 'J' && params == "":
		if v.r < len(v.rows) {
			row := v.rows[v.r]
			if v.c < len(row) {
				v.rows[v.r] = row[:v.c]
			}
			v.rows = v.rows[:v.r+1]
		}
	case final == 'K' && params == "":
		if row := v.rows[v.r]; v.c < len(row) {
			v.rows[v.r] = row[:v.c]
		}
	}
	v.seqs = append(v.seqs, params+string(final))
}

// text 는 화면 전체다 (행 끝 공백 제거, 끝 빈 행 제거).
func (v *vscreen) text() string {
	var lines []string
	for _, row := range v.rows {
		var b strings.Builder
		for _, r := range row {
			if r != 0 {
				b.WriteRune(r)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// cursor 는 화면상의 커서 위치다 (대기 상태면 마지막 열).
func (v *vscreen) cursor() (int, int) {
	if v.c == v.w {
		return v.r, v.w - 1
	}
	return v.r, v.c
}
