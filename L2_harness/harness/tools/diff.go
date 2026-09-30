package tools

import (
	"fmt"
	"strings"
)

const (
	diffContext  = 4
	maxDiffCells = 4_000_000 // LCS 표 상한. 넘으면 가운데 구간을 통째로 삭제·추가로 보인다
)

type diffOp struct {
	kind byte // ' ', '-', '+'
	line string
}

// unifiedDiff 는 old→new 의 unified diff 다 (문맥 4줄, 파일 머리만).
func unifiedDiff(name, old, new string) string {
	ops := diffLines(splitLines(old), splitLines(new))
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", name, name)
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-diffContext, 0)
		end := i
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*diffContext {
				end = min(end+diffContext, len(ops))
				break
			}
			end = run
		}
		oldStart, newStart := 1, 1
		for _, op := range ops[:start] {
			if op.kind != '+' {
				oldStart++
			}
			if op.kind != '-' {
				newStart++
			}
		}
		oldLen, newLen := 0, 0
		for _, op := range ops[start:end] {
			if op.kind != '+' {
				oldLen++
			}
			if op.kind != '-' {
				newLen++
			}
		}
		fmt.Fprintf(&b, "@@ -%s +%s @@\n", hunkRange(oldStart, oldLen), hunkRange(newStart, newLen))
		for _, op := range ops[start:end] {
			b.WriteByte(op.kind)
			b.WriteString(op.line)
			b.WriteByte('\n')
		}
		i = end
	}
	return b.String()
}

func hunkRange(start, n int) string {
	if n == 0 {
		start--
	}
	return fmt.Sprintf("%d,%d", start, n)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// diffLines 는 공통 앞뒤를 떼고 가운데를 LCS 로 비교한다.
func diffLines(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []diffOp
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	ops = append(ops, lcsOps(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

func lcsOps(a, b []string) []diffOp {
	var ops []diffOp
	if len(a)*len(b) > maxDiffCells {
		for _, l := range a {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range b {
			ops = append(ops, diffOp{'+', l})
		}
		return ops
	}
	// t[i][j] = a[i:] 와 b[j:] 의 LCS 길이
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i, j = i+1, j+1
		case t[i+1][j] >= t[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
