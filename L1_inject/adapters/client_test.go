package adapters_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

// AD-FR-01
func TestUnknownProviderOrAPI(t *testing.T) {
	c := adapters.New(adapters.Options{NoRemoteCatalog: true, APIKey: func(string) string { return "k" }})
	for _, req := range []contract.Request{simple("nope", "m"), func() contract.Request {
		r := simple("anthropic", "m")
		r.Model.API = "openai-chat"
		return r
	}()} {
		var evs []contract.StreamEvent
		for ev := range c.Call(context.Background(), req) {
			evs = append(evs, ev)
		}
		if len(evs) != 1 || evs[0].Kind != contract.EventError || evs[0].Error.Class != contract.ClassInvalidRequest {
			t.Fatalf("%s: %+v", req.Model, evs)
		}
	}
}

// AD-FR-50: 키가 없으면 보내지 않고, 메시지에는 provider 이름만.
func TestMissingKey(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	cfg, _ := adapters.ParseConfig([]byte(`{"providers":{"anthropic":{"baseUrl":"` + f.URL + `"}}}`))
	var asked []string
	c := adapters.New(adapters.Options{Config: cfg, NoRemoteCatalog: true, APIKey: func(p string) string { asked = append(asked, p); return "" }})
	ce := failed(t, collect(t, c, simple("anthropic", "m")))
	if ce.Class != contract.ClassAuth || ce.Message != `missing API key for provider "anthropic"` || f.body("/v1/messages") != nil {
		t.Fatalf("%+v", ce)
	}
	if len(asked) != 1 || asked[0] != "anthropic" {
		t.Fatalf("APIKey asked %v", asked)
	}
}

// AD-FR-48: APIKey 는 호출마다 불려 바뀐 키가 다음 요청에 쓰인다.
func TestAPIKeyPerCall(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	cfg, _ := adapters.ParseConfig([]byte(`{"providers":{"anthropic":{"baseUrl":"` + f.URL + `"}}}`))
	key := "sk-first"
	c := adapters.New(adapters.Options{Config: cfg, NoRemoteCatalog: true, APIKey: func(string) string { return key }})
	for _, k := range []string{"sk-first", "sk-second"} {
		key = k
		done(t, collect(t, c, simple("anthropic", "m")))
		if got := f.header("/v1/messages").Get("x-api-key"); got != k {
			t.Fatalf("x-api-key %q, want %q", got, k)
		}
	}
}

// AD-FR-48, AD-C6: APIKey 가 nil 이면 프로세스 환경에 키가 있어도 읽지 않는다.
func TestNilAPIKeyReadsNoEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-process-env")
	f := newFake(t)
	f.sseText("/v1/messages", anthropicReply("end_turn"))
	cfg, _ := adapters.ParseConfig([]byte(`{"providers":{"anthropic":{"baseUrl":"` + f.URL + `"}}}`))
	c := adapters.New(adapters.Options{Config: cfg, NoRemoteCatalog: true})
	ce := failed(t, collect(t, c, simple("anthropic", "m")))
	if ce.Class != contract.ClassAuth || f.body("/v1/messages") != nil {
		t.Fatalf("%+v", ce)
	}
}

// AD-FR-32, 34: HTTP 에러는 분류되고 키는 가려진다.
func TestHTTPErrorClassifiedAndMasked(t *testing.T) {
	f := newFake(t)
	f.on("POST", "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"slow down, key ` + testKey + `"}}`))
	})
	ce := failed(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m")))
	if ce.Class != contract.ClassRateLimit || ce.RetryAfter != 3*time.Second || ce.Status != 429 {
		t.Fatalf("%+v", ce)
	}
	if strings.Contains(ce.Message+ce.Raw, "SECRETKEY") {
		t.Fatalf("key leaked: %+v", ce)
	}
	f.on("POST", "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"prompt is too long: 300000 tokens"}}`))
	})
	if ce := failed(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m"))); ce.Class != contract.ClassOverflow {
		t.Fatalf("%+v", ce)
	}
}

func TestConnectionError(t *testing.T) {
	f := newFake(t)
	c := client(t, f, "anthropic", "")
	f.Close()
	if ce := failed(t, collect(t, c, simple("anthropic", "m"))); ce.Class != contract.ClassTransientNetwork {
		t.Fatalf("%+v", ce)
	}
}

// 스트림이 끝 이벤트 없이 끊기면 transient_network.
func TestTruncatedStream(t *testing.T) {
	f := newFake(t)
	f.sseText("/v1/messages", sseLines(anthropicText))
	if ce := failed(t, collect(t, client(t, f, "anthropic", ""), simple("anthropic", "m"))); ce.Class != contract.ClassTransientNetwork {
		t.Fatalf("%+v", ce)
	}
}

// AD-FR-02
func TestCancel(t *testing.T) {
	f := newFake(t)
	release := make(chan struct{})
	f.on("POST", "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseLines(anthropicText)))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := client(t, f, "anthropic", "")
	var evs []contract.StreamEvent
	for ev := range c.Call(ctx, simple("anthropic", "m")) {
		evs = append(evs, ev)
		if ev.Kind == contract.EventStart {
			cancel()
		}
	}
	ce := evs[len(evs)-1]
	if ce.Kind != contract.EventError || ce.Error.Class != contract.ClassUnknown || ce.Error.Message != "aborted" {
		t.Fatalf("%+v", evs)
	}
}

// 소비자가 중간에 그만둬도 panic·누수 없이 끝난다.
func TestEarlyBreak(t *testing.T) {
	f := newFake(t)
	f.sse("/v1/messages", "anthropic_stream.sse")
	for range client(t, f, "anthropic", "").Call(context.Background(), anthropicScenario()) {
		break
	}
}

// AD-FR-51: 응답 id 가 없으면 무작위 id.
func TestGeneratedMessageID(t *testing.T) {
	f := newFake(t)
	f.sseText("/chat/completions", chatChunks(`{"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"stop"}]}`))
	m := done(t, collect(t, client(t, f, "golden", goldenChat), simple("golden", "m")))
	if m.ID == "" || m.Validate() != nil {
		t.Fatalf("%+v", m)
	}
}

// AD-NFR-02: 실제 provider 호출은 키가 있을 때만.
func TestLiveProviders(t *testing.T) {
	cases := map[string]string{"anthropic": "ANTHROPIC_API_KEY", "openai": "OPENAI_API_KEY", "deepseek": "DEEPSEEK_API_KEY"}
	models := map[string]string{"anthropic": "claude-haiku-4-5", "openai": "gpt-5-mini", "deepseek": "deepseek-chat"}
	for p, env := range cases {
		t.Run(p, func(t *testing.T) {
			if os.Getenv(env) == "" {
				t.Skipf("%s not set", env)
			}
			c := adapters.New(adapters.Options{APIKey: func(p string) string { return os.Getenv(cases[p]) }})
			req := simple(p, models[p])
			req.Messages = []contract.Message{user("u1", "Reply with the single word: ok")}
			m := done(t, collect(t, c, req))
			if m.Usage.Output == 0 {
				t.Fatalf("%+v", m)
			}
		})
	}
}
