package sets

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// RepeatOptions 는 repeat 세트의 설정이다 (3.2d).
type RepeatOptions struct {
	Warn   int
	Stop   int
	Exempt []string
}

// ErrRepeat 는 같은 tool call 이 Stop 턴 이어져 run 이 끝났다는 뜻이다 (ST-FR-67).
var ErrRepeat = errors.New("repeat: same tool calls repeated")

const (
	repeatWarn        = 5
	repeatStop        = 8
	keyRepeat         = "progress"
	repeatArgsLimit   = 400
	repeatResultLimit = 200
)

type repeatState struct {
	Run   string   `json:"run"`
	Count int      `json:"count"`
	Last  []string `json:"last"`
}

// Repeat 는 turn.finish 만 채운 반복 감지 세트다 (ST-FR-64~69).
func Repeat(d Deps, o RepeatOptions) (inject.FunctionSet, error) {
	d = d.filled()
	if o.Warn <= 0 {
		o.Warn = repeatWarn
	}
	if o.Stop <= 0 {
		o.Stop = repeatStop
	}
	if o.Warn >= o.Stop {
		return inject.FunctionSet{}, errors.New("sets: repeat: Warn must be less than Stop")
	}
	return inject.FunctionSet{Name: "repeat", Description: "stop repeating the same tool calls turn after turn", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		sigs, err := ctx.Next()
		if err != nil {
			return nil, err
		}
		st := ctx.State()
		rs, _ := getJSON[repeatState](st, keyRepeat)
		if rs.Run != ctx.Flow().RunID() {
			rs = repeatState{Run: ctx.Flow().RunID()}
		}
		calls := ctx.Tools().Calls()
		if !slices.ContainsFunc(calls, func(c contract.ToolCall) bool { return !slices.Contains(o.Exempt, c.Name) }) {
			rs.Count = 0
		} else if fp := fingerprint(calls); slices.Equal(fp, rs.Last) {
			rs.Count++
		} else {
			rs.Count, rs.Last = 1, fp
		}
		if err := st.Set(keyRepeat, rs); err != nil {
			return nil, err
		}
		switch rs.Count {
		case o.Stop:
			return []inject.TurnFinishSignal{ctx.Fail(fmt.Errorf("%w: %d turns in a row", ErrRepeat, o.Stop))}, nil
		case o.Warn:
			msg := contract.Message{ID: d.NewID(), Role: contract.RoleUser, Timestamp: d.Now(), Content: []contract.Block{{Kind: contract.BlockText,
				Text: redirect(o.Warn, calls, ctx.Tools().Results(), o.Exempt)}}}
			return append([]inject.TurnFinishSignal{ctx.InjectInput(msg)}, sigs...), nil
		}
		return sigs, nil
	}}, nil
}

// fingerprint 는 호출마다 이름과 ArgsDigest 를 이어 정렬한 목록이다 (ST-FR-65).
func fingerprint(calls []contract.ToolCall) []string {
	fp := make([]string, len(calls))
	for i, c := range calls {
		fp[i] = c.Name + "\x00" + c.ArgsDigest()
	}
	slices.Sort(fp)
	return fp
}

// redirect 는 지시 메시지 본문이다 (ST-FR-68).
func redirect(n int, calls []contract.ToolCall, results []contract.ToolResult, exempt []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repeated tool calls: you made the same tool calls %d turns in a row with identical arguments.\n\n", n)
	for _, c := range calls {
		if slices.Contains(exempt, c.Name) {
			continue
		}
		out := "(no text result)"
		if i := slices.IndexFunc(results, func(r contract.ToolResult) bool { return r.CallID == c.ID }); i >= 0 {
			var texts []string
			for _, bl := range results[i].Content {
				if bl.Kind == contract.BlockText {
					texts = append(texts, bl.Text)
				}
			}
			if s := summarize(strings.Join(texts, "\n"), repeatResultLimit); s != "" {
				out = s
			}
		}
		fmt.Fprintf(&b, "- %s %s\n  last result: %s\n", c.Name, summarize(string(c.Args), repeatArgsLimit), out)
	}
	b.WriteString("\nDo not make these calls again with the same arguments. Use different arguments, choose another tool, or finish with what you have.")
	return b.String()
}

// summarize 는 연속 공백을 하나로 줄이고 limit rune 을 넘으면 자르고 "…" 를 붙인다.
func summarize(s string, limit int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return string(r)
}
