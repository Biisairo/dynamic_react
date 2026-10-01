package adapters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

const goldenChat = `{"api":"openai-chat","requiresKey":true,"developerRole":true,"maxTokensField":"max_completion_tokens","thinking":"openai"}`

func chatChunks(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: " + c + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func chatText(finish string, usage string) string {
	f := `null`
	if finish != "" {
		f = `"` + finish + `"`
	}
	chunks := []string{
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"he"},"finish_reason":null}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":` + f + `}]}`,
	}
	if usage != "" {
		chunks = append(chunks, usage)
	}
	return chatChunks(chunks...)
}

// AD-FR-17~22, AD-NFR-01: 05 문서 3.1
func TestChatGoldenRequest(t *testing.T) {
	f := newFake(t)
	f.sse("/chat/completions", "chat_stream.sse")
	c := client(t, f, "golden", goldenChat)
	collect(t, c, scenario("golden", "gpt-5", "call_abc", nil, nil))
	sameJSON(t, f.body("/chat/completions"), "chat_request.json")
	if h := f.header("/chat/completions"); h.Get("Authorization") != "Bearer "+testKey || h.Get("x-api-key") != "" {
		t.Fatalf("headers %v", h)
	}
}

// AD-FR-25~27: 05 문서 3.2
func TestChatStream(t *testing.T) {
	f := newFake(t)
	f.sse("/chat/completions", "chat_stream.sse")
	evs := collect(t, client(t, f, "golden", goldenChat), simple("golden", "gpt-5"))
	if k := kinds(evs); k != "start,toolcall_start,toolcall_delta,toolcall_delta,toolcall_end,usage,done" {
		t.Fatalf("kinds %s", k)
	}
	m := done(t, evs)
	tc := m.Content[0].ToolCall
	if m.ID != "chatcmpl-1" || tc.ID != "call_abc" || tc.Name != "read" || string(tc.Args) != `{"path":"config.py"}` {
		t.Fatalf("message %+v %+v", m, tc)
	}
	if *m.Usage != (contract.Usage{Input: 412, CacheRead: 2048, Output: 87, Reasoning: 64}) {
		t.Fatalf("usage %+v", m.Usage)
	}
	if m.StopReason.Kind != contract.StopToolUse {
		t.Fatalf("stop %+v", m.StopReason)
	}
}

func TestChatTextAndStop(t *testing.T) {
	for raw, want := range map[string]contract.StopKind{
		"stop": contract.StopStop, "end": contract.StopStop, "": contract.StopStop, "length": contract.StopLength,
		"tool_calls": contract.StopToolUse, "function_call": contract.StopToolUse, "content_filter": contract.StopError, "odd": contract.StopError,
	} {
		f := newFake(t)
		f.sseText("/chat/completions", chatText(raw, ""))
		m := done(t, collect(t, client(t, f, "golden", goldenChat), simple("golden", "m")))
		if m.StopReason.Kind != want || m.Content[0].Text != "hello" {
			t.Errorf("%q -> %+v %+v", raw, m.StopReason, m.Content)
		}
	}
}

// AD-FR-26: usage 변형
func TestChatUsageVariants(t *testing.T) {
	cases := map[string]struct {
		chunk string
		want  contract.Usage
	}{
		"choices empty": {`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":60,"cache_write_tokens":10}}}`,
			contract.Usage{Input: 30, CacheRead: 60, CacheWrite: 10, Output: 5}},
		"deepseek": {`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":40}}`,
			contract.Usage{Input: 60, CacheRead: 40, Output: 5}},
		"kimi": {`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"cached_tokens":20}}`,
			contract.Usage{Input: 80, CacheRead: 20, Output: 5}},
		"in choice": {`{"choices":[{"index":0,"delta":{},"finish_reason":null,"usage":{"prompt_tokens":9,"completion_tokens":2}}]}`,
			contract.Usage{Input: 9, Output: 2}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake(t)
			f.sseText("/chat/completions", chatText("stop", c.chunk))
			m := done(t, collect(t, client(t, f, "golden", goldenChat), simple("golden", "m")))
			if *m.Usage != c.want {
				t.Fatalf("usage %+v", m.Usage)
			}
		})
	}
}

// AD-FR-18, 21~24: 프로필별 차이
func TestChatCompat(t *testing.T) {
	cases := []struct {
		name, patch string
		check       func(t *testing.T, b map[string]any)
	}{
		{"system role, no usage streaming, max_tokens", `{"api":"openai-chat","developerRole":false,"usageInStreaming":false,"maxTokensField":"max_tokens","thinking":"none"}`,
			func(t *testing.T, b map[string]any) {
				if b["messages"].([]any)[0].(map[string]any)["role"] != "system" || b["stream_options"] != nil || b["max_tokens"] != float64(16384) ||
					b["max_completion_tokens"] != nil || b["reasoning_effort"] != nil {
					t.Fatalf("body %v", b)
				}
			}},
		{"deepseek thinking + reasoning field", `{"api":"openai-chat","thinking":"deepseek","reasoningField":"reasoning_content"}`,
			func(t *testing.T, b map[string]any) {
				asst := b["messages"].([]any)[2].(map[string]any)
				if b["thinking"].(map[string]any)["type"] != "enabled" || b["reasoning_effort"] != "medium" || asst["reasoning_content"] != "config.py 를 읽어야 한다." {
					t.Fatalf("body %v", b)
				}
			}},
		{"zai", `{"api":"openai-chat","thinking":"zai"}`, func(t *testing.T, b map[string]any) {
			th := b["thinking"].(map[string]any)
			if th["type"] != "enabled" || th["clear_thinking"] != false || b["reasoning_effort"] != nil {
				t.Fatalf("body %v", b)
			}
		}},
		{"openrouter", `{"api":"openai-chat","thinking":"openrouter"}`, func(t *testing.T, b map[string]any) {
			if b["reasoning"].(map[string]any)["effort"] != "medium" {
				t.Fatalf("body %v", b)
			}
		}},
		{"ant-ling", `{"api":"openai-chat","thinking":"ant-ling"}`, func(t *testing.T, b map[string]any) {
			if b["reasoning"].(map[string]any)["effort"] != "medium" {
				t.Fatalf("body %v", b)
			}
		}},
		{"together", `{"api":"openai-chat","thinking":"together"}`, func(t *testing.T, b map[string]any) {
			if b["reasoning"].(map[string]any)["enabled"] != true {
				t.Fatalf("body %v", b)
			}
		}},
		{"chat-template", `{"api":"openai-chat","thinking":"chat-template"}`, func(t *testing.T, b map[string]any) {
			if b["chat_template_kwargs"].(map[string]any)["enable_thinking"] != true {
				t.Fatalf("body %v", b)
			}
		}},
		{"tool result name", `{"api":"openai-chat","toolResultName":true}`, func(t *testing.T, b map[string]any) {
			if b["messages"].([]any)[3].(map[string]any)["name"] != "read" {
				t.Fatalf("body %v", b)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.sseText("/chat/completions", chatText("stop", ""))
			collect(t, client(t, f, "p", c.patch), scenario("p", "m", "call_abc", nil, nil))
			c.check(t, jsonOf(t, f.body("/chat/completions")))
		})
	}
}

// AD-FR-24: 추론 텍스트는 세 필드 이름 모두 받는다.
func TestChatReasoningIn(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		f := newFake(t)
		f.sseText("/chat/completions", chatChunks(
			`{"id":"c","choices":[{"index":0,"delta":{"`+field+`":"think"},"finish_reason":null}]}`,
			`{"id":"c","choices":[{"index":0,"delta":{"content":"ans"},"finish_reason":"stop"}]}`))
		m := done(t, collect(t, client(t, f, "golden", goldenChat), simple("golden", "m")))
		if len(m.Content) != 2 || m.Content[0].Kind != contract.BlockThinking || m.Content[0].Text != "think" || m.Content[1].Text != "ans" {
			t.Errorf("%s: %+v", field, m.Content)
		}
	}
}

// AD-FR-20, 28: 이미지 결과 분리, 빈 assistant 제외
func TestChatImagesAndEmpty(t *testing.T) {
	f := newFake(t)
	f.sseText("/chat/completions", chatText("stop", ""))
	req := simple("golden", "m")
	req.Messages = []contract.Message{
		user("u1", "q"),
		{ID: "a0", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{text("")}},
		{ID: "a1", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{
			text("looking"), {Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: "c1", Name: "shot", Args: json.RawMessage(`{}`)}}}},
		result("r1", "c1", "", contract.StatusOK, contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}),
	}
	collect(t, client(t, f, "golden", goldenChat), req)
	msgs := jsonOf(t, f.body("/chat/completions"))["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages %v", msgs)
	}
	if a := msgs[1].(map[string]any); a["content"] != "looking" {
		t.Fatalf("assistant %v", a)
	}
	tool := msgs[2].(map[string]any)
	img := msgs[3].(map[string]any)
	if tool["role"] != "tool" || !strings.Contains(tool["content"].(string), "(see attached image)") || img["role"] != "user" {
		t.Fatalf("tool %v img %v", tool, img)
	}
	parts := img["content"].([]any)
	url := parts[len(parts)-1].(map[string]any)["image_url"].(map[string]any)["url"]
	if url != "data:image/png;base64,AQ==" {
		t.Fatalf("image url %v", url)
	}
}

// AD-FR-29: 키가 필요 없는 로컬 서버는 키가 없으면 Authorization 없이 보내고, 키가 있으면 붙인다.
func TestChatNoKey(t *testing.T) {
	for key, want := range map[string]string{"": "", "local-key": "Bearer local-key"} {
		f := newFake(t)
		f.sseText("/chat/completions", chatText("stop", ""))
		cfg, _ := adapters.ParseConfig([]byte(`{"providers":{"ollama":{"baseUrl":"` + f.URL + `"}}}`))
		c := adapters.New(adapters.Options{Config: cfg, NoRemoteCatalog: true, APIKey: func(string) string { return key }})
		done(t, collect(t, c, simple("ollama", "llama3")))
		if got := f.header("/chat/completions").Get("Authorization"); got != want {
			t.Fatalf("key %q: Authorization %q, want %q", key, got, want)
		}
	}
}

func TestChatToolChoice(t *testing.T) {
	f := newFake(t)
	f.sseText("/chat/completions", chatText("stop", ""))
	req := simple("golden", "m")
	req.Tools = []contract.ToolSpec{{Name: "read", InputSchema: readSchema}}
	req.Options.ToolChoice = &contract.ToolChoice{Mode: contract.ToolChoiceTool, Name: "read"}
	collect(t, client(t, f, "golden", goldenChat), req)
	got, _ := json.Marshal(jsonOf(t, f.body("/chat/completions"))["tool_choice"])
	if string(got) != `{"function":{"name":"read"},"type":"function"}` {
		t.Fatalf("tool_choice %s", got)
	}
}
