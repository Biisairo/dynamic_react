package sets

import (
	"unicode"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

const imageTokens = 1200

// EstimateText 는 텍스트의 토큰 수 추정이다 (ST-FR-20).
func EstimateText(s string) int {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return (len(s) + 3) / 4
	}
	cjk, rest := 0, 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
			cjk++
			continue
		}
		rest += len(string(r))
	}
	return cjk + (rest+3)/4
}

func estimateBlock(b contract.Block) int {
	switch b.Kind {
	case contract.BlockText, contract.BlockThinking:
		return EstimateText(b.Text)
	case contract.BlockImage:
		return imageTokens
	case contract.BlockToolCall:
		return EstimateText(b.ToolCall.Name) + EstimateText(string(b.ToolCall.Args))
	}
	return 0
}

// EstimateMessage 는 메시지 하나의 추정이다. ProviderData·opaque 는 0 이다 (ST-FR-21).
func EstimateMessage(m contract.Message) int {
	n := 0
	for _, b := range m.Content {
		n += estimateBlock(b)
	}
	if m.Result != nil {
		for _, b := range m.Result.Content {
			n += estimateBlock(b)
		}
	}
	return n
}

// EstimateContext 는 투영의 컨텍스트 크기 추정이다 (ST-FR-22).
// 가장 최근 편집보다 뒤의 성공한 응답이 있으면 그 usage 에 이후 메시지 추정을 더한다.
func EstimateContext(proj []contract.Message, edits []contract.ContextEdit, system []contract.Block, tools []contract.ToolSpec) int {
	last := -1
	for i := len(proj) - 1; i >= 0; i-- {
		if succeeded(proj[i]) {
			last = i
			break
		}
	}
	if last >= 0 && afterEdits(proj[last], edits) {
		n := proj[last].Usage.Total()
		for _, m := range proj[last+1:] {
			n += EstimateMessage(m)
		}
		return n
	}
	n := 0
	for _, m := range proj {
		n += EstimateMessage(m)
	}
	for _, b := range system {
		n += estimateBlock(b)
	}
	for _, t := range tools {
		n += EstimateText(t.Name) + EstimateText(t.Description) + EstimateText(string(t.InputSchema))
	}
	return n
}

func succeeded(m contract.Message) bool {
	if m.Role != contract.RoleAssistant || m.Usage == nil {
		return false
	}
	return m.StopReason == nil || (m.StopReason.Kind != contract.StopError && m.StopReason.Kind != contract.StopAborted)
}

func afterEdits(m contract.Message, edits []contract.ContextEdit) bool {
	for _, e := range edits {
		if !m.Timestamp.After(e.Timestamp) {
			return false
		}
	}
	return true
}
