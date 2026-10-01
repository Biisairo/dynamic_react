package adapters

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// responsesDialect 는 OpenAI Responses 다 (adapters 3.3a).
type responsesDialect struct{}

func (responsesDialect) path() string { return "/responses" }

func (responsesDialect) auth(h http.Header, key string) { h.Set("Authorization", "Bearer "+key) }

// reasoningPD 는 reasoning·function_call item 의 ProviderData payload 다.
type reasoningPD struct {
	ID               string `json:"id"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
}

func (responsesDialect) build(req contract.Request, p Profile, info record) ([]byte, *contract.CallError) {
	body := obj{"model": req.Model.ID, "stream": true, "store": false}
	if p.EncryptedReasoning {
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	max := req.Options.MaxOutputTokens
	if max == 0 {
		max = info.out
	}
	if max > 0 {
		body["max_output_tokens"] = max
	}
	if level := req.Options.Thinking; level != "" {
		body["reasoning"] = obj{"effort": level, "summary": "auto"}
	}
	body["input"] = responsesInput(req, p)
	if len(req.Tools) > 0 {
		tools := make([]obj, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = obj{"type": "function", "name": t.Name, "parameters": t.InputSchema}
			if t.Description != "" {
				tools[i]["description"] = t.Description
			}
		}
		body["tools"] = tools
	}
	if tc := req.Options.ToolChoice; tc != nil {
		if tc.Mode == contract.ToolChoiceTool {
			body["tool_choice"] = obj{"type": "function", "name": tc.Name}
		} else {
			body["tool_choice"] = string(tc.Mode)
		}
	}
	return withExtra(body, req.Options.Extra)
}

// responsesInput 은 AD-FR-41, 43 이다.
func responsesInput(req contract.Request, p Profile) []obj {
	var in []obj
	if sys := joinText(req.System, "\n\n"); sys != "" {
		role := "system"
		if p.DeveloperRole {
			role = "developer"
		}
		in = append(in, obj{"role": role, "content": sys})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case contract.RoleUser, contract.RoleSystem:
			if parts := responsesParts(m.Content); len(parts) > 0 {
				in = append(in, obj{"role": string(m.Role), "content": parts})
			}
		case contract.RoleAssistant:
			for _, b := range m.Content {
				switch b.Kind {
				case contract.BlockText:
					if b.Text != "" {
						in = append(in, obj{"type": "message", "role": "assistant", "content": []obj{{"type": "output_text", "text": b.Text}}})
					}
				case contract.BlockThinking:
					var pd reasoningPD
					if samePD(b.ProviderData, req.Model) && json.Unmarshal(b.ProviderData.Payload, &pd) == nil && pd.ID != "" {
						item := obj{"type": "reasoning", "id": pd.ID, "summary": []obj{}}
						if b.Text != "" {
							item["summary"] = []obj{{"type": "summary_text", "text": b.Text}}
						}
						if pd.EncryptedContent != "" {
							item["encrypted_content"] = pd.EncryptedContent
						}
						in = append(in, item)
					}
				case contract.BlockToolCall:
					item := obj{"type": "function_call", "call_id": normalizeID(b.ToolCall.ID), "name": b.ToolCall.Name, "arguments": string(b.ToolCall.Args)}
					var pd reasoningPD
					if samePD(b.ProviderData, req.Model) && json.Unmarshal(b.ProviderData.Payload, &pd) == nil && pd.ID != "" {
						item["id"] = pd.ID
					}
					in = append(in, item)
				}
			}
		case contract.RoleToolResult:
			r := m.Result
			item := obj{"type": "function_call_output", "call_id": normalizeID(r.CallID)}
			if hasImage(r.Content) {
				item["output"] = responsesParts(r.Content)
			} else {
				item["output"] = joinText(r.Content, "\n")
			}
			in = append(in, item)
		}
	}
	return in
}

func responsesParts(bs []contract.Block) []obj {
	var parts []obj
	for _, b := range bs {
		switch {
		case b.Kind == contract.BlockText && b.Text != "":
			parts = append(parts, obj{"type": "input_text", "text": b.Text})
		case b.Kind == contract.BlockImage:
			parts = append(parts, obj{"type": "input_image", "image_url": dataURL(b.Image)})
		}
	}
	return parts
}

type responsesItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	EncryptedContent string `json:"encrypted_content"`
}

type responsesEvent struct {
	Type        string        `json:"type"`
	OutputIndex int           `json:"output_index"`
	Item        responsesItem `json:"item"`
	Delta       string        `json:"delta"`
	Code        string        `json:"code"`
	Message     string        `json:"message"`
	Error       struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Response struct {
		ID                string `json:"id"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			Input        int `json:"input_tokens"`
			InputDetails struct {
				Cached int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			Output        int `json:"output_tokens"`
			OutputDetails struct {
				Reasoning int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	} `json:"response"`
}

// parse 는 AD-FR-45~47 이다.
func (responsesDialect) parse(r io.Reader, s *stream) error {
	calls, refused := false, false
	return readSSE(r, func(_, data string) bool {
		var e responsesEvent
		if json.Unmarshal([]byte(data), &e) != nil {
			return !s.ended
		}
		i := e.OutputIndex
		switch e.Type {
		case "response.created":
			s.id = e.Response.ID
			return s.start()
		case "response.output_item.added":
			if !s.start() {
				return false
			}
			switch e.Item.Type {
			case "reasoning":
				s.block(i, contract.BlockThinking)
			case "message":
				s.block(i, contract.BlockText)
			case "function_call":
				calls = true
				return s.toolStart(i, e.Item.CallID, e.Item.Name)
			}
		case "response.reasoning_summary_text.delta":
			return s.thinking(i, e.Delta)
		case "response.output_text.delta":
			return s.text(i, e.Delta)
		case "response.refusal.delta":
			refused = true
			return s.text(i, e.Delta)
		case "response.function_call_arguments.delta":
			return s.toolDelta(i, e.Delta)
		case "response.output_item.done":
			switch e.Item.Type {
			case "reasoning":
				pd, _ := json.Marshal(reasoningPD{ID: e.Item.ID, EncryptedContent: e.Item.EncryptedContent})
				s.block(i, contract.BlockThinking).ProviderData = s.providerData(pd)
			case "function_call":
				pd, _ := json.Marshal(reasoningPD{ID: e.Item.ID})
				if b := s.blocks[i]; b != nil {
					b.ProviderData = s.providerData(pd)
				}
				return s.toolEnd(i, e.Item.Arguments)
			}
		case "response.completed", "response.incomplete":
			if s.id == "" {
				s.id = e.Response.ID
			}
			if u := e.Response.Usage; u != nil {
				s.usage = contract.Usage{Input: max(u.Input-u.InputDetails.Cached, 0), CacheRead: u.InputDetails.Cached,
					Output: u.Output, Reasoning: u.OutputDetails.Reasoning}
			}
			stop := contract.StopReason{Kind: contract.StopStop, Raw: "completed"}
			switch {
			case e.Type == "response.incomplete" && e.Response.IncompleteDetails.Reason == "max_output_tokens":
				stop = contract.StopReason{Kind: contract.StopLength, Raw: "max_output_tokens"}
			case e.Type == "response.incomplete":
				stop = contract.StopReason{Kind: contract.StopError, Raw: e.Response.IncompleteDetails.Reason}
			case refused:
				stop = contract.StopReason{Kind: contract.StopError, Raw: "refusal"}
			case calls:
				stop = contract.StopReason{Kind: contract.StopToolUse, Raw: "completed"}
			}
			if !s.start() || !s.usageEvent() {
				return false
			}
			s.done(stop)
			return false
		case "response.failed":
			s.fail(classifyStreamError(e.Response.Error.Code, e.Response.Error.Message, s.secrets))
			return false
		case "error":
			kind, msg := e.Code, e.Message
			if e.Error.Message != "" || e.Error.Code != "" || e.Error.Type != "" {
				kind, msg = e.Error.Type+" "+e.Error.Code, e.Error.Message
			}
			s.fail(classifyStreamError(kind, msg, s.secrets))
			return false
		}
		return !s.ended
	})
}
