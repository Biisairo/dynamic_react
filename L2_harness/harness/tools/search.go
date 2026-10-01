package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

const ignoreNote = "Searches all files including hidden and ignored ones; set respectIgnore to skip files ignored by .gitignore."

var grepSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"pattern":{"type":"string","description":"Search pattern (regex or literal string)"},` +
	`"path":{"type":"string","description":"Directory or file to search (default: current directory)"},` +
	`"glob":{"type":"string","description":"Filter files by glob pattern, e.g. '*.ts' or '**/*.spec.ts'"},` +
	`"ignoreCase":{"type":"boolean","description":"Case-insensitive search (default: false)"},` +
	`"literal":{"type":"boolean","description":"Treat pattern as literal string instead of regex (default: false)"},` +
	`"context":{"type":"integer","description":"Number of lines to show before and after each match (default: 0)"},` +
	`"limit":{"type":"integer","description":"Maximum number of matches to return (default: 100)"},` +
	`"respectIgnore":{"type":"boolean","description":"Skip files ignored by .gitignore (default: false)"}},"required":["pattern"]}`)

var findSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"pattern":{"type":"string","description":"Glob pattern to match files, e.g. '*.ts', '**/*.json', or 'src/**/*.spec.ts'"},` +
	`"path":{"type":"string","description":"Directory to search in (default: current directory)"},` +
	`"limit":{"type":"integer","description":"Maximum number of results (default: 1000)"},` +
	`"respectIgnore":{"type":"boolean","description":"Skip files ignored by .gitignore (default: false)"}},"required":["pattern"]}`)

var lsSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"path":{"type":"string","description":"Directory to list (default: current directory)"},` +
	`"limit":{"type":"integer","description":"Maximum number of entries to return (default: 500)"}}}`)

// Grep 은 grep 도구다 (HN-FR-46, 47). 자체 절단을 하므로 Truncate 는 항등이다.
func Grep(e env.Env) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "grep", InputSchema: grepSchema, Description: fmt.Sprintf(
			"Search file contents for a pattern. Returns matching lines with file paths and line numbers. %s "+
				"Output is truncated to %d matches or %dKB (whichever is hit first). Long lines are truncated to %d chars.",
			ignoreNote, grepLimit, maxBytes/1024, grepLineMax)},
		Run:      func(ctx context.Context, call contract.ToolCall) contract.ToolResult { return runGrep(ctx, e, call) },
		Truncate: identity,
	}
}

// Find 는 find 도구다 (HN-FR-48).
func Find(e env.Env) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "find", InputSchema: findSchema, Description: fmt.Sprintf(
			"Search for files by glob pattern. Returns matching file paths relative to the search directory. %s "+
				"Output is truncated to %d results or %dKB (whichever is hit first).", ignoreNote, findLimit, maxBytes/1024)},
		Run:      func(ctx context.Context, call contract.ToolCall) contract.ToolResult { return runFind(ctx, e, call) },
		Truncate: identity,
	}
}

// Ls 는 ls 도구다 (HN-FR-49).
func Ls(e env.Env) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "ls", InputSchema: lsSchema, Description: fmt.Sprintf(
			"List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. "+
				"Output is truncated to %d entries or %dKB (whichever is hit first).", lsLimit, maxBytes/1024)},
		Run:      func(ctx context.Context, call contract.ToolCall) contract.ToolResult { return runLs(ctx, e, call) },
		Truncate: identity,
	}
}

func runGrep(ctx context.Context, e env.Env, call contract.ToolCall) contract.ToolResult {
	var a struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		Glob          string `json:"glob"`
		IgnoreCase    bool   `json:"ignoreCase"`
		Literal       bool   `json:"literal"`
		Context       int    `json:"context"`
		Limit         *int   `json:"limit"`
		RespectIgnore bool   `json:"respectIgnore"`
	}
	if err := decode(call, &a); err != nil {
		return errResult(call.ID, err.Error())
	}
	expr := a.Pattern
	if a.Literal {
		expr = regexp.QuoteMeta(expr)
	}
	if a.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return errResult(call.ID, fmt.Sprintf("Invalid regex: %v", err))
	}
	var g *pathGlob
	if a.Glob != "" {
		pg, err := compileGlob(a.Glob)
		if err != nil {
			return errResult(call.ID, err.Error())
		}
		g = &pg
	}
	name := cmp.Or(a.Path, ".")
	root, fi, err := searchRoot(ctx, e, name)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	s := grepState{re: re, context: max(a.Context, 0), limit: limitArg(a.Limit, grepLimit)}
	if !fi.IsDir {
		s.file(ctx, e, path.Base(root), root)
	} else if err := walk(ctx, e, root, a.RespectIgnore, func(rel, abs string, fi env.FileInfo) bool {
		if fi.IsDir || fi.Symlink || (g != nil && !g.match(rel)) {
			return false
		}
		return s.file(ctx, e, rel, abs)
	}); err != nil {
		return errResult(call.ID, err.Error())
	}
	if s.matches == 0 {
		return okResult(call.ID, textBlock("No matches found"))
	}
	out, cut := headBytes(s.lines)
	var notices []string
	if s.limitHit {
		notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", s.limit, s.limit*2))
	}
	if cut {
		notices = append(notices, formatSize(maxBytes)+" limit reached")
	}
	if s.longLines {
		notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", grepLineMax))
	}
	return okResult(call.ID, textBlock(withNotices(out, notices)))
}

type grepState struct {
	re                  *regexp.Regexp
	context, limit      int
	matches             int
	lines               []string
	limitHit, longLines bool
}

// file 은 파일 하나를 검사한다. limit 을 넘는 match 를 만나면 true (멈춤).
func (s *grepState) file(ctx context.Context, e env.Env, rel, abs string) bool {
	data, err := e.ReadFile(ctx, abs)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], "\r", "")
	}
	for i, l := range lines {
		if !s.re.MatchString(l) {
			continue
		}
		if s.matches == s.limit {
			s.limitHit = true
			return true
		}
		s.matches++
		for n := max(i-s.context, 0); n <= min(i+s.context, len(lines)-1); n++ {
			sep := "-"
			if n == i {
				sep = ":"
			}
			s.lines = append(s.lines, fmt.Sprintf("%s%s%d%s %s", rel, sep, n+1, sep, s.cut(lines[n])))
		}
	}
	return false
}

func (s *grepState) cut(l string) string {
	if utf8.RuneCountInString(l) <= grepLineMax {
		return l
	}
	s.longLines = true
	return string([]rune(l)[:grepLineMax]) + "... [truncated]"
}

func runFind(ctx context.Context, e env.Env, call contract.ToolCall) contract.ToolResult {
	var a struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		Limit         *int   `json:"limit"`
		RespectIgnore bool   `json:"respectIgnore"`
	}
	if err := decode(call, &a); err != nil {
		return errResult(call.ID, err.Error())
	}
	if a.Pattern == "" {
		return errResult(call.ID, "pattern is required")
	}
	g, err := compileGlob(a.Pattern)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	name := cmp.Or(a.Path, ".")
	root, fi, err := searchRoot(ctx, e, name)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	if !fi.IsDir {
		return errResult(call.ID, "Not a directory: "+name)
	}
	limit := limitArg(a.Limit, findLimit)
	var found []string
	hit := false
	if err := walk(ctx, e, root, a.RespectIgnore, func(rel, abs string, fi env.FileInfo) bool {
		if !g.match(rel) {
			return false
		}
		if len(found) == limit {
			hit = true
			return true
		}
		found = append(found, rel+dirSuffix(fi))
		return false
	}); err != nil {
		return errResult(call.ID, err.Error())
	}
	if len(found) == 0 {
		return okResult(call.ID, textBlock("No files found matching pattern"))
	}
	return okResult(call.ID, textBlock(listing(found, hit, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", limit, limit*2))))
}

func runLs(ctx context.Context, e env.Env, call contract.ToolCall) contract.ToolResult {
	var a struct {
		Path  string `json:"path"`
		Limit *int   `json:"limit"`
	}
	if err := decode(call, &a); err != nil {
		return errResult(call.ID, err.Error())
	}
	name := cmp.Or(a.Path, ".")
	root, fi, err := searchRoot(ctx, e, name)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	if !fi.IsDir {
		return errResult(call.ID, "Not a directory: "+name)
	}
	entries, err := e.ListDir(ctx, root)
	if err != nil {
		return errResult(call.ID, fmt.Sprintf("Cannot read directory: %v", err))
	}
	if len(entries) == 0 {
		return okResult(call.ID, textBlock("(empty directory)"))
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	limit := limitArg(a.Limit, lsLimit)
	var out []string
	for _, ent := range entries[:min(limit, len(entries))] {
		out = append(out, ent.Name+dirSuffix(ent))
	}
	return okResult(call.ID, textBlock(listing(out, len(entries) > limit, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2))))
}

func dirSuffix(fi env.FileInfo) string {
	if fi.IsDir {
		return "/"
	}
	return ""
}

// listing 은 find·ls 의 결과 문구다: 50KB 절단과 안내.
func listing(lines []string, limitHit bool, limitNote string) string {
	out, cut := headBytes(lines)
	var notices []string
	if limitHit {
		notices = append(notices, limitNote)
	}
	if cut {
		notices = append(notices, formatSize(maxBytes)+" limit reached")
	}
	return withNotices(out, notices)
}
