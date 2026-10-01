// Package agent 는 사용자 환경 위에서 도는 완성된 에이전트다 (agent SPEC, R17).
// 설정 파일·환경변수를 해석하고 osenv·adapters 를 만들어 harness.New 에 넣는다.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/adapters"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
	"github.com/Biisairo/dynamic_react/L2_harness/osenv"
)

type Options struct {
	Cwd             string // "" → os.Getwd
	ConfigPath      string // "" → <home>/.dynamic_react/config.json
	Model           string // "provider/id", 설정의 agent.model 을 덮음
	Observer        core.Observer
	Getenv          func(string) string // nil → os.Getenv
	NoRemoteCatalog bool                // adapters.Options.NoRemoteCatalog 로 전달
	Tools           []string            // 도구 상한 (AG-FR-21)
	Permissions     Permissions         // surface 프로필 규칙 (AG-FR-12)
	Approver        Approver            // nil = 묻기 없이 허용 (AG-FR-18)
	OnEvent         func(harness.Event) // AG-FR-27
}

type Agent struct {
	h          *harness.Agent
	env        *osenv.Env
	config     json.RawMessage
	sessionDir string
	rules      *ruleSet
	approver   Approver
	observer   core.Observer

	mu      sync.Mutex
	busy    map[string]bool            // AG-FR-22
	allowed map[string]map[string]bool // 세션 → allow_session 규칙 (AG-FR-19)
}

// fileConfig 는 설정 파일의 최상위다 (AG-FR-02).
type fileConfig struct {
	SessionDir  string          `json:"sessionDir"`
	Agent       json.RawMessage `json:"agent"`
	Providers   json.RawMessage `json:"providers"`
	Models      json.RawMessage `json:"models"`
	Permissions *Permissions    `json:"permissions"`
}

// New 는 AG-FR-09 순서로 조립한다.
func New(ctx context.Context, o Options) (*Agent, error) {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	cwd, err := filepath.Abs(o.Cwd)
	if err != nil {
		return nil, fmt.Errorf("agent: cwd: %w", err)
	}
	e, err := osenv.New(cwd)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	fc, err := loadConfig(ctx, e, o.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("agent: config: %w", err)
	}
	cfg, provider, err := agentConfig(fc.Agent, o.Model)
	if err != nil {
		return nil, err
	}
	dir, err := sessionDir(getenv, fc.SessionDir, e.Home())
	if err != nil {
		return nil, fmt.Errorf("agent: sessionDir: %w", err)
	}
	acfg, err := adaptersConfig(fc)
	if err != nil {
		return nil, fmt.Errorf("agent: config: %w", err)
	}
	client := adapters.New(adapters.Options{Getenv: getenv, Config: acfg, NoRemoteCatalog: o.NoRemoteCatalog})
	p, ok := client.Profile(provider)
	if !ok {
		return nil, fmt.Errorf("agent: unknown provider %q", provider)
	}
	if p.APIKeyEnv != "" && getenv(p.APIKeyEnv) == "" {
		return nil, fmt.Errorf("agent: missing API key: set %s", p.APIKeyEnv)
	}
	project, err := loadProject(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("agent: project config: %w", err)
	}
	var perms Permissions
	for _, p := range []*Permissions{fc.Permissions, &project, &o.Permissions} {
		if p != nil {
			perms.Allow = append(perms.Allow, p.Allow...)
			perms.Ask = append(perms.Ask, p.Ask...)
			perms.Deny = append(perms.Deny, p.Deny...)
		}
	}
	rules, err := compileRules(perms, e.Cwd(), e.Home())
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	a := &Agent{env: e, sessionDir: dir, rules: rules, approver: o.Approver, observer: o.Observer,
		busy: map[string]bool{}, allowed: map[string]map[string]bool{}}
	h, err := harness.New(ctx, harness.Options{Env: e, Model: client, SessionDir: dir, OnEvent: o.OnEvent, Gate: a.gate})
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	if cfg, err = limitTools(cfg, o.Tools, h.Tools().Names()); err != nil {
		return nil, fmt.Errorf("agent: tools: %w", err)
	}
	a.h, a.config = h, cfg
	return a, nil
}

// loadProject 는 <cwd>/.dynamic_react/config.json 의 permissions 다. 파일이 없으면 비어 있다 (AG-FR-12).
func loadProject(ctx context.Context, e *osenv.Env) (Permissions, error) {
	p := path.Join(e.Cwd(), ".dynamic_react", "config.json")
	b, err := e.ReadFile(ctx, p)
	if errors.Is(err, fs.ErrNotExist) {
		return Permissions{}, nil
	}
	if err != nil {
		return Permissions{}, err
	}
	var pc struct {
		Permissions Permissions `json:"permissions"`
	}
	if err := decodeStrict(b, &pc); err != nil {
		return Permissions{}, fmt.Errorf("%s: %w", p, err)
	}
	return pc.Permissions, nil
}

// limitTools 는 AG-FR-21 이다: 실제 도구 = limit ∩ (설정 tools, 비면 registry 전부), registry 순서.
func limitTools(raw json.RawMessage, limit, registry []string) (json.RawMessage, error) {
	if limit == nil {
		return raw, nil
	}
	known := map[string]bool{}
	for _, n := range registry {
		known[n] = true
	}
	want := map[string]bool{}
	for _, n := range limit {
		if !known[n] {
			return nil, fmt.Errorf("unknown tool %q", n)
		}
		want[n] = true
	}
	cfg, err := sets.ParseConfig(raw)
	if err != nil {
		return nil, err
	}
	configured := map[string]bool{}
	for _, n := range cfg.Tools {
		configured[n] = true
	}
	var names []string
	for _, n := range registry {
		if want[n] && (len(cfg.Tools) == 0 || configured[n]) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no tool left after applying the limit")
	}
	var m map[string]json.RawMessage
	json.Unmarshal(raw, &m)
	m["tools"], _ = json.Marshal(names)
	return json.Marshal(m)
}

// loadConfig 는 AG-FR-01·02 다. 기본 경로에 파일이 없으면 빈 설정이다.
func loadConfig(ctx context.Context, e *osenv.Env, configPath string) (fileConfig, error) {
	p, explicit := configPath, configPath != ""
	switch {
	case explicit && !path.IsAbs(p):
		p = path.Join(e.Cwd(), p)
	case !explicit && e.Home() == "":
		return fileConfig{}, nil
	case !explicit:
		p = path.Join(e.Home(), ".dynamic_react", "config.json")
	}
	b, err := e.ReadFile(ctx, p)
	if !explicit && errors.Is(err, fs.ErrNotExist) {
		return fileConfig{}, nil
	}
	if err != nil {
		return fileConfig{}, err
	}
	var fc fileConfig
	if err := decodeStrict(b, &fc); err != nil {
		return fileConfig{}, fmt.Errorf("%s: %w", p, err)
	}
	return fc, nil
}

// decodeStrict 는 모르는 키와 뒤따르는 데이터를 거부한다.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

// agentConfig 는 AG-FR-04·05 다: 모델을 덮은 sets 설정 raw 와 provider 이름.
func agentConfig(raw json.RawMessage, model string) (json.RawMessage, string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if model != "" {
		provider, id, ok := strings.Cut(model, "/")
		if !ok {
			return nil, "", fmt.Errorf("agent: model %q: want provider/id", model)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, "", fmt.Errorf("agent: config: agent: %w", err)
		}
		ref, _ := json.Marshal(sets.ConfigModel{Provider: provider, ID: id})
		if m == nil {
			m = map[string]json.RawMessage{}
		}
		m["model"] = ref
		raw, _ = json.Marshal(m)
	}
	cfg, err := sets.ParseConfig(raw)
	if err != nil {
		return nil, "", fmt.Errorf("agent: config: %w", err)
	}
	return raw, cfg.Model.Provider, nil
}

// sessionDir 는 AG-FR-07 이다: DR_SESSION_DIR > 설정 > 기본.
func sessionDir(getenv func(string) string, configured, home string) (string, error) {
	dir := getenv(session.DirEnv)
	if dir == "" {
		dir = configured
	}
	if dir == "" {
		dir = session.DefaultDir(getenv, home)
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home == "" {
			return "", fmt.Errorf("%q needs a home directory", dir)
		}
		dir = path.Join(home, dir[1:])
	}
	return path.Clean(dir), nil
}

// adaptersConfig 는 AG-FR-03 이다.
func adaptersConfig(fc fileConfig) (adapters.Config, error) {
	m := map[string]json.RawMessage{}
	if fc.Providers != nil {
		m["providers"] = fc.Providers
	}
	if fc.Models != nil {
		m["models"] = fc.Models
	}
	b, _ := json.Marshal(m)
	return adapters.ParseConfig(b)
}

func (a *Agent) Harness() *harness.Agent { return a.h }
func (a *Agent) SessionDir() string      { return a.sessionDir }
