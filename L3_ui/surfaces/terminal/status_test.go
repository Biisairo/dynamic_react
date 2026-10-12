package terminal

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func baseStatus() statusInfo {
	return statusInfo{sid: "s1", model: "p/m", mode: "plan",
		sets:  []harness.SetState{{Name: "base", On: true}, {Name: "guard", On: false}, {Name: "plan", On: true}},
		tools: []string{"read", "bash"}}
}

// TM-FR-61, 3.8a: 줄 형식과 경우별 표시.
func TestStatusLines(t *testing.T) {
	term := New(Options{Width: func() int { return 200 }})
	in := baseStatus()
	got := strings.Join(term.statusLines(in), "\n")
	want := "── 상태\n   세션 s1 · 모델 p/m · 모드 plan\n   세트 base plan\n   도구 read bash\n   기록 없음"
	if got != want {
		t.Fatalf("no stats %q", got)
	}

	in.ok = true
	in.stats = harness.Stats{Model: contract.ModelRef{Provider: "p", ID: "m"}, Context: 3600, Window: 128000, Threshold: 111616,
		LastRun: harness.UsageTotals{Requests: 5, Prompt: 12100, CacheRead: 10400, Ceiling: 10800, CeilingRead: 9800},
		Session: harness.UsageTotals{Requests: 1, Prompt: 2000, CacheRead: 0}}
	got = strings.Join(term.statusLines(in), "\n")
	want = "── 상태\n   세션 s1 · 모델 p/m · 모드 plan\n   세트 base plan\n   도구 read bash\n" +
		"   컨텍스트 3.6k / 128.0k (3%) · 압축 기준 111.6k\n" +
		"   마지막 run 요청 5 · 입력 12.1k · 캐시 10.4k (86%) · 효율 91%\n" +
		"   세션 누적 요청 1 · 입력 2.0k · 캐시 0 (0%) · 효율 -\n" +
		"   (효율 = 캐시 ÷ 직전 요청 크기, 같은 모델 기준 근삿값)"
	if got != want {
		t.Fatalf("stats\n%s\nwant\n%s", got, want)
	}

	in.stats.Model.ID = "fallback"
	in.stats.Threshold = 0
	in.stats.Session.Prompt = 0
	lines := term.statusLines(in)
	if lines[1] != "   세션 s1 · 모델 p/m (마지막 요청 p/fallback) · 모드 plan" || lines[4] != "   컨텍스트 3.6k / 128.0k (3%) · 압축 꺼짐" ||
		lines[6] != "   세션 누적 요청 1 · 입력 0 · 캐시 0 (-) · 효율 -" {
		t.Fatalf("variants %q", lines)
	}
	in.stats.Window = 0
	if lines = term.statusLines(in); lines[4] != "   컨텍스트 3.6k / 창 크기 모름" {
		t.Fatalf("no window %q", lines[4])
	}
	none := statusInfo{mode: "기본"}
	if lines = term.statusLines(none); lines[1] != "   세션 없음 · 모델 없음 · 모드 기본" || lines[len(lines)-1] != "   기록 없음" {
		t.Fatalf("empty %q", lines)
	}

	narrow := New(Options{Width: func() int { return 12 }, Color: true})
	lines = narrow.statusLines(in)
	if displayWidth(lines[1]) > 12 || !strings.HasPrefix(lines[len(lines)-1], "\x1b[2m") {
		t.Fatalf("narrow/color %q", lines)
	}
}

func usageReply(s string, prompt, cached, completion int) string {
	return sse(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"`+s+`"},"finish_reason":"stop"}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":`+strconv.Itoa(prompt)+`,"completion_tokens":`+strconv.Itoa(completion)+
			`,"prompt_tokens_details":{"cached_tokens":`+strconv.Itoa(cached)+`}}}`)
}

// TM-FR-32, 61: run_end 의 캐시 비율, /status 종단 (세션 전 → 기록 없음, 두 run 뒤 값), /help.
func TestRunStatus(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, usageReply("one", 2000, 0, 10), usageReply("two", 2100, 1920, 10))
	h := e.start(t, context.Background(), config(l.URL, "", ""), Start{})
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, ">") })
	h.typ("/status\r")
	h.wait("── 상태\n   세션 없음 · 모델 test/m · 모드 기본\n   세트 base require cache guard truncate resilient\n   도구 read bash edit write grep find ls\n   기록 없음")
	h.typ("hi\r")
	h.wait("입력 2.0k (캐시 0%) / 출력 10 토큰")
	h.typ("again\r")
	h.wait("입력 2.1k (캐시 91%) / 출력 10 토큰")
	h.typ("/status\r")
	h.wait("   컨텍스트 2.1k / 100.0k (2%) · 압축 기준 83.6k\n" +
		"   마지막 run 요청 1 · 입력 2.1k · 캐시 1.9k (91%) · 효율 96%\n" +
		"   세션 누적 요청 2 · 입력 4.1k · 캐시 1.9k (47%) · 효율 96%")
	h.typ("/help\r")
	h.wait("/status")
}
