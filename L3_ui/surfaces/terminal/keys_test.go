package terminal

import (
	"fmt"
	"strings"
	"testing"
)

func (k key) String() string {
	switch k.kind {
	case keyRune:
		return fmt.Sprintf("rune(%q)", k.r)
	case keyPaste:
		return fmt.Sprintf("paste(%q)", k.text)
	case keyKittyFlags:
		return fmt.Sprintf("kitty(%d)", k.n)
	}
	return keyNames[k.kind]
}

func keysString(ks []key) string {
	var out []string
	for _, k := range ks {
		out = append(out, k.String())
	}
	return strings.Join(out, " ")
}

func feedAll(chunks ...string) []key {
	var p parser
	var out []key
	for _, c := range chunks {
		out = append(out, p.feed([]byte(c))...)
	}
	return out
}

// TM-FR-09, 10: 세 인코딩 표.
func TestParseTable(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"\r", "enter"},
		{"\x1b[13u", "enter"},
		{"\x1b[27;1;13~", "enter"},
		{"\x1b[13;2u", "newline"},
		{"\x1b[27;2;13~", "newline"},
		{"\x1b\r", "alt-enter"},
		{"\x1b[13;3u", "alt-enter"},
		{"\x1b[27;3;13~", "alt-enter"},
		{"\n", "newline"},
		{"\x1b[106;5u", "newline"},
		{"\x1b[27;5;106~", "newline"},
		{"\x1b[27u", "esc"},
		{"\x1b[27;1;27~", "esc"},
		{"\x03", "ctrl-c"},
		{"\x1b[99;5u", "ctrl-c"},
		{"\x1b[27;5;99~", "ctrl-c"},
		{"\x04", "ctrl-d"},
		{"\x1b[100;5u", "ctrl-d"},
		{"\x1b[27;5;100~", "ctrl-d"},
		{"\x01", "home"},
		{"\x1b[97;5u", "home"},
		{"\x1b[27;5;97~", "home"},
		{"\x05", "end"},
		{"\x1b[101;5u", "end"},
		{"\x1b[27;5;101~", "end"},
		{"\x7f", "backspace"},
		{"\x08", "backspace"},
		{"\x1b[127u", "backspace"},
		{"\x1b[3~", "delete"},
		{"\x1b[D", "left"},
		{"\x1b[C", "right"},
		{"\x1b[A", "up"},
		{"\x1b[B", "down"},
		{"\x1bOD", "left"},
		{"\x1bOA", "up"},
		{"\x1b[1;5D", "left"},
		{"\x1b[1;2A", "up"},
		{"\x1b[H", "home"},
		{"\x1b[F", "end"},
		{"\x1b[1~", "home"},
		{"\x1b[4~", "end"},
		{"\x1bOH", "home"},
		{"\x1bOF", "end"},
		{"a", "rune('a')"},
		{"\t", "rune('\\t')"},
		{"한", "rune('한')"},
		{"\x1b[97u", "rune('a')"},
		{"\x1b[65;2u", "rune('A')"},
		{"\x1b[?1u", "kitty(1)"},
		{"\x1b[?62;22c", "da"},
	}
	for _, c := range cases {
		if got := keysString(feedAll(c.in)); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

// TM-FR-10: 표 밖의 키·시퀀스는 무시.
func TestParseIgnored(t *testing.T) {
	for _, in := range []string{"\x1b[15~", "\x1b[99;3u", "\x1bx", "\x1b[200;5u", "\x02", "\x1b[27;9;13~", "\x1b[?25h", "\x1b[Z", "\x1bOP", "\x1b[28;3;13~", "\x1b[27;5u", "\x1b[127;2u"} {
		if got := feedAll(in); len(got) != 0 {
			t.Errorf("%q: got %s", in, keysString(got))
		}
	}
	if got := keysString(feedAll("\x1b[@a\x1b[99:67;5u")); got != "rune('a') ctrl-c" {
		t.Fatalf("@ final, alternate key: %s", got)
	}
	if got := keysString(feedAll("a\x1b[15~b")); got != "rune('a') rune('b')" {
		t.Fatalf("around unknown: %s", got)
	}
}

// TM-FR-09: 1바이트씩 쪼개도 같다. UTF-8 도 쪼개진다.
func TestParseSplit(t *testing.T) {
	in := "a\x1b[13;2u한\x1b[27;3;13~\x1b[1;5D\x1bOA\x1b[200~x\ny\x1b[201~\r"
	var chunks []string
	for i := 0; i < len(in); i++ {
		chunks = append(chunks, in[i:i+1])
	}
	want := keysString(feedAll(in))
	if got := keysString(feedAll(chunks...)); got != want {
		t.Fatalf("split %q\nwhole %q", got, want)
	}
	if want != "rune('a') newline rune('한') alt-enter left up paste(\"x\\ny\") enter" {
		t.Fatalf("whole %s", want)
	}
}

// TM-FR-10: 붙여넣기 — \r·\n·\r\n 은 줄바꿈 하나, 제어 바이트는 버림, 여러 read 에 걸침.
func TestParsePaste(t *testing.T) {
	got := keysString(feedAll("\x1b[200~a\r\nb\rc\x03\x1b", "[Ad\t\x1b[201~z"))
	if got != `paste("a\nb\nc[Ad\t") rune('z')` {
		t.Fatalf("paste %s", got)
	}
}

// TM-FR-10: 단독 ESC 는 시간 초과(flush) 뒤 Esc, ESC 뒤 다른 바이트가 오면 시퀀스.
func TestParseLoneEsc(t *testing.T) {
	var p parser
	if ks := p.feed([]byte("\x1b")); len(ks) != 0 || !p.loneEsc() {
		t.Fatalf("lone esc pending: %s %v", keysString(ks), p.loneEsc())
	}
	if ks := p.flush(); keysString(ks) != "esc" || p.loneEsc() {
		t.Fatalf("flush %s", keysString(ks))
	}
	if ks := p.feed([]byte("\x1b")); len(ks) != 0 {
		t.Fatal(keysString(ks))
	}
	if ks := p.feed([]byte("\r")); keysString(ks) != "alt-enter" {
		t.Fatalf("alt-enter %s", keysString(ks))
	}
	if ks := p.feed([]byte("\x1b[")); len(ks) != 0 || p.loneEsc() {
		t.Fatalf("partial csi %s %v", keysString(ks), p.loneEsc())
	}
	if ks := p.flush(); len(ks) != 0 {
		t.Fatalf("flush of partial csi %s", keysString(ks))
	}
	if ks := p.feed([]byte("A")); keysString(ks) != "up" {
		t.Fatalf("csi continued %s", keysString(ks))
	}
	if ks := p.feed([]byte("\x1b\x1b[A")); keysString(ks) != "esc up" {
		t.Fatalf("esc esc %s", keysString(ks))
	}
}

// 잘못된 UTF-8 은 버린다.
func TestParseInvalidUTF8(t *testing.T) {
	if got := keysString(feedAll("\xffa")); got != "rune('a')" {
		t.Fatalf("invalid utf8 %s", got)
	}
}

// TM-FR-10: 붙여넣기 중 끝 표시를 기다리며 남은 ESC 는 단독 Esc 가 아니다.
func TestParsePasteEscPending(t *testing.T) {
	var p parser
	p.feed([]byte("\x1b[200~"))
	if ks := p.feed([]byte("\x1b")); len(ks) != 0 || p.loneEsc() || len(p.flush()) != 0 {
		t.Fatalf("esc inside paste: %s %v", keysString(ks), p.loneEsc())
	}
	if ks := p.feed([]byte("[201~")); keysString(ks) != `paste("")` {
		t.Fatal(keysString(ks))
	}
}
