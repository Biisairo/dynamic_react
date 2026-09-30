package prompt_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/prompt"
)

var fourTools = []prompt.Tool{
	{Name: "read", Snippet: "Read file contents"},
	{Name: "bash", Snippet: "Execute bash commands"},
	{Name: "custom"},
}

func one(t *testing.T, bs []contract.Block) string {
	t.Helper()
	if len(bs) != 1 || bs[0].Kind != contract.BlockText {
		t.Fatalf("blocks %+v", bs)
	}
	return bs[0].Text
}

// HN-FR-17 골든
func TestBuildGolden(t *testing.T) {
	got := one(t, prompt.Build(prompt.Input{
		Cwd:          "/w",
		Tools:        fourTools,
		Append:       "Extra rule.",
		ContextFiles: []prompt.File{{Path: "/w/AGENTS.md", Content: "Use tabs."}},
		Skills:       []prompt.Skill{{Name: "a&b", Description: "does <x>", Path: "/h/.agents/skills/a/SKILL.md"}},
	}))
	want := prompt.DefaultPreamble + `

<tools>
- read: Read file contents
- bash: Execute bash commands

In addition to the tools above, you may have access to other custom tools depending on the project.
</tools>

<rules>
- Use bash for file operations like ls, rg, find
- Be concise in your responses
- Show file paths clearly when working with files
</rules>

<addendum>
Extra rule.
</addendum>

<project_context>
Project-specific instructions and guidelines:

<project_instructions path="/w/AGENTS.md">
Use tabs.
</project_instructions>
</project_context>

<skills>
The following skills provide specialized instructions for specific tasks.
Use the read tool to load a skill's file when the task matches its description.
When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.

<available_skills>
  <skill>
    <name>a&amp;b</name>
    <description>does &lt;x&gt;</description>
    <location>/h/.agents/skills/a/SKILL.md</location>
  </skill>
</available_skills>
</skills>

<cwd>
/w
</cwd>`
	if got != want {
		t.Fatalf("got:\n%s\n---want:\n%s", got, want)
	}
}

func TestBuildVariants(t *testing.T) {
	got := one(t, prompt.Build(prompt.Input{Cwd: "/w"}))
	if !strings.Contains(got, "<tools>\n(none)\n\nIn addition") || strings.Contains(got, "Use bash") ||
		strings.Contains(got, "\n<addendum>\n") || strings.Contains(got, "\n<project_context>\n") || strings.Contains(got, "\n<skills>\n") {
		t.Fatalf("minimal:\n%s", got)
	}
	got = one(t, prompt.Build(prompt.Input{Cwd: "/w", Preamble: "Custom.", Tools: fourTools}))
	if !strings.HasPrefix(got, "Custom.\n\n<cwd>") || strings.Contains(got, "\n<tools>\n") || strings.Contains(got, "\n<rules>\n") {
		t.Fatalf("custom:\n%s", got)
	}
	noRead := one(t, prompt.Build(prompt.Input{Cwd: "/w", Tools: []prompt.Tool{{Name: "bash", Snippet: "x"}},
		Skills: []prompt.Skill{{Name: "s", Description: "d", Path: "/p"}}}))
	if strings.Contains(noRead, "\n<skills>\n") {
		t.Fatal("skills without read tool")
	}
}

// HN-FR-19
func TestContextFiles(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/repo/sub/deep", "/h")
	e.SetDir("/repo/.git")
	e.SetFile("/AGENTS.md", "above root")
	e.SetFile("/repo/AGENTS.md", "root")
	e.SetFile("/repo/sub/CLAUDE.md", "sub claude")
	e.SetFile("/repo/sub/AGENTS.md", "sub agents")
	files, err := prompt.ContextFiles(ctx, e, e.Cwd())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "/repo/AGENTS.md,/repo/sub/AGENTS.md,/repo/sub/CLAUDE.md" || files[0].Content != "root" {
		t.Fatalf("git repo %v", paths)
	}

	e = envtest.New("/x/y", "/h")
	e.SetFile("/AGENTS.md", "top")
	e.SetFile("/x/CLAUDE.md", "x")
	e.SetDir("/x/y/AGENTS.md") // 디렉터리는 무시
	files, _ = prompt.ContextFiles(ctx, e, e.Cwd())
	if len(files) != 2 || files[0].Path != "/AGENTS.md" || files[1].Path != "/x/CLAUDE.md" {
		t.Fatalf("no git %+v", files)
	}

	e = envtest.New("/wt/a", "/h")
	e.SetFile("/wt/.git", "gitdir: elsewhere") // worktree 의 .git 파일
	e.SetFile("/AGENTS.md", "above")
	if files, _ := prompt.ContextFiles(ctx, e, e.Cwd()); len(files) != 0 {
		t.Fatalf(".git file not a root %+v", files)
	}

	e.FailOn("/wt/a/AGENTS.md", errors.New("io"))
	if _, err := prompt.ContextFiles(ctx, e, e.Cwd()); err == nil {
		t.Fatal("env error ignored")
	}
}

// HN-FR-20
func TestSkills(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/w", "/h")
	e.SetFile("/h/.agents/skills/a/SKILL.md", "---\nname: alpha\ndescription: A skill\n---\nbody")
	e.SetFile("/w/.agents/skills/b/SKILL.md", "---\nname: alpha\ndescription: duplicate\n---\n")
	e.SetFile("/h/.claude/skills/c/SKILL.md", "---\nname: \"gamma\"\ndescription: >\n  multi\n  line\nother: 1\n---\n")
	e.SetFile("/w/.claude/skills/d/SKILL.md", "no frontmatter")
	e.SetFile("/w/.claude/skills/e/SKILL.md", "---\nname: eps\n---\n")
	e.SetFile("/w/.claude/skills/g/SKILL.md", "---\nname: unterminated\ndescription: x\n")
	e.SetDir("/w/.claude/skills/f")
	e.SetFile("/w/.claude/skills/file.txt", "x")
	skills, err := prompt.Skills(ctx, e, "/w", "/h")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0] != (prompt.Skill{Name: "alpha", Description: "A skill", Path: "/h/.agents/skills/a/SKILL.md"}) ||
		skills[1] != (prompt.Skill{Name: "gamma", Description: "multi line", Path: "/h/.claude/skills/c/SKILL.md"}) {
		t.Fatalf("skills %+v", skills)
	}
	skills, _ = prompt.Skills(ctx, e, "/w", "")
	if len(skills) != 1 || skills[0].Path != "/w/.agents/skills/b/SKILL.md" {
		t.Fatalf("no home %+v", skills)
	}
	e.FailOn("/w/.agents/skills", errors.New("io"))
	if _, err := prompt.Skills(ctx, e, "/w", ""); err == nil {
		t.Fatal("env error ignored")
	}
}
