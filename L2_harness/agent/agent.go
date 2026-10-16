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
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
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
	NoRemoteCatalog bool                // adapters.Options.NoRemoteCatalog 로 전달
	Tools           []string            // 도구 상한 (AG-FR-21)
	Permissions     Permissions         // surface 프로필 규칙 (AG-FR-12)
	Approver        Approver            // nil = 묻기 없이 허용 (AG-FR-18)
	OnEvent         func(harness.Event) // AG-FR-27

	Sets        func(d sets.Deps) []harness.SetEntry        // AG-FR-40
	ToolSet     func(def []sets.Tool) []sets.Tool           // AG-FR-40
	System      func(def []contract.Block) []contract.Block // AG-FR-40
	Summarizer  func(def sets.Summarizer) sets.Summarizer   // AG-FR-40
	Store       func(def harness.Store) harness.Store       // AG-FR-40
	ModelCaller func(def sets.ModelCaller) sets.ModelCaller // AG-FR-41
	Modes       []harness.Mode                              // AG-FR-48 (R27)
}

type Agent struct {
	h           *harness.Agent
	env         *osenv.Env
	client      *adapters.Client
	cfgPath     string // AG-FR-01 의 경로, "" = 없음 (home 없음)
	cfgExplicit bool
	authPath    string // AG-FR-36, "" = 없음
	sessionDir  string
	rules       *ruleSet
	approver    Approver
	observer    core.Observer

	smu    sync.Mutex        // config·model·keys 와 설정·키 파일 쓰기 (AG-FR-35)
	config json.RawMessage   // AG-FR-04 의 raw
	model  string            // "provider/id", "" = 없음
	keys   map[string]string // provider → 키 (AG-FR-36)

	mu      sync.Mutex
	busy    map[string]bool            // AG-FR-22
	allowed map[string]map[string]bool // 세션 → allow_session 규칙 (AG-FR-19)
}

// fileConfig 는 설정 파일의 최상위다 (AG-FR-02).
type fileConfig struct {
	SessionDir  string          `json:"sessionDir"`
	ShellPath   string          `json:"shellPath"`
	Agent       json.RawMessage `json:"agent"`
	Providers   json.RawMessage `json:"providers"`
	Models      json.RawMessage `json:"models"`
	Permissions *Permissions    `json:"permissions"`
	Sets        json.RawMessage `json:"sets"`
}

// New 는 AG-FR-09 순서로 조립한다.
func New(ctx context.Context, o Options) (*Agent, error) {
	cwd, err := filepath.Abs(o.Cwd)
	if err != nil {
		return nil, fmt.Errorf("agent: cwd: %w", err)
	}
	e, err := osenv.New(cwd, "")
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	cfgPath, explicit := configPath(e, o.ConfigPath)
	fc, err := loadConfig(ctx, e, cfgPath, explicit)
	if err != nil {
		return nil, fmt.Errorf("agent: config: %w", err)
	}
	if fc.ShellPath != "" {
		if !path.IsAbs(fc.ShellPath) {
			return nil, fmt.Errorf("agent: config: shellPath %q is not absolute", fc.ShellPath)
		}
		if e, err = osenv.New(cwd, fc.ShellPath); err != nil {
			return nil, fmt.Errorf("agent: %w", err)
		}
	}
	cfg, model, err := agentConfig(fc.Agent, o.Model)
	if err != nil {
		return nil, err
	}
	layout, err := parseSets(fc.Sets)
	if err != nil {
		return nil, fmt.Errorf("agent: config: sets: %w", err)
	}
	var authPath string
	if e.Home() != "" {
		authPath = path.Join(e.Home(), ".dynamic_react", "auth.json")
	}
	keys, err := loadAuth(ctx, e, authPath)
	if err != nil {
		return nil, fmt.Errorf("agent: auth: %w", err)
	}
	dir, err := sessionDir(fc.SessionDir, e.Home())
	if err != nil {
		return nil, fmt.Errorf("agent: sessionDir: %w", err)
	}
	acfg, err := adaptersConfig(fc)
	if err != nil {
		return nil, fmt.Errorf("agent: config: %w", err)
	}
	a := &Agent{env: e, cfgPath: cfgPath, cfgExplicit: explicit, authPath: authPath, sessionDir: dir, model: model, keys: keys,
		approver: o.Approver, observer: o.Observer, busy: map[string]bool{}, allowed: map[string]map[string]bool{}}
	a.client = adapters.New(adapters.Options{APIKey: a.apiKey, Config: acfg, NoRemoteCatalog: o.NoRemoteCatalog})
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
	if a.rules, err = compileRules(perms, e.Cwd(), e.Home()); err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	var caller sets.ModelCaller = a.client
	if o.ModelCaller != nil {
		if caller = o.ModelCaller(caller); caller == nil {
			return nil, errors.New("agent: ModelCaller returned nil")
		}
	}
	h, err := harness.New(ctx, harness.Options{Env: e, Model: caller, SessionDir: dir, OnEvent: o.OnEvent, Gate: a.gate,
		Sets: o.Sets, Layout: layout, ToolSet: o.ToolSet, System: o.System, Summarizer: o.Summarizer, Store: o.Store, Modes: o.Modes})
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
	var cfg struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
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
	json.Unmarshal(raw, &m) //nolint:errcheck // LINT-REPORT #1
	m["tools"], _ = json.Marshal(names)
	return json.Marshal(m)
}

// configPath 는 AG-FR-01 의 경로와 명시 여부다. home 이 없고 명시하지 않았으면 "".
func configPath(e *osenv.Env, p string) (string, bool) {
	switch {
	case p != "" && !path.IsAbs(p):
		return path.Join(e.Cwd(), p), true
	case p != "":
		return p, true
	case e.Home() == "":
		return "", false
	}
	return path.Join(e.Home(), ".dynamic_react", "config.json"), false
}

// loadConfig 는 AG-FR-01·02 다. 기본 경로에 파일이 없으면 빈 설정이다.
func loadConfig(ctx context.Context, e *osenv.Env, p string, explicit bool) (fileConfig, error) {
	if p == "" {
		return fileConfig{}, nil
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

// splitRef 는 AG-FR-05 다: 첫 "/" 에서 나누고 어느 쪽도 비면 안 된다.
func splitRef(ref string) (string, string, error) {
	provider, id, ok := strings.Cut(ref, "/")
	if !ok || provider == "" || id == "" {
		return "", "", fmt.Errorf("agent: model %q: want provider/id", ref)
	}
	return provider, id, nil
}

// withModel 은 sets 설정 객체 raw 의 model 을 바꾼다.
func withModel(raw json.RawMessage, provider, id string) (json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	m["model"], _ = json.Marshal(sets.ConfigModel{Provider: provider, ID: id})
	return json.Marshal(m)
}

// agentConfig 는 AG-FR-04·05 다: 모델을 덮은 sets 설정 raw 와 "provider/id" (모델이 없으면 "").
func agentConfig(raw json.RawMessage, override string) (json.RawMessage, string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if override != "" {
		provider, id, err := splitRef(override)
		if err != nil {
			return nil, "", err
		}
		if raw, err = withModel(raw, provider, id); err != nil {
			return nil, "", fmt.Errorf("agent: config: agent: %w", err)
		}
	}
	if _, err := harness.ParseAux(raw); err != nil {
		return nil, "", fmt.Errorf("agent: config: %w", err)
	}
	cfg, err := sets.ParseConfig(raw)
	if err == nil {
		return raw, cfg.Model.Provider + "/" + cfg.Model.ID, nil
	}
	var m struct {
		Model *sets.ConfigModel `json:"model"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil, "", fmt.Errorf("agent: config: %w", err)
	}
	// 모델만 없을 수 있다: 나머지 검사는 자리표시 모델로 한다 (AG-FR-04).
	placeholder, err := withModel(raw, "_", "_")
	if err == nil {
		_, err = sets.ParseConfig(placeholder)
	}
	if err != nil {
		return nil, "", fmt.Errorf("agent: config: %w", err)
	}
	return raw, "", nil
}

// sessionDir 는 AG-FR-07 이다: 설정 > 기본.
func sessionDir(configured, home string) (string, error) {
	dir := configured
	if dir == "" {
		dir = session.DefaultDir(home)
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
