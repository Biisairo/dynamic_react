package terminal

import (
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func prefixEv(pb harness.PrefixBreak) harness.Event {
	return harness.Event{Kind: harness.EventPrefixBreak, Prefix: &pb}
}

// TM-FR-60: Part 네 종류의 문구, Expected 면 줄 없음, 폭으로 자름, 색.
func TestRenderPrefixBreak(t *testing.T) {
	term, v := newTestTerminal(80, false)
	emit(term,
		prefixEv(harness.PrefixBreak{Part: harness.PrefixTools, At: -1, Total: 12}),
		prefixEv(harness.PrefixBreak{Part: harness.PrefixSystem, At: -1, Total: 3}),
		prefixEv(harness.PrefixBreak{Part: harness.PrefixMessages, At: 5, Kept: 5, Total: 12}),
		prefixEv(harness.PrefixBreak{Part: harness.PrefixMessages, At: 0, Total: 12, Expected: true,
			Edits: []contract.EditKind{contract.EditReplaceRange}}),
		prefixEv(harness.PrefixBreak{Part: harness.PrefixModel, At: -1, Model: contract.ModelRef{Provider: "openai", ID: "gpt-x"}}),
	)
	want := "⚠ 캐시 접두 깨짐: tools 변경 (0/12 메시지 유지)\n" +
		"⚠ 캐시 접두 깨짐: system 변경 (0/3 메시지 유지)\n" +
		"⚠ 캐시 접두 깨짐: 메시지 #5 변경, 편집 없음 (5/12 유지)\n" +
		"· 캐시 새로 시작: 모델 변경 → openai/gpt-x"
	if body(v) != want {
		t.Fatalf("%q\nwant %q", body(v), want)
	}

	term, v = newTestTerminal(20, false)
	emit(term, prefixEv(harness.PrefixBreak{Part: harness.PrefixTools, At: -1, Total: 12}))
	if got := body(v); !strings.HasPrefix(got, "⚠ 캐시") || !strings.HasSuffix(got, "…") || displayWidth(got) > 20 {
		t.Fatalf("narrow %q", got)
	}

	term, v = newTestTerminal(80, true)
	emit(term,
		prefixEv(harness.PrefixBreak{Part: harness.PrefixTools, At: -1, Total: 1}),
		prefixEv(harness.PrefixBreak{Part: harness.PrefixModel, At: -1, Model: contract.ModelRef{Provider: "p", ID: "m"}}))
	if v.sgr < 4 {
		t.Fatalf("sgr %d", v.sgr)
	}
}
