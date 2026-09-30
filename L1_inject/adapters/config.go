package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// Config 는 사용자 설정이다 (AD-FR-49). 키 값을 담는 필드는 없다 (AD-C2).
type Config struct {
	Providers map[string]ProfilePatch `json:"providers,omitempty"`
	Models    map[string]ModelPatch   `json:"models,omitempty"`
}

// ProfilePatch 는 프로필 부분 덮어쓰기다. 새 provider 는 api·baseUrl 이 필수다.
type ProfilePatch struct {
	API                *API            `json:"api,omitempty"`
	BaseURL            *string         `json:"baseUrl,omitempty"`
	APIKeyEnv          *string         `json:"apiKeyEnv,omitempty"`
	Lookup             *LookupKind     `json:"lookup,omitempty"`
	CatalogID          *string         `json:"catalogId,omitempty"`
	DeveloperRole      *bool           `json:"developerRole,omitempty"`
	UsageInStreaming   *bool           `json:"usageInStreaming,omitempty"`
	MaxTokensField     *string         `json:"maxTokensField,omitempty"`
	Thinking           *ThinkingFormat `json:"thinking,omitempty"`
	ReasoningField     *string         `json:"reasoningField,omitempty"`
	ToolResultName     *bool           `json:"toolResultName,omitempty"`
	EncryptedReasoning *bool           `json:"encryptedReasoning,omitempty"`
}

// ModelPatch 는 모델 하나의 수치다 (카탈로그 1순위, R11).
type ModelPatch struct {
	ContextWindow   *int                   `json:"contextWindow,omitempty"`
	MaxOutputTokens *int                   `json:"maxOutputTokens,omitempty"`
	Capabilities    *contract.Capabilities `json:"capabilities,omitempty"`
}

// LoadConfig 는 설정 파일을 읽는다.
func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("adapters: config: %w", err)
	}
	return ParseConfig(b)
}

// ParseConfig 는 모르는 키를 거부한다.
func ParseConfig(b []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("adapters: config: %w", err)
	}
	builtin := BuiltinProfiles()
	for name, p := range c.Providers {
		if p.API != nil && !p.API.valid() {
			return Config{}, fmt.Errorf("adapters: config: provider %q: unknown api %q", name, *p.API)
		}
		if p.Lookup != nil && !p.Lookup.valid() {
			return Config{}, fmt.Errorf("adapters: config: provider %q: unknown lookup %q", name, *p.Lookup)
		}
		if p.Thinking != nil && !p.Thinking.valid() {
			return Config{}, fmt.Errorf("adapters: config: provider %q: unknown thinking %q", name, *p.Thinking)
		}
		if _, ok := builtin[name]; !ok && (p.API == nil || p.BaseURL == nil) {
			return Config{}, fmt.Errorf("adapters: config: new provider %q needs api and baseUrl", name)
		}
	}
	for key := range c.Models {
		if p, id, ok := strings.Cut(key, "/"); !ok || p == "" || id == "" {
			return Config{}, fmt.Errorf("adapters: config: model key %q must be provider/id", key)
		}
	}
	return c, nil
}
