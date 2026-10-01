// Package harness 는 L1(inject + sets) 위의 코딩 에이전트를 함수 하나(Agent)로 조립한다 (R13, R14).
// 실제 입출력은 하지 않는다: 파일·프로세스는 Env, 모델은 sets.ModelCaller 로 받는다 (HN-C1).
package harness

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/prompt"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/tools"
)

// Env 는 env.Env 의 별칭이다.
type Env = env.Env

type Options struct {
	Env        Env                  // 필수
	Model      sets.ModelCaller     // 필수
	SessionDir string               // 필수, 절대 경로
	Tools      []sets.Tool          // 추가·교체 (같은 이름은 덮음)
	Extra      []inject.FunctionSet // base·resilient 위에 겹칠 세트
	Observer   core.Observer
	Core       core.Options // Observer 는 위 필드가 이긴다
	Now        func() time.Time
	NewID      func() string
	OnEvent    func(Event)                                                  // HN-FR-31
	Gate       func(ctx context.Context, in GateInput) *contract.ToolResult // HN-FR-30
}

type RunRequest struct {
	SessionID string // 비면 session.NewID
	Input     []contract.Message
	Config    json.RawMessage // sets 설정 JSON
	Observer  core.Observer   // 이 run 에만 쓰는 Observer (HN-FR-27)
}

// Agent 는 조립된 에이전트다. 여러 run 을 동시에 시작할 수 있다 (HN-FR-28).
type Agent struct {
	kernel *core.Kernel
	disp   core.Dispatcher
	copts  core.Options
	system []contract.Block
	tools  *sets.Registry
	now    func() time.Time
	sf     *surface
	store  *session.Store
	model  sets.ModelCaller
	sum    sets.Summarizer // run 밖에서는 이벤트 없이 통과한다 (HN-FR-36)
	newID  func() string
}

// New 는 도구·세션·요약기·시스템 프롬프트를 한 번 만들고 base·resilient(+Extra) 로 Dispatcher 를 만든다 (HN-FR-26).
func New(ctx context.Context, o Options) (*Agent, error) {
	switch {
	case o.Env == nil:
		return nil, errors.New("harness: Env required")
	case o.Model == nil:
		return nil, errors.New("harness: Model required")
	case !path.IsAbs(o.SessionDir):
		return nil, fmt.Errorf("harness: SessionDir must be an absolute path: %q", o.SessionDir)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	reg := sets.NewRegistry(tools.All(o.Env)...)
	for _, t := range o.Tools {
		reg.Add(t)
	}
	store, err := session.New(o.Env, o.SessionDir)
	if err != nil {
		return nil, err
	}
	system, err := buildSystem(ctx, o, reg)
	if err != nil {
		return nil, err
	}
	sf := &surface{onEvent: o.OnEvent, gate: o.Gate}
	var summarizer sets.Summarizer = summary.New(o.Model, o.Now)
	var sleep func(context.Context, time.Duration) error
	if o.OnEvent != nil {
		summarizer, sleep = eventSummarizer{sf, summarizer}, sf.sleep
	}
	deps := sets.Deps{Model: o.Model, Tools: reg, Store: store, System: system,
		Summarizer: summarizer, Sleep: sleep, Now: o.Now, NewID: o.NewID}
	fsets := append([]inject.FunctionSet{sets.Base(deps), sets.Resilient(deps)}, o.Extra...)
	if sf.active() {
		fsets = sf.wrap(fsets)
	}
	disp, err := inject.BindWith(inject.BindOptions{StateStore: sets.StateStoreOf(store)}, fsets...)
	if err != nil {
		return nil, err
	}
	copts := o.Core
	if o.Observer != nil {
		copts.Observer = o.Observer
	}
	newID := o.NewID
	if newID == nil {
		newID = randomID
	}
	return &Agent{kernel: core.New(disp, copts), disp: disp, copts: copts, system: system, tools: reg, now: o.Now, sf: sf,
		store: store, model: o.Model, sum: summarizer, newID: newID}, nil
}

func buildSystem(ctx context.Context, o Options, reg *sets.Registry) ([]contract.Block, error) {
	files, err := prompt.ContextFiles(ctx, o.Env, o.Env.Cwd())
	if err != nil {
		return nil, fmt.Errorf("harness: context files: %w", err)
	}
	skills, err := prompt.Skills(ctx, o.Env, o.Env.Cwd(), o.Env.Home())
	if err != nil {
		return nil, fmt.Errorf("harness: skills: %w", err)
	}
	var lines []prompt.Tool
	for _, name := range reg.Names() {
		snippet := tools.Snippet(name)
		if snippet == "" {
			t, _ := reg.Get(name)
			snippet, _, _ = strings.Cut(t.Spec.Description, "\n")
		}
		lines = append(lines, prompt.Tool{Name: name, Snippet: snippet})
	}
	return prompt.Build(prompt.Input{Tools: lines,
		ContextFiles: files, Skills: skills, Cwd: o.Env.Cwd()}), nil
}

// Start 는 설정을 검사하고 run 을 시작한다. 두 번째 반환값은 쓰인 sessionID 다 (HN-FR-27).
func (a *Agent) Start(ctx context.Context, r RunRequest) (*core.Run, string, error) {
	if _, err := sets.ParseConfig(r.Config); err != nil {
		return nil, "", err
	}
	sid := r.SessionID
	if sid == "" {
		sid = session.NewID(a.now(), rand.Reader)
	}
	if err := session.CheckID(sid); err != nil {
		return nil, "", err
	}
	if a.sf.active() {
		rs := newRunState()
		ctx = context.WithValue(ctx, runKey{}, rs)
		if a.sf.onEvent != nil {
			ctx = tools.WithProgress(ctx, a.sf.progress(rs))
		}
	}
	k := a.kernel
	if r.Observer != nil {
		opts := a.copts
		opts.Observer = r.Observer
		k = core.New(a.disp, opts)
	}
	return k.Start(ctx, core.RunInput{SessionID: sid, Input: r.Input, Config: r.Config}), sid, nil
}

// System 은 조립된 시스템 프롬프트다 (HN-FR-29).
func (a *Agent) System() []contract.Block { return append([]contract.Block(nil), a.system...) }

// Tools 는 도구 레지스트리다 (HN-FR-29).
func (a *Agent) Tools() sets.ToolRegistry { return a.tools }

// Emit 은 OnEvent 를 같은 직렬화로 부른다 (HN-FR-35).
func (a *Agent) Emit(ev Event) { a.sf.emit(ev) }
