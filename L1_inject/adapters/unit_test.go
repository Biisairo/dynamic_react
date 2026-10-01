package adapters

import (
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func TestReadSSE(t *testing.T) {
	in := ": comment\n" +
		"event: message_start\n" +
		"data: {\"a\":1}\n\n" +
		"data: line1\n" +
		"data: line2\n\n" +
		"data:nospace\r\n\r\n" +
		"event: ping\n\n" +
		"data: [DONE]\n\n" +
		"data: {\"tail\":true}"
	var got []string
	err := readSSE(strings.NewReader(in), func(event, data string) bool {
		got = append(got, event+"|"+data)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"message_start|{\"a\":1}", "|line1\nline2", "|nospace", "|[DONE]", "|{\"tail\":true}"}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("got %q", got)
	}
	// 콜백이 false 면 멈춘다.
	n := 0
	readSSE(strings.NewReader("data: 1\n\ndata: 2\n\n"), func(string, string) bool { n++; return false })
	if n != 1 {
		t.Fatalf("did not stop: %d", n)
	}
}

// AD-FR-32
func TestClassifyHTTP(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "7")
	cases := []struct {
		status int
		header http.Header
		body   string
		class  contract.ErrorClass
	}{
		{429, h, `{}`, contract.ClassRateLimit},
		{529, nil, `overloaded`, contract.ClassOverloaded},
		{503, nil, ``, contract.ClassOverloaded},
		{401, nil, ``, contract.ClassAuth},
		{403, nil, ``, contract.ClassAuth},
		{413, nil, ``, contract.ClassPayloadTooLarge},
		{400, nil, `{"error":{"message":"prompt is too long: 250000 tokens > 200000 maximum"}}`, contract.ClassOverflow},
		{400, nil, `This model's MAXIMUM CONTEXT LENGTH is 128000`, contract.ClassOverflow},
		{400, nil, `{"error":{"code":"context_length_exceeded"}}`, contract.ClassOverflow},
		{400, nil, `bad field`, contract.ClassInvalidRequest},
		{404, nil, ``, contract.ClassInvalidRequest},
		{422, nil, ``, contract.ClassInvalidRequest},
		{500, nil, ``, contract.ClassTransientNetwork},
		{502, nil, ``, contract.ClassTransientNetwork},
		{418, nil, ``, contract.ClassUnknown},
	}
	for _, c := range cases {
		ce := classifyHTTP(c.status, c.header, []byte(c.body), nil)
		if ce.Class != c.class || ce.Status != c.status {
			t.Errorf("%d %q: %+v", c.status, c.body, ce)
		}
	}
	if ce := classifyHTTP(429, h, nil, nil); ce.RetryAfter != 7*time.Second {
		t.Errorf("retry-after %v", ce.RetryAfter)
	}
	h2 := http.Header{}
	h2.Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
	if ce := classifyHTTP(429, h2, nil, nil); ce.RetryAfter <= 0 || ce.RetryAfter > 4*time.Second {
		t.Errorf("retry-after date %v", ce.RetryAfter)
	}
}

// AD-FR-33: 목록은 한 곳에 있고 고정된다.
func TestOverflowPhrases(t *testing.T) {
	want := []string{"prompt is too long", "maximum context length", "context_length_exceeded", "context length exceeded",
		"prompt too long; exceeded", "input is too long", "too many tokens"}
	if strings.Join(overflowPhrases(), "|") != strings.Join(want, "|") {
		t.Fatalf("phrases %v", overflowPhrases())
	}
}

// AD-FR-34
func TestMaskSecrets(t *testing.T) {
	key := "sk-ant-api03-SECRETSECRET"
	body := `{"error":"invalid x-api-key sk-ant-api03-SECRETSECRET","auth":"Bearer sk-ant-api03-SECRETSECRET"}`
	ce := classifyHTTP(401, nil, []byte(body), []string{key})
	if strings.Contains(ce.Message+ce.Raw, "SECRET") {
		t.Fatalf("key leaked: %+v", ce)
	}
	err := transportError(errors.New("Get https://x?key="+key+": timeout"), []string{key})
	if strings.Contains(err.Message, "SECRET") || err.Class != contract.ClassTransientNetwork {
		t.Fatalf("transport %+v", err)
	}
	if mask("abc", nil) != "abc" || mask("x", []string{""}) != "x" {
		t.Fatal("mask with empty key")
	}
}

// AD-FR-03
func TestNormalizeID(t *testing.T) {
	cases := map[string]string{
		"toolu_01A":             "toolu_01A",
		"call.abc|fc/1":         "call_abc_fc_1",
		"한글":                    "__",
		strings.Repeat("a", 80): strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := normalizeID(in); got != want {
			t.Errorf("normalizeID(%q) = %q", in, got)
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(normalizeID("x y z")) {
		t.Fatal("invalid chars kept")
	}
}

// AD-FR-30, R12: 내장 프로필 표
func TestBuiltinProfiles(t *testing.T) {
	ps := BuiltinProfiles()
	want := map[string][3]string{
		"anthropic":         {"anthropic-messages", "https://api.anthropic.com", "ANTHROPIC_API_KEY"},
		"minimax":           {"anthropic-messages", "https://api.minimax.io/anthropic", "MINIMAX_API_KEY"},
		"minimax-cn":        {"anthropic-messages", "https://api.minimaxi.com/anthropic", "MINIMAX_CN_API_KEY"},
		"fireworks":         {"anthropic-messages", "https://api.fireworks.ai/inference", "FIREWORKS_API_KEY"},
		"vercel-ai-gateway": {"anthropic-messages", "https://ai-gateway.vercel.sh", "AI_GATEWAY_API_KEY"},
		"openai":            {"openai-responses", "https://api.openai.com/v1", "OPENAI_API_KEY"},
		"xai":               {"openai-responses", "https://api.x.ai/v1", "XAI_API_KEY"},
		"deepseek":          {"openai-chat", "https://api.deepseek.com", "DEEPSEEK_API_KEY"},
		"zai":               {"openai-chat", "https://api.z.ai/api/paas/v4", "ZAI_API_KEY"},
		"xiaomi":            {"openai-chat", "https://api.xiaomimimo.com/v1", "XIAOMI_API_KEY"},
		"moonshotai":        {"openai-chat", "https://api.moonshot.ai/v1", "MOONSHOT_API_KEY"},
		"moonshotai-cn":     {"openai-chat", "https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
		"openrouter":        {"openai-chat", "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
		"groq":              {"openai-chat", "https://api.groq.com/openai/v1", "GROQ_API_KEY"},
		"cerebras":          {"openai-chat", "https://api.cerebras.ai/v1", "CEREBRAS_API_KEY"},
		"together":          {"openai-chat", "https://api.together.ai/v1", "TOGETHER_API_KEY"},
		"nvidia":            {"openai-chat", "https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY"},
		"huggingface":       {"openai-chat", "https://router.huggingface.co/v1", "HF_TOKEN"},
		"baseten":           {"openai-chat", "https://inference.baseten.co/v1", "BASETEN_API_KEY"},
		"ant-ling":          {"openai-chat", "https://api.ant-ling.com/v1", "ANT_LING_API_KEY"},
		"vllm":              {"openai-chat", "http://localhost:8000/v1", ""},
		"llamacpp":          {"openai-chat", "http://localhost:8080/v1", ""},
		"ollama":            {"openai-chat", "http://localhost:11434/v1", ""},
	}
	if len(ps) != len(want) {
		t.Fatalf("%d profiles, want %d", len(ps), len(want))
	}
	for name, w := range want {
		p, ok := ps[name]
		if !ok || string(p.API) != w[0] || p.BaseURL != w[1] || p.RequiresKey != (w[2] != "") {
			t.Errorf("%s: %+v", name, p)
		}
	}
	lookups := map[string]LookupKind{"anthropic": LookupAnthropic, "openrouter": LookupOpenRouter, "ollama": LookupOllama,
		"llamacpp": LookupLlamaCpp, "vllm": LookupVLLM, "deepseek": LookupNone, "openai": LookupNone}
	for name, l := range lookups {
		if ps[name].Lookup != l {
			t.Errorf("%s lookup %s", name, ps[name].Lookup)
		}
	}
	compat := map[string]struct {
		dev      bool
		maxField string
		think    ThinkingFormat
		reason   string
	}{
		"deepseek":   {false, "max_tokens", ThinkDeepSeek, "reasoning_content"},
		"zai":        {false, "max_tokens", ThinkZai, ""},
		"xiaomi":     {true, "max_completion_tokens", ThinkOpenAI, ""},
		"moonshotai": {false, "max_tokens", ThinkNone, ""},
		"openrouter": {false, "max_completion_tokens", ThinkOpenRouter, ""},
		"groq":       {true, "max_completion_tokens", ThinkOpenAI, ""},
		"cerebras":   {false, "max_completion_tokens", ThinkOpenAI, ""},
		"together":   {false, "max_tokens", ThinkTogether, ""},
		"nvidia":     {false, "max_tokens", ThinkNone, ""},
		"ant-ling":   {false, "max_tokens", ThinkAntLing, ""},
		"ollama":     {false, "max_tokens", ThinkNone, ""},
	}
	for name, c := range compat {
		p := ps[name]
		if p.DeveloperRole != c.dev || p.MaxTokensField != c.maxField || p.Thinking != c.think || p.ReasoningField != c.reason || !p.UsageInStreaming {
			t.Errorf("%s compat %+v", name, p)
		}
	}
	if !ps["openai"].EncryptedReasoning || ps["xai"].EncryptedReasoning || !ps["openai"].DeveloperRole {
		t.Error("responses compat")
	}
	ps["anthropic"] = Profile{}
	if BuiltinProfiles()["anthropic"].BaseURL == "" {
		t.Fatal("BuiltinProfiles shares state")
	}
}

// AD-C1, C2: 구독·OAuth 경로가 코드에 없다.
func TestNoSubscriptionPaths(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	banned := regexp.MustCompile(`(?i)oauth|ANTHROPIC_AUTH_TOKEN|chatgpt\.com|githubcopilot|coding/paas|token-plan|api\.kimi\.com|getenv|_API_KEY|HF_TOKEN`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if loc := banned.FindIndex(b); loc != nil {
			t.Errorf("%s contains %q", f, b[loc[0]:loc[1]])
		}
	}
}

// AD-FR-49, AD-C2
func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{
		"providers": {"mine": {"api": "openai-chat", "baseUrl": "http://x/v1", "requiresKey": true, "developerRole": true},
		              "anthropic": {"baseUrl": "http://proxy"}},
		"models": {"mine/m1": {"contextWindow": 32000, "maxOutputTokens": 4096, "capabilities": {"vision": true}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["mine"].API == nil || *cfg.Providers["mine"].API != APIChat || !*cfg.Providers["mine"].RequiresKey || *cfg.Models["mine/m1"].ContextWindow != 32000 {
		t.Fatalf("cfg %+v", cfg)
	}
	for name, bad := range map[string]string{
		"unknown top key":      `{"catalog": {}}`,
		"unknown provider key": `{"providers": {"x": {"apiKey": "sk-123"}}}`,
		"env name field":       `{"providers": {"anthropic": {"apiKeyEnv": "K"}}}`,
		"unknown model key":    `{"models": {"a/b": {"window": 1}}}`,
		"bad api":              `{"providers": {"x": {"api": "gemini"}}}`,
		"bad model key":        `{"models": {"noslash": {}}}`,
		"not json":             `{`,
	} {
		if _, err := ParseConfig([]byte(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestProfileOverride(t *testing.T) {
	cfg, _ := ParseConfig([]byte(`{"providers": {"anthropic": {"baseUrl": "http://proxy"}, "mine": {"api": "openai-chat", "baseUrl": "http://m"}}}`))
	c := New(Options{Config: cfg})
	p, ok := c.Profile("anthropic")
	if !ok || p.BaseURL != "http://proxy" || !p.RequiresKey || p.API != APIAnthropic {
		t.Fatalf("patched %+v", p)
	}
	m, ok := c.Profile("mine")
	if !ok || m.API != APIChat || m.MaxTokensField != "max_completion_tokens" || m.Thinking != ThinkOpenAI || !m.UsageInStreaming || m.RequiresKey {
		t.Fatalf("new provider defaults %+v", m)
	}
	if _, ok := c.Profile("nope"); ok {
		t.Fatal("unknown provider")
	}
	cfg, _ = ParseConfig([]byte(`{"providers": {"ollama": {"requiresKey": true}, "anthropic": {"requiresKey": false}}}`))
	c = New(Options{Config: cfg})
	if o, _ := c.Profile("ollama"); !o.RequiresKey {
		t.Fatal("requiresKey true not applied")
	}
	if a, _ := c.Profile("anthropic"); a.RequiresKey {
		t.Fatal("requiresKey false not applied")
	}
	if _, err := ParseConfig([]byte(`{"providers": {"nobase": {"api": "openai-chat"}}}`)); err == nil {
		t.Fatal("new provider without baseUrl accepted")
	}
}

// AD-FR-36c: 스냅샷은 내장 프로필 provider 만 담는다.
func TestSnapshotScope(t *testing.T) {
	var v struct {
		Models map[string]map[string]any
	}
	if err := json.Unmarshal(snapshotJSON, &v); err != nil || len(v.Models) == 0 {
		t.Fatalf("snapshot %v", err)
	}
	ps := BuiltinProfiles()
	for p := range v.Models {
		if _, ok := ps[p]; !ok {
			t.Errorf("snapshot provider %q has no profile", p)
		}
	}
}

// AD-C6: 테스트가 아닌 코드는 os 를 import 하지 않는다 (internal/ 생성기 제외).
func TestNoUserEnvironmentImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob %v %v", files, err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		pf, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range pf.Imports {
			if im.Path.Value == `"os"` {
				t.Errorf("%s imports os", f)
			}
		}
	}
}

// AD-C5
func TestImportsOnlyStdlibAndContract(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") &&
			p != "github.com/Biisairo/dynamic_react/L1_inject/adapters" && p != "github.com/Biisairo/dynamic_react/L0_core/contract" {
			t.Errorf("forbidden dependency %s", p)
		}
	}
}
