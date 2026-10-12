package terminal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// TM-FR-55: off 줄은 Color 면 흐리고, 줄은 폭으로 자른다
func TestSetLines(t *testing.T) {
	term := New(Options{Width: func() int { return 16 }, Color: true})
	got := term.setLines([]harness.SetState{{Name: "base", Description: "io", On: true}, {Name: "guard", Description: "checks tools", On: false}})
	want := []string{"1. on base  io", "\x1b[2m2. off guard  c…\x1b[0m", "  (다음 run 부터 적용 · /set <name> on|off|to <n>)"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("%q", got)
	}
}

// TM-FR-55, 56: /sets 목록, /set on·off·to, 저장, 실행 중 거부, 형식·이름 오류, harness 거부
func TestRunSets(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, ">") })
	h.typ("/sets\r")
	h.wait("1. on base  session load/save, input, edits, tool execution\n2. on require  request, model call")
	h.wait("4. on guard  reject unknown tools")
	h.wait("6. on resilient  retry, overflow recovery, threshold compaction, follow-up\n7. on repeat  stop repeating the same tool calls turn after turn\n  (다음 run 부터 적용 · /set <name> on|off|to <n>)")

	h.typ("/set guard off\r")
	h.wait("── 세트 guard 끔 (다음 run 부터, 저장됨)")
	h.typ("/sets\r")
	h.wait("4. off guard  ")
	b, err := os.ReadFile(filepath.Join(e.home, ".dynamic_react", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Sets []struct {
			Name string
			On   bool
		}
	}
	if err := json.Unmarshal(b, &cfg); err != nil || len(cfg.Sets) != 7 || cfg.Sets[3].Name != "guard" || cfg.Sets[3].On {
		t.Fatalf("saved %s", b)
	}
	h.typ("/set guard to 6\r")
	h.wait("── 세트 guard → 6번 (다음 run 부터, 저장됨)")
	h.typ("/set guard on\r")
	h.wait("── 세트 guard 켬 (다음 run 부터, 저장됨)")
	h.typ("/sets\r")
	h.wait("5. on resilient  retry")
	h.wait("6. on guard  reject")

	h.typ("/set ghost on\r")
	h.wait("✗ 없는 세트: ghost (/sets)")
	const usage = "✗ 사용법: /set <name> on|off|to <n>"
	for i, bad := range []string{"/set", "/set guard", "/set guard maybe", "/set guard on x", "/set guard to", "/set guard to x", "/set guard to 0", "/set guard to 8", "/set guard to 1 x"} {
		h.typ(bad + "\r")
		h.waitCount(usage, i+1)
	}
	h.typ("/set require off\r")
	h.wait("✗ agent: harness: sets:")
	h.typ("/sets\r")
	h.waitCount("2. on require  ", 3)

	gate := l.block()
	h.typ("hi\r")
	h.wait("● 실행 중")
	h.typ("/set guard off\r")
	h.wait("✗ 실행 중에는 세트를 바꿀 수 없습니다")
	h.typ("/sets\r")
	h.waitCount("6. on guard  ", 2)
	gate <- struct{}{}
	h.wait("── completed")
	h.typ("/help\r")
	h.wait("/sets · /set <name> on|off|to <n>")
}
