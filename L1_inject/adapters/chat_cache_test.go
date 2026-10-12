package adapters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

func cachePatch(kind string) string {
	return `{"api":"openai-chat","requiresKey":true,"maxTokensField":"max_tokens","thinking":"openai","cacheControl":"` + kind + `"}`
}

var ephemeralMark = map[string]any{"type": "ephemeral"}

// cacheReq 는 [u1 q, a1 looking+call, r1 ok, a2 call만, r2 이미지 결과] 와 힌트다.
func cacheReq(model string, hints ...contract.CacheHint) contract.Request {
	req := simple("golden", model)
	req.System = []contract.Block{text("sys")}
	call := func(id string) contract.Block {
		return contract.Block{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: id, Name: "shot", Args: json.RawMessage(`{}`)}}
	}
	req.Messages = []contract.Message{
		user("u1", "q"),
		{ID: "a1", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{text("looking"), call("c1")}},
		result("r1", "c1", "ok", contract.StatusOK),
		{ID: "a2", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{call("c2")}},
		result("r2", "c2", "", contract.StatusOK, contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}),
	}
	req.Options.CacheHints = hints
	return req
}

// marked 는 Chat 메시지마다 cache_control 이 붙은 text 파트 수다. system 은 "sys".
func marked(t *testing.T, body []byte) []int {
	t.Helper()
	var out []int
	for _, m := range jsonOf(t, body)["messages"].([]any) {
		n := 0
		if parts, ok := m.(map[string]any)["content"].([]any); ok {
			for _, p := range parts {
				pm := p.(map[string]any)
				if cc, ok := pm["cache_control"].(map[string]any); ok && pm["type"] == "text" && cc["type"] == ephemeralMark["type"] {
					n++
				}
			}
		}
		out = append(out, n)
	}
	return out
}

func send(t *testing.T, kind string, req contract.Request) []byte {
	t.Helper()
	f := newFake(t)
	f.sseText("/chat/completions", chatText("stop", ""))
	collect(t, client(t, f, "golden", cachePatch(kind)), req)
	return f.body("/chat/completions")
}

// AD-FR-54, 55
func TestChatCacheControl(t *testing.T) {
	sys := contract.CacheHint{Target: contract.CacheSystem, Index: 0}
	msg := func(i int) contract.CacheHint { return contract.CacheHint{Target: contract.CacheMessage, Index: i} }
	// chat 메시지: 0 system, 1 user q, 2 assistant looking, 3 tool ok, 4 assistant(call), 5 tool(image), 6 user(image)
	cases := []struct {
		name, kind, model string
		hints             []contract.CacheHint
		want              string
	}{
		{"off", "", "m", []contract.CacheHint{sys, msg(2)}, "0000000"},
		{"system and tool result", "anthropic", "m", []contract.CacheHint{sys, msg(2)}, "1001000"},
		{"user string", "anthropic", "m", []contract.CacheHint{msg(0)}, "0100000"},
		{"call only falls back", "anthropic", "m", []contract.CacheHint{msg(3)}, "0001000"},
		{"image result → last chat message", "anthropic", "m", []contract.CacheHint{msg(4)}, "0000001"},
		{"no hints", "anthropic", "m", nil, "0000000"},
		{"same target twice", "anthropic", "m", []contract.CacheHint{msg(2), msg(3)}, "0001000"},
		{"models: anthropic/", "anthropic-models", "anthropic/claude-x", []contract.CacheHint{sys, msg(0)}, "1100000"},
		{"models: other", "anthropic-models", "openai/gpt-x", []contract.CacheHint{sys, msg(0)}, "0000000"},
	}
	for _, c := range cases {
		body := send(t, c.kind, cacheReq(c.model, c.hints...))
		var got strings.Builder
		for _, n := range marked(t, body) {
			got.WriteByte(byte('0' + n))
		}
		if got.String() != c.want {
			t.Errorf("%s: %s, want %s\n%s", c.name, got.String(), c.want, body)
		}
	}
	// 문자열 content 는 text 파트 하나로 바뀐다
	body := send(t, "anthropic", cacheReq("m", sys, msg(0)))
	msgs := jsonOf(t, body)["messages"].([]any)
	for _, i := range []int{0, 1} {
		parts := msgs[i].(map[string]any)["content"].([]any)
		if len(parts) != 1 || parts[0].(map[string]any)["text"] == "" {
			t.Fatalf("message %d parts %v", i, parts)
		}
	}
	if msgs[2].(map[string]any)["content"] != "looking" {
		t.Fatalf("unmarked message changed %v", msgs[2])
	}
}

// AD-FR-55: 이미지가 있는 user 메시지는 마지막 text 파트에 붙는다.
func TestChatCacheControlImageUser(t *testing.T) {
	req := simple("golden", "m")
	req.System = []contract.Block{text("sys")}
	req.Messages = []contract.Message{{ID: "u1", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{
		text("first"), {Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}, text("second")}}}
	req.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheMessage, Index: 0}}
	body := send(t, "anthropic", req)
	parts := jsonOf(t, body)["messages"].([]any)[1].(map[string]any)["content"].([]any)
	lastText := parts[len(parts)-1].(map[string]any)
	if lastText["text"] != "second" || lastText["cache_control"] == nil || parts[0].(map[string]any)["cache_control"] != nil {
		t.Fatalf("parts %v", parts)
	}
}

// AD-FR-54: 모르는 cacheControl 값은 설정 에러, 내장 openrouter 는 설정으로 끌 수 있다.
func TestChatCacheControlConfig(t *testing.T) {
	if _, err := adapters.ParseConfig([]byte(`{"providers":{"openrouter":{"cacheControl":"bogus"}}}`)); err == nil {
		t.Fatal("unknown cacheControl accepted")
	}
	cfg, err := adapters.ParseConfig([]byte(`{"providers":{"openrouter":{"cacheControl":""}}}`))
	if err != nil || cfg.Providers["openrouter"].CacheControl == nil || *cfg.Providers["openrouter"].CacheControl != "" {
		t.Fatalf("override %v %+v", err, cfg.Providers["openrouter"])
	}
}
