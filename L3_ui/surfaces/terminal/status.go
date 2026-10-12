package terminal

import (
	"fmt"
	"strings"

	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// statusInfo 는 /status 에 그릴 값이다 (TM-FR-61).
type statusInfo struct {
	sid, model, mode string
	sets             []harness.SetState
	tools            []string
	stats            harness.Stats
	ok               bool
}

// showStatus 는 TM-FR-61 이다.
func (t *Terminal) showStatus() {
	in := statusInfo{sid: t.sid, mode: "기본", sets: t.a.Sets(), tools: t.a.ToolNames()}
	in.model, _ = t.a.Model()
	if t.sid != "" {
		if st, err := t.a.ModeStatus(t.ctx, t.sid); err == nil && st.Active != "" {
			in.mode = st.Active
		}
		in.stats, in.ok = t.a.Stats(t.sid)
	}
	t.update(func() {
		for _, l := range t.statusLines(in) {
			t.line(l)
		}
	})
}

// statusLines 는 3.8a 의 줄들이다.
func (t *Terminal) statusLines(in statusInfo) []string {
	orNone := func(s string) string {
		if s == "" {
			return "없음"
		}
		return sanitize(s)
	}
	model := orNone(in.model)
	if ref := in.stats.Model.Provider + "/" + in.stats.Model.ID; in.ok && ref != in.model {
		model += " (마지막 요청 " + sanitize(ref) + ")"
	}
	var on []string
	for _, s := range in.sets {
		if s.On {
			on = append(on, sanitize(s.Name))
		}
	}
	lines := []string{
		"── 상태",
		fmt.Sprintf("   세션 %s · 모델 %s · 모드 %s", orNone(in.sid), model, sanitize(in.mode)),
		"   세트 " + strings.Join(on, " "),
		"   도구 " + sanitize(strings.Join(in.tools, " ")),
	}
	if !in.ok {
		lines = append(lines, "   기록 없음")
	} else {
		st := in.stats
		ctxLine := fmt.Sprintf("   컨텍스트 %s / 창 크기 모름", formatTokens(st.Context))
		if st.Window > 0 {
			ctxLine = fmt.Sprintf("   컨텍스트 %s / %s (%s) · 압축 꺼짐", formatTokens(st.Context), formatTokens(st.Window), percent(st.Context, st.Window))
			if st.Threshold > 0 {
				ctxLine = strings.TrimSuffix(ctxLine, "압축 꺼짐") + "압축 기준 " + formatTokens(st.Threshold)
			}
		}
		lines = append(lines, ctxLine, totalsLine("마지막 run", st.LastRun), totalsLine("세션 누적", st.Session))
	}
	for i, l := range lines {
		lines[i] = truncate(l, t.scr.cols())
	}
	if in.ok {
		lines = append(lines, t.paint(sgrDim, truncate("   (효율 = 캐시 ÷ 직전 요청 크기, 같은 모델 기준 근삿값)", t.scr.cols())))
	}
	return lines
}

func totalsLine(name string, u harness.UsageTotals) string {
	hit := "(-)"
	if u.Prompt > 0 {
		hit = "(" + percent(u.CacheRead, u.Prompt) + ")"
	}
	eff := "-"
	if u.Ceiling > 0 {
		eff = percent(u.CeilingRead, u.Ceiling)
	}
	return fmt.Sprintf("   %s 요청 %d · 입력 %s · 캐시 %s %s · 효율 %s", name, u.Requests, formatTokens(u.Prompt), formatTokens(u.CacheRead), hit, eff)
}

// percent 는 a÷b 를 정수 반올림한 백분율이다. b 는 0 보다 크다.
func percent(a, b int) string { return fmt.Sprintf("%d%%", (a*200+b)/(2*b)) }
