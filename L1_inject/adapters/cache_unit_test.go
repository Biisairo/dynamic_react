package adapters

import "testing"

// AD-FR-54: 내장 openrouter 만 anthropic-models, 나머지는 끔.
func TestBuiltinCacheControl(t *testing.T) {
	for name, p := range BuiltinProfiles() {
		want := CacheControlKind("")
		if name == "openrouter" {
			want = CacheControlAnthropicModels
		}
		if p.CacheControl != want {
			t.Errorf("%s cacheControl %q", name, p.CacheControl)
		}
	}
}

// AD-FR-56: 내장 프로필 중 openai 만 PromptCacheKey.
func TestBuiltinPromptCacheKey(t *testing.T) {
	for name, p := range BuiltinProfiles() {
		if p.PromptCacheKey != (name == "openai") {
			t.Errorf("%s promptCacheKey %v", name, p.PromptCacheKey)
		}
	}
}
