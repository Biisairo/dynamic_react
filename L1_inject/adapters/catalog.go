package adapters

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

const (
	modeAdaptive = "adaptive"
	modeBudget   = "budget"
)

// record 는 한 출처가 아는 모델 수치다. 0·nil·"" 은 모름이다.
type record struct {
	found    bool
	cw, out  int
	vision   *bool
	thinking *bool
	mode     string
}

// under 는 r 이 모르는 필드를 lower 로 채운다 (R11 필드 단위 우선순위).
func (r record) under(lower record) record {
	r.found = r.found || lower.found
	if r.cw == 0 {
		r.cw = lower.cw
	}
	if r.out == 0 {
		r.out = lower.out
	}
	if r.vision == nil {
		r.vision = lower.vision
	}
	if r.thinking == nil {
		r.thinking = lower.thinking
	}
	if r.mode == "" {
		r.mode = lower.mode
	}
	return r
}

func boolp(b bool) *bool { return &b }

// lookupResult 는 provider 조회 한 번의 결과다. list 면 여러 모델, 아니면 하나.
type lookupResult struct {
	models map[string]record
	all    *record // 서버 하나가 한 모델만 서빙할 때 (llama.cpp)
}

// Lookup 은 모델 정보를 출처 순서대로 모은다 (AD-FR-35~39).
func (c *Client) Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool) {
	p, ok := c.profiles[provider]
	if !ok {
		return contract.ModelInfo{}, false
	}
	r := c.lookup(ctx, provider, id)
	if !r.found {
		return contract.ModelInfo{}, false
	}
	info := contract.ModelInfo{Ref: contract.ModelRef{Provider: provider, ID: id, API: string(p.API)}, ContextWindow: r.cw, MaxOutputTokens: r.out}
	info.Capabilities.Vision = r.vision != nil && *r.vision
	info.Capabilities.Thinking = r.thinking != nil && *r.thinking
	return info, true
}

func (c *Client) lookup(ctx context.Context, provider, id string) record {
	p, ok := c.profiles[provider]
	if !ok {
		return record{}
	}
	user := c.userRecord(provider, id)
	prov := c.providerRecord(ctx, p, provider, id)
	remote := c.remoteRecord(ctx, p, provider, id)
	c.mu.Lock()
	snap := c.snap[provider][id]
	c.mu.Unlock()
	return user.under(prov).under(remote).under(snap)
}

func (c *Client) userRecord(provider, id string) record {
	m, ok := c.models[provider+"/"+id]
	if !ok {
		return record{}
	}
	r := record{found: true}
	if m.ContextWindow != nil {
		r.cw = *m.ContextWindow
	}
	if m.MaxOutputTokens != nil {
		r.out = *m.MaxOutputTokens
	}
	if m.Capabilities != nil {
		r.vision, r.thinking = boolp(m.Capabilities.Vision), boolp(m.Capabilities.Thinking)
	}
	return r
}

// ---- provider 조회 (2순위) ----

func host(base string) string {
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
}

func (c *Client) providerRecord(ctx context.Context, p Profile, provider, id string) record {
	perModel := p.Lookup == LookupAnthropic || p.Lookup == LookupOllama
	key := provider
	if perModel {
		key += "\x00" + id
	}
	c.mu.Lock()
	res, cached := c.lookups[key]
	c.mu.Unlock()
	if !cached {
		var ok bool
		if res, ok = c.fetchProvider(ctx, p, id); !ok {
			return record{}
		}
		c.mu.Lock()
		c.lookups[key] = res
		c.mu.Unlock()
	}
	if res.all != nil {
		return *res.all
	}
	return res.models[id]
}

// fetchProvider 는 조회 API 를 부른다. 두 번째 값이 false 면 실패(캐시하지 않음). 404 는 "없음" 으로 확정한다.
func (c *Client) fetchProvider(ctx context.Context, p Profile, id string) (lookupResult, bool) {
	base := strings.TrimRight(p.BaseURL, "/")
	var key string
	if p.APIKeyEnv != "" {
		key = c.getenv(p.APIKeyEnv)
	}
	get := func(method, u string, body any) ([]byte, int, bool) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return nil, 0, false
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			if p.Lookup == LookupAnthropic {
				req.Header.Set("x-api-key", key)
				req.Header.Set("anthropic-version", anthropicVersion)
			} else {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, 0, false
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		return b, resp.StatusCode, err == nil
	}
	one := func(r record) lookupResult { return lookupResult{models: map[string]record{id: r}} }
	switch p.Lookup {
	case LookupAnthropic:
		b, st, ok := get(http.MethodGet, base+"/v1/models/"+url.PathEscape(id), nil)
		if !ok || (st != 200 && st != 404) {
			return lookupResult{}, false
		}
		if st == 404 {
			return lookupResult{}, true
		}
		var m struct {
			MaxInput     int `json:"max_input_tokens"`
			MaxTokens    int `json:"max_tokens"`
			Capabilities struct {
				ImageInput struct{ Supported bool } `json:"image_input"`
				Thinking   struct {
					Supported bool
					Types     struct {
						Adaptive struct{ Supported bool }
						Enabled  struct{ Supported bool }
					}
				}
			}
		}
		if json.Unmarshal(b, &m) != nil {
			return lookupResult{}, false
		}
		r := record{found: true, cw: m.MaxInput, out: m.MaxTokens,
			vision: boolp(m.Capabilities.ImageInput.Supported), thinking: boolp(m.Capabilities.Thinking.Supported)}
		switch {
		case m.Capabilities.Thinking.Types.Adaptive.Supported:
			r.mode = modeAdaptive
		case m.Capabilities.Thinking.Types.Enabled.Supported:
			r.mode = modeBudget
		}
		return one(r), true
	case LookupOpenRouter:
		b, st, ok := get(http.MethodGet, base+"/models", nil)
		if !ok || st != 200 {
			return lookupResult{}, false
		}
		var v struct {
			Data []struct {
				ID            string `json:"id"`
				ContextLength int    `json:"context_length"`
				TopProvider   struct {
					MaxCompletionTokens int `json:"max_completion_tokens"`
				} `json:"top_provider"`
				Architecture struct {
					InputModalities []string `json:"input_modalities"`
				} `json:"architecture"`
				Reasoning json.RawMessage `json:"reasoning"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &v) != nil {
			return lookupResult{}, false
		}
		res := lookupResult{models: map[string]record{}}
		for _, m := range v.Data {
			r := record{found: true, cw: m.ContextLength, out: m.TopProvider.MaxCompletionTokens}
			if m.Architecture.InputModalities != nil {
				r.vision = boolp(contains(m.Architecture.InputModalities, "image"))
			}
			if len(m.Reasoning) > 0 && string(m.Reasoning) != "null" {
				r.thinking = boolp(true)
			}
			res.models[m.ID] = r
		}
		return res, true
	case LookupOllama:
		b, st, ok := get(http.MethodPost, host(base)+"/api/show", map[string]string{"model": id})
		if !ok || (st != 200 && st != 404) {
			return lookupResult{}, false
		}
		if st == 404 {
			return lookupResult{}, true
		}
		var v struct {
			ModelInfo    map[string]json.RawMessage `json:"model_info"`
			Capabilities []string                   `json:"capabilities"`
		}
		if json.Unmarshal(b, &v) != nil {
			return lookupResult{}, false
		}
		r := record{found: true}
		for k, raw := range v.ModelInfo {
			if strings.HasSuffix(k, ".context_length") {
				json.Unmarshal(raw, &r.cw)
			}
		}
		if v.Capabilities != nil {
			r.vision, r.thinking = boolp(contains(v.Capabilities, "vision")), boolp(contains(v.Capabilities, "thinking"))
		}
		return one(r), true
	case LookupLlamaCpp:
		b, st, ok := get(http.MethodGet, host(base)+"/props", nil)
		if !ok || st != 200 {
			return lookupResult{}, false
		}
		var v struct {
			Settings struct {
				NCtx int `json:"n_ctx"`
			} `json:"default_generation_settings"`
		}
		if json.Unmarshal(b, &v) != nil {
			return lookupResult{}, false
		}
		return lookupResult{all: &record{found: true, cw: v.Settings.NCtx}}, true
	case LookupVLLM:
		b, st, ok := get(http.MethodGet, base+"/models", nil)
		if !ok || st != 200 {
			return lookupResult{}, false
		}
		var v struct {
			Data []struct {
				ID          string `json:"id"`
				MaxModelLen int    `json:"max_model_len"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &v) != nil {
			return lookupResult{}, false
		}
		res := lookupResult{models: map[string]record{}}
		for _, m := range v.Data {
			res.models[m.ID] = record{found: true, cw: m.MaxModelLen}
		}
		return res, true
	}
	return lookupResult{}, true
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// ---- 원격 카탈로그 (3순위) ----

func (c *Client) remoteRecord(ctx context.Context, p Profile, provider, id string) record {
	if c.remote == "" {
		return record{}
	}
	c.remoteMu.Lock()
	defer c.remoteMu.Unlock()
	if c.remoteCache == nil {
		c.remoteCache = c.fetchRemote(ctx)
		if c.remoteCache == nil {
			return record{}
		}
	}
	cat := p.CatalogID
	if cat == "" {
		cat = provider
	}
	return c.remoteCache[cat][id]
}

func (c *Client) fetchRemote(ctx context.Context) map[string]map[string]record {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.remote, nil)
	if err != nil {
		return nil
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var v map[string]struct {
		Models map[string]struct {
			Limit struct {
				Context int `json:"context"`
				Output  int `json:"output"`
			} `json:"limit"`
			Modalities *struct {
				Input []string `json:"input"`
			} `json:"modalities"`
			Reasoning        *bool `json:"reasoning"`
			ReasoningOptions []struct {
				Type string `json:"type"`
			} `json:"reasoning_options"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return nil
	}
	out := make(map[string]map[string]record, len(v))
	for pname, prov := range v {
		ms := make(map[string]record, len(prov.Models))
		for id, m := range prov.Models {
			r := record{found: true, cw: m.Limit.Context, out: m.Limit.Output, thinking: m.Reasoning}
			if m.Modalities != nil {
				r.vision = boolp(contains(m.Modalities.Input, "image"))
			}
			for _, o := range m.ReasoningOptions {
				switch o.Type {
				case "effort":
					r.mode = modeAdaptive
				case "budget_tokens":
					if r.mode == "" {
						r.mode = modeBudget
					}
				}
			}
			ms[id] = r
		}
		out[pname] = ms
	}
	return out
}

// ---- 내장 스냅샷 (4순위) ----

//go:embed snapshot.json
var snapshotJSON []byte

func loadSnapshot() map[string]map[string]record {
	var v struct {
		Models map[string]map[string]struct {
			ContextWindow   int    `json:"contextWindow"`
			MaxOutputTokens int    `json:"maxOutputTokens"`
			Vision          bool   `json:"vision"`
			Thinking        bool   `json:"thinking"`
			ThinkingMode    string `json:"thinkingMode"`
		} `json:"models"`
	}
	if json.Unmarshal(snapshotJSON, &v) != nil {
		return nil
	}
	out := make(map[string]map[string]record, len(v.Models))
	for p, ms := range v.Models {
		out[p] = make(map[string]record, len(ms))
		for id, m := range ms {
			out[p][id] = record{found: true, cw: m.ContextWindow, out: m.MaxOutputTokens,
				vision: boolp(m.Vision), thinking: boolp(m.Thinking), mode: m.ThinkingMode}
		}
	}
	return out
}
