package adapters_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func anthropicScenario() contract.Request {
	pd := &contract.ProviderData{Provider: "anthropic", Model: "claude-opus-5-5", Payload: []byte("EqQBCkYI...")}
	req := scenario("anthropic", "claude-opus-5-5", "toolu_01A", pd, nil)
	req.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheSystem, Index: 0}, {Target: contract.CacheMessage, Index: 2}}
	return req
}

func sseLines(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		var m map[string]any
		json.Unmarshal([]byte(e), &m)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", m["type"], e)
	}
	return b.String()
}

const anthropicText = `{"type":"message_start","message":{"id":"msg_02","model":"m","usage":{"input_tokens":5,"output_tokens":1}}}`

func anthropicReply(stop string, extra ...string) string {
	evs := append([]string{anthropicText,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_stop","index":0}`}, extra...)
	evs = append(evs, `{"type":"message_delta","delta":{"stop_reason":"`+stop+`"},"usage":{"output_tokens":3}}`, `{"type":"message_stop"}`)
	return sseLines(evs...)
}

// AD-FR-07~13, AD-NFR-01: 05 문서 2.1 의 요청을 중립 형식에서 생성한다.
func TestAnthropicGoldenRequest(t *testing.T) {
	f := newFake(t)
	f.sse("/v1/messages", "anthropic_stream.sse")
	c := client(t, f, "anthropic", "")
	collect(t, c, anthropicScenario())
	sameJSON(t, f.body("/v1/messages"), "anthropic_request.json")
	h := f.header("/v1/messages")
	if h.Get("x-api-key") != testKey || h.Get("anthropic-version") == "" || h.Get("Authorization") != "" {
		t.Fatalf("headers %v", h)
	}
}

// AD-FR-14~16: 05 문서 2.2 의 스트림을 파싱한다.
func TestAnthropicStream(t *testing.T) {
	f := newFake(t)
	f.sse("/v1/messages", "anthropic_stream.sse")
	evs := collect(t, client(t, f, "anthropic", ""), anthropicScenario())
	if k := kinds(evs); !strings.HasPrefix(k, "start,") || !strings.Contains(k, "thinking_delta,thinking_delta") ||
		!strings.Contains(k, "toolcall_start,toolcall_delta,toolcall_delta,toolcall_end") || !strings.HasSuffix(k, "done") {
		t.Fatalf("kinds %s", k)
	}
	m := done(t, evs)
	if m.ID != "msg_01" || m.Role != contract.RoleAssistant || !m.Timestamp.Equal(ts) {
		t.Fatalf("message %+v", m)
	}
	if len(m.Content) != 2 {
		t.Fatalf("content %+v", m.Content)
	}
	th := m.Content[0]
	if th.Kind != contract.BlockThinking || th.Text != "config.py 를 읽어야 한다." || th.ProviderData == nil ||
		string(th.ProviderData.Payload) != "EqQBCkYI..." || th.ProviderData.Provider != "anthropic" || th.ProviderData.Model != "claude-opus-5-5" {
		t.Fatalf("thinking %+v", th)
	}
	tc := m.Content[1].ToolCall
	if tc.ID != "toolu_01A" || tc.Name != "read" || string(tc.Args) != `{"path":"config.py"}` {
		t.Fatalf("tool call %+v %s", tc, tc.Args)
	}
	if *m.Usage != (contract.Usage{Input: 412, CacheRead: 2048, Output: 87}) {
		t.Fatalf("usage %+v", m.Usage)
	}
	if m.StopReason.Kind != contract.StopToolUse || m.StopReason.Raw != "tool_use" {
		t.Fatalf("stop %+v", m.StopReason)
	}
	for _, e := range evs {
		if e.Kind == contract.EventToolCallEnd && string(e.ToolCall.Args) != `{"path":"config.py"}` {
			t.Fatalf("toolcall_end args %s", e.ToolCall.Args)
		}
	}
}

// AD-FR-16
func TestAnthropicStopReasons(t *testing.T) {
	for raw, want := range map[string]contract.StopKind{
		"end_turn": contract.StopStop, "stop_sequence": contract.StopStop, "pause_turn": contract.StopStop,
		"max_tokens": contract.StopLength, "tool_use": contract.StopToolUse, "refusal": contract.StopError, "weird": contract.StopError,
	} {
		f := newFake(t)
		f.sseText("/v1/messages", anthropicReply(raw))
		m := done(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m")))
		if m.StopReason.Kind != want || m.StopReason.Raw != raw {
			t.Errorf("%s -> %+v", raw, m.StopReason)
		}
	}
}

// AD-FR-15: message_delta 에 input 이 없으면 start 값을 유지, 있으면 덮는다.
func TestAnthropicUsageMerge(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", sseLines(anthropicText,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"cache_read_input_tokens":4}}`,
		`{"type":"message_stop"}`))
	m := done(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m")))
	if *m.Usage != (contract.Usage{Input: 5, Output: 9, CacheRead: 4}) {
		t.Fatalf("usage %+v", m.Usage)
	}
}

// AD-FR-11: redacted_thinking 은 opaque 블록으로 받고 그대로 되돌려 보낸다.
func TestAnthropicRedactedThinking(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", sseLines(anthropicText,
		`{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"ZZZ"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`, `{"type":"message_stop"}`))
	c := client(t, f, "anthropic", "")
	m := done(t, collect(t, c, simple("anthropic", "m")))
	b := m.Content[0]
	if b.Kind != contract.BlockOpaque || b.Opaque.Kind != "redacted_thinking" || string(b.Opaque.Payload) != "ZZZ" || b.Opaque.Provider != "anthropic" {
		t.Fatalf("block %+v", b)
	}
	req := simple("anthropic", "m")
	m.ID = "a1"
	req.Messages = append(req.Messages, m, user("u2", "again"))
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	collect(t, c, req)
	if !strings.Contains(string(f.body("/v1/messages")), `{"data":"ZZZ","type":"redacted_thinking"}`) &&
		!strings.Contains(string(f.body("/v1/messages")), `{"type":"redacted_thinking","data":"ZZZ"}`) {
		t.Fatalf("redacted not replayed: %s", f.body("/v1/messages"))
	}
}

// AD-FR-04: 다른 모델의 서명·서명 없는 thinking 은 text 로, 빈 블록·빈 메시지는 빼고 (AD-FR-05), id 는 정규화 (AD-FR-03).
func TestAnthropicMessageConversion(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	call := contract.ToolCall{ID: "call.1|x", Name: "read", Args: json.RawMessage(`{}`)}
	req := simple("anthropic", "claude-x")
	req.Messages = []contract.Message{
		user("u1", "q"),
		{ID: "a1", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{
			{Kind: contract.BlockThinking, Text: "other model", ProviderData: &contract.ProviderData{Provider: "anthropic", Model: "claude-y", Payload: []byte("sig")}},
			{Kind: contract.BlockThinking, Text: "unsigned"},
			text(""),
			{Kind: contract.BlockToolCall, ToolCall: &call},
		}},
		result("r1", "call.1|x", "one", contract.StatusOK),
		result("r2", "call.1|x", "two", contract.StatusError, contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1, 2}}}),
		{ID: "e1", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{text("")}},
		user("u2", "next"),
	}
	collect(t, client(t, f, "anthropic", ""), req)
	body := jsonOf(t, f.body("/v1/messages"))
	msgs := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages %d: %v", len(msgs), msgs)
	}
	asst := msgs[1].(map[string]any)["content"].([]any)
	if len(asst) != 3 || asst[0].(map[string]any)["type"] != "text" || asst[0].(map[string]any)["text"] != "other model" ||
		asst[1].(map[string]any)["text"] != "unsigned" || asst[2].(map[string]any)["id"] != "call_1_x" {
		t.Fatalf("assistant %v", asst)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 {
		t.Fatalf("tool results not grouped (AD-FR-10): %v", msgs[2])
	}
	r2 := results[1].(map[string]any)
	inner := r2["content"].([]any)
	if r2["tool_use_id"] != "call_1_x" || r2["is_error"] != true || len(inner) != 2 || inner[1].(map[string]any)["type"] != "image" {
		t.Fatalf("result %v", r2)
	}
	src := inner[1].(map[string]any)["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "AQI=" {
		t.Fatalf("image source %v", src)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatal("thinking sent without Options.Thinking")
	}
}

// AD-FR-13, 13a
func TestAnthropicThinkingAndLimits(t *testing.T) {
	modelInfo := func(adaptive, enabled bool, max int) string {
		return fmt.Sprintf(`{"id":"m","max_input_tokens":200000,"max_tokens":%d,"capabilities":{"image_input":{"supported":true},"thinking":{"supported":true,"types":{"adaptive":{"supported":%v},"enabled":{"supported":%v}}}}}`, max, adaptive, enabled)
	}
	cases := []struct {
		name     string
		info     string
		thinking string
		maxOut   int
		extra    map[string]json.RawMessage
		wantErr  contract.ErrorClass
		check    func(map[string]any) bool
	}{
		{"adaptive", modelInfo(true, false, 64000), "high", 100, nil, "", func(b map[string]any) bool {
			return b["thinking"].(map[string]any)["type"] == "adaptive" && b["output_config"].(map[string]any)["effort"] == "high"
		}},
		{"unknown capability -> adaptive", "", "max", 100, nil, "", func(b map[string]any) bool {
			return b["thinking"].(map[string]any)["type"] == "adaptive"
		}},
		{"budget only -> error", modelInfo(false, true, 64000), "low", 100, nil, contract.ClassInvalidRequest, nil},
		{"budget via extra", modelInfo(false, true, 64000), "", 100, map[string]json.RawMessage{"thinking": json.RawMessage(`{"type":"enabled","budget_tokens":2048}`)}, "", func(b map[string]any) bool {
			return b["thinking"].(map[string]any)["budget_tokens"] == float64(2048) && b["output_config"] == nil
		}},
		{"unknown level", "", "extreme", 100, nil, contract.ClassInvalidRequest, nil},
		{"max tokens from catalog", modelInfo(true, false, 64000), "", 0, nil, "", func(b map[string]any) bool {
			return b["max_tokens"] == float64(64000)
		}},
		{"max tokens unknown", "", "", 0, nil, contract.ClassInvalidRequest, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.sseText("/v1/messages", anthropicReply("end_turn"))
			if c.info != "" {
				f.on("GET", "/v1/models/m", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(c.info)) })
			}
			req := simple("anthropic", "m")
			req.Options.Thinking, req.Options.MaxOutputTokens, req.Options.Extra = c.thinking, c.maxOut, c.extra
			evs := collect(t, client(t, f, "anthropic", `{"lookup":"anthropic"}`), req)
			if c.wantErr != "" {
				if ce := failed(t, evs); ce.Class != c.wantErr {
					t.Fatalf("class %s", ce.Class)
				}
				if f.body("/v1/messages") != nil {
					t.Fatal("request sent")
				}
				return
			}
			if b := jsonOf(t, f.body("/v1/messages")); !c.check(b) {
				t.Fatalf("body %v", b)
			}
		})
	}
}

// AD-FR-12: 캐시 힌트는 가리키는 위치의 마지막 블록에.
func TestAnthropicCacheHints(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	req := simple("anthropic", "m")
	req.System = []contract.Block{text("a"), text("b")}
	req.Messages = []contract.Message{{ID: "u1", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{text("x"), text("y")}}}
	req.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheSystem, Index: 0}, {Target: contract.CacheMessage, Index: 0}}
	collect(t, client(t, f, "anthropic", ""), req)
	b := jsonOf(t, f.body("/v1/messages"))
	sys := b["system"].([]any)
	if sys[0].(map[string]any)["cache_control"] == nil || sys[1].(map[string]any)["cache_control"] != nil {
		t.Fatalf("system %v", sys)
	}
	blocks := b["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if blocks[0].(map[string]any)["cache_control"] != nil || blocks[1].(map[string]any)["cache_control"] == nil {
		t.Fatalf("message %v", blocks)
	}
}

// ToolChoice 는 그대로 옮긴다 (거부는 provider 몫).
func TestAnthropicToolChoiceAndExtra(t *testing.T) {
	for mode, want := range map[contract.ToolChoiceMode]string{
		contract.ToolChoiceAuto: `{"type":"auto"}`, contract.ToolChoiceNone: `{"type":"none"}`,
		contract.ToolChoiceRequired: `{"type":"any"}`, contract.ToolChoiceTool: `{"name":"read","type":"tool"}`,
	} {
		f := newFake(t)
		f.sseText("/v1/messages", anthropicReply("end_turn"))
		req := simple("anthropic", "m")
		req.Tools = []contract.ToolSpec{{Name: "read", InputSchema: readSchema}}
		req.Options.ToolChoice = &contract.ToolChoice{Mode: mode}
		if mode == contract.ToolChoiceTool {
			req.Options.ToolChoice.Name = "read"
		}
		req.Options.Extra = map[string]json.RawMessage{"temperature": json.RawMessage(`0.2`), "max_tokens": json.RawMessage(`7`)}
		collect(t, client(t, f, "anthropic", ""), req)
		b := jsonOf(t, f.body("/v1/messages"))
		got, _ := json.Marshal(b["tool_choice"])
		if string(got) != want || b["temperature"] != 0.2 || b["max_tokens"] != float64(7) {
			t.Errorf("%s: tool_choice %s temperature %v max %v", mode, got, b["temperature"], b["max_tokens"])
		}
	}
}

// 스트림 도중 error 이벤트.
func TestAnthropicStreamError(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", sseLines(anthropicText,
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	ce := failed(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m")))
	if ce.Class != contract.ClassOverloaded {
		t.Fatalf("class %+v", ce)
	}
}
