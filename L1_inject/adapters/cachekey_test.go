package adapters_test

import (
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func withKey(req contract.Request, key string) contract.Request {
	req.Options.CacheKey = key
	return req
}

func cacheKeyOf(t *testing.T, body []byte) (string, bool) {
	t.Helper()
	v, ok := jsonOf(t, body)["prompt_cache_key"]
	if !ok {
		return "", false
	}
	return v.(string), true
}

// AD-FR-56: Responses·Chat 은 플래그가 켜져 있고 키가 있을 때만 보내고, 64 rune 으로 자른다. Anthropic 은 보내지 않는다.
func TestPromptCacheKey(t *testing.T) {
	long := strings.Repeat("가", 70)
	resp := func(patch, key string) (string, bool) {
		f := newFake(t)
		f.sse("/responses", "responses_stream.sse")
		collect(t, client(t, f, "openai", patch), withKey(responsesScenario(), key))
		return cacheKeyOf(t, f.body("/responses"))
	}
	if k, ok := resp("", "sess-1"); !ok || k != "sess-1" {
		t.Fatalf("openai builtin %q %v", k, ok)
	}
	if k, _ := resp("", long); k != strings.Repeat("가", 64) {
		t.Fatalf("clamp %d runes", len([]rune(k)))
	}
	if _, ok := resp("", ""); ok {
		t.Fatal("empty key sent")
	}
	if _, ok := resp(`{"promptCacheKey":false}`, "sess-1"); ok {
		t.Fatal("disabled flag sent key")
	}

	chat := func(patch string) (string, bool) {
		f := newFake(t)
		f.sseText("/chat/completions", chatText("stop", ""))
		collect(t, client(t, f, "golden", patch), withKey(simple("golden", "m"), "sess-2"))
		return cacheKeyOf(t, f.body("/chat/completions"))
	}
	if _, ok := chat(goldenChat); ok {
		t.Fatal("chat without flag sent key")
	}
	if k, ok := chat(strings.TrimSuffix(goldenChat, "}") + `,"promptCacheKey":true}`); !ok || k != "sess-2" {
		t.Fatalf("chat flag %q %v", k, ok)
	}

	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	collect(t, client(t, f, "anthropic", `{"promptCacheKey":true}`), withKey(simple("anthropic", "m"), "sess-3"))
	if _, ok := cacheKeyOf(t, f.body("/v1/messages")); ok {
		t.Fatal("anthropic sent prompt_cache_key")
	}
}
