// Package summary 는 pi 요약 프롬프트로 압축 구간을 요약하는 sets.Summarizer 다 (3.5).
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// 문안 출처: pi coding-agent core/compaction (utils.ts SUMMARIZATION_SYSTEM_PROMPT, compaction.ts SUMMARIZATION_PROMPT·UPDATE_SUMMARIZATION_PROMPT). 고지는 THIRD_PARTY_NOTICES.md.
const SystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const Prompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const UpdatePrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const toolResultMaxChars = 2000

// Summarizer 는 ModelCaller 로 요약을 요청한다. 재시도하지 않는다 (HN-FR-25).
type Summarizer struct {
	model sets.ModelCaller
	now   func() time.Time
}

func New(model sets.ModelCaller, now func() time.Time) *Summarizer {
	if now == nil {
		now = time.Now
	}
	return &Summarizer{model: model, now: now}
}

func (s *Summarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	text := "<conversation>\n" + Serialize(r.Messages) + "\n</conversation>\n\n"
	instr := Prompt
	if r.Previous != "" {
		text += "<previous-summary>\n" + r.Previous + "\n</previous-summary>\n\n"
		instr = UpdatePrompt
	}
	req := contract.Request{
		Model:  r.Model,
		System: []contract.Block{{Kind: contract.BlockText, Text: SystemPrompt}},
		Messages: []contract.Message{{ID: "summary-request", Role: contract.RoleUser, Timestamp: s.now(),
			Content: []contract.Block{{Kind: contract.BlockText, Text: text + instr}}}},
		Options: contract.Options{MaxOutputTokens: r.MaxTokens},
	}
	var resp *contract.Message
	for ev, err := range s.model.Call(ctx, req) {
		if err != nil {
			return "", fmt.Errorf("summary: %w", err)
		}
		switch ev.Kind {
		case contract.EventError:
			return "", fmt.Errorf("summary: %w", ev.Error)
		case contract.EventDone:
			resp = ev.Message
		}
		if resp != nil {
			break
		}
	}
	if resp == nil {
		return "", errors.New("summary: stream ended without response")
	}
	if resp.StopReason != nil && resp.StopReason.Kind != contract.StopStop && resp.StopReason.Kind != contract.StopToolUse {
		return "", fmt.Errorf("summary: incomplete response (stop reason %s)", resp.StopReason.Kind)
	}
	var parts []string
	for _, b := range resp.Content {
		if b.Kind == contract.BlockText {
			parts = append(parts, b.Text)
		}
	}
	out := strings.Join(parts, "\n")
	if strings.TrimSpace(out) == "" {
		return "", errors.New("summary: empty response")
	}
	return out, nil
}

// Serialize 는 대화를 요약용 텍스트로 바꾼다 (pi serializeConversation).
func Serialize(msgs []contract.Message) string {
	var parts []string
	for _, m := range msgs {
		switch m.Role {
		case contract.RoleUser:
			if t := contentText(m.Content); t != "" {
				parts = append(parts, "[User]: "+t)
			}
		case contract.RoleAssistant:
			var thinking, texts, calls []string
			for _, b := range m.Content {
				switch b.Kind {
				case contract.BlockThinking:
					thinking = append(thinking, b.Text)
				case contract.BlockText:
					texts = append(texts, b.Text)
				case contract.BlockToolCall:
					calls = append(calls, b.ToolCall.Name+"("+formatArgs(b.ToolCall.Args)+")")
				}
			}
			if len(thinking) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinking, "\n"))
			}
			if len(texts) > 0 {
				parts = append(parts, "[Assistant]: "+strings.Join(texts, "\n"))
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case contract.RoleToolResult:
			if m.Result != nil {
				if t := contentText(m.Result.Content); t != "" {
					parts = append(parts, "[Tool result]: "+truncate(t, toolResultMaxChars))
				}
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func contentText(bs []contract.Block) string {
	var out []string
	for _, b := range bs {
		switch b.Kind {
		case contract.BlockText:
			out = append(out, b.Text)
		case contract.BlockImage:
			out = append(out, "[image]")
		}
	}
	return strings.Join(out, "\n")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return fmt.Sprintf("%s\n\n[... %d more characters truncated]", string(r[:n]), len(r)-n)
}

// formatArgs 는 최상위 키 순서를 지켜 k=JSON 을 ", " 로 잇는다. 객체가 아니면 원문.
func formatArgs(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return string(raw)
	}
	var parts []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return string(raw)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return string(raw)
		}
		var buf bytes.Buffer
		json.Compact(&buf, v)
		parts = append(parts, fmt.Sprintf("%v=%s", k, buf.String()))
	}
	return strings.Join(parts, ", ")
}
