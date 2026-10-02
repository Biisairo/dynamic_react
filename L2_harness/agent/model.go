package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Biisairo/dynamic_react/L2_harness/osenv"
)

// ErrNoModel 은 모델이 설정되지 않은 모델 문제다 (AG-FR-32).
var ErrNoModel = errors.New("no model configured")

// ErrNoAPIKey 는 키가 필요한 provider 에 키가 없는 모델 문제다 (AG-FR-08).
var ErrNoAPIKey = errors.New("missing API key")

// authAPIKey 는 키 파일 항목의 type 이다 (pi auth.json, AG-FR-36).
const authAPIKey = "api_key"

type ModelChoice struct{ Provider, ID string }

type ProviderChoice struct {
	Name        string
	RequiresKey bool // 프로필의 RequiresKey
	HasKey      bool // auth.json 에 키가 있음
}

type authEntry struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// problem 은 ref 의 모델 문제다 (AG-FR-08, 32). smu 를 잡고 부른다.
func (a *Agent) problem(ref string) error {
	if ref == "" {
		return ErrNoModel
	}
	provider, _, _ := strings.Cut(ref, "/")
	p, ok := a.client.Profile(provider)
	if !ok {
		return fmt.Errorf("unknown provider %q", provider)
	}
	if p.RequiresKey && a.keys[provider] == "" {
		return fmt.Errorf("%w for %q", ErrNoAPIKey, provider)
	}
	return nil
}

// runConfig 는 Start·Compact 에 넘길 설정이다. 모델 문제가 있으면 그 에러 (AG-FR-10, 31).
func (a *Agent) runConfig() (json.RawMessage, error) {
	a.smu.Lock()
	defer a.smu.Unlock()
	return a.config, a.problem(a.model)
}

// apiKey 는 adapters 의 Options.APIKey 다 (AG-FR-36).
func (a *Agent) apiKey(provider string) string {
	a.smu.Lock()
	defer a.smu.Unlock()
	return a.keys[provider]
}

// Model 은 AG-FR-32 다.
func (a *Agent) Model() (string, error) {
	a.smu.Lock()
	defer a.smu.Unlock()
	return a.model, a.problem(a.model)
}

// Models 는 AG-FR-33 이다.
func (a *Agent) Models() []ModelChoice {
	a.smu.Lock()
	defer a.smu.Unlock()
	var out []ModelChoice
	for _, m := range a.client.Models() {
		if p, _ := a.client.Profile(m.Provider); !p.RequiresKey || a.keys[m.Provider] != "" {
			out = append(out, ModelChoice{Provider: m.Provider, ID: m.ID})
		}
	}
	return out
}

// Providers 는 AG-FR-39 다.
func (a *Agent) Providers() []ProviderChoice {
	a.smu.Lock()
	defer a.smu.Unlock()
	names := a.client.Providers()
	out := make([]ProviderChoice, 0, len(names))
	for _, n := range names {
		p, _ := a.client.Profile(n)
		out = append(out, ProviderChoice{Name: n, RequiresKey: p.RequiresKey, HasKey: a.keys[n] != ""})
	}
	return out
}

// SetModel 은 AG-FR-34 다.
func (a *Agent) SetModel(ctx context.Context, ref string) error {
	provider, id, err := splitRef(ref)
	if err != nil {
		return err
	}
	a.smu.Lock()
	defer a.smu.Unlock()
	if err := a.problem(ref); err != nil {
		return err
	}
	cfg, err := withModel(a.config, provider, id)
	if err != nil {
		return err
	}
	if err := a.saveModel(ctx, provider, id); err != nil {
		return err
	}
	a.config, a.model = cfg, ref
	return nil
}

// saveModel 은 사용자 설정 파일의 agent.model 만 바꿔 쓴다 (AG-FR-34).
func (a *Agent) saveModel(ctx context.Context, provider, id string) error {
	return a.saveConfig(ctx, func(top map[string]json.RawMessage) error {
		ag := top["agent"]
		if len(ag) == 0 {
			ag = json.RawMessage(`{}`)
		}
		var err error
		if top["agent"], err = withModel(ag, provider, id); err != nil {
			return fmt.Errorf("agent: config: %s: agent: %w", a.cfgPath, err)
		}
		return nil
	})
}

// saveConfig 는 사용자 설정 파일을 읽어 change 로 최상위 키를 바꾸고 다시 쓴다. 다른 키는 그대로 둔다 (AG-FR-34, 45).
func (a *Agent) saveConfig(ctx context.Context, change func(top map[string]json.RawMessage) error) error {
	b, err := a.env.ReadFile(ctx, a.cfgPath)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !a.cfgExplicit:
		b = []byte(`{}`)
	case err != nil:
		return fmt.Errorf("agent: config: %w", err)
	}
	var fc fileConfig
	if err := decodeStrict(b, &fc); err != nil {
		return fmt.Errorf("agent: config: %s: %w", a.cfgPath, err)
	}
	var top map[string]json.RawMessage
	json.Unmarshal(b, &top)
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	if err := change(top); err != nil {
		return err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return fmt.Errorf("agent: config: %w", err)
	}
	if err := a.env.WriteFile(ctx, a.cfgPath, append(out, '\n')); err != nil {
		return fmt.Errorf("agent: config: %w", err)
	}
	return nil
}

// SetKey 는 AG-FR-37 이다.
func (a *Agent) SetKey(ctx context.Context, provider, key string) error {
	if _, ok := a.client.Profile(provider); !ok {
		return fmt.Errorf("unknown provider %q", provider)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty API key")
	}
	entry, _ := json.Marshal(authEntry{Type: authAPIKey, Key: key})
	return a.updateAuth(ctx, provider, entry, key)
}

// RemoveKey 는 AG-FR-38 이다.
func (a *Agent) RemoveKey(ctx context.Context, provider string) error {
	return a.updateAuth(ctx, provider, nil, "")
}

// updateAuth 는 키 파일의 provider 항목을 entry 로 바꾸거나(nil 이면 지움) 쓰고, 성공하면 메모리의 키를 바꾼다.
func (a *Agent) updateAuth(ctx context.Context, provider string, entry json.RawMessage, key string) error {
	a.smu.Lock()
	defer a.smu.Unlock()
	if a.authPath == "" {
		return errors.New("agent: auth: no home directory")
	}
	raw := map[string]json.RawMessage{}
	b, err := a.env.ReadFile(ctx, a.authPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("agent: auth: %w", err)
	default:
		if raw, _, err = parseAuth(b); err != nil {
			return fmt.Errorf("agent: auth: %s: %w", a.authPath, err)
		}
	}
	if entry == nil {
		if _, ok := raw[provider]; !ok {
			return fmt.Errorf("no stored API key for %q", provider)
		}
		delete(raw, provider)
	} else {
		raw[provider] = entry
	}
	if err := writePrivate(a.authPath, raw); err != nil {
		return fmt.Errorf("agent: auth: %w", err)
	}
	if entry == nil {
		delete(a.keys, provider)
	} else {
		a.keys[provider] = key
	}
	return nil
}

// loadAuth 는 AG-FR-36 이다. 파일이 없으면 키가 없다.
func loadAuth(ctx context.Context, e *osenv.Env, p string) (map[string]string, error) {
	if p == "" {
		return map[string]string{}, nil
	}
	b, err := e.ReadFile(ctx, p)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	_, keys, err := parseAuth(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return keys, nil
}

// parseAuth 는 키 파일의 항목 원문과 키다. 에러 문구에 키 값을 넣지 않는다 (AG-C1).
func parseAuth(b []byte) (map[string]json.RawMessage, map[string]string, error) {
	var raw map[string]json.RawMessage
	if err := decodeStrict(b, &raw); err != nil {
		return nil, nil, err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	keys := make(map[string]string, len(raw))
	for name, v := range raw {
		var e authEntry
		if err := decodeStrict(v, &e); err != nil {
			return nil, nil, fmt.Errorf("%q: %w", name, err)
		}
		if e.Type != authAPIKey {
			return nil, nil, fmt.Errorf("%q: unsupported type %q", name, e.Type)
		}
		keys[name] = e.Key
	}
	return raw, keys, nil
}

// writePrivate 는 같은 디렉터리의 임시 파일(0600)에 쓰고 rename 한다. 디렉터리가 없으면 0700 으로 만든다 (AG-FR-37).
func writePrivate(p string, raw map[string]json.RawMessage) error {
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".auth-*.json")
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
