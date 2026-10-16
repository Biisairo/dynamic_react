package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
)

// Mode 는 이름 붙은 배치 변형이다: 기본 배치 위에 Push 를 쌓고 Off 를 끈다 (HN-FR-55, 56).
type Mode struct {
	Name, Description string
	Push              []string
	Off               []string
}

type ModeOrigin string

const (
	ModeUser  ModeOrigin = "user"
	ModeModel ModeOrigin = "model"
	ModeSet   ModeOrigin = "set"
)

// ModeRequest 는 전환 요청이다. To "" = 기본 모드로.
type ModeRequest struct {
	To, Arg string
	Origin  ModeOrigin
	Reason  string
}

// ModeStatus 는 세션의 모드 상태다. Active "" = 기본 모드.
type ModeStatus struct {
	Active, Arg string
	Pending     *ModeRequest
}

type ModeChange struct {
	From, To, Arg string
	Origin        ModeOrigin
	Reason        string
}

// ModeNamespace 는 모드 상태의 세션 상태 네임스페이스이고 세트 이름으로 쓸 수 없다 (HN-FR-55, 59).
const ModeNamespace = "harness.mode"

var (
	ErrUnknownMode = errors.New("harness: unknown mode")
	ErrModeDenied  = errors.New("harness: mode request denied")
)

const (
	keyModeActive  = "active"
	keyModeArg     = "arg"
	keyModePending = "pending"
)

// checkModes 는 HN-FR-55 의 정의 검사다.
func checkModes(reg []SetEntry, modes []Mode) error {
	known := map[string]bool{}
	for _, e := range reg {
		known[e.Set.Name] = true
	}
	seen := map[string]bool{}
	for _, m := range modes {
		if m.Name == "" {
			return errors.New("harness: mode: empty name")
		}
		if seen[m.Name] {
			return fmt.Errorf("harness: mode %s: duplicate name", m.Name)
		}
		seen[m.Name] = true
		push := map[string]bool{}
		for _, n := range m.Push {
			switch {
			case !known[n]:
				return fmt.Errorf("harness: mode %s: unknown set %q", m.Name, n)
			case push[n]:
				return fmt.Errorf("harness: mode %s: set %q pushed twice", m.Name, n)
			}
			push[n] = true
		}
		for _, n := range m.Off {
			switch {
			case !known[n]:
				return fmt.Errorf("harness: mode %s: unknown set %q", m.Name, n)
			case push[n]:
				return fmt.Errorf("harness: mode %s: set %q both pushed and off", m.Name, n)
			}
		}
	}
	return nil
}

// modeLayout 은 기본 배치에서 Push 를 빼고 Off 를 끈 뒤 Push 를 맨 위에 켜서 붙인다 (HN-FR-56).
func modeLayout(base []SetState, m Mode) []SetState {
	push, off := map[string]bool{}, map[string]bool{}
	for _, n := range m.Push {
		push[n] = true
	}
	for _, n := range m.Off {
		off[n] = true
	}
	desc := map[string]string{}
	var out []SetState
	for _, s := range base {
		desc[s.Name] = s.Description
		if push[s.Name] {
			continue
		}
		if off[s.Name] {
			s.On = false
		}
		out = append(out, s)
	}
	for _, n := range m.Push {
		out = append(out, SetState{Name: n, Description: desc[n], On: true})
	}
	return out
}

// bundle 은 기본 배치와 모드 배치의 Bind 결과 묶음이다. 모두 같은 RunTable 을 쓴다 (HN-FR-57).
type bundle struct {
	def   *inject.Dispatcher
	modes map[string]*inject.Dispatcher
}

func (b *bundle) pick(mode string) *inject.Dispatcher {
	if d, ok := b.modes[mode]; ok {
		return d
	}
	return b.def
}

func (a *Agent) bindAll(layout []SetState) (*bundle, error) {
	def, err := a.bind(layout)
	if err != nil {
		return nil, err
	}
	b := &bundle{def: def, modes: map[string]*inject.Dispatcher{}}
	for _, m := range a.modes {
		d, err := a.bind(modeLayout(layout, m))
		if err != nil {
			return nil, fmt.Errorf("harness: mode %s: %w", m.Name, err)
		}
		b.modes[m.Name] = d
	}
	return b, nil
}

// Modes 는 등록한 모드의 복사본이다 (HN-FR-56).
func (a *Agent) Modes() []Mode {
	out := make([]Mode, len(a.modes))
	for i, m := range a.modes {
		out[i] = Mode{Name: m.Name, Description: m.Description,
			Push: append([]string(nil), m.Push...), Off: append([]string(nil), m.Off...)}
	}
	return out
}

func (a *Agent) hasMode(name string) bool {
	for _, m := range a.modes {
		if m.Name == name {
			return true
		}
	}
	return false
}

// modeTable 은 세션별 모드 상태의 메모리 사본이다 (HN-FR-59).
type modeTable struct {
	mu   sync.Mutex
	sess map[string]*ModeStatus
}

func clonePending(p *ModeRequest) *ModeRequest {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

func (s ModeStatus) clone() ModeStatus {
	s.Pending = clonePending(s.Pending)
	return s
}

func checkSession(sid string) error {
	if sid == "" {
		return errors.New("harness: mode: empty session id")
	}
	return session.CheckID(sid)
}

// modeLocked 는 세션의 상태를 돌려준다. 처음이면 Store 에서 읽고 HN-FR-60 으로 고친다. a.mt.mu 를 잡은 채 부른다.
func (a *Agent) modeLocked(ctx context.Context, sid string) (*ModeStatus, []ModeChange, error) {
	if st, ok := a.mt.sess[sid]; ok {
		return st, nil, nil
	}
	sess, err := a.store.Load(ctx, sid)
	if err != nil {
		return nil, nil, fmt.Errorf("harness: mode: %w", err)
	}
	st := &ModeStatus{}
	if ns, ok := sess.State[ModeNamespace]; ok {
		if v, ok := ns.Values[keyModeActive]; ok {
			_ = json.Unmarshal(v, &st.Active)
		}
		if v, ok := ns.Values[keyModeArg]; ok {
			_ = json.Unmarshal(v, &st.Arg)
		}
		if v, ok := ns.Values[keyModePending]; ok {
			var p ModeRequest
			if json.Unmarshal(v, &p) == nil {
				st.Pending = &p
			}
		}
	}
	var fixes []ModeChange
	fixed := *st
	if fixed.Active != "" && !a.hasMode(fixed.Active) {
		fixes = append(fixes, ModeChange{From: fixed.Active, Origin: ModeUser, Reason: "unknown mode " + fixed.Active})
		fixed.Active, fixed.Arg = "", ""
	}
	if p := fixed.Pending; p != nil && p.To != "" && !a.hasMode(p.To) {
		fixes = append(fixes, ModeChange{From: fixed.Active, Origin: ModeUser, Reason: "unknown mode " + p.To})
		fixed.Pending = nil
	}
	if len(fixes) > 0 {
		if err := a.saveMode(ctx, sid, fixed); err != nil {
			return nil, nil, err
		}
		st = &fixed
	}
	a.mt.sess[sid] = st
	return st, fixes, nil
}

func (a *Agent) saveMode(ctx context.Context, sid string, st ModeStatus) error {
	d := inject.StateDelta{Set: map[string]json.RawMessage{}}
	d.Set[keyModeActive], _ = json.Marshal(st.Active)
	d.Set[keyModeArg], _ = json.Marshal(st.Arg)
	if st.Pending != nil {
		d.Set[keyModePending], _ = json.Marshal(st.Pending)
	} else {
		d.Delete = []string{keyModePending}
	}
	if err := a.store.SaveState(ctx, sid, ModeNamespace, d); err != nil {
		return fmt.Errorf("harness: mode: save: %w", err)
	}
	return nil
}

func (a *Agent) emitModes(runID, sid string, cs []ModeChange) {
	for i := range cs {
		a.sf.emit(Event{Kind: EventMode, RunID: runID, SessionID: sid, Mode: &cs[i]})
	}
}

// ModeStatus 는 세션의 모드 상태 복사본이다 (HN-FR-61).
func (a *Agent) ModeStatus(ctx context.Context, sessionID string) (ModeStatus, error) {
	if err := checkSession(sessionID); err != nil {
		return ModeStatus{}, err
	}
	a.mt.mu.Lock()
	st, fixes, err := a.modeLocked(ctx, sessionID)
	var out ModeStatus
	if err == nil {
		out = st.clone()
	}
	a.mt.mu.Unlock()
	a.emitModes("", sessionID, fixes)
	return out, err
}

// RequestMode 는 권한 표(HN-FR-62)로 판정해 예약한다. 세션이 실행 중이어도 받는다.
func (a *Agent) RequestMode(ctx context.Context, sessionID string, r ModeRequest) error {
	if err := checkSession(sessionID); err != nil {
		return err
	}
	switch r.Origin {
	case ModeUser, ModeModel, ModeSet:
	default:
		return fmt.Errorf("harness: mode: unknown origin %q", r.Origin)
	}
	if r.To != "" && !a.hasMode(r.To) {
		return fmt.Errorf("%w: %q", ErrUnknownMode, r.To)
	}
	a.mt.mu.Lock()
	st, fixes, err := a.modeLocked(ctx, sessionID)
	if err == nil {
		err = a.decide(ctx, sessionID, st, r)
	}
	a.mt.mu.Unlock()
	a.emitModes("", sessionID, fixes)
	return err
}

// decide 는 HN-FR-62·63 이다. a.mt.mu 를 잡은 채 부른다.
func (a *Agent) decide(ctx context.Context, sid string, st *ModeStatus, r ModeRequest) error {
	next := st.clone()
	req := r
	switch {
	case r.Origin == ModeModel && r.To == "":
		return fmt.Errorf("%w: the model cannot leave a mode", ErrModeDenied)
	case r.Origin == ModeModel && (st.Active != "" || st.Pending != nil):
		return fmt.Errorf("%w: a mode is already active or pending", ErrModeDenied)
	case r.Origin == ModeSet && r.To != "":
		return fmt.Errorf("%w: a set cannot enter a mode", ErrModeDenied)
	case r.Origin == ModeSet && st.Active == "":
		return fmt.Errorf("%w: no active mode to leave", ErrModeDenied)
	case r.Origin == ModeSet && st.Pending != nil && st.Pending.Origin == ModeUser:
		return nil
	case r.Origin == ModeUser && r.To == "" && st.Active == "":
		next.Pending = nil
	case st.Pending == nil && r.To == st.Active && r.Arg == st.Arg:
		return nil
	default:
		next.Pending = &req
	}
	if err := a.saveMode(ctx, sid, next); err != nil {
		return err
	}
	*st = next
	return nil
}

// applyPending 은 예약을 적용하고 지금 활성 모드를 돌려준다 (HN-FR-64).
func (a *Agent) applyPending(ctx context.Context, runID, sid string) (string, error) {
	a.mt.mu.Lock()
	st, fixes, err := a.modeLocked(ctx, sid)
	if err != nil {
		a.mt.mu.Unlock()
		return "", err
	}
	active := st.Active
	if p := st.Pending; p != nil {
		next := ModeStatus{Active: p.To, Arg: p.Arg}
		if err := a.saveMode(ctx, sid, next); err != nil {
			a.mt.mu.Unlock()
			a.emitModes(runID, sid, fixes)
			return "", err
		}
		fixes = append(fixes, ModeChange{From: st.Active, To: p.To, Arg: p.Arg, Origin: p.Origin, Reason: p.Reason})
		*st = next
		active = next.Active
	}
	a.mt.mu.Unlock()
	a.emitModes(runID, sid, fixes)
	return active, nil
}

// runSwitch 는 run 하나의 Dispatcher 다. turn.start 직전에만 예약을 적용해 바인딩을 바꾼다 (HN-FR-66).
type runSwitch struct {
	a   *Agent
	b   *bundle
	sid string

	mu  sync.Mutex
	cur *inject.Dispatcher
}

func (s *runSwitch) Dispatch(ctx context.Context, step core.Step, input any, view core.View) (core.Result, error) {
	if step == core.StepTurnStart && len(s.a.modes) > 0 {
		active, err := s.a.applyPending(ctx, view.RunID(), s.sid)
		if err != nil {
			return core.Result{}, err
		}
		s.mu.Lock()
		s.cur = s.b.pick(active)
		s.mu.Unlock()
	}
	s.mu.Lock()
	d := s.cur
	s.mu.Unlock()
	return d.Dispatch(ctx, step, input, view)
}

type sessionKey struct{}

func sessionOf(ctx context.Context) string {
	sid, _ := ctx.Value(sessionKey{}).(string)
	return sid
}

// modeTool 은 모델의 진입 도구다 (HN-FR-68, 69).
func modeTool(modes []Mode, a **Agent) sets.Tool {
	names := make([]string, len(modes))
	var desc strings.Builder
	desc.WriteString("Switch this session into a mode from the next turn. You cannot leave a mode yourself; " +
		"the mode ends when its own check decides or the user exits it.")
	for i, m := range modes {
		names[i] = m.Name
		fmt.Fprintf(&desc, "\n- %s: %s", m.Name, m.Description)
	}
	enum, _ := json.Marshal(names)
	schema := json.RawMessage(`{"type":"object","properties":{` +
		`"name":{"type":"string","enum":` + string(enum) + `,"description":"Mode to enter"},` +
		`"arg":{"type":"string","description":"Optional argument for the mode, e.g. the goal or task"}},` +
		`"required":["name"]}`)
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "mode", Description: desc.String(), InputSchema: schema},
		Run: func(ctx context.Context, call contract.ToolCall) contract.ToolResult {
			errResult := func(msg string) contract.ToolResult {
				return contract.ToolResult{CallID: call.ID, Status: contract.StatusError,
					Content: []contract.Block{{Kind: contract.BlockText, Text: msg}}}
			}
			var args struct {
				Name string `json:"name"`
				Arg  string `json:"arg"`
			}
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return errResult(fmt.Sprintf("invalid arguments: %v", err))
			}
			sid := sessionOf(ctx)
			if sid == "" || *a == nil {
				return errResult("mode: no session")
			}
			if err := (*a).RequestMode(ctx, sid, ModeRequest{To: args.Name, Arg: args.Arg, Origin: ModeModel}); err != nil {
				return errResult(err.Error())
			}
			return contract.ToolResult{CallID: call.ID, Status: contract.StatusOK,
				Content: []contract.Block{{Kind: contract.BlockText, Text: fmt.Sprintf("Mode %q will apply from the next turn.", args.Name)}}}
		},
	}
}

// modeSwitcher 는 세트용 sets.ModeSwitcher 다 (HN-FR-71).
type modeSwitcher struct{ a *Agent }

func (m *modeSwitcher) Current(ctx context.Context, sid string) (sets.ModeState, error) {
	if m.a == nil {
		return sets.ModeState{}, nil
	}
	st, err := m.a.ModeStatus(ctx, sid)
	return sets.ModeState{Active: st.Active, Arg: st.Arg}, err
}

func (m *modeSwitcher) Exit(ctx context.Context, sid, reason string) error {
	if m.a == nil {
		return sets.ErrNoModes
	}
	return m.a.RequestMode(ctx, sid, ModeRequest{Origin: ModeSet, Reason: reason})
}
