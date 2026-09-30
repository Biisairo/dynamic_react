package tools

import (
	"strings"
	"unicode/utf8"
)

type truncation struct {
	content          string
	truncated        bool
	byBytes          bool
	outputLines      int
	firstLineTooLong bool
}

// truncateHead 는 앞에서부터 온전한 줄을 maxLines·maxBytes 안에서 담는다 (pi truncateHead).
func truncateHead(lines []string) truncation {
	n, size := 0, 0
	for i, l := range lines {
		add := len(l)
		if i > 0 {
			add++
		}
		if n == maxLines {
			return truncation{content: strings.Join(lines[:n], "\n"), truncated: true, outputLines: n}
		}
		if size+add > maxBytes {
			if n == 0 {
				return truncation{truncated: true, byBytes: true, firstLineTooLong: true}
			}
			return truncation{content: strings.Join(lines[:n], "\n"), truncated: true, byBytes: true, outputLines: n}
		}
		n, size = n+1, size+add
	}
	return truncation{content: strings.Join(lines, "\n"), outputLines: n}
}

type tailTruncation struct {
	content     string
	truncated   bool
	byBytes     bool
	outputLines int
	partial     bool // 마지막 한 줄이 maxBytes 를 넘어 그 끝부분만 담음
}

// truncateTail 은 끝에서부터 온전한 줄을 maxLines·maxBytes 안에서 담는다 (pi truncateTail).
func truncateTail(lines []string) tailTruncation {
	n, size := 0, 0
	for i := len(lines) - 1; i >= 0; i-- {
		add := len(lines[i])
		if n > 0 {
			add++
		}
		if n == maxLines {
			return tailTruncation{content: strings.Join(lines[i+1:], "\n"), truncated: true, outputLines: n}
		}
		if size+add > maxBytes {
			if n == 0 {
				return tailTruncation{content: lastBytes(lines[i], maxBytes), truncated: true, byBytes: true, outputLines: 1, partial: true}
			}
			return tailTruncation{content: strings.Join(lines[i+1:], "\n"), truncated: true, byBytes: true, outputLines: n}
		}
		n, size = n+1, size+add
	}
	return tailTruncation{content: strings.Join(lines, "\n"), outputLines: n}
}

// lastBytes 는 s 의 끝 n 바이트 이하를 UTF-8 경계에서 자른다.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
