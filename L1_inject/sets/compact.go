package sets

import (
	"context"
	"strings"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

const (
	summaryHeader    = "The conversation history before this point was compacted into the following summary:\n\n"
	maxSummaryTokens = 16384
)

// ApplyEdits 는 편집 로그를 순서대로 적용한 투영을 돌려준다. 대상 ID 가 없는 편집은 건너뛴다.
func ApplyEdits(transcript []contract.Message, edits []contract.ContextEdit) []contract.Message {
	out := append([]contract.Message(nil), transcript...)
	for _, e := range edits {
		switch e.Kind {
		case contract.EditReplaceRange:
			from, to := indexOf(out, e.From), indexOf(out, e.To)
			if from < 0 || to < from {
				continue
			}
			next := append([]contract.Message(nil), out[:from]...)
			next = append(next, e.Messages...)
			out = append(next, out[to+1:]...)
		case contract.EditHide:
			if i := indexOf(out, e.Target); i >= 0 {
				out = append(out[:i:i], out[i+1:]...)
			}
		case contract.EditTruncateAfter:
			if i := indexOf(out, e.Target); i >= 0 {
				out = out[:i+1]
			}
		}
	}
	return out
}

func indexOf(ms []contract.Message, id string) int {
	for i, m := range ms {
		if m.ID == id {
			return i
		}
	}
	return -1
}

// Compact 는 투영의 앞 구간을 요약으로 대체하는 편집을 만든다 (ST-FR-23~27).
// ok=false 는 압축할 구간이 없음, err 는 요약 실패다.
func Compact(ctx context.Context, s Summarizer, model contract.ModelRef, proj []contract.Message,
	keepRecent, reserve, maxOut int, now time.Time, editID, summaryID string) (contract.ContextEdit, bool, error) {
	cut := cutPoint(proj, keepRecent)
	if s == nil || cut <= 0 {
		return contract.ContextEdit{}, false, nil
	}
	seg := proj[:cut]
	msgs, previous := seg, ""
	if text, ok := summaryText(seg[0]); ok {
		msgs, previous = seg[1:], text
	}
	if len(msgs) == 0 {
		return contract.ContextEdit{}, false, nil
	}
	limit := min(reserve*8/10, maxSummaryTokens)
	if maxOut > 0 {
		limit = min(limit, maxOut)
	}
	text, err := s.Summarize(ctx, SummaryRequest{Model: model, Messages: msgs, Previous: previous, MaxTokens: limit})
	if err != nil {
		return contract.ContextEdit{}, false, err
	}
	sum := contract.Message{ID: summaryID, Role: contract.RoleUser, Timestamp: now,
		Content: []contract.Block{{Kind: contract.BlockText, Text: summaryHeader + text}}}
	return contract.ContextEdit{ID: editID, Kind: contract.EditReplaceRange, Timestamp: now,
		From: seg[0].ID, To: seg[len(seg)-1].ID, Messages: []contract.Message{sum}}, true, nil
}

// cutPoint 는 남길 구간의 첫 인덱스다. 끝에서 keepRecent 이상 쌓인 지점에서 앞쪽으로
// user·assistant 메시지를 찾는다 (tool_result 는 호출과 떼지 않기 위해 절단점이 될 수 없다). 없으면 -1.
func cutPoint(proj []contract.Message, keepRecent int) int {
	acc := 0
	for i := len(proj) - 1; i >= 0; i-- {
		acc += EstimateMessage(proj[i])
		if acc < keepRecent {
			continue
		}
		for j := i; j >= 0; j-- {
			if r := proj[j].Role; r == contract.RoleUser || r == contract.RoleAssistant {
				return j
			}
		}
		return -1
	}
	return -1
}

func summaryText(m contract.Message) (string, bool) {
	if m.Role != contract.RoleUser || len(m.Content) == 0 || m.Content[0].Kind != contract.BlockText {
		return "", false
	}
	return strings.CutPrefix(m.Content[0].Text, summaryHeader)
}
