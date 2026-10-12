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
