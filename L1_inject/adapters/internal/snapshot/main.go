// snapshot 은 oh-my-pi 의 packages/catalog/src/models.json 에서 내장 프로필 provider 의 모델만 추려
// adapters 의 내장 스냅샷(snapshot.json)을 만든다 (adapters AD-FR-36c).
//
//	go run ./internal/snapshot -src <oh-my-pi>/packages/catalog/src/models.json -out snapshot.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"sort"
)

// Snapshot 은 생성 결과다. adapters 가 embed 해 읽는다.
type Snapshot struct {
	Models map[string]map[string]ModelInfo `json:"models"`
}

type ModelInfo struct {
	ContextWindow   int    `json:"contextWindow,omitempty"`
	MaxOutputTokens int    `json:"maxOutputTokens,omitempty"`
	Vision          bool   `json:"vision,omitempty"`
	Thinking        bool   `json:"thinking,omitempty"`
	ThinkingMode    string `json:"thinkingMode,omitempty"` // adaptive | budget
}

type source struct {
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
	Input         []string `json:"input"`
	Reasoning     bool     `json:"reasoning"`
	Thinking      struct {
		Mode string `json:"mode"`
	} `json:"thinking"`
}

func mode(m string) string {
	switch m {
	case "anthropic-adaptive", "effort":
		return "adaptive"
	case "budget", "anthropic-budget-effort":
		return "budget"
	}
	return ""
}

func main() {
	src := flag.String("src", "", "oh-my-pi packages/catalog/src/models.json")
	out := flag.String("out", "snapshot.json", "output file")
	flag.Parse()
	b, err := os.ReadFile(*src)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var all map[string]map[string]source
	if err := json.Unmarshal(b, &all); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	snap := Snapshot{Models: map[string]map[string]ModelInfo{}}
	for _, p := range profileProviders {
		models, ok := all[p]
		if !ok {
			continue
		}
		ids := make([]string, 0, len(models))
		for id := range models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		snap.Models[p] = map[string]ModelInfo{}
		for _, id := range ids {
			m := models[id]
			snap.Models[p][id] = ModelInfo{
				ContextWindow:   m.ContextWindow,
				MaxOutputTokens: m.MaxTokens,
				Vision:          slices.Contains(m.Input, "image"),
				Thinking:        m.Reasoning,
				ThinkingMode:    mode(m.Thinking.Mode),
			}
		}
	}
	data, _ := json.MarshalIndent(snap, "", " ")
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// profileProviders 는 adapters 내장 프로필의 provider 다 (AD-FR-30 표).
var profileProviders = []string{
	"anthropic", "minimax", "minimax-cn", "fireworks", "vercel-ai-gateway", "openai", "xai",
	"deepseek", "zai", "xiaomi", "moonshotai", "moonshotai-cn", "openrouter", "groq", "cerebras",
	"together", "nvidia", "huggingface", "baseten", "ant-ling",
}
