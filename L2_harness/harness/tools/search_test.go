package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/tools"
)

// listFail 은 지정한 경로의 ListDir 만 실패시킨다 (Stat 은 그대로).
type listFail struct {
	*envtest.Env
	path string
}

func (l listFail) ListDir(ctx context.Context, p string) ([]env.FileInfo, error) {
	if p == l.path {
		return nil, errors.New("boom")
	}
	return l.Env.ListDir(ctx, p)
}

func tree() *envtest.Env {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.go", "package a\nfunc Foo() {}\n// foo bar\n")
	e.SetFile("/w/bin.dat", "Foo\x00rest")
	e.SetFile("/w/notes.txt", "FOO\n")
	e.SetFile("/w/.hidden", "Foo hidden")
	e.SetFile("/w/.git/config", "Foo git")
	e.SetFile("/w/sub/b.go", "x\nFoo2\ny\nz\n")
	e.SetFile("/w/sub/deep/d.go", "nothing")
	e.SetFile("/w/link/x.go", "Foo linked")
	e.SetSymlink("/w/link")
	e.SetFile("/w/flink.go", "Foo file link")
	e.SetSymlink("/w/flink.go")
	return e
}

// HN-FR-43, 45, 48
func TestFind(t *testing.T) {
	e := tree()
	f := tools.Find(e)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"pattern": "*.go"}, "a.go\nflink.go\nsub/b.go\nsub/deep/d.go"},
		{map[string]any{"pattern": "*"}, ".hidden\na.go\nbin.dat\nflink.go\nlink/\nnotes.txt\nsub/\nsub/b.go\nsub/deep/\nsub/deep/d.go"},
		{map[string]any{"pattern": "deep"}, "sub/deep/"},
		{map[string]any{"pattern": "link"}, "link/"},
		{map[string]any{"pattern": "sub/*.go"}, "sub/b.go"},
		{map[string]any{"pattern": "deep/*.go"}, "sub/deep/d.go"},
		{map[string]any{"pattern": "/deep/*.go"}, "No files found matching pattern"},
		{map[string]any{"pattern": "/sub/*"}, "sub/b.go\nsub/deep/"},
		{map[string]any{"pattern": "**/d.go"}, "sub/deep/d.go"},
		{map[string]any{"pattern": "sub/**/*.go"}, "sub/b.go\nsub/deep/d.go"},
		{map[string]any{"pattern": "?.go"}, "a.go\nsub/b.go\nsub/deep/d.go"},
		{map[string]any{"pattern": "[ab].*"}, "a.go\nsub/b.go"},
		{map[string]any{"pattern": "[ab]*"}, "a.go\nbin.dat\nsub/b.go"},
		{map[string]any{"pattern": "*.go", "path": "sub"}, "b.go\ndeep/d.go"},
		{map[string]any{"pattern": "*.go", "path": "/w/sub/deep"}, "d.go"},
		{map[string]any{"pattern": "config"}, "No files found matching pattern"},
		{map[string]any{"pattern": "*.go", "limit": 2}, "a.go\nflink.go\n\n[2 results limit reached. Use limit=4 for more, or refine pattern]"},
		{map[string]any{"pattern": "*.go", "limit": 4}, "a.go\nflink.go\nsub/b.go\nsub/deep/d.go"},
		{map[string]any{"pattern": "*.go", "limit": 0}, "a.go\n\n[1 results limit reached. Use limit=2 for more, or refine pattern]"},
	}
	for _, c := range cases {
		if got := wantOK(t, run(t, f, c.args)); got != c.want {
			t.Errorf("%v:\n got %q\nwant %q", c.args, got, c.want)
		}
	}
	wantErr(t, run(t, f, map[string]any{"pattern": ""}), "pattern is required")
	wantErr(t, run(t, f, map[string]any{"pattern": "[a"}), "Invalid glob pattern: [a")
	wantErr(t, run(t, f, map[string]any{"pattern": "*", "path": "nope"}), "Path not found: nope")
	wantErr(t, run(t, f, map[string]any{"pattern": "*", "path": "a.go"}), "Not a directory: a.go")
	e.FailOn("/w/sub", errors.New("denied"))
	wantErr(t, run(t, f, map[string]any{"pattern": "*", "path": "sub"}), "Cannot access sub: ")
}

// HN-FR-43: 하위 디렉터리 ListDir 에러는 건너뛰고, 시작 디렉터리 에러는 error 결과.
func TestFindListErrors(t *testing.T) {
	e := tree()
	if got := wantOK(t, run(t, tools.Find(listFail{e, "/w/sub"}), map[string]any{"pattern": "*.go"})); got != "a.go\nflink.go" {
		t.Fatalf("sub skip %q", got)
	}
	wantErr(t, run(t, tools.Find(listFail{e, "/w"}), map[string]any{"pattern": "*.go"}), "boom")
}

// HN-FR-43: 취소된 ctx 는 error 결과.
func TestSearchCanceled(t *testing.T) {
	e := tree()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tl := range []struct {
		name string
		args string
	}{{"find", `{"pattern":"*"}`}, {"grep", `{"pattern":"x"}`}} {
		var r contract.ToolResult
		if tl.name == "find" {
			r = tools.Find(e).Run(ctx, contract.ToolCall{ID: "c", Name: tl.name, Args: json.RawMessage(tl.args)})
		} else {
			r = tools.Grep(e).Run(ctx, contract.ToolCall{ID: "c", Name: tl.name, Args: json.RawMessage(tl.args)})
		}
		if r.Status != contract.StatusError || !strings.Contains(text(r), "context canceled") {
			t.Errorf("%s: %s %q", tl.name, r.Status, text(r))
		}
	}
}

// HN-FR-44: gitignore 문법. 각 경우는 /w 를 git 루트로 두고 find "*" 결과를 본다.
func TestRespectIgnore(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		path   string
		want   string
		noRepo bool
	}{
		{name: "basename at any depth",
			files: map[string]string{"/w/.gitignore": "*.log\n", "/w/a.log": "", "/w/sub/b.log": "", "/w/c.txt": ""},
			want:  ".gitignore\nc.txt\nsub/"},
		{name: "leading slash anchors",
			files: map[string]string{"/w/.gitignore": "/a.log\n", "/w/a.log": "", "/w/sub/a.log": ""},
			want:  ".gitignore\nsub/\nsub/a.log"},
		{name: "middle slash anchors",
			files: map[string]string{"/w/.gitignore": "sub/*.log\n", "/w/sub/a.log": "", "/w/x/sub/a.log": ""},
			want:  ".gitignore\nsub/\nx/\nx/sub/\nx/sub/a.log"},
		{name: "trailing slash is dir only",
			files: map[string]string{"/w/.gitignore": "build/\n", "/w/build/o": "", "/w/sub/build": ""},
			want:  ".gitignore\nsub/\nsub/build"},
		{name: "negation after",
			files: map[string]string{"/w/.gitignore": "*.log\n!keep.log\n", "/w/a.log": "", "/w/keep.log": ""},
			want:  ".gitignore\nkeep.log"},
		{name: "last rule wins",
			files: map[string]string{"/w/.gitignore": "!keep.log\n*.log\n", "/w/keep.log": ""},
			want:  ".gitignore"},
		{name: "negation inside ignored dir has no effect",
			files: map[string]string{"/w/.gitignore": "dir/\n!dir/keep\n", "/w/dir/keep": "", "/w/dir/x": ""},
			want:  ".gitignore"},
		{name: "double star",
			files: map[string]string{"/w/.gitignore": "a/**/b\n**/gen\n", "/w/a/b": "", "/w/a/x/y/b": "", "/w/a/c": "", "/w/p/q/gen/z": ""},
			want:  ".gitignore\na/\na/c\na/x/\na/x/y/\np/\np/q/"},
		{name: "comments blanks escapes trailing spaces",
			files: map[string]string{"/w/.gitignore": "# comment\n\n\\#hash\n\\!bang\na.txt   \n", "/w/#hash": "", "/w/!bang": "", "/w/a.txt": "", "/w/# comment": ""},
			want:  "# comment\n.gitignore"},
		{name: "bad lines dropped",
			files: map[string]string{"/w/.gitignore": "/\n[bad\n!\n*.log\n", "/w/a.log": "", "/w/[bad": ""},
			want:  ".gitignore\n[bad"},
		{name: "question and class",
			files: map[string]string{"/w/.gitignore": "?.o\n[xy].c\n", "/w/a.o": "", "/w/ab.o": "", "/w/x.c": "", "/w/z.c": ""},
			want:  ".gitignore\nab.o\nz.c"},
		{name: "nested gitignore applies below its dir",
			files: map[string]string{"/w/sub/.gitignore": "*.tmp\n", "/w/a.tmp": "", "/w/sub/b.tmp": "", "/w/sub/c": ""},
			want:  "a.tmp\nsub/\nsub/.gitignore\nsub/c"},
		{name: "nested negation overrides parent",
			files: map[string]string{"/w/.gitignore": "*.tmp\n", "/w/sub/.gitignore": "!keep.tmp\n", "/w/a.tmp": "", "/w/sub/keep.tmp": ""},
			want:  ".gitignore\nsub/\nsub/.gitignore\nsub/keep.tmp"},
		{name: "ancestor gitignore up to repo root",
			files: map[string]string{"/w/.gitignore": "*.log\n", "/w/sub/b.log": "", "/w/sub/c": ""},
			path:  "sub", want: "c"},
		{name: "ancestor anchored pattern relative to its dir",
			files: map[string]string{"/w/.gitignore": "/sub/x\n", "/w/sub/x": "", "/w/sub/y": ""},
			path:  "sub", want: "y"},
		{name: "no repo: ancestors not read",
			files: map[string]string{"/w/.gitignore": "*.log\n", "/w/sub/b.log": ""},
			path:  "sub", want: "b.log", noRepo: true},
		{name: "ancestors above repo root not read",
			files: map[string]string{"/.gitignore": "*.log\n", "/w/b.log": ""},
			want:  "b.log"},
	}
	for _, c := range cases {
		e := envtest.New("/w", "/h")
		if !c.noRepo {
			e.SetDir("/w/.git")
		}
		for p, content := range c.files {
			e.SetFile(p, content)
		}
		args := map[string]any{"pattern": "*", "respectIgnore": true}
		if c.path != "" {
			args["path"] = c.path
		}
		if got := wantOK(t, run(t, tools.Find(e), args)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// HN-FR-44: 기본(respectIgnore 없음)은 전부, .git 은 파일이어도 저장소 경계.
func TestRespectIgnoreDefaults(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/.git", "gitdir: elsewhere")
	e.SetFile("/w/.gitignore", "*.log\n")
	e.SetFile("/w/sub/a.log", "Foo")
	if got := wantOK(t, run(t, tools.Find(e), map[string]any{"pattern": "*.log"})); got != "sub/a.log" {
		t.Fatalf("default %q", got)
	}
	if got := wantOK(t, run(t, tools.Find(e), map[string]any{"pattern": ".git"})); got != ".git" {
		t.Fatalf(".git file is not skipped %q", got)
	}
	if got := wantOK(t, run(t, tools.Find(e), map[string]any{"pattern": "*.log", "path": "sub", "respectIgnore": true})); got != "No files found matching pattern" {
		t.Fatalf(".git file boundary %q", got)
	}
	if got := wantOK(t, run(t, tools.Grep(e), map[string]any{"pattern": "Foo", "respectIgnore": true})); got != "No matches found" {
		t.Fatalf("grep respectIgnore %q", got)
	}
	e.FailOn("/w/.gitignore", errors.New("unreadable"))
	if got := wantOK(t, run(t, tools.Find(e), map[string]any{"pattern": "*.log", "respectIgnore": true})); got != "sub/a.log" {
		t.Fatalf("unreadable gitignore %q", got)
	}
}

// HN-FR-46, 47
func TestGrep(t *testing.T) {
	e := tree()
	g := tools.Grep(e)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"pattern": "Foo"}, ".hidden:1: Foo hidden\na.go:2: func Foo() {}\nsub/b.go:2: Foo2"},
		{map[string]any{"pattern": "foo", "ignoreCase": true}, ".hidden:1: Foo hidden\na.go:2: func Foo() {}\na.go:3: // foo bar\nnotes.txt:1: FOO\nsub/b.go:2: Foo2"},
		{map[string]any{"pattern": "Foo()", "literal": true}, "a.go:2: func Foo() {}"},
		{map[string]any{"pattern": "Foo()"}, ".hidden:1: Foo hidden\na.go:2: func Foo() {}\nsub/b.go:2: Foo2"},
		{map[string]any{"pattern": "F.O", "literal": true, "ignoreCase": true}, "No matches found"},
		{map[string]any{"pattern": "foo", "ignoreCase": true, "glob": "*.txt"}, "notes.txt:1: FOO"},
		{map[string]any{"pattern": "Foo", "glob": "sub/*.go"}, "sub/b.go:2: Foo2"},
		{map[string]any{"pattern": "Foo2", "path": "sub", "context": 1}, "b.go-1- x\nb.go:2: Foo2\nb.go-3- y"},
		{map[string]any{"pattern": "^[xy]$", "path": "sub", "context": 1}, "b.go:1: x\nb.go-2- Foo2\nb.go-2- Foo2\nb.go:3: y\nb.go-4- z"},
		{map[string]any{"pattern": "x", "path": "sub", "context": -3}, "b.go:1: x"},
		{map[string]any{"pattern": "foo", "path": "a.go"}, "a.go:3: // foo bar"},
		{map[string]any{"pattern": "Foo", "path": "a.go", "glob": "*.txt", "respectIgnore": true}, "a.go:2: func Foo() {}"},
		{map[string]any{"pattern": "Foo", "path": "bin.dat"}, "No matches found"},
		{map[string]any{"pattern": "zzz"}, "No matches found"},
		{map[string]any{"pattern": "Foo", "limit": 1}, ".hidden:1: Foo hidden\n\n[1 matches limit reached. Use limit=2 for more, or refine pattern]"},
		{map[string]any{"pattern": "Foo", "limit": 3}, ".hidden:1: Foo hidden\na.go:2: func Foo() {}\nsub/b.go:2: Foo2"},
		{map[string]any{"pattern": "Foo", "limit": -1}, ".hidden:1: Foo hidden\n\n[1 matches limit reached. Use limit=2 for more, or refine pattern]"},
	}
	for _, c := range cases {
		if got := wantOK(t, run(t, g, c.args)); got != c.want {
			t.Errorf("%v:\n got %q\nwant %q", c.args, got, c.want)
		}
	}
	wantErr(t, run(t, g, map[string]any{"pattern": "("}), "Invalid regex: ")
	wantErr(t, run(t, g, map[string]any{"pattern": "x", "glob": "[a"}), "Invalid glob pattern: [a")
	wantErr(t, run(t, g, map[string]any{"pattern": "x", "path": "nope"}), "Path not found: nope")
}

// HN-FR-46, 47: 줄 처리와 절단.
func TestGrepLines(t *testing.T) {
	e := envtest.New("/w", "/h")
	g := tools.Grep(e)
	e.SetFile("/w/crlf.txt", "a\r\nFoo\r\nb")
	if got := wantOK(t, run(t, g, map[string]any{"pattern": "Foo$"})); got != "crlf.txt:2: Foo" {
		t.Fatalf("crlf %q", got)
	}
	e.SetFile("/w/crlf.txt", "")
	long := strings.Repeat("가", 600)
	e.SetFile("/w/long.txt", long)
	want := "long.txt:1: " + strings.Repeat("가", 500) + "... [truncated]\n\n[Some lines truncated to 500 chars. Use read tool to see full lines]"
	if got := wantOK(t, run(t, g, map[string]any{"pattern": "가"})); got != want {
		t.Fatalf("long line %q", got)
	}
	e.SetFile("/w/long.txt", "")
	var b strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&b, "match %03d %s\n", i, strings.Repeat("x", 90))
	}
	e.SetFile("/w/many.txt", b.String())
	got := wantOK(t, run(t, g, map[string]any{"pattern": "match", "limit": 1000}))
	if !strings.HasSuffix(got, "\n\n[50.0KB limit reached]") || len(got) > 50*1024+100 || !strings.HasPrefix(got, "many.txt:1: match 000") {
		t.Fatalf("50KB: len %d tail %q", len(got), got[len(got)-60:])
	}
	got = wantOK(t, run(t, g, map[string]any{"pattern": "match", "limit": 999}))
	if !strings.HasSuffix(got, "\n\n[999 matches limit reached. Use limit=1998 for more, or refine pattern. 50.0KB limit reached]") {
		t.Fatalf("both notices %q", got[len(got)-120:])
	}
	e.SetFile("/w/nul.txt", strings.Repeat("a", 8000)+"\x00 match")
	if got := wantOK(t, run(t, g, map[string]any{"pattern": "match", "path": "nul.txt"})); got != "nul.txt:1: "+strings.Repeat("a", 500)+"... [truncated]\n\n[Some lines truncated to 500 chars. Use read tool to see full lines]" {
		t.Fatalf("NUL after 8000 bytes is text: %q", got[:40])
	}
	e.FailOn("/w/many.txt", errors.New("unreadable"))
	if got := wantOK(t, run(t, g, map[string]any{"pattern": "match", "glob": "many.txt"})); got != "No matches found" {
		t.Fatalf("unreadable file skipped %q", got)
	}
}

// HN-FR-49
func TestLs(t *testing.T) {
	e := envtest.New("/w", "/h")
	l := tools.Ls(e)
	e.SetFile("/w/B.txt", "")
	e.SetFile("/w/a.txt", "")
	e.SetDir("/w/c")
	e.SetDir("/w/.git")
	e.SetFile("/w/b.txt", "")
	e.SetDir("/w/empty")
	e.SetFile("/h/n", "")
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, ".git/\na.txt\nB.txt\nb.txt\nc/\nempty/"},
		{map[string]any{"path": ""}, ".git/\na.txt\nB.txt\nb.txt\nc/\nempty/"},
		{map[string]any{"limit": 2}, ".git/\na.txt\n\n[2 entries limit reached. Use limit=4 for more]"},
		{map[string]any{"limit": 6}, ".git/\na.txt\nB.txt\nb.txt\nc/\nempty/"},
		{map[string]any{"limit": 0}, ".git/\n\n[1 entries limit reached. Use limit=2 for more]"},
		{map[string]any{"path": "empty"}, "(empty directory)"},
		{map[string]any{"path": "~"}, "n"},
	}
	for _, c := range cases {
		if got := wantOK(t, run(t, l, c.args)); got != c.want {
			t.Errorf("%v:\n got %q\nwant %q", c.args, got, c.want)
		}
	}
	wantErr(t, run(t, l, map[string]any{"path": "a.txt"}), "Not a directory: a.txt")
	wantErr(t, run(t, l, map[string]any{"path": "zz"}), "Path not found: zz")
	wantErr(t, run(t, tools.Ls(listFail{e, "/w/c"}), map[string]any{"path": "c"}), "Cannot read directory: boom")
	for i := range 600 {
		e.SetFile(fmt.Sprintf("/w/big/%03d-%s", i, strings.Repeat("y", 100)), "")
	}
	if got := wantOK(t, run(t, l, map[string]any{"path": "big", "limit": 600})); !strings.HasSuffix(got, "\n\n[50.0KB limit reached]") || len(got) > 50*1024+100 {
		t.Fatalf("50KB len %d", len(got))
	}
}

// HN-FR-06: 잘못된 인자·경로는 error 결과.
func TestSearchBadArgs(t *testing.T) {
	e := envtest.New("/w", "")
	for _, tl := range []sets.Tool{tools.Grep(e), tools.Find(e), tools.Ls(e)} {
		r := tl.Run(context.Background(), contract.ToolCall{ID: "c", Name: tl.Spec.Name, Args: json.RawMessage(`{"path":1}`)})
		if r.Status != contract.StatusError || !strings.Contains(text(r), "invalid arguments") {
			t.Errorf("%s: %s %q", tl.Spec.Name, r.Status, text(r))
		}
		wantErr(t, run(t, tl, map[string]any{"pattern": "x", "path": "~/a"}), "home directory unknown")
	}
}

// HN-FR-49: 50KB 경계 — 정확히 50KB 면 자르지 않는다.
func TestLsByteBoundary(t *testing.T) {
	for _, c := range []struct {
		last int
		cut  bool
	}{{512, false}, {513, true}} {
		e := envtest.New("/w", "")
		for i := range 100 {
			n := 508
			if i == 99 {
				n = c.last - 3
			}
			e.SetFile(fmt.Sprintf("/w/d/%03d%s", i, strings.Repeat("y", n)), "")
		}
		got := wantOK(t, run(t, tools.Ls(e), map[string]any{"path": "d"}))
		if cut := strings.HasSuffix(got, "[50.0KB limit reached]"); cut != c.cut || (!cut && len(got) != 50*1024) {
			t.Errorf("last %d: cut %v len %d", c.last, cut, len(got))
		}
	}
}
