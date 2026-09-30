package adapters

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// anthropicDialect 는 Anthropic Messages 다 (adapters 3.2).
type anthropicDialect struct{}

func (anthropicDialect) path() string { return "/v1/messages" }

func (anthropicDialect) auth(h http.Header, key string) {
	h.Set("x-api-key", key)
	h.Set("anthropic-version", anthropicVersion)
}

type obj = map[string]any

var ephemeral = obj{"type": "ephemeral"}

func (anthropicDialect) build(req contract.Request, p Profile, info record) ([]byte, *contract.CallError) {
	body := obj{"model": req.Model.ID, "stream": true}
	max := req.Options.MaxOutputTokens
	if max == 0 {
		max = info.out
	}
	if max == 0 {
		return nil, invalid("max_tokens is required: set Options.MaxOutputTokens or the model's maxOutputTokens")
	}
	body["max_tokens"] = max

	var system []obj
	for _, b := range req.System {
		if b.Text != "" {
			system = append(system, obj{"type": "text", "text": b.Text})
		}
	}
	msgs, pos := anthropicMessages(req)
	for _, h := range req.Options.CacheHints {
		switch h.Target {
		case contract.CacheSystem:
			if h.Index < len(system) {
				system[h.Index]["cache_control"] = ephemeral
			}
		case contract.CacheMessage:
			if at, ok := pos[h.Index]; ok {
				msgs[at[0]]["content"].([]obj)[at[1]]["cache_control"] = ephemeral
			}
		}
	}
	if len(system) > 0 {
		body["system"] = system
	}
	body["messages"] = msgs

	if len(req.Tools) > 0 {
		tools := make([]obj, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = obj{"name": t.Name, "input_schema": t.InputSchema}
			if t.Description != "" {
				tools[i]["description"] = t.Description
			}
		}
		body["tools"] = tools
	}
	if tc := req.Options.ToolChoice; tc != nil {
		switch tc.Mode {
		case contract.ToolChoiceRequired:
			body["tool_choice"] = obj{"type": "any"}
		case contract.ToolChoiceTool:
			body["tool_choice"] = obj{"type": "tool", "name": tc.Name}
		default:
			body["tool_choice"] = obj{"type": string(tc.Mode)}
		}
	}
	// AD-FR-13
	if level := req.Options.Thinking; level != "" {
		if !validLevel(level) {
			return nil, invalid("unknown thinking level %q", level)
		}
		if info.mode == modeBudget {
			return nil, invalid("model %s supports only budget thinking: set Options.Extra[\"thinking\"]", req.Model.ID)
		}
		body["thinking"] = obj{"type": "adaptive"}
		body["output_config"] = obj{"effort": level}
	}
	return withExtra(body, req.Options.Extra)
}

// anthropicMessages 는 메시지를 변환하고, 원본 메시지 인덱스 → (방언 메시지, 마지막 블록) 을 돌려준다 (AD-FR-12).
func anthropicMessages(req contract.Request) ([]obj, map[int][2]int) {
	var msgs []obj
	pos := map[int][2]int{}
	lastWasResult := false
	for i, m := range req.Messages {
		if m.Role == contract.RoleToolResult {
			block := anthropicResult(*m.Result)
			if lastWasResult {
				cur := msgs[len(msgs)-1]
				cur["content"] = append(cur["content"].([]obj), block)
			} else {
				msgs = append(msgs, obj{"role": "user", "content": []obj{block}})
			}
			lastWasResult = true
			pos[i] = [2]int{len(msgs) - 1, len(msgs[len(msgs)-1]["content"].([]obj)) - 1}
			continue
		}
		lastWasResult = false
		var content []obj
		for _, b := range m.Content {
			if c := anthropicBlock(b, req.Model); c != nil {
				content = append(content, c)
			}
		}
		if len(content) == 0 { // AD-FR-05
			continue
		}
		role := "user"
		switch m.Role {
		case contract.RoleAssistant:
			role = "assistant"
		case contract.RoleSystem:
			role = "system"
		}
		msgs = append(msgs, obj{"role": role, "content": content})
		pos[i] = [2]int{len(msgs) - 1, len(content) - 1}
	}
	return msgs, pos
}

func anthropicBlock(b contract.Block, model contract.ModelRef) obj {
	switch b.Kind {
	case contract.BlockText:
		if b.Text == "" {
			return nil
		}
		return obj{"type": "text", "text": b.Text}
	case contract.BlockImage:
		return obj{"type": "image", "source": obj{"type": "base64", "media_type": b.Image.MediaType, "data": base64.StdEncoding.EncodeToString(b.Image.Data)}}
	case contract.BlockThinking:
		if samePD(b.ProviderData, model) && len(b.ProviderData.Payload) > 0 {
			return obj{"type": "thinking", "thinking": b.Text, "signature": string(b.ProviderData.Payload)}
		}
		if b.Text == "" {
			return nil
		}
		return obj{"type": "text", "text": b.Text} // AD-FR-04
	case contract.BlockToolCall:
		return obj{"type": "tool_use", "id": normalizeID(b.ToolCall.ID), "name": b.ToolCall.Name, "input": b.ToolCall.Args}
	case contract.BlockOpaque:
		if b.Opaque.Provider == model.Provider && b.Opaque.Kind == "redacted_thinking" {
			return obj{"type": "redacted_thinking", "data": string(b.Opaque.Payload)}
		}
	}
	return nil
}

func anthropicResult(r contract.ToolResult) obj {
	content := []obj{}
	for _, b := range r.Content {
		if c := anthropicBlock(b, contract.ModelRef{}); c != nil {
			content = append(content, c)
		}
	}
	return obj{"type": "tool_result", "tool_use_id": normalizeID(r.CallID), "content": content, "is_error": r.Status != contract.StatusOK}
}

// anthropicStop 은 AD-FR-16 이다.
func anthropicStop(raw string) contract.StopReason {
	k := contract.StopError
	switch raw {
	case "end_turn", "stop_sequence", "pause_turn":
		k = contract.StopStop
	case "max_tokens":
		k = contract.StopLength
	case "tool_use":
		k = contract.StopToolUse
	}
	return contract.StopReason{Kind: k, Raw: raw}
}

type anthropicUsage struct {
	Input      *int `json:"input_tokens"`
	CacheRead  *int `json:"cache_read_input_tokens"`
	CacheWrite *int `json:"cache_creation_input_tokens"`
	Output     *int `json:"output_tokens"`
}

// apply 는 있는 값만 덮는다 (AD-FR-15).
func (u anthropicUsage) apply(dst *contract.Usage) {
	set := func(d *int, v *int) {
		if v != nil {
			*d = *v
		}
	}
	set(&dst.Input, u.Input)
	set(&dst.CacheRead, u.CacheRead)
	set(&dst.CacheWrite, u.CacheWrite)
	set(&dst.Output, u.Output)
}

type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string         `json:"id"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type     string `json:"type"`
		ID       string `json:"id"`
		Name     string `json:"name"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
		Data     string `json:"data"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parse 는 AD-FR-14 다.
func (anthropicDialect) parse(r io.Reader, s *stream) error {
	var stop contract.StopReason
	return readSSE(r, func(_, data string) bool {
		var e anthropicEvent
		if json.Unmarshal([]byte(data), &e) != nil {
			return !s.ended
		}
		switch e.Type {
		case "message_start":
			s.id = e.Message.ID
			e.Message.Usage.apply(&s.usage)
			return s.start()
		case "content_block_start":
			switch e.ContentBlock.Type {
			case "text":
				s.block(e.Index, contract.BlockText)
				return s.text(e.Index, e.ContentBlock.Text)
			case "thinking":
				return s.thinking(e.Index, e.ContentBlock.Thinking)
			case "redacted_thinking":
				b := s.block(e.Index, contract.BlockOpaque)
				b.Opaque = &contract.Opaque{Provider: s.provider, Kind: "redacted_thinking", Payload: []byte(e.ContentBlock.Data)}
			case "tool_use":
				return s.toolStart(e.Index, e.ContentBlock.ID, e.ContentBlock.Name)
			}
		case "content_block_delta":
			switch e.Delta.Type {
			case "text_delta":
				return s.text(e.Index, e.Delta.Text)
			case "thinking_delta":
				return s.thinking(e.Index, e.Delta.Thinking)
			case "signature_delta":
				b := s.block(e.Index, contract.BlockThinking)
				var prev []byte
				if b.ProviderData != nil {
					prev = b.ProviderData.Payload
				}
				b.ProviderData = s.providerData(append(prev, e.Delta.Signature...))
			case "input_json_delta":
				return s.toolDelta(e.Index, e.Delta.PartialJSON)
			}
		case "content_block_stop":
			if b := s.blocks[e.Index]; b != nil && b.Kind == contract.BlockToolCall {
				return s.toolEnd(e.Index, "")
			}
		case "message_delta":
			if e.Delta.StopReason != "" {
				stop = anthropicStop(e.Delta.StopReason)
			}
			if e.Usage != nil {
				e.Usage.apply(&s.usage)
			}
			return s.usageEvent()
		case "message_stop":
			if stop.Kind == "" {
				stop = anthropicStop("")
			}
			s.done(stop)
			return false
		case "error":
			s.fail(classifyStreamError(e.Error.Type, e.Error.Message, s.secrets))
			return false
		}
		return !s.ended
	})
}
