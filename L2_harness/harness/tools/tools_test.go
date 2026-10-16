package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/tools"
)

func run(t *testing.T, tl sets.Tool, args any) contract.ToolResult {
	t.Helper()
	b, _ := json.Marshal(args)
	if err := sets.CheckArgs(tl.Spec.InputSchema, b); err != nil {
		t.Fatalf("args rejected by schema: %v", err)
	}
	r := tl.Run(context.Background(), contract.ToolCall{ID: "c", Name: tl.Spec.Name, Args: b})
	if r.CallID != "c" {
		t.Fatalf("call id %q", r.CallID)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("invalid result: %v", err)
	}
	return r
}

func text(r contract.ToolResult) string {
	var out []string
	for _, b := range r.Content {
		if b.Kind == contract.BlockText {
			out = append(out, b.Text)
		}
	}
	return strings.Join(out, "\n")
}

func wantOK(t *testing.T, r contract.ToolResult) string {
	t.Helper()
	if r.Status != contract.StatusOK {
		t.Fatalf("status %s: %s", r.Status, text(r))
	}
	return text(r)
}

func wantErr(t *testing.T, r contract.ToolResult, sub string) {
	t.Helper()
	if r.Status != contract.StatusError || !strings.Contains(text(r), sub) {
		t.Fatalf("want error containing %q, got %s: %s", sub, r.Status, text(r))
	}
}

func lines(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	return b.String()
}

// HN-FR-07, 08, 09
func TestAll(t *testing.T) {
	all := tools.All(envtest.New("/w", "/h"))
	var names []string
	for _, tl := range all {
		names = append(names, tl.Spec.Name)
		var s struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(tl.Spec.InputSchema, &s); err != nil || s.Type != "object" || tl.Spec.Validate() != nil {
			t.Errorf("%s schema %s", tl.Spec.Name, tl.Spec.InputSchema)
		}
		if tl.Spec.Description == "" || tools.Snippet(tl.Spec.Name) == "" || tl.Run == nil {
			t.Errorf("%s description/snippet/run", tl.Spec.Name)
		}
	}
	if strings.Join(names, ",") != "read,bash,edit,write,grep,find,ls" {
		t.Fatalf("order %v", names)
	}
	r := contract.ToolResult{CallID: "c", Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockText, Text: strings.Repeat("x\n", 5000)}}}
	for _, tl := range all {
		mutating := tl.Spec.Name == "edit" || tl.Spec.Name == "write"
		if mutating && tl.Truncate != nil {
			t.Errorf("%s Truncate must be nil", tl.Spec.Name)
		}
		if !mutating && (tl.Truncate == nil || text(tl.Truncate(r)) != text(r)) {
			t.Errorf("%s Truncate must be identity", tl.Spec.Name)
		}
	}
	if tools.Snippet("unknown") != "" {
		t.Fatal("unknown snippet")
	}
}

// HN-FR-05
func TestPaths(t *testing.T) {
	e := envtest.New("/w", "/h")
	w := tools.Write(e)
	if got := wantOK(t, run(t, w, map[string]string{"path": "sub/a.txt", "content": "x"})); !strings.Contains(got, "sub/a.txt") || strings.Contains(got, "/w/") {
		t.Fatalf("relative display %q", got)
	}
	if _, ok := e.File("/w/sub/a.txt"); !ok {
		t.Fatal("relative not under cwd")
	}
	wantOK(t, run(t, w, map[string]string{"path": "~/n.txt", "content": "x"}))
	if _, ok := e.File("/h/n.txt"); !ok {
		t.Fatal("~ not home")
	}
	if got := wantOK(t, run(t, w, map[string]string{"path": "../o.txt", "content": "x"})); !strings.Contains(got, "/o.txt") {
		t.Fatalf("outside display %q", got)
	}
	if got := wantOK(t, run(t, w, map[string]string{"path": "/w/./p/../q.txt", "content": "x"})); !strings.Contains(got, "q.txt") {
		t.Fatalf("clean %q", got)
	}
	if _, ok := e.File("/w/q.txt"); !ok {
		t.Fatal("absolute not cleaned")
	}
	noHome := tools.Write(envtest.New("/w", ""))
	wantErr(t, run(t, noHome, map[string]string{"path": "~/n.txt", "content": "x"}), "home")
	wantErr(t, run(t, w, map[string]string{"path": "", "content": "x"}), "path")
}

// 3.2 read
func TestRead(t *testing.T) {
	e := envtest.New("/w", "/h")
	r := tools.Read(e)
	e.SetFile("/w/s.txt", "l1\nl2")
	if got := wantOK(t, run(t, r, map[string]any{"path": "s.txt"})); got != "l1\nl2" {
		t.Fatalf("simple %q", got)
	}
	e.SetFile("/w/ten.txt", strings.TrimSuffix(lines(1, 10), "\n"))
	if got := wantOK(t, run(t, r, map[string]any{"path": "ten.txt", "offset": 3, "limit": 2})); got != "line3\nline4\n\n[6 more lines in file. Use offset=5 to continue.]" {
		t.Fatalf("offset/limit %q", got)
	}
	if got := wantOK(t, run(t, r, map[string]any{"path": "ten.txt", "offset": 9})); got != "line9\nline10" {
		t.Fatalf("offset to end %q", got)
	}
	wantErr(t, run(t, r, map[string]any{"path": "ten.txt", "offset": 11}), "beyond end of file")

	e.SetFile("/w/big.txt", strings.TrimSuffix(lines(1, 3000), "\n"))
	got := wantOK(t, run(t, r, map[string]any{"path": "big.txt"}))
	if !strings.HasPrefix(got, "line1\n") || !strings.Contains(got, "line2000\n\n[Showing lines 1-2000 of 3000. Use offset=2001 to continue.]") || strings.Contains(got, "line2001") {
		t.Fatalf("line limit tail %q", got[len(got)-120:])
	}

	row := strings.Repeat("x", 99)
	e.SetFile("/w/wide.txt", strings.Repeat(row+"\n", 1000))
	got = wantOK(t, run(t, r, map[string]any{"path": "wide.txt"}))
	if !strings.Contains(got, "(50.0KB limit). Use offset=513 to continue.]") || strings.Count(got, row) != 512 {
		t.Fatalf("byte limit rows=%d tail %q", strings.Count(got, row), got[len(got)-100:])
	}

	e.SetFile("/w/huge.txt", strings.Repeat("y", 60*1024))
	if got := wantOK(t, run(t, r, map[string]any{"path": "huge.txt"})); !strings.HasPrefix(got, "[Line 1 is 60.0KB, exceeds 50.0KB limit.") {
		t.Fatalf("first line %q", got)
	}

	e.SetFile("/w/empty.txt", "")
	if got := wantOK(t, run(t, r, map[string]any{"path": "empty.txt"})); got != "" {
		t.Fatalf("empty %q", got)
	}

	png := "\x89PNG\r\n\x1a\n" + "rest"
	e.SetFile("/w/pic.dat", png)
	res := run(t, r, map[string]any{"path": "pic.dat"})
	if res.Status != contract.StatusOK || len(res.Content) != 2 || res.Content[1].Kind != contract.BlockImage ||
		res.Content[1].Image.MediaType != "image/png" || string(res.Content[1].Image.Data) != png || !strings.Contains(res.Content[0].Text, "image/png") {
		t.Fatalf("image %+v", res)
	}
	for sig, mt := range map[string]string{"\xff\xd8\xff\xe0jfif": "image/jpeg", "GIF89a...": "image/gif", "RIFF\x00\x00\x00\x00WEBPVP8 ": "image/webp"} { //nolint:gocritic // "VP8 " 의 공백은 WebP 형식의 일부다
		e.SetFile("/w/img", sig)
		if res := run(t, r, map[string]any{"path": "img"}); len(res.Content) != 2 || res.Content[1].Image.MediaType != mt {
			t.Errorf("%s not detected", mt)
		}
	}

	wantErr(t, run(t, r, map[string]any{"path": "nope.txt"}), "not found")
	e.FailOn("/w/s.txt", errors.New("disk fault"))
	wantErr(t, run(t, r, map[string]any{"path": "s.txt"}), "disk fault")
}

// 3.2 write
func TestWrite(t *testing.T) {
	e := envtest.New("/w", "/h")
	w := tools.Write(e)
	if got := wantOK(t, run(t, w, map[string]string{"path": "d/e/f.txt", "content": "hello"})); got != "Successfully wrote 5 bytes to d/e/f.txt" {
		t.Fatalf("result %q", got)
	}
	if c, _ := e.File("/w/d/e/f.txt"); c != "hello" {
		t.Fatalf("content %q", c)
	}
	e.FailOn("/w/x.txt", errors.New("read-only"))
	wantErr(t, run(t, w, map[string]string{"path": "x.txt", "content": "y"}), "read-only")
}

type edit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

func doEdit(t *testing.T, e *envtest.Env, file string, edits ...edit) contract.ToolResult {
	t.Helper()
	return run(t, tools.Edit(e), map[string]any{"path": file, "edits": edits})
}

// 3.2 edit
func TestEdit(t *testing.T) {
	e := envtest.New("/w", "/h")
	e.SetFile("/w/a.txt", "a\nb\nc\n")
	got := wantOK(t, doEdit(t, e, "a.txt", edit{"b", "B"}))
	wantDiff := "--- a.txt\n+++ a.txt\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	if !strings.HasPrefix(got, "Successfully replaced 1 block(s) in a.txt.") || !strings.HasSuffix(got, wantDiff) {
		t.Fatalf("result %q", got)
	}
	if c, _ := e.File("/w/a.txt"); c != "a\nB\nc\n" {
		t.Fatalf("content %q", c)
	}

	e.SetFile("/w/m.txt", "one two three")
	wantOK(t, doEdit(t, e, "m.txt", edit{"three", "3"}, edit{"one", "1"}))
	if c, _ := e.File("/w/m.txt"); c != "1 two 3" {
		t.Fatalf("multi %q", c)
	}

	e.SetFile("/w/d.txt", "x y x")
	wantErr(t, doEdit(t, e, "d.txt", edit{"zzz", "q"}), "Could not find")
	wantErr(t, doEdit(t, e, "d.txt", edit{"x", "q"}), "2 occurrences")
	wantErr(t, doEdit(t, e, "d.txt", edit{"x y", "q"}, edit{"y x", "r"}), "overlap")
	wantErr(t, doEdit(t, e, "d.txt", edit{"y", "y"}), "No changes")
	wantErr(t, doEdit(t, e, "d.txt", edit{"", "q"}), "must not be empty")
	wantErr(t, doEdit(t, e, "none.txt", edit{"a", "b"}), "not found")
	if c, _ := e.File("/w/d.txt"); c != "x y x" {
		t.Fatalf("failed edit wrote %q", c)
	}

	e.SetFile("/w/crlf.txt", "\ufeffa\r\nb\r\n")
	wantOK(t, doEdit(t, e, "crlf.txt", edit{"a\nb", "x\ny"}))
	if c, _ := e.File("/w/crlf.txt"); c != "\ufeffx\r\ny\r\n" {
		t.Fatalf("crlf/bom %q", c)
	}
}

// edit fuzzy 정규화 (pi normalizeForFuzzyMatch)
func TestEditFuzzy(t *testing.T) {
	cases := []struct {
		name, file, old, new, want string
	}{
		{"smart quotes, other lines kept", "keep “q”\nsay “hi”  \n", `say "hi"`, "bye", "keep “q”\nbye\n"},
		{"trailing spaces", "a  \nb\t\n", "a\nb", "c", "c\n"},
		{"dashes", "x — y\n", "x - y", "z", "z\n"},
		{"special spaces", "p q\n", "p q", "r", "r\n"},
		{"single quotes", "it’s\n", "it's", "ok", "ok\n"},
		{"NFKC fullwidth", "f（x）：１\n", "f(x):1", "g", "g\n"},
		{"NFKC ligature", "\ufb01le\n", "file", "doc", "doc\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := envtest.New("/w", "/h")
			e.SetFile("/w/f.txt", c.file)
			wantOK(t, doEdit(t, e, "f.txt", edit{c.old, c.new}))
			if got, _ := e.File("/w/f.txt"); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

// HN-FR-10: 같은 파일의 동시 edit 는 직렬화된다.
func TestMutationQueue(t *testing.T) {
	e := &slowEnv{Env: envtest.New("/w", "/h")}
	e.SetFile("/w/q.txt", "A\nB\n")
	all := tools.All(e)
	ed := all[2]
	var wg sync.WaitGroup
	for _, ch := range []edit{{"A", "a"}, {"B", "b"}} {
		wg.Add(1)
		go func(ch edit) {
			defer wg.Done()
			args, _ := json.Marshal(map[string]any{"path": "q.txt", "edits": []edit{ch}})
			ed.Run(context.Background(), contract.ToolCall{ID: "c", Name: "edit", Args: args})
		}(ch)
	}
	wg.Wait()
	if c, _ := e.File("/w/q.txt"); c != "a\nb\n" {
		t.Fatalf("lost update %q", c)
	}
}

type slowEnv struct{ *envtest.Env }

func (s *slowEnv) ReadFile(ctx context.Context, p string) ([]byte, error) {
	b, err := s.Env.ReadFile(ctx, p)
	time.Sleep(30 * time.Millisecond)
	return b, err
}

func script(e *envtest.Env, output string, res env.ExecResult, err error) {
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		io.WriteString(out, output)
		return res, err
	})
}

// 3.2 bash
func TestBash(t *testing.T) {
	e := envtest.New("/w", "/h")
	b := tools.Bash(e)
	script(e, "hello\n", env.ExecResult{}, nil)
	if got := wantOK(t, run(t, b, map[string]any{"command": "echo hello", "timeout": 2})); got != "hello\n" {
		t.Fatalf("output %q", got)
	}
	if req := e.ExecRequests()[0]; req.Command != "echo hello" || req.Dir != "/w" || req.Timeout != 2*time.Second {
		t.Fatalf("request %+v", req)
	}
	script(e, "", env.ExecResult{}, nil)
	if got := wantOK(t, run(t, b, map[string]any{"command": "true"})); got != "(no output)" {
		t.Fatalf("empty %q", got)
	}
	script(e, "oops\n", env.ExecResult{ExitCode: 2}, nil)
	wantErr(t, run(t, b, map[string]any{"command": "false"}), "oops\n\n\nCommand exited with code 2")
	script(e, "", env.ExecResult{TimedOut: true}, nil)
	wantErr(t, run(t, b, map[string]any{"command": "sleep 9", "timeout": 1}), "Command timed out after 1 seconds")
	script(e, "", env.ExecResult{}, errors.New("no shell"))
	wantErr(t, run(t, b, map[string]any{"command": "x"}), "no shell")
	wantErr(t, run(t, b, map[string]any{"command": "x", "timeout": 0}), "timeout")
	wantErr(t, run(t, b, map[string]any{"command": "x", "timeout": -1}), "timeout")
}

func TestBashTruncation(t *testing.T) {
	e := envtest.New("/w", "/h")
	b := tools.Bash(e)
	full := lines(1, 3000)
	script(e, full, env.ExecResult{}, nil)
	got := wantOK(t, run(t, b, map[string]any{"command": "seq"}))
	if !strings.HasPrefix(got, "line1001\n") || strings.Contains(got, "line1000\n") || !strings.Contains(got, "[Showing lines 1001-3000 of 3000. Full output: /tmp/") {
		t.Fatalf("line tail %q", got[:40]+" ... "+got[len(got)-80:])
	}
	path := got[strings.LastIndex(got, "Full output: ")+len("Full output: ") : len(got)-1]
	if c, _ := e.File(path); c != full {
		t.Fatalf("spill %q len %d", path, len(c))
	}

	row := strings.Repeat("z", 99) + "\n"
	big := strings.Repeat(row, 2000) // 200KB, 2000 lines
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		for i := 0; i < 2000; i++ {
			io.WriteString(out, row) // 조각 단위 쓰기
		}
		return env.ExecResult{}, nil
	})
	got = wantOK(t, run(t, b, map[string]any{"command": "big"}))
	if strings.Count(got, row) != 512 || !strings.Contains(got, "[Showing lines 1489-2000 of 2000 (50.0KB limit). Full output: /tmp/") {
		t.Fatalf("byte tail rows=%d %q", strings.Count(got, row), got[len(got)-100:])
	}
	path = got[strings.LastIndex(got, "Full output: ")+len("Full output: ") : len(got)-1]
	if c, _ := e.File(path); c != big {
		t.Fatalf("spill len %d", len(c))
	}
}

// bash: 한 줄이 50KB 를 넘으면 그 줄의 끝부분만
func TestBashPartialLine(t *testing.T) {
	e := envtest.New("/w", "/h")
	script(e, strings.Repeat("q", 60*1024), env.ExecResult{}, nil)
	got := wantOK(t, run(t, tools.Bash(e), map[string]any{"command": "x"}))
	if !strings.HasPrefix(got, strings.Repeat("q", 50*1024)+"\n\n[Showing last 50.0KB of line 1 (line is 60.0KB). Full output: /tmp/") {
		t.Fatalf("partial %q", got[len(got)-120:])
	}
}

// edit diff: 떨어진 변경은 hunk 를 나눈다
func TestEditDiffHunks(t *testing.T) {
	e := envtest.New("/w", "/h")
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	e.SetFile("/w/h.txt", b.String())
	got := wantOK(t, doEdit(t, e, "h.txt", edit{"l2\n", "X\n"}, edit{"l18\n", "Y1\nY2\n"}))
	want := "--- h.txt\n+++ h.txt\n" +
		"@@ -1,6 +1,6 @@\n l1\n-l2\n+X\n l3\n l4\n l5\n l6\n" +
		"@@ -14,7 +14,8 @@\n l14\n l15\n l16\n l17\n-l18\n+Y1\n+Y2\n l19\n l20\n"
	if !strings.HasSuffix(got, want) {
		t.Fatalf("diff:\n%s", got)
	}
}

// HN-FR-33: 출력 조각을 UTF-8 경계에서 progress 로 흘린다.
func TestBashProgress(t *testing.T) {
	e := envtest.New("/w", "/h")
	parts := []string{"ab", "c\xea\xb0", "\x80d\n", "\xea"}
	e.SetScript(func(ctx context.Context, req env.ExecRequest, out io.Writer) (env.ExecResult, error) {
		for _, p := range parts {
			io.WriteString(out, p)
		}
		return env.ExecResult{}, nil
	})
	var got []string
	var ids []string
	ctx := tools.WithProgress(context.Background(), func(callID, chunk string) {
		ids = append(ids, callID)
		got = append(got, chunk)
	})
	b := tools.Bash(e)
	b.Run(ctx, contract.ToolCall{ID: "c9", Name: "bash", Args: json.RawMessage(`{"command":"x"}`)})
	if strings.Join(got, "") != strings.Join(parts, "") {
		t.Fatalf("chunks %q", got)
	}
	for i, c := range got[:len(got)-1] {
		if !utf8.ValidString(c) || c == "" {
			t.Errorf("chunk %d %q splits a rune", i, c)
		}
	}
	if got[1] != "c" || got[2] != "가d\n" || got[len(got)-1] != "\xea" || ids[0] != "c9" {
		t.Fatalf("chunks %q ids %v", got, ids)
	}
	got = nil
	b.Run(context.Background(), contract.ToolCall{ID: "c", Name: "bash", Args: json.RawMessage(`{"command":"x"}`)})
	if got != nil {
		t.Fatal("progress without WithProgress")
	}
}
