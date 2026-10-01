package adapters_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
)

type noNetwork struct{ t *testing.T }

func (n noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.t.Errorf("network request %s %s", r.Method, r.URL)
	return nil, errors.New("no network")
}

func modelsClient(t *testing.T, cfg adapters.Config) *adapters.Client {
	t.Helper()
	return adapters.New(adapters.Options{Config: cfg, HTTPClient: &http.Client{Transport: noNetwork{t}},
		APIKey: func(string) string { return testKey }, RemoteCatalogURL: "http://catalog.invalid/api.json"})
}

func checkSorted(t *testing.T, ms []adapters.ModelRef) {
	t.Helper()
	for i := 1; i < len(ms); i++ {
		a, b := ms[i-1], ms[i]
		if a.Provider > b.Provider || a.Provider == b.Provider && a.ID >= b.ID {
			t.Fatalf("not strictly sorted at %d: %+v, %+v", i, a, b)
		}
	}
}

func indexOf(ms []adapters.ModelRef, provider, id string) int {
	for i, m := range ms {
		if m.Provider == provider && m.ID == id {
			return i
		}
	}
	return -1
}

// AD-FR-52: 내장 스냅샷만
func TestModelsSnapshot(t *testing.T) {
	cl := modelsClient(t, adapters.Config{})
	ms := cl.Models()
	checkSorted(t, ms)
	for _, want := range [][2]string{{"anthropic", "claude-opus-5-5"}, {"deepseek", "deepseek-v4-pro"}} {
		if indexOf(ms, want[0], want[1]) < 0 {
			t.Fatalf("missing %v in %d models", want, len(ms))
		}
	}
	for _, m := range ms {
		if _, ok := cl.Profile(m.Provider); !ok || m.ID == "" {
			t.Fatalf("bad entry %+v", m)
		}
	}
}

// AD-FR-52: 설정 models 를 합치고, 프로필 없는 provider 는 빼고, 중복 없이
func TestModelsConfig(t *testing.T) {
	base := modelsClient(t, adapters.Config{}).Models()
	cfg, err := adapters.ParseConfig([]byte(`{
		"providers":{"local":{"api":"openai-chat","baseUrl":"http://127.0.0.1:1"}},
		"models":{
			"openai/my-ft":{"contextWindow":1000},
			"local/m1":{},
			"local/a/b":{},
			"nope/x":{},
			"anthropic/claude-opus-5-5":{"contextWindow":5}
		}}`))
	if err != nil {
		t.Fatal(err)
	}
	ms := modelsClient(t, cfg).Models()
	checkSorted(t, ms)
	if len(ms) != len(base)+3 {
		t.Fatalf("len %d, want %d", len(ms), len(base)+3)
	}
	for _, want := range [][2]string{{"openai", "my-ft"}, {"local", "m1"}, {"local", "a/b"}, {"anthropic", "claude-opus-5-5"}} {
		if indexOf(ms, want[0], want[1]) < 0 {
			t.Fatalf("missing %v", want)
		}
	}
	if indexOf(ms, "nope", "x") >= 0 {
		t.Fatal("unknown provider listed")
	}
}

// AD-FR-52: provider/id 형식이 아닌 키(ParseConfig 를 거치지 않은 Config)는 목록에 없다
func TestModelsMalformedKey(t *testing.T) {
	base := modelsClient(t, adapters.Config{}).Models()
	ms := modelsClient(t, adapters.Config{Models: map[string]adapters.ModelPatch{"openai": {}, "openai/": {}}}).Models()
	if len(ms) != len(base) {
		t.Fatalf("len %d, want %d: %v", len(ms), len(base), ms)
	}
}

// AD-FR-53: 프로필 이름 목록, 사전순, 설정 provider 포함
func TestProviders(t *testing.T) {
	base := modelsClient(t, adapters.Config{}).Providers()
	cfg, err := adapters.ParseConfig([]byte(`{"providers":{"aaa":{"api":"openai-chat","baseUrl":"http://x"},"anthropic":{"baseUrl":"http://p"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	ps := modelsClient(t, cfg).Providers()
	if len(ps) != len(base)+1 || ps[0] != "aaa" {
		t.Fatalf("%v", ps)
	}
	for i := 1; i < len(ps); i++ {
		if ps[i-1] >= ps[i] {
			t.Fatalf("not sorted: %v", ps)
		}
	}
	for _, p := range base {
		if _, ok := modelsClient(t, adapters.Config{}).Profile(p); !ok {
			t.Fatalf("%q has no profile", p)
		}
	}
}
