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

// notesHeader 는 판정 요청의 사용자 메시지 블록 머리다 (MX-FR-03).
const notesHeader = "Messages the user sent while the goal was active (they may add to or change the criteria; ignore those that do not):"

const (
	noteChars  = 500  // 메시지 하나 (MX-FR-03)
	notesChars = 2000 // 블록의 메시지 줄 합
	notesKept  = 20   // 상태에 두는 수 (MX-FR-11)
)

// base 가 공개하는 입력함 소비 수 (sets ST-FR-43).
const (
	baseNamespace    = "base"
	keyInboxConsumed = "inbox.consumed"
)

var jsonObject = regexp.MustCompile(`(?s)\{.*\}`)

type progress struct {
	Arg   string `json:"arg"`
	Turns int    `json:"turns"`
}

// notes 는 goal 진행 중 사용자 메시지다 (MX-FR-11).
type notes struct {
	Arg      string   `json:"arg"`
	Messages []string `json:"messages"`
}

func loadNotes(st inject.State, arg string) notes {
	var n notes
	if v, ok := st.Get("notes"); ok {
		json.Unmarshal(v, &n)
	}
	if n.Arg != arg {
		n = notes{Arg: arg}
	}
	return n
}

// injectedIDs 는 이 run 에서 inject_input 으로 들어온 메시지 ID 다.
func injectedIDs(f inject.Flow) map[string]bool {
	ids := map[string]bool{}
	for _, r := range f.Signals() {
		for _, s := range r.Signals {
			if s.Kind == core.SigInjectInput && s.Message != nil {
				ids[s.Message.ID] = true
			}
		}
	}
	return ids
}

// inboxPending 은 base 가 아직 소비하지 않은 입력함 메시지가 있는지다.
func inboxPending(st inject.State, c inject.Conv) bool {
	consumed := 0
	if raw, ok := st.Read(baseNamespace, keyInboxConsumed); ok {
		json.Unmarshal(raw, &consumed)
	}
	return len(c.Inbox()) > consumed
}

// notesBlock 은 판정 요청에 붙일 사용자 메시지 블록이다. 넣을 것이 없으면 "" (MX-FR-03).
func notesBlock(msgs []string) string {
	var lines []string
	total := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		r := []rune(msgs[i])
		if len(r) > noteChars {
			r = append(r[:noteChars], '…')
		}
		line := "- " + string(r)
		if total += len([]rune(line)); total > notesChars {
			break
		}
		lines = append([]string{line}, lines...)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\n" + notesHeader + "\n" + strings.Join(lines, "\n")
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
		TurnStart: func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
			msgs, sigs, err := ctx.Next()
			if err != nil {
				return msgs, sigs, err
			}
			st, err := modes.Current(ctx.Context(), ctx.Flow().SessionID())
			if err != nil || st.Arg == "" {
				return msgs, sigs, err
			}
			injected := injectedIDs(ctx.Flow())
			n := loadNotes(ctx.State(), st.Arg)
			added := false
			for _, m := range msgs {
				if t := textOf(m); m.Role == contract.RoleUser && !injected[m.ID] && t != "" {
					n.Messages, added = append(n.Messages, t), true
				}
			}
			if !added {
				return msgs, sigs, nil
			}
			n.Messages = n.Messages[max(0, len(n.Messages)-notesKept):]
			if err := ctx.State().Set("notes", n); err != nil {
				return nil, nil, err
			}
			return msgs, sigs, nil
		},
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			sigs, err := ctx.Next()
			if err != nil || !hasSettle(sigs) || inboxPending(ctx.State(), ctx.Conv()) {
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
			verdict, reason := judge(ctx, d, st.Arg, textOf(ctx.Output().Response()), loadNotes(ctx.State(), st.Arg).Messages)
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
func judge(ctx *inject.TurnFinishCtx, d sets.Deps, goal, response string, userNotes []string) (string, string) {
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
			"Is the goal satisfied — done, blocked, or continue?%s", goal, response, notesBlock(userNotes)))},
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
