package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

// keyedConfig 는 키가 필요 없는 "test" 와 키가 필요한 "keyed" 를 같은 가짜 서버로 등록한 설정이다 (모델 없음).
func keyedConfig(url, extra string) string {
	return `{"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"},` +
		`"keyed":{"api":"openai-chat","baseUrl":"` + url + `","requiresKey":true}},` +
		`"models":{"test/m":{"contextWindow":100000,"maxOutputTokens":1000},"keyed/k":{}}` + extra + `}`
}

func mkAgent(t *testing.T, o agent.Options) *agent.Agent {
	t.Helper()
	a, err := agent.New(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func requestModel(t *testing.T, body string) string {
	t.Helper()
	var req struct{ Model string }
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("request %q: %v", body, err)
	}
	return req.Model
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func hasModel(ms []agent.ModelChoice, provider, id string) bool {
	for _, m := range ms {
		if m.Provider == provider && m.ID == id {
			return true
		}
	}
	return false
}

// AG-FR-04, 10, 32: 모델 없음 → New 성공, Model()·Start 가 ErrNoModel, 잠금 없음
func TestNoModel(t *testing.T) {
	for name, cfg := range map[string]string{
		"no file":       "",
		"empty agent":   `{"agent":{}}`,
		"partial model": `{"agent":{"model":{"provider":"test"},"thinking":"low"}}`,
		"null agent":    `{"agent":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			if cfg != "" {
				f.writeConfig(t, cfg)
			}
			a := mkAgent(t, f.opts())
			if ref, err := a.Model(); ref != "" || !errors.Is(err, agent.ErrNoModel) || err.Error() != "no model configured" {
				t.Fatalf("Model %q %v", ref, err)
			}
			for range 2 {
				if r, err := a.Start(context.Background(), "s1", user("hi")); r != nil || !errors.Is(err, agent.ErrNoModel) {
					t.Fatalf("Start %v %v", r, err)
				}
			}
			if _, err := a.Compact(context.Background(), "s1"); !errors.Is(err, agent.ErrNoModel) {
				t.Fatalf("Compact after failed Start: %v", err)
			}
		})
	}
	for name, cfg := range map[string]string{
		"unknown agent key": `{"agent":{"colour":1}}`,
		"bad model type":    `{"agent":{"model":5}}`,
		"agent not object":  `{"agent":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.writeConfig(t, cfg)
			if _, err := agent.New(context.Background(), f.opts()); err == nil || !strings.HasPrefix(err.Error(), "agent: config:") {
				t.Fatalf("err %v", err)
			}
		})
	}
}

// AG-FR-33: 키가 필요 없거나 키가 있는 provider 의 모델만, adapters 순서
func TestModels(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, keyedConfig(l.URL, ""))
	ms := mkAgent(t, f.opts()).Models()
	if !hasModel(ms, "test", "m") || hasModel(ms, "keyed", "k") || hasModel(ms, "anthropic", "claude-opus-5-5") {
		t.Fatalf("without keys: %v", ms)
	}
	f.writeAuth(t, `{"keyed":{"type":"api_key","key":"sk-k"},"anthropic":{"type":"api_key","key":"sk-a"}}`)
	ms = mkAgent(t, f.opts()).Models()
	if !hasModel(ms, "test", "m") || !hasModel(ms, "keyed", "k") || !hasModel(ms, "anthropic", "claude-opus-5-5") {
		t.Fatalf("with keys: %v", ms)
	}
	for i := 1; i < len(ms); i++ {
		a, b := ms[i-1], ms[i]
		if a.Provider > b.Provider || a.Provider == b.Provider && a.ID >= b.ID {
			t.Fatalf("order at %d: %v %v", i, a, b)
		}
	}
}

// AG-FR-34: 검증 실패면 파일·모델 그대로
func TestSetModelRejects(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	a := mkAgent(t, f.opts())
	cfgPath := filepath.Join(f.home, ".dynamic_react", "config.json")
	for _, ref := range []string{"x", "x/", "/y", "nope/m"} {
		if err := a.SetModel(context.Background(), ref); err == nil {
			t.Errorf("%q accepted", ref)
		}
	}
	if err := a.SetModel(context.Background(), "nope/m"); err == nil || err.Error() != `unknown provider "nope"` {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := os.Stat(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config written on error: %v", err)
	}
	f.writeConfig(t, keyedConfig(l.URL, ""))
	before := readFile(t, cfgPath)
	a = mkAgent(t, f.opts())
	if err := a.SetModel(context.Background(), "keyed/k"); !errors.Is(err, agent.ErrNoAPIKey) {
		t.Fatalf("missing key: %v", err)
	}
	if readFile(t, cfgPath) != before {
		t.Fatal("config changed on error")
	}
	if _, err := a.Model(); !errors.Is(err, agent.ErrNoModel) {
		t.Fatalf("model changed on error: %v", err)
	}
}

// AG-FR-34: 기본 경로에 파일이 없으면 만들고, 다음 Start 가 그 모델을 쓴다
func TestSetModelCreates(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	a := mkAgent(t, f.opts())
	if err := a.SetModel(context.Background(), "ollama/llama3"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.home, ".dynamic_react")
	p := filepath.Join(dir, "config.json")
	want := "{\n  \"agent\": {\n    \"model\": {\n      \"provider\": \"ollama\",\n      \"id\": \"llama3\"\n    }\n  }\n}\n"
	if got := readFile(t, p); got != want {
		t.Fatalf("file %q", got)
	}
	if mode(t, p) != 0o644 || mode(t, dir) != 0o755 {
		t.Fatalf("modes %v %v", mode(t, p), mode(t, dir))
	}
	if ref, err := a.Model(); ref != "ollama/llama3" || err != nil {
		t.Fatalf("Model %q %v", ref, err)
	}
	f.writeConfig(t, `{"agent":null}`)
	o := f.opts()
	o.Model = "ollama/flag"
	if err := mkAgent(t, o).SetModel(context.Background(), "ollama/llama3"); err != nil || readFile(t, p) != want {
		t.Fatalf("null agent: %v %q", err, readFile(t, p))
	}
	f.writeConfig(t, keyedConfig(l.URL, ""))
	a = mkAgent(t, f.opts())
	if err := a.SetModel(context.Background(), "test/x/y"); err != nil {
		t.Fatal(err)
	}
	if s, _ := runOnce(t, a, "", "hi"); s.Reason != core.SettleCompleted || requestModel(t, l.body(0)) != "x/y" {
		t.Fatalf("%+v %s", s, l.body(0))
	}
}

// AG-FR-34: 다른 키 보존, Options.Model 보다 이김, 도구 상한 유지, 명시 경로
func TestSetModelPreserves(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	os.WriteFile(filepath.Join(f.cwd, "c.json"), []byte(keyedConfig(l.URL,
		`,"sessionDir":"~/s","agent":{"thinking":"low","model":{"provider":"test","id":"old"}},"permissions":{"allow":["read"]}`)), 0o644)
	o := f.opts()
	o.ConfigPath, o.Model, o.Tools = "c.json", "test/flag", []string{"read"}
	a := mkAgent(t, o)
	if err := a.SetModel(context.Background(), "test/new"); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(f.cwd, "c.json"))), &got); err != nil {
		t.Fatal(err)
	}
	var orig map[string]json.RawMessage
	json.Unmarshal([]byte(keyedConfig(l.URL, `,"sessionDir":"~/s","permissions":{"allow":["read"]}`)), &orig)
	for k, v := range orig {
		var a, b any
		json.Unmarshal(v, &a)
		json.Unmarshal(got[k], &b)
		if fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("key %s changed: %s → %s", k, v, got[k])
		}
	}
	var ag map[string]any
	json.Unmarshal(got["agent"], &ag)
	if fmt.Sprint(ag) != "map[model:map[id:new provider:test] thinking:low]" {
		t.Fatalf("agent %v", ag)
	}
	if s, _ := runOnce(t, a, "", "hi"); s.Reason != core.SettleCompleted || requestModel(t, l.body(0)) != "new" {
		t.Fatalf("%+v %s", s, l.body(0))
	}
	if strings.Contains(l.body(0), `"bash"`) || !strings.Contains(l.body(0), `"read"`) {
		t.Fatalf("tool limit lost: %.300s", l.body(0))
	}
}

// AG-FR-34: 쓰기 실패면 에러이고 모델 그대로
func TestSetModelWriteFailure(t *testing.T) {
	f := setup(t)
	o := f.opts()
	o.Model = "ollama/a"
	a := mkAgent(t, o)
	dir := filepath.Join(f.home, ".dynamic_react")
	os.MkdirAll(dir, 0o500)
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := a.SetModel(context.Background(), "ollama/b"); err == nil {
		t.Fatal("write into read-only dir succeeded")
	}
	if ref, _ := a.Model(); ref != "ollama/a" {
		t.Fatalf("model changed: %q", ref)
	}
	if err := os.WriteFile(filepath.Join(f.cwd, "c.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o = agent.Options{Cwd: f.cwd, ConfigPath: "c.json", NoRemoteCatalog: true}
	a = mkAgent(t, o)
	os.WriteFile(filepath.Join(f.cwd, "c.json"), []byte(`{"theme":1}`), 0o644)
	if err := a.SetModel(context.Background(), "ollama/b"); err == nil || readFile(t, filepath.Join(f.cwd, "c.json")) != `{"theme":1}` {
		t.Fatalf("invalid file rewritten: %v", err)
	}
	os.Remove(filepath.Join(f.cwd, "c.json"))
	if err := a.SetModel(context.Background(), "ollama/b"); err == nil {
		t.Fatal("missing explicit config created")
	}
}

// AG-FR-36: 키 파일 검사
func TestAuthFileValidation(t *testing.T) {
	for name, content := range map[string]string{
		"not json":      `{"anthropic":`,
		"trailing data": `{} {}`,
		"unknown key":   `{"anthropic":{"type":"api_key","key":"sk-secret-1","env":{}}}`,
		"oauth":         `{"anthropic":{"type":"oauth","access":"sk-secret-1"}}`,
		"key number":    `{"anthropic":{"type":"api_key","key":5}}`,
		"not object":    `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			f.writeAuth(t, content)
			_, err := agent.New(context.Background(), f.opts())
			if err == nil || !strings.HasPrefix(err.Error(), "agent: auth:") || strings.Contains(err.Error(), "sk-secret-1") {
				t.Fatalf("err %v", err)
			}
		})
	}
}

// AG-FR-37~39: SetKey·RemoveKey·Providers
func TestKeys(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, keyedConfig(l.URL, `,"agent":{"model":{"provider":"keyed","id":"k"}}`))
	a := mkAgent(t, f.opts())
	ctx := context.Background()
	authPath := filepath.Join(f.home, ".dynamic_react", "auth.json")
	for _, c := range []struct{ provider, key, want string }{
		{"nope", "sk-x", `unknown provider "nope"`},
		{"keyed", "", "empty API key"},
		{"keyed", " \t\n", "empty API key"},
	} {
		if err := a.SetKey(ctx, c.provider, c.key); err == nil || err.Error() != c.want {
			t.Errorf("SetKey(%q, %q) = %v, want %s", c.provider, c.key, err, c.want)
		}
	}
	if _, err := os.Stat(authPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth written on error: %v", err)
	}
	if err := a.RemoveKey(ctx, "keyed"); err == nil || err.Error() != `no stored API key for "keyed"` {
		t.Fatalf("RemoveKey missing: %v", err)
	}
	os.Remove(filepath.Join(f.home, ".dynamic_react", "config.json"))
	os.Remove(filepath.Join(f.home, ".dynamic_react"))
	if err := a.SetKey(ctx, "keyed", "  sk-k\n"); err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"keyed\": {\n    \"type\": \"api_key\",\n    \"key\": \"sk-k\"\n  }\n}\n"; readFile(t, authPath) != want {
		t.Fatalf("auth %q", readFile(t, authPath))
	}
	if mode(t, authPath) != 0o600 || mode(t, filepath.Dir(authPath)) != 0o700 {
		t.Fatalf("modes %v %v", mode(t, authPath), mode(t, filepath.Dir(authPath)))
	}
	if ref, err := a.Model(); ref != "keyed/k" || err != nil {
		t.Fatalf("Model %q %v", ref, err)
	}
	if !hasModel(a.Models(), "keyed", "k") {
		t.Fatal("keyed model not listed")
	}
	if s, _ := runOnce(t, a, "", "hi"); s.Reason != core.SettleCompleted || l.header(0).Get("Authorization") != "Bearer sk-k" {
		t.Fatalf("%+v %v", s, l.header(0))
	}
	ps := a.Providers()
	idx := map[string]agent.ProviderChoice{}
	for i, p := range ps {
		idx[p.Name] = p
		if i > 0 && ps[i-1].Name >= p.Name {
			t.Fatalf("providers order %v", ps)
		}
	}
	if idx["keyed"] != (agent.ProviderChoice{Name: "keyed", RequiresKey: true, HasKey: true}) ||
		idx["test"] != (agent.ProviderChoice{Name: "test"}) ||
		idx["anthropic"] != (agent.ProviderChoice{Name: "anthropic", RequiresKey: true}) {
		t.Fatalf("providers %v", idx)
	}

	os.WriteFile(authPath, []byte(`{"zzz": {"type":"api_key","key":"z"}, "keyed": {"type":"api_key","key":"sk-k"}}`), 0o600)
	if err := a.SetKey(ctx, "anthropic", "sk-a"); err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]string
	json.Unmarshal([]byte(readFile(t, authPath)), &m)
	if m["zzz"]["key"] != "z" || m["keyed"]["key"] != "sk-k" || m["anthropic"]["key"] != "sk-a" || len(m) != 3 {
		t.Fatalf("auth %v", m)
	}
	if err := a.RemoveKey(ctx, "keyed"); err != nil {
		t.Fatal(err)
	}
	m = nil
	json.Unmarshal([]byte(readFile(t, authPath)), &m)
	if _, ok := m["keyed"]; ok || len(m) != 2 {
		t.Fatalf("after remove %v", m)
	}
	if _, err := a.Model(); !errors.Is(err, agent.ErrNoAPIKey) {
		t.Fatalf("after remove Model %v", err)
	}
	if hasModel(a.Models(), "keyed", "k") {
		t.Fatal("keyed model still listed")
	}

	os.WriteFile(authPath, []byte(`{"keyed":{"type":"oauth"}}`), 0o600)
	if err := a.SetKey(ctx, "keyed", "sk-new"); err == nil || readFile(t, authPath) != `{"keyed":{"type":"oauth"}}` {
		t.Fatalf("invalid auth rewritten: %v", err)
	}
	if _, err := a.Model(); !errors.Is(err, agent.ErrNoAPIKey) {
		t.Fatalf("key set despite error: %v", err)
	}
}

// AG-FR-37: 쓰기 실패면 에러이고 키 그대로. home 이 없으면 쓸 수 없다
func TestKeyWriteFailure(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, keyedConfig(l.URL, `,"agent":{"model":{"provider":"keyed","id":"k"}}`))
	a := mkAgent(t, f.opts())
	dir := filepath.Join(f.home, ".dynamic_react")
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := a.SetKey(context.Background(), "keyed", "sk-k"); err == nil {
		t.Fatal("write into read-only dir succeeded")
	}
	if _, err := a.Model(); !errors.Is(err, agent.ErrNoAPIKey) {
		t.Fatalf("key set despite write failure: %v", err)
	}
	os.Chmod(dir, 0o755)
	t.Setenv("HOME", "")
	os.WriteFile(filepath.Join(f.cwd, "c.json"), []byte(`{"sessionDir":"`+f.home+`/s"}`), 0o644)
	a = mkAgent(t, agent.Options{Cwd: f.cwd, ConfigPath: "c.json", NoRemoteCatalog: true})
	if err := a.SetKey(context.Background(), "anthropic", "sk-a"); err == nil {
		t.Fatal("no home: SetKey succeeded")
	}
}

// AG-FR-06: shellPath
func TestShellPath(t *testing.T) {
	f := setup(t)
	f.writeConfig(t, `{"shellPath":"bin/sh"}`)
	if _, err := agent.New(context.Background(), f.opts()); err == nil || !strings.HasPrefix(err.Error(), "agent: config: shellPath") {
		t.Fatalf("relative shellPath: %v", err)
	}
	shell := filepath.Join(f.home, "myshell")
	os.WriteFile(shell, []byte("#!/bin/sh\necho custom-shell\nexec /bin/sh \"$@\"\n"), 0o755)
	l := newLLM(t, toolReply("bash", `{"command":"echo hi"}`), textReply("done"))
	f.writeConfig(t, testConfig(l.URL, `,"shellPath":"`+shell+`"`))
	a := mkAgent(t, f.opts())
	if s, _ := runOnce(t, a, "", "run"); s.Reason != core.SettleCompleted || !strings.Contains(l.body(1), "custom-shell") {
		t.Fatalf("%+v %s", s, l.body(1))
	}
}

// AG-FR-35: 동시 호출
func TestModelConcurrency(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	f.writeConfig(t, keyedConfig(l.URL, ""))
	a := mkAgent(t, f.opts())
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.SetModel(ctx, fmt.Sprintf("test/m%d", i))
			a.SetKey(ctx, "keyed", fmt.Sprintf("sk-%d", i))
			a.Model()
			a.Models()
			a.Providers()
			if r, err := a.Start(ctx, fmt.Sprintf("s%d", i), user("hi")); err == nil {
				r.Wait()
			}
			a.Compact(ctx, fmt.Sprintf("s%d", i))
			a.RemoveKey(ctx, "keyed")
		}()
	}
	wg.Wait()
	if ref, err := a.Model(); err != nil || !strings.HasPrefix(ref, "test/m") {
		t.Fatalf("%q %v", ref, err)
	}
}
