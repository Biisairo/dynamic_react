package sets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	defaultKeepRecentTokens = 20000
	defaultMaxAttempts      = 3
	defaultBaseDelayMs      = 2000
	defaultMaxDelayMs       = 60000
)

// Config 는 루프 설정(RunInput.Config)이다 (ST-FR-01). API 키 필드는 없다 (ST-FR-02).
type Config struct {
	Model           ConfigModel            `json:"model"`
	MaxOutputTokens int                    `json:"maxOutputTokens"`
	Thinking        string                 `json:"thinking"`
	Tools           []string               `json:"tools"`
	Compaction      Compaction             `json:"compaction"`
	Retry           Retry                  `json:"retry"`
	Aux             map[string]ConfigModel `json:"aux"` // 역할 → 보조 모델 (ST-FR-54)
}

type ConfigModel struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type Compaction struct {
	Enabled          bool `json:"enabled"`
	ReserveTokens    int  `json:"reserveTokens"`
	KeepRecentTokens int  `json:"keepRecentTokens"`
}

type Retry struct {
	MaxAttempts int `json:"maxAttempts"`
	BaseDelayMs int `json:"baseDelayMs"`
	MaxDelayMs  int `json:"maxDelayMs"`
}

// rawConfig 는 enabled 의 "없음" 과 false 를 구별하려고 둔다.
type rawConfig struct {
	Model           *ConfigModel `json:"model"`
	MaxOutputTokens int          `json:"maxOutputTokens"`
	Thinking        string       `json:"thinking"`
	Tools           []string     `json:"tools"`
	Compaction      struct {
		Enabled          *bool `json:"enabled"`
		ReserveTokens    int   `json:"reserveTokens"`
		KeepRecentTokens int   `json:"keepRecentTokens"`
	} `json:"compaction"`
	Retry Retry                  `json:"retry"`
	Aux   map[string]ConfigModel `json:"aux"`
}

// ParseConfig 는 설정을 읽고 빈 값·0 을 기본값으로 채운다. 모르는 키는 에러다.
func ParseConfig(raw json.RawMessage) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var r rawConfig
	if err := dec.Decode(&r); err != nil {
		return Config{}, fmt.Errorf("sets: config: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("sets: config: trailing data")
	}
	if r.Model == nil || r.Model.Provider == "" || r.Model.ID == "" {
		return Config{}, errors.New("sets: config: model.provider and model.id required")
	}
	for role, m := range r.Aux {
		if role == "" || m.Provider == "" || m.ID == "" {
			return Config{}, fmt.Errorf("sets: config: aux %q: role, provider and id required", role)
		}
	}
	c := Config{
		Model:           *r.Model,
		MaxOutputTokens: r.MaxOutputTokens,
		Thinking:        r.Thinking,
		Tools:           r.Tools,
		Compaction: Compaction{
			Enabled:          r.Compaction.Enabled == nil || *r.Compaction.Enabled,
			ReserveTokens:    r.Compaction.ReserveTokens,
			KeepRecentTokens: orDefault(r.Compaction.KeepRecentTokens, defaultKeepRecentTokens),
		},
		Retry: Retry{
			MaxAttempts: orDefault(r.Retry.MaxAttempts, defaultMaxAttempts),
			BaseDelayMs: orDefault(r.Retry.BaseDelayMs, defaultBaseDelayMs),
			MaxDelayMs:  orDefault(r.Retry.MaxDelayMs, defaultMaxDelayMs),
		},
		Aux: r.Aux,
	}
	return c, nil
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
