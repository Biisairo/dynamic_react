package adapters

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// chatDialect 는 OpenAI Chat Completions 와 호환 서버다 (adapters 3.3).
type chatDialect struct{}

func (chatDialect) path() string { return "/chat/completions" }

func (chatDialect) auth(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) }

func (chatDialect) build(req contract.Request, p Profile, info record) ([]byte, *contract.CallError) {
	body := obj{"model": req.Model.ID, "stream": true}
	if p.UsageInStreaming {
		body["stream_options"] = obj{"include_usage": true}
	}
	max := req.Options.MaxOutputTokens
	if max == 0 {
		max = info.out
	}
	if max > 0 {
		body[p.MaxTokensField] = max
	}
	body["messages"] = chatMessages(req, p)
	if len(req.Tools) > 0 {
		tools := make([]obj, len(req.Tools))
		for i, t := range req.Tools {
			fn := obj{"name": t.Name, "parameters": t.InputSchema}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			tools[i] = obj{"type": "function", "function": fn}
		}
		body["tools"] = tools
	}
	if tc := req.Options.ToolChoice; tc != nil {
		if tc.Mode == contract.ToolChoiceTool {
			body["tool_choice"] = obj{"type": "function", "function": obj{"name": tc.Name}}
		} else {
			body["tool_choice"] = string(tc.Mode)
		}
	}
	// AD-FR-23
	if level := req.Options.Thinking; level != "" {
		switch p.Thinking {
		case ThinkOpenAI:
			body["reasoning_effort"] = level
		case ThinkDeepSeek:
			body["thinking"] = obj{"type": "enabled"}
			body["reasoning_effort"] = level
		case ThinkZai:
			body["thinking"] = obj{"type": "enabled", "clear_thinking": false}
		case ThinkOpenRouter, ThinkAntLing:
			body["reasoning"] = obj{"effort": level}
		case ThinkTogether:
			body["reasoning"] = obj{"enabled": true}
		case ThinkChatTemplate:
			body["chat_template_kwargs"] = obj{"enable_thinking": true}
		}
	}
	return withExtra(body, req.Options.Extra)
}

const seeImage = "(see attached image)"

func chatMessages(req contract.Request, p Profile) []obj {
	var msgs []obj
	if sys := joinText(req.System, "\n\n"); sys != "" {
		role := "system"
		if p.DeveloperRole {
			role = "developer"
		}
		msgs = append(msgs, obj{"role": role, "content": sys})
	}
	names := map[string]string{} // call id → 도구 이름 (RequiresToolResultName)
	for _, m := range req.Messages {
		switch m.Role {
		case contract.RoleUser, contract.RoleSystem:
			if c := chatUserContent(m.Content); c != nil {
				msgs = append(msgs, obj{"role": string(m.Role), "content": c})
			}
		case contract.RoleAssistant:
			a := obj{"role": "assistant"}
			var calls []obj
			var reasoning string
			for _, b := range m.Content {
				switch b.Kind {
				case contract.BlockToolCall:
					names[b.ToolCall.ID] = b.ToolCall.Name
					calls = append(calls, obj{"id": normalizeID(b.ToolCall.ID), "type": "function",
						"function": obj{"name": b.ToolCall.Name, "arguments": string(b.ToolCall.Args)}})
				case contract.BlockThinking:
					reasoning += b.Text
				}
			}
			text := joinText(m.Content, "")
			if text == "" && len(calls) == 0 { // AD-FR-28
				continue
			}
			if text == "" {
				a["content"] = nil
			} else {
				a["content"] = text
			}
			if len(calls) > 0 {
				a["tool_calls"] = calls
			}
			if p.ReasoningField != "" && reasoning != "" { // AD-FR-24
				a[p.ReasoningField] = reasoning
			}
			msgs = append(msgs, a)
		case contract.RoleToolResult:
			r := m.Result
			text := joinText(r.Content, "\n")
			img := hasImage(r.Content)
			if img {
				if text != "" {
					text += "\n"
				}
				text += seeImage
			}
			t := obj{"role": "tool", "tool_call_id": normalizeID(r.CallID), "content": text}
			if p.ToolResultName {
				t["name"] = names[r.CallID]
			}
			msgs = append(msgs, t)
			if img { // AD-FR-20
				parts := []obj{{"type": "text", "text": "Attached image(s) from tool result:"}}
				for _, b := range r.Content {
					if b.Kind == contract.BlockImage {
						parts = append(parts, obj{"type": "image_url", "image_url": obj{"url": dataURL(b.Image)}})
					}
				}
				msgs = append(msgs, obj{"role": "user", "content": parts})
			}
		}
	}
	return msgs
}

// chatUserContent 는 text 만이면 문자열, 이미지가 있으면 파트 배열이다. 비면 nil.
func chatUserContent(bs []contract.Block) any {
	if !hasImage(bs) {
		if t := joinText(bs, "\n"); t != "" {
			return t
		}
		return nil
	}
	var parts []obj
	for _, b := range bs {
		switch {
		case b.Kind == contract.BlockText && b.Text != "":
			parts = append(parts, obj{"type": "text", "text": b.Text})
		case b.Kind == contract.BlockImage:
			parts = append(parts, obj{"type": "image_url", "image_url": obj{"url": dataURL(b.Image)}})
		}
	}
	return parts
}

// chatStop 은 AD-FR-27 이다.
func chatStop(raw string) contract.StopReason {
	k := contract.StopError
	switch raw {
	case "stop", "end", "":
		k = contract.StopStop
	case "length":
		k = contract.StopLength
	case "tool_calls", "function_call":
		k = contract.StopToolUse
	}
	return contract.StopReason{Kind: k, Raw: raw}
}

type chatUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Details    struct {
		Cached     *int `json:"cached_tokens"`
		CacheWrite int  `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CacheHit         *int `json:"prompt_cache_hit_tokens"`
	Cached           *int `json:"cached_tokens"`
	CompletionDetail struct {
		Reasoning int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// normalize 는 AD-FR-26 이다.
func (u chatUsage) normalize() contract.Usage {
	read := 0
	switch {
	case u.Details.Cached != nil:
		read = *u.Details.Cached
	case u.CacheHit != nil:
		read = *u.CacheHit
	case u.Cached != nil:
		read = *u.Cached
	}
	in := u.Prompt - read - u.Details.CacheWrite
	if in < 0 {
		in = 0
	}
	return contract.Usage{Input: in, CacheRead: read, CacheWrite: u.Details.CacheWrite, Output: u.Completion, Reasoning: u.CompletionDetail.Reasoning}
}

type chatChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ReasoningText    string `json:"reasoning_text"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string    `json:"finish_reason"`
		Usage        *chatUsage `json:"usage"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// parse 는 AD-FR-25 다.
func (chatDialect) parse(r io.Reader, s *stream) error {
	var stop *contract.StopReason
	tools := map[int]int{} // tool_calls index → 블록 Index
	finish := func() {
		if stop == nil {
			st := chatStop("")
			stop = &st
		}
		s.done(*stop)
	}
	err := readSSE(r, func(_, data string) bool {
		if data == "[DONE]" {
			finish() // finish_reason 이 끝까지 null 이면 stop (AD-FR-27)
			return false
		}
		var c chatChunk
		if json.Unmarshal([]byte(data), &c) != nil {
			return !s.ended
		}
		if c.Error != nil {
			s.fail(classifyStreamError(fmt.Sprint(c.Error.Type, " ", c.Error.Code), c.Error.Message, s.secrets))
			return false
		}
		if !s.start() {
			return false
		}
		if c.ID != "" {
			s.id = c.ID
		}
		usage := c.Usage
		for _, ch := range c.Choices {
			if ch.Usage != nil {
				usage = ch.Usage
			}
			d := ch.Delta
			for _, rt := range []string{d.ReasoningContent, d.Reasoning, d.ReasoningText} {
				if rt != "" && !s.thinking(s.index("reasoning"), rt) {
					return false
				}
			}
			if d.Content != "" && !s.text(s.index("text"), d.Content) {
				return false
			}
			for _, tc := range d.ToolCalls {
				i, ok := tools[tc.Index]
				if !ok {
					i = s.index(fmt.Sprint("tool:", tc.Index))
					tools[tc.Index] = i
					if !s.toolStart(i, tc.ID, tc.Function.Name) {
						return false
					}
				}
				if !s.toolDelta(i, tc.Function.Arguments) {
					return false
				}
			}
			if ch.FinishReason != nil {
				st := chatStop(*ch.FinishReason)
				stop = &st
				idx := make([]int, 0, len(tools))
				for _, i := range tools {
					idx = append(idx, i)
				}
				sort.Ints(idx)
				for _, i := range idx {
					if !s.toolEnd(i, "") {
						return false
					}
				}
			}
		}
		if usage != nil {
			s.usage = usage.normalize()
			return s.usageEvent()
		}
		return !s.ended
	})
	if err == nil && !s.ended && stop != nil {
		finish() // [DONE] 없이 끝나는 서버
	}
	return err
}
