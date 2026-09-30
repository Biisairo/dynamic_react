package adapters

// API 는 provider 방언이다.
type API string

const (
	APIAnthropic API = "anthropic-messages"
	APIChat      API = "openai-chat"
	APIResponses API = "openai-responses"
)

func (a API) valid() bool { return a == APIAnthropic || a == APIChat || a == APIResponses }

// LookupKind 는 provider 조회 API 방식이다 (AD-FR-36).
type LookupKind string

const (
	LookupNone       LookupKind = "none"
	LookupAnthropic  LookupKind = "anthropic"
	LookupOpenRouter LookupKind = "openrouter"
	LookupOllama     LookupKind = "ollama"
	LookupLlamaCpp   LookupKind = "llamacpp"
	LookupVLLM       LookupKind = "vllm"
)

func (l LookupKind) valid() bool {
	switch l {
	case LookupNone, LookupAnthropic, LookupOpenRouter, LookupOllama, LookupLlamaCpp, LookupVLLM:
		return true
	}
	return false
}

// ThinkingFormat 은 Chat Completions 의 thinking 요청 형식이다 (AD-FR-23).
type ThinkingFormat string

const (
	ThinkNone         ThinkingFormat = "none"
	ThinkOpenAI       ThinkingFormat = "openai"
	ThinkDeepSeek     ThinkingFormat = "deepseek"
	ThinkZai          ThinkingFormat = "zai"
	ThinkOpenRouter   ThinkingFormat = "openrouter"
	ThinkAntLing      ThinkingFormat = "ant-ling"
	ThinkTogether     ThinkingFormat = "together"
	ThinkChatTemplate ThinkingFormat = "chat-template"
)

func (f ThinkingFormat) valid() bool {
	switch f {
	case ThinkNone, ThinkOpenAI, ThinkDeepSeek, ThinkZai, ThinkOpenRouter, ThinkAntLing, ThinkTogether, ThinkChatTemplate:
		return true
	}
	return false
}

// Profile 은 provider 하나의 방언·주소·키·호환 옵션이다 (AD-FR-29).
type Profile struct {
	API                API            `json:"api"`
	BaseURL            string         `json:"baseUrl"`
	APIKeyEnv          string         `json:"apiKeyEnv"`
	Lookup             LookupKind     `json:"lookup"`
	CatalogID          string         `json:"catalogId"`
	DeveloperRole      bool           `json:"developerRole"`
	UsageInStreaming   bool           `json:"usageInStreaming"`
	MaxTokensField     string         `json:"maxTokensField"`
	Thinking           ThinkingFormat `json:"thinking"`
	ReasoningField     string         `json:"reasoningField"`
	ToolResultName     bool           `json:"toolResultName"`
	EncryptedReasoning bool           `json:"encryptedReasoning"`
}

const (
	maxCompletionTokens = "max_completion_tokens"
	maxTokens           = "max_tokens"
)

// chatDefaults 는 표에 없는 Chat 호환 값의 기본이다 (AD-FR-30 compat 표).
func chatDefaults(p Profile) Profile {
	p.UsageInStreaming = true
	if p.MaxTokensField == "" {
		p.MaxTokensField = maxCompletionTokens
	}
	if p.Thinking == "" {
		p.Thinking = ThinkOpenAI
	}
	if p.Lookup == "" {
		p.Lookup = LookupNone
	}
	return p
}

func chat(base, env string, dev bool, maxField string, think ThinkingFormat) Profile {
	return chatDefaults(Profile{API: APIChat, BaseURL: base, APIKeyEnv: env, DeveloperRole: dev, MaxTokensField: maxField, Thinking: think})
}

func anthropic(base, env string, lookup LookupKind) Profile {
	return Profile{API: APIAnthropic, BaseURL: base, APIKeyEnv: env, Lookup: lookup}
}

// BuiltinProfiles 는 내장 프로필이다 (AD-FR-30, R12). 원본: pi providers/*.ts, openai-completions.ts detectCompat.
// 구독 요금제 엔드포인트는 넣지 않는다 (AD-C1).
func BuiltinProfiles() map[string]Profile {
	local := func(base string, l LookupKind) Profile {
		p := chat(base, "", false, maxTokens, ThinkNone)
		p.Lookup = l
		return p
	}
	openrouter := chat("https://openrouter.ai/api/v1", "OPENROUTER_API_KEY", false, "", ThinkOpenRouter)
	openrouter.Lookup = LookupOpenRouter
	deepseek := chat("https://api.deepseek.com", "DEEPSEEK_API_KEY", false, maxTokens, ThinkDeepSeek)
	deepseek.ReasoningField = "reasoning_content"
	return map[string]Profile{
		"anthropic":         anthropic("https://api.anthropic.com", "ANTHROPIC_API_KEY", LookupAnthropic),
		"minimax":           anthropic("https://api.minimax.io/anthropic", "MINIMAX_API_KEY", LookupNone),
		"minimax-cn":        anthropic("https://api.minimaxi.com/anthropic", "MINIMAX_CN_API_KEY", LookupNone),
		"fireworks":         anthropic("https://api.fireworks.ai/inference", "FIREWORKS_API_KEY", LookupNone),
		"vercel-ai-gateway": anthropic("https://ai-gateway.vercel.sh", "AI_GATEWAY_API_KEY", LookupNone),
		"openai":            {API: APIResponses, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY", Lookup: LookupNone, DeveloperRole: true, EncryptedReasoning: true},
		"xai":               {API: APIResponses, BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY", Lookup: LookupNone},
		"deepseek":          deepseek,
		"zai":               chat("https://api.z.ai/api/paas/v4", "ZAI_API_KEY", false, maxTokens, ThinkZai),
		"xiaomi":            chat("https://api.xiaomimimo.com/v1", "XIAOMI_API_KEY", true, "", ThinkOpenAI),
		"moonshotai":        chat("https://api.moonshot.ai/v1", "MOONSHOT_API_KEY", false, maxTokens, ThinkNone),
		"moonshotai-cn":     chat("https://api.moonshot.cn/v1", "MOONSHOT_API_KEY", false, maxTokens, ThinkNone),
		"openrouter":        openrouter,
		"groq":              chat("https://api.groq.com/openai/v1", "GROQ_API_KEY", true, "", ThinkOpenAI),
		"cerebras":          chat("https://api.cerebras.ai/v1", "CEREBRAS_API_KEY", false, "", ThinkOpenAI),
		"together":          chat("https://api.together.ai/v1", "TOGETHER_API_KEY", false, maxTokens, ThinkTogether),
		"nvidia":            chat("https://integrate.api.nvidia.com/v1", "NVIDIA_API_KEY", false, maxTokens, ThinkNone),
		"huggingface":       chat("https://router.huggingface.co/v1", "HF_TOKEN", true, "", ThinkOpenAI),
		"baseten":           chat("https://inference.baseten.co/v1", "BASETEN_API_KEY", true, "", ThinkOpenAI),
		"ant-ling":          chat("https://api.ant-ling.com/v1", "ANT_LING_API_KEY", false, maxTokens, ThinkAntLing),
		"vllm":              local("http://localhost:8000/v1", LookupVLLM),
		"llamacpp":          local("http://localhost:8080/v1", LookupLlamaCpp),
		"ollama":            local("http://localhost:11434/v1", LookupOllama),
	}
}

// apply 는 부분 덮어쓰기를 적용한다.
func (pp ProfilePatch) apply(p Profile) Profile {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setB := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	if pp.API != nil {
		p.API = *pp.API
	}
	set(&p.BaseURL, pp.BaseURL)
	set(&p.APIKeyEnv, pp.APIKeyEnv)
	if pp.Lookup != nil {
		p.Lookup = *pp.Lookup
	}
	set(&p.CatalogID, pp.CatalogID)
	setB(&p.DeveloperRole, pp.DeveloperRole)
	setB(&p.UsageInStreaming, pp.UsageInStreaming)
	set(&p.MaxTokensField, pp.MaxTokensField)
	if pp.Thinking != nil {
		p.Thinking = *pp.Thinking
	}
	set(&p.ReasoningField, pp.ReasoningField)
	setB(&p.ToolResultName, pp.ToolResultName)
	setB(&p.EncryptedReasoning, pp.EncryptedReasoning)
	return p
}
