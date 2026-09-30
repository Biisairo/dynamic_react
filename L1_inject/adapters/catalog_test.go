package adapters_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

type catalogEnv struct {
	f       *fake
	remote  *fake
	remoteN atomic.Int32
}

// catalogClient 는 provider 조회 서버와 원격 카탈로그 서버를 둔 Client 다.
func catalogClient(t *testing.T, e *catalogEnv, cfgJSON string, remote bool) *adapters.Client {
	t.Helper()
	cfg, err := adapters.ParseConfig([]byte(cfgJSON))
	if err != nil {
		t.Fatal(err)
	}
	opts := adapters.Options{Config: cfg, Getenv: func(string) string { return testKey }, Now: func() time.Time { return ts }, NoRemoteCatalog: !remote}
	if e.remote != nil {
		opts.RemoteCatalogURL = e.remote.URL + "/api.json"
	}
	return adapters.New(opts)
}

func providerCfg(name, lookup, base string, extra string) string {
	return `{"providers":{"` + name + `":{"api":"openai-chat","baseUrl":"` + base + `","lookup":"` + lookup + `"` + extra + `}}}`
}

// AD-FR-36: 조회 방식별 필드 매핑
func TestLookupKinds(t *testing.T) {
	cases := []struct {
		name, lookup, method, path, body string
		want                             contract.ModelInfo
	}{
		{"anthropic", "anthropic", "GET", "/v1/models/m",
			`{"id":"m","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{"image_input":{"supported":true},"thinking":{"supported":true,"types":{"adaptive":{"supported":true}}}}}`,
			contract.ModelInfo{ContextWindow: 1000000, MaxOutputTokens: 128000, Capabilities: contract.Capabilities{Vision: true, Thinking: true}}},
		{"openrouter", "openrouter", "GET", "/models",
			`{"data":[{"id":"other"},{"id":"m","context_length":200000,"top_provider":{"max_completion_tokens":8000},"architecture":{"input_modalities":["text","image"]},"reasoning":{"supported_efforts":["low"]}}]}`,
			contract.ModelInfo{ContextWindow: 200000, MaxOutputTokens: 8000, Capabilities: contract.Capabilities{Vision: true, Thinking: true}}},
		{"ollama", "ollama", "POST", "/api/show",
			`{"model_info":{"general.architecture":"qwen3","qwen3.context_length":40960},"capabilities":["completion","vision","thinking","tools"]}`,
			contract.ModelInfo{ContextWindow: 40960, Capabilities: contract.Capabilities{Vision: true, Thinking: true}}},
		{"llamacpp", "llamacpp", "GET", "/props", `{"default_generation_settings":{"n_ctx":8192}}`,
			contract.ModelInfo{ContextWindow: 8192}},
		{"vllm", "vllm", "GET", "/v1/models", `{"data":[{"id":"m","max_model_len":32768}]}`,
			contract.ModelInfo{ContextWindow: 32768}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := &catalogEnv{f: newFake(t)}
			e.f.on(c.method, c.path, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(c.body)) })
			base := e.f.URL
			if c.lookup == "vllm" || c.lookup == "llamacpp" || c.lookup == "ollama" {
				base += "/v1" // 로컬 서버: base 는 /v1, 조회는 host 기준 (vllm 은 base 기준)
			}
			cl := catalogClient(t, e, providerCfg("p", c.lookup, base, `,"apiKeyEnv":"K"`), false)
			info, ok := cl.Lookup(context.Background(), "p", "m")
			if !ok {
				t.Fatal("not found")
			}
			c.want.Ref = contract.ModelRef{Provider: "p", ID: "m", API: "openai-chat"}
			if info != c.want {
				t.Fatalf("info %+v\nwant %+v", info, c.want)
			}
			if c.lookup == "ollama" {
				var req map[string]string
				json.Unmarshal(e.f.body("/api/show"), &req)
				if req["model"] != "m" {
					t.Fatalf("show request %v", req)
				}
			}
			if c.lookup == "anthropic" && e.f.header("/v1/models/m").Get("x-api-key") != testKey {
				t.Fatal("anthropic lookup without key")
			}
		})
	}
}

const remoteBody = `{"p":{"id":"p","models":{
  "m":{"id":"m","limit":{"context":64000,"output":4000},"modalities":{"input":["text","image"]},"reasoning":true,"reasoning_options":[{"type":"effort"}]},
  "b":{"id":"b","limit":{"context":200000,"output":64000},"reasoning":true,"reasoning_options":[{"type":"budget_tokens","min":1024}]}}},
 "renamed":{"id":"renamed","models":{"x":{"id":"x","limit":{"context":1000}}}}}`

func remoteServer(t *testing.T, e *catalogEnv, body string) {
	e.remote = newFake(t)
	e.remote.on("GET", "/api.json", func(w http.ResponseWriter, r *http.Request) {
		e.remoteN.Add(1)
		w.Write([]byte(body))
	})
}

// R11, 3.6: 필드 단위 우선순위 — 사용자 설정 > provider 조회 > 원격 > 스냅샷
func TestLookupPriority(t *testing.T) {
	e := &catalogEnv{f: newFake(t)}
	e.f.on("GET", "/models", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"m","context_length":128000}]}`)) // 창 크기만 준다
	})
	remoteServer(t, e, remoteBody)
	cfg := `{"providers":{"p":{"api":"openai-chat","baseUrl":"` + e.f.URL + `","lookup":"openrouter"}},
	         "models":{"p/m":{"maxOutputTokens":999}}}`
	cl := catalogClient(t, e, cfg, true)
	info, ok := cl.Lookup(context.Background(), "p", "m")
	if !ok || info.ContextWindow != 128000 || info.MaxOutputTokens != 999 || !info.Capabilities.Vision || !info.Capabilities.Thinking {
		t.Fatalf("info %+v", info)
	}
	// 조회 방식 none + 원격만
	cl = catalogClient(t, e, providerCfg("p", "none", e.f.URL, ""), true)
	info, ok = cl.Lookup(context.Background(), "p", "b")
	if !ok || info.ContextWindow != 200000 || info.MaxOutputTokens != 64000 {
		t.Fatalf("remote only %+v", info)
	}
	// CatalogID
	cl = catalogClient(t, e, providerCfg("q", "none", e.f.URL, `,"catalogId":"renamed"`), true)
	if info, ok := cl.Lookup(context.Background(), "q", "x"); !ok || info.ContextWindow != 1000 {
		t.Fatalf("catalogId %+v", info)
	}
	// 사용자 설정만 있는 모델
	cl = catalogClient(t, e, `{"providers":{"p":{"api":"openai-chat","baseUrl":"`+e.f.URL+`"}},"models":{"p/custom":{"contextWindow":5}}}`, false)
	if info, ok := cl.Lookup(context.Background(), "p", "custom"); !ok || info.ContextWindow != 5 {
		t.Fatalf("config only %+v", info)
	}
	// 아무 출처도 모름
	if _, ok := cl.Lookup(context.Background(), "p", "ghost"); ok {
		t.Fatal("unknown model found")
	}
	if _, ok := cl.Lookup(context.Background(), "nope", "m"); ok {
		t.Fatal("unknown provider found")
	}
}

// AD-FR-36c: 내장 스냅샷이 마지막 출처
func TestLookupSnapshot(t *testing.T) {
	cl := adapters.New(adapters.Options{NoRemoteCatalog: true, Getenv: func(string) string { return "" }})
	info, ok := cl.Lookup(context.Background(), "deepseek", "deepseek-v4-pro")
	if !ok || info.ContextWindow <= 0 || info.MaxOutputTokens <= 0 {
		t.Fatalf("snapshot %+v %v", info, ok)
	}
	if info, ok := cl.Lookup(context.Background(), "anthropic", "claude-opus-5-5"); !ok || info.ContextWindow != 1000000 || !info.Capabilities.Thinking {
		t.Fatalf("snapshot anthropic %+v", info)
	}
}

// AD-FR-36b, 39: 캐시, 실패 재시도, 동시성
func TestLookupCaching(t *testing.T) {
	e := &catalogEnv{f: newFake(t)}
	var calls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	e.f.on("GET", "/models", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"data":[{"id":"m","context_length":7}]}`))
	})
	remoteServer(t, e, `{}`)
	cl := catalogClient(t, e, providerCfg("p", "openrouter", e.f.URL, ""), true)
	if _, ok := cl.Lookup(context.Background(), "p", "m"); ok {
		t.Fatal("found while failing")
	}
	fail.Store(false)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, ok := cl.Lookup(context.Background(), "p", "m"); !ok || info.ContextWindow != 7 {
				t.Errorf("info %+v", info)
			}
		}()
	}
	wg.Wait()
	before := calls.Load()
	cl.Lookup(context.Background(), "p", "m")
	if calls.Load() != before || before > 21 {
		t.Fatalf("not cached: %d calls", calls.Load())
	}
	if e.remoteN.Load() != 1 {
		t.Fatalf("remote fetched %d times", e.remoteN.Load())
	}
}

func TestRemoteCatalogFailureRetried(t *testing.T) {
	e := &catalogEnv{}
	var fail atomic.Bool
	fail.Store(true)
	e.remote = newFake(t)
	e.remote.on("GET", "/api.json", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(remoteBody))
	})
	e.f = newFake(t)
	cl := catalogClient(t, e, providerCfg("p", "none", e.f.URL, ""), true)
	if _, ok := cl.Lookup(context.Background(), "p", "m"); ok {
		t.Fatal("found while remote failing")
	}
	fail.Store(false)
	if info, ok := cl.Lookup(context.Background(), "p", "m"); !ok || info.ContextWindow != 64000 {
		t.Fatalf("not retried %+v", info)
	}
	// NoRemoteCatalog 이면 부르지 않는다.
	var n atomic.Int32
	e.remote.on("GET", "/api.json", func(w http.ResponseWriter, r *http.Request) { n.Add(1) })
	catalogClient(t, e, providerCfg("p", "none", e.f.URL, ""), false).Lookup(context.Background(), "p", "zz")
	if n.Load() != 0 {
		t.Fatal("remote called while disabled")
	}
}

// AD-FR-13 과 카탈로그: 원격 카탈로그가 budget 전용이라고 하면 thinking level 은 invalid_request.
func TestThinkingModeFromRemote(t *testing.T) {
	e := &catalogEnv{f: newFake(t)}
	e.f.sseText("/v1/messages", anthropicReply("end_turn"))
	remoteServer(t, e, `{"anthropic":{"models":{"b":{"id":"b","limit":{"context":200000,"output":64000},"reasoning":true,"reasoning_options":[{"type":"budget_tokens"}]}}}}`)
	cl := catalogClient(t, e, `{"providers":{"anthropic":{"baseUrl":"`+e.f.URL+`","lookup":"none"}}}`, true)
	req := simple("anthropic", "b")
	req.Options.Thinking = "high"
	if ce := failed(t, collect(t, cl, req)); ce.Class != contract.ClassInvalidRequest {
		t.Fatalf("%+v", ce)
	}
}
