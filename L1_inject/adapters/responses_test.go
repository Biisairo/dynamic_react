package adapters_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func responsesScenario() contract.Request {
	think := &contract.ProviderData{Provider: "openai", Model: "gpt-5", Payload: []byte(`{"id":"rs_01","encrypted_content":"gAAAAABo..."}`)}
	call := &contract.ProviderData{Provider: "openai", Model: "gpt-5", Payload: []byte(`{"id":"fc_01"}`)}
	return scenario("openai", "gpt-5", "call_abc", think, call)
}

func respEvents(evs ...string) string {
	var b strings.Builder
	for _, e := range evs {
		var m map[string]any
		json.Unmarshal([]byte(e), &m)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", m["type"], e)
	}
	return b.String()
}

func respText(final string) string {
	return respEvents(
		`{"type":"response.created","response":{"id":"resp_2","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"hi"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}`,
		final)
}

// AD-FR-40~44, AD-NFR-01: 05 문서 4.1
func TestResponsesGoldenRequest(t *testing.T) {
	f := newFake(t)
	f.sse("/responses", "responses_stream.sse")
	collect(t, client(t, f, "openai", ""), responsesScenario())
	sameJSON(t, f.body("/responses"), "responses_request.json")
	if f.header("/responses").Get("Authorization") != "Bearer "+testKey {
		t.Fatal("auth header")
	}
}

// AD-FR-45~47: 05 문서 4.2
func TestResponsesStream(t *testing.T) {
	f := newFake(t)
	f.sse("/responses", "responses_stream.sse")
	evs := collect(t, client(t, f, "openai", ""), responsesScenario())
	if k := kinds(evs); k != "start,thinking_delta,toolcall_start,toolcall_delta,toolcall_delta,toolcall_end,usage,done" {
		t.Fatalf("kinds %s", k)
	}
	m := done(t, evs)
	if m.ID != "resp_01" || len(m.Content) != 2 {
		t.Fatalf("message %+v", m)
	}
	th := m.Content[0]
	var pd map[string]string
	json.Unmarshal(th.ProviderData.Payload, &pd)
	if th.Kind != contract.BlockThinking || th.Text != "config.py 를 읽어야 한다." || pd["id"] != "rs_01" || pd["encrypted_content"] != "gAAAAABo..." ||
		th.ProviderData.Model != "gpt-5" {
		t.Fatalf("thinking %+v %s", th, th.ProviderData.Payload)
	}
	tc := m.Content[1]
	json.Unmarshal(tc.ProviderData.Payload, &pd)
	if tc.ToolCall.ID != "call_abc" || string(tc.ToolCall.Args) != `{"path":"config.py"}` || pd["id"] != "fc_01" {
		t.Fatalf("call %+v %s", tc.ToolCall, tc.ProviderData.Payload)
	}
	if *m.Usage != (contract.Usage{Input: 412, CacheRead: 2048, Output: 87, Reasoning: 64}) || m.StopReason.Kind != contract.StopToolUse {
		t.Fatalf("usage %+v stop %+v", m.Usage, m.StopReason)
	}
}

func TestResponsesEndings(t *testing.T) {
	cases := []struct {
		name, final string
		stop        contract.StopKind
		class       contract.ErrorClass
		msg         string
	}{
		{"completed text", `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":3,"output_tokens":1}}}`, contract.StopStop, "", ""},
		{"incomplete length", `{"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, contract.StopLength, "", ""},
		{"incomplete filter", `{"type":"response.incomplete","response":{"id":"r","status":"incomplete","incomplete_details":{"reason":"content_filter"}}}`, contract.StopError, "", ""},
		{"failed overflow", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"context_length_exceeded","message":"too long"}}}`, "", contract.ClassOverflow, ""},
		{"failed rate", `{"type":"response.failed","response":{"id":"r","status":"failed","error":{"code":"rate_limit_exceeded","message":"slow"}}}`, "", contract.ClassRateLimit, ""},
		{"error event", `{"type":"error","code":"server_error","message":"boom"}`, "", contract.ClassUnknown, "boom"},
		{"error event nested", `{"type":"error","error":{"type":"insufficient_quota","code":"credit_balance_exhausted","message":"no credits"},"sequence_number":2}`, "", contract.ClassUnknown, "no credits"},
		{"error event nested type", `{"type":"error","error":{"type":"authentication_error","code":"invalid_api_key","message":"bad key"}}`, "", contract.ClassAuth, "bad key"},
		{"error event nested code", `{"type":"error","error":{"type":"invalid_request","code":"context_length_exceeded","message":"long"}}`, "", contract.ClassOverflow, "long"},
		{"error event nested rate", `{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`, "", contract.ClassRateLimit, "slow down"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.sseText("/responses", respText(c.final))
			evs := collect(t, client(t, f, "openai", ""), simple("openai", "gpt-5"))
			if c.class != "" {
				if ce := failed(t, evs); ce.Class != c.class || c.msg != "" && ce.Message != c.msg {
					t.Fatalf("class %+v", ce)
				}
				return
			}
			m := done(t, evs)
			if m.StopReason.Kind != c.stop || m.Content[0].Text != "hi" {
				t.Fatalf("stop %+v content %+v", m.StopReason, m.Content)
			}
		})
	}
}

func TestResponsesRefusal(t *testing.T) {
	f := newFake(t)
	f.sseText("/responses", respEvents(
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m"}}`,
		`{"type":"response.refusal.delta","output_index":0,"delta":"no"}`,
		`{"type":"response.completed","response":{"id":"r","status":"completed"}}`))
	m := done(t, collect(t, client(t, f, "openai", ""), simple("openai", "gpt-5")))
	if m.StopReason.Kind != contract.StopError || m.Content[0].Text != "no" {
		t.Fatalf("%+v %+v", m.StopReason, m.Content)
	}
}

// AD-FR-04: 다른 모델의 reasoning·item id 는 보내지 않는다. 텍스트·이미지 결과 변환 (AD-FR-43).
func TestResponsesConversion(t *testing.T) {
	f := newFake(t)
	f.sseText("/responses", respText(`{"type":"response.completed","response":{"id":"r","status":"completed"}}`))
	req := responsesScenario()
	req.Model.ID = "gpt-6"
	req.Messages = append(req.Messages,
		contract.Message{ID: "a2", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{text("done")}},
		result("r2", "call_abc", "pic", contract.StatusOK, contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}))
	collect(t, client(t, f, "openai", `{"developerRole":false,"encryptedReasoning":false}`), req)
	b := jsonOf(t, f.body("/responses"))
	if b["include"] != nil {
		t.Fatal("include sent")
	}
	in := b["input"].([]any)
	if in[0].(map[string]any)["role"] != "system" {
		t.Fatalf("system role %v", in[0])
	}
	for _, it := range in {
		m := it.(map[string]any)
		if m["type"] == "reasoning" {
			t.Fatal("foreign reasoning sent")
		}
		if m["type"] == "function_call" && m["id"] != nil {
			t.Fatal("foreign item id sent")
		}
	}
	asst := in[4].(map[string]any)
	if asst["type"] != "message" || asst["role"] != "assistant" || asst["content"].([]any)[0].(map[string]any)["type"] != "output_text" {
		t.Fatalf("assistant %v", asst)
	}
	out := in[5].(map[string]any)["output"].([]any)
	if out[0].(map[string]any)["type"] != "input_text" || out[1].(map[string]any)["image_url"] != "data:image/png;base64,AQ==" {
		t.Fatalf("image output %v", out)
	}
}
