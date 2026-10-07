package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// judgeMarker 는 판정 지시의 첫 문장이다. 테스트가 판정 요청을 구별하는 데도 쓴다.
const judgeMarker = "You are a strict judge evaluating whether an autonomous agent has achieved a user's stated goal."

// judgeSystem 은 Hermes JUDGE_SYSTEM_PROMPT 의 done/blocked/continue 부분 요약이다 (MX-FR-03).
const judgeSystem = judgeMarker + ` You receive the goal and the agent's most recent response. Decide one verdict.

DONE: the response explicitly confirms the goal was completed or clearly shows the final deliverable was produced. DONE requires the deliverable to actually exist.
BLOCKED: the response explains the goal is unachievable as stated, or that progress needs user input. BLOCKED is a refusal, not a completion.
CONTINUE: not done and there is a concrete next step the agent can take now. This is the default when in doubt.

Reply ONLY with one JSON object: {"verdict": "done"|"blocked"|"continue", "reason": "<one sentence>"}`

const judgeResponseChars = 4000

var jsonObject = regexp.MustCompile(`(?s)\{.*\}`)

type progress struct {
	Arg   string `json:"arg"`
	Turns int    `json:"turns"`
}

func newID(d sets.Deps) string {
	if d.NewID != nil {
		return d.NewID()
	}
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func now(d sets.Deps) time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func userMessage(d sets.Deps, text string) contract.Message {
	return contract.Message{ID: newID(d), Role: contract.RoleUser, Timestamp: now(d),
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

func textOf(m contract.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Kind == contract.BlockText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func hasSettle(sigs []inject.TurnFinishSignal) bool {
	for _, s := range sigs {
		if s.Kind() == core.SigSettle {
			return true
		}
	}
	return false
}

// GoalSet 은 goal 모드의 세트다: 목표 주입과 턴 끝 판정 (MX-FR-01~04).
func GoalSet(d sets.Deps, maxTurns int) inject.FunctionSet {
	modes := sets.ModesOf(d)
	return inject.FunctionSet{Name: "goal", Description: "goal mode: inject the goal, judge after each turn",
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			msgs, sigs, err := ctx.Next()
			if err != nil {
				return msgs, sigs, err
			}
			st, err := modes.Current(ctx.Context(), ctx.Flow().SessionID())
			if err != nil || st.Arg == "" {
				return msgs, sigs, err
			}
			rule := fmt.Sprintf("[goal mode] Goal: %s\nWork toward this goal. When it is complete, say so and show "+
				"concrete evidence (command output, file contents, test results).", st.Arg)
			return append(msgs, userMessage(d, rule)), sigs, nil
		},
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			sigs, err := ctx.Next()
			if err != nil || !hasSettle(sigs) {
				return sigs, err
			}
			sid := ctx.Flow().SessionID()
			st, err := modes.Current(ctx.Context(), sid)
			if err != nil {
				return nil, err
			}
			if st.Arg == "" {
				return sigs, modes.Exit(ctx.Context(), sid, "no goal")
			}
			verdict, reason := judge(ctx, d, st.Arg, textOf(ctx.Output().Response()))
			switch verdict {
			case "done":
				return sigs, modes.Exit(ctx.Context(), sid, "goal achieved: "+reason)
			case "blocked":
				return sigs, modes.Exit(ctx.Context(), sid, "goal blocked: "+reason)
			}
			var p progress
			if v, ok := ctx.State().Get("progress"); ok {
				json.Unmarshal(v, &p)
			}
			if p.Arg != st.Arg {
				p = progress{Arg: st.Arg}
			}
			p.Turns++
			if err := ctx.State().Set("progress", p); err != nil {
				return nil, err
			}
			if p.Turns >= maxTurns {
				return sigs, modes.Exit(ctx.Context(), sid, fmt.Sprintf("goal turn budget %d/%d", p.Turns, maxTurns))
			}
			next := fmt.Sprintf("[Continuing toward your standing goal]\nGoal: %s\nJudge: %s\n"+
				"Take the next concrete step. If the goal is complete, say so with evidence and stop.", st.Arg, reason)
			var out []inject.TurnFinishSignal
			for _, s := range sigs {
				if s.Kind() != core.SigSettle {
					out = append(out, s)
				}
			}
			return append(out, ctx.InjectInput(userMessage(d, next)), ctx.ContinueTurn()), nil
		}}
}

// judge 는 보조 모델 goal_judge 의 판정이다. 실패는 continue 로 본다 (MX-FR-03, fail-open).
func judge(ctx *inject.TurnFinishCtx, d sets.Deps, goal, response string) (string, string) {
	cfg, err := sets.ParseConfig(ctx.Env().Config())
	if err != nil {
		return "continue", "judge config: " + err.Error()
	}
	if r := []rune(response); len(r) > judgeResponseChars {
		response = string(r[:judgeResponseChars])
	}
	ref := sets.AuxModel(cfg, "goal_judge")
	msg, err := sets.Complete(ctx.Context(), d.Model, contract.Request{
		Model:  contract.ModelRef{Provider: ref.Provider, ID: ref.ID},
		System: []contract.Block{{Kind: contract.BlockText, Text: judgeSystem}},
		Messages: []contract.Message{userMessage(d, fmt.Sprintf("Goal:\n%s\n\nAgent's most recent response:\n%s\n\n"+
			"Is the goal satisfied — done, blocked, or continue?", goal, response))},
	})
	if err != nil {
		return "continue", "judge error: " + err.Error()
	}
	var v struct{ Verdict, Reason string }
	raw := jsonObject.FindString(textOf(msg))
	if raw == "" || json.Unmarshal([]byte(raw), &v) != nil {
		return "continue", "judge reply was not JSON"
	}
	switch v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict)); v.Verdict {
	case "done", "blocked", "continue":
		return v.Verdict, v.Reason
	}
	return "continue", "unknown verdict " + v.Verdict
}
