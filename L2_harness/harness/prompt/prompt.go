// Package prompt 는 시스템 프롬프트 조립과 컨텍스트 파일·스킬 탐색이다 (3.4). 파일은 env.FileSystem 으로만 읽는다.
package prompt

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

// DefaultPreamble 은 기본 문안이다 (사용자 확정, 번역은 SPEC 부록 A).
const DefaultPreamble = `You are an expert coding assistant operating inside dynamic_react, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files with the tools listed below.

How this environment works:
- Tools run in the working directory shown in <cwd>. Relative paths resolve against it, and ~ resolves to the home directory.
- Large tool output is truncated. read tells you the offset to continue from; bash keeps the end of the output and reports the path of a file with the full output.
- edit replaces exact text: read the file first, copy oldText exactly, and make each oldText unique. All edits in one call apply to the original file.
- A tool result that says "aborted" was interrupted before it finished. Check the current state before running it again.
- In long sessions, earlier messages may be replaced by a summary of them. Treat the summary as a reliable record of prior work.
- <project_context> holds instructions from the project's AGENTS.md and CLAUDE.md files. Follow them for work in this project.`

type Tool struct {
	Name    string
	Snippet string // 비면 tools 절에 넣지 않는다
}

type File struct {
	Path    string
	Content string
}

type Skill struct {
	Name        string
	Description string
	Path        string
}

type Input struct {
	Preamble     string // 있으면 preamble·tools·rules 를 대체한다 (pi customPrompt)
	Append       string
	Tools        []Tool
	ContextFiles []File
	Skills       []Skill
	Cwd          string
}

// Build 는 시스템 프롬프트 text 블록 하나다 (HN-FR-17).
func Build(in Input) []contract.Block {
	has := map[string]bool{}
	for _, t := range in.Tools {
		has[t.Name] = true
	}
	parts := []string{in.Preamble}
	if in.Preamble == "" {
		parts = []string{DefaultPreamble, section("tools", toolsText(in.Tools)), section("rules", rulesText(has))}
	}
	if in.Append != "" {
		parts = append(parts, section("addendum", in.Append))
	}
	if len(in.ContextFiles) > 0 {
		parts = append(parts, section("project_context", contextText(in.ContextFiles)))
	}
	if has["read"] && len(in.Skills) > 0 {
		parts = append(parts, section("skills", skillsText(in.Skills)))
	}
	parts = append(parts, section("cwd", in.Cwd))
	return []contract.Block{{Kind: contract.BlockText, Text: strings.Join(parts, "\n\n")}}
}

func section(name, body string) string { return "<" + name + ">\n" + body + "\n</" + name + ">" }

func toolsText(tools []Tool) string {
	var lines []string
	for _, t := range tools {
		if t.Snippet != "" {
			lines = append(lines, "- "+t.Name+": "+t.Snippet)
		}
	}
	list := "(none)"
	if len(lines) > 0 {
		list = strings.Join(lines, "\n")
	}
	return list + "\n\nIn addition to the tools above, you may have access to other custom tools depending on the project."
}

func rulesText(has map[string]bool) string {
	var rules []string
	if has["bash"] && !has["grep"] && !has["find"] && !has["ls"] {
		rules = append(rules, "Use bash for file operations like ls, rg, find")
	}
	rules = append(rules, "Be concise in your responses", "Show file paths clearly when working with files")
	return "- " + strings.Join(rules, "\n- ")
}

func contextText(files []File) string {
	parts := []string{"Project-specific instructions and guidelines:"}
	for _, f := range files {
		parts = append(parts, fmt.Sprintf("<project_instructions path=%q>\n%s\n</project_instructions>", f.Path, f.Content))
	}
	return strings.Join(parts, "\n\n")
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func skillsText(skills []Skill) string {
	lines := []string{
		"The following skills provide specialized instructions for specific tasks.",
		"Use the read tool to load a skill's file when the task matches its description.",
		"When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.",
		"",
		"<available_skills>",
	}
	for _, s := range skills {
		lines = append(lines, "  <skill>",
			"    <name>"+xmlEscaper.Replace(s.Name)+"</name>",
			"    <description>"+xmlEscaper.Replace(s.Description)+"</description>",
			"    <location>"+xmlEscaper.Replace(s.Path)+"</location>",
			"  </skill>")
	}
	return strings.Join(append(lines, "</available_skills>"), "\n")
}

// ContextFiles 는 cwd 부터 git 루트(없으면 /)까지의 AGENTS.md·CLAUDE.md 를 루트 쪽부터 돌려준다 (HN-FR-19).
func ContextFiles(ctx context.Context, fsys env.FileSystem, cwd string) ([]File, error) {
	var dirs []string
	for d := path.Clean(cwd); ; d = path.Dir(d) {
		dirs = append(dirs, d)
		root, err := exists(ctx, fsys, path.Join(d, ".git"), false)
		if err != nil {
			return nil, err
		}
		if root || d == "/" {
			break
		}
	}
	var out []File
	for i := len(dirs) - 1; i >= 0; i-- {
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			p := path.Join(dirs[i], name)
			ok, err := exists(ctx, fsys, p, true)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			b, err := fsys.ReadFile(ctx, p)
			if err != nil {
				return nil, err
			}
			out = append(out, File{Path: p, Content: string(b)})
		}
	}
	return out, nil
}

// exists 는 p 가 있는지다. fileOnly 면 디렉터리는 없는 것으로 본다.
func exists(ctx context.Context, fsys env.FileSystem, p string, fileOnly bool) (bool, error) {
	info, err := fsys.Stat(ctx, p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !fileOnly || !info.IsDir, nil
}

// Skills 는 스킬 디렉터리들의 */SKILL.md frontmatter 를 모은다. 같은 이름은 먼저 찾은 것이 이긴다 (HN-FR-20).
func Skills(ctx context.Context, fsys env.FileSystem, cwd, home string) ([]Skill, error) {
	var bases []string
	for _, sub := range []string{".agents/skills", ".claude/skills"} {
		if home != "" {
			bases = append(bases, path.Join(home, sub))
		}
		bases = append(bases, path.Join(cwd, sub))
	}
	seen := map[string]bool{}
	var out []Skill
	for _, base := range bases {
		entries, err := fsys.ListDir(ctx, base)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, ent := range entries {
			if !ent.IsDir {
				continue
			}
			p := path.Join(base, ent.Name, "SKILL.md")
			b, err := fsys.ReadFile(ctx, p)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			fm := frontmatter(string(b))
			name, desc := fm["name"], fm["description"]
			if name == "" || desc == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Skill{Name: name, Description: desc, Path: p})
		}
	}
	return out, nil
}

// frontmatter 는 "---" 로 둘러싼 최상위 "key: value" 를 읽는다. 접힌 값(> |)은 들여쓴 다음 줄들을 잇는다.
func frontmatter(s string) map[string]string {
	body, ok := strings.CutPrefix(strings.ReplaceAll(s, "\r\n", "\n"), "---\n")
	if !ok {
		return nil
	}
	end := strings.Index(body, "\n---")
	if end < 0 {
		return nil
	}
	out := map[string]string{}
	lines := strings.Split(body[:end], "\n")
	for i := 0; i < len(lines); i++ {
		key, val, ok := strings.Cut(lines[i], ":")
		if !ok || strings.HasPrefix(lines[i], " ") {
			continue
		}
		val = strings.TrimSpace(val)
		if val == ">" || val == "|" {
			sep := " "
			if val == "|" {
				sep = "\n"
			}
			var folded []string
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") {
				i++
				folded = append(folded, strings.TrimSpace(lines[i]))
			}
			val = strings.Join(folded, sep)
		}
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		out[strings.TrimSpace(key)] = val
	}
	return out
}
