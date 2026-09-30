package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"golang.org/x/text/unicode/norm"
)

var editSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},` +
	`"edits":{"type":"array","description":"Replacements matched against the original file","items":{"type":"object","properties":{` +
	`"oldText":{"type":"string","description":"Exact text to find. Must be unique and must not overlap other edits"},` +
	`"newText":{"type":"string","description":"Replacement text"}},"required":["oldText","newText"]}}},"required":["path","edits"]}`)

const bom = "\ufeff"

type textEdit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

func editTool(e env.Env, q *queue) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "edit", InputSchema: editSchema,
			Description: "Edit a file by replacing exact text. Each edits[].oldText must match a unique, non-overlapping region of the original file; " +
				"all edits are matched against the original content, not after earlier edits. Returns a unified diff."},
		Run: func(ctx context.Context, call contract.ToolCall) contract.ToolResult {
			var a struct {
				Path  string     `json:"path"`
				Edits []textEdit `json:"edits"`
			}
			if err := decode(call, &a); err != nil {
				return errResult(call.ID, err.Error())
			}
			abs, err := resolve(e, a.Path)
			if err != nil {
				return errResult(call.ID, err.Error())
			}
			name := display(e, abs)
			defer q.lock(abs)()
			data, err := e.ReadFile(ctx, abs)
			if errors.Is(err, fs.ErrNotExist) {
				return errResult(call.ID, "File not found: "+a.Path)
			}
			if err != nil {
				return errResult(call.ID, fmt.Sprintf("Cannot read %s: %v", name, err))
			}
			text, hasBOM := strings.CutPrefix(string(data), bom)
			ending := detectLineEnding(text)
			base := normalizeToLF(text)
			updated, err := applyEdits(base, a.Edits, name)
			if err != nil {
				return errResult(call.ID, err.Error())
			}
			out := restoreLineEndings(updated, ending)
			if hasBOM {
				out = bom + out
			}
			if err := e.WriteFile(ctx, abs, []byte(out)); err != nil {
				return errResult(call.ID, fmt.Sprintf("Cannot write %s: %v", name, err))
			}
			return okResult(call.ID, textBlock(fmt.Sprintf("Successfully replaced %d block(s) in %s.\n\n%s",
				len(a.Edits), name, unifiedDiff(name, base, updated))))
		},
	}
}

func detectLineEnding(s string) string {
	lf := strings.Index(s, "\n")
	crlf := strings.Index(s, "\r\n")
	if lf >= 0 && crlf >= 0 && crlf < lf {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(s, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

var fuzzyReplacer = strings.NewReplacer(
	"‘", "'", "’", "'", "‚", "'", "‛", "'",
	"“", `"`, "”", `"`, "„", `"`, "‟", `"`,
	"‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-", "−", "-",
	" ", " ", " ", " ", " ", " ", " ", " ", " ", " ", " ", " ",
	" ", " ", " ", " ", " ", " ", " ", " ", " ", " ", " ", " ", "　", " ",
)

// normalizeFuzzy 는 pi normalizeForFuzzyMatch 다: NFKC → 줄 끝 공백 제거 → 따옴표·대시·특수 공백 치환.
func normalizeFuzzy(s string) string {
	lines := strings.Split(norm.NFKC.String(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	return fuzzyReplacer.Replace(strings.Join(lines, "\n"))
}

type match struct {
	edit, index, length int
	newText             string
}

// find 는 정확 매칭, 없으면 fuzzy 매칭이다. fuzzy 면 위치는 정규화 공간의 것이다 (pi fuzzyFindText).
func find(content, old string) (idx, length int, fuzzy bool) {
	if i := strings.Index(content, old); i >= 0 {
		return i, len(old), false
	}
	nOld := normalizeFuzzy(old)
	if i := strings.Index(normalizeFuzzy(content), nOld); i >= 0 {
		return i, len(nOld), true
	}
	return -1, 0, false
}

// applyEdits 는 LF 로 정규화된 내용에 편집을 원본 기준으로 적용한다 (pi applyEditsToNormalizedContent).
func applyEdits(content string, edits []textEdit, name string) (string, error) {
	many := len(edits) > 1
	label := func(i int, one, multi string) string {
		if many {
			return fmt.Sprintf(multi, i)
		}
		return one
	}
	norm := make([]textEdit, len(edits))
	usedFuzzy := false
	for i, ed := range edits {
		norm[i] = textEdit{normalizeToLF(ed.OldText), normalizeToLF(ed.NewText)}
		if norm[i].OldText == "" {
			return "", fmt.Errorf("%s must not be empty in %s.", label(i, "oldText", "edits[%d].oldText"), name)
		}
	}
	for _, ed := range norm {
		if _, _, fz := find(content, ed.OldText); fz {
			usedFuzzy = true
		}
	}
	base := content
	if usedFuzzy {
		base = normalizeFuzzy(content)
	}
	var ms []match
	for i, ed := range norm {
		idx, length, _ := find(base, ed.OldText)
		if idx < 0 {
			return "", fmt.Errorf("Could not find %s in %s. The old text must match exactly including all whitespace and newlines.",
				label(i, "the exact text", "edits[%d]"), name)
		}
		if n := strings.Count(normalizeFuzzy(base), normalizeFuzzy(ed.OldText)); n > 1 {
			return "", fmt.Errorf("Found %d occurrences of %s in %s. The text must be unique. Please provide more context to make it unique.",
				n, label(i, "the text", "edits[%d]"), name)
		}
		ms = append(ms, match{edit: i, index: idx, length: length, newText: ed.NewText})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].index < ms[j].index })
	for i := 1; i < len(ms); i++ {
		if ms[i-1].index+ms[i-1].length > ms[i].index {
			return "", fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				ms[i-1].edit, ms[i].edit, name)
		}
	}
	var out string
	if usedFuzzy {
		out = replacePreservingLines(content, base, ms)
	} else {
		out = replace(base, ms, 0)
	}
	if out == content {
		return "", fmt.Errorf("No changes made to %s. The replacement produced identical content.", name)
	}
	return out, nil
}

// replace 는 정렬된 ms 를 뒤에서부터 적용한다. offset 은 s 가 원래 내용에서 시작하는 위치다.
func replace(s string, ms []match, offset int) string {
	for i := len(ms) - 1; i >= 0; i-- {
		at := ms[i].index - offset
		s = s[:at] + ms[i].newText + s[at+ms[i].length:]
	}
	return s
}

// splitKeepEnds 는 줄을 개행 포함으로 나눈다.
func splitKeepEnds(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// replacePreservingLines 는 정규화 공간(base)의 교체를 원본에 겹친다. 건드린 줄만 base 에서 다시 쓰고
// 나머지 줄은 원본 바이트를 유지한다 (pi applyReplacementsPreservingUnchangedLines). 정규화는 줄 수를 바꾸지 않는다.
func replacePreservingLines(original, base string, ms []match) string {
	orig := splitKeepEnds(original)
	baseLines := splitKeepEnds(base)
	starts := make([]int, len(baseLines)+1)
	for i, l := range baseLines {
		starts[i+1] = starts[i] + len(l)
	}
	lineOf := func(pos int) int {
		return sort.Search(len(baseLines), func(i int) bool { return starts[i+1] > pos })
	}
	type group struct {
		from, to int
		ms       []match
	}
	var groups []group
	for _, m := range ms {
		from, to := lineOf(m.index), lineOf(max(m.index+m.length-1, m.index))+1
		if n := len(groups); n > 0 && from < groups[n-1].to {
			groups[n-1].to = max(groups[n-1].to, to)
			groups[n-1].ms = append(groups[n-1].ms, m)
			continue
		}
		groups = append(groups, group{from, to, []match{m}})
	}
	var b strings.Builder
	next := 0
	for _, g := range groups {
		b.WriteString(strings.Join(orig[next:g.from], ""))
		b.WriteString(replace(base[starts[g.from]:starts[g.to]], g.ms, starts[g.from]))
		next = g.to
	}
	b.WriteString(strings.Join(orig[next:], ""))
	return b.String()
}
