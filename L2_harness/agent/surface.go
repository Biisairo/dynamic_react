package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/tools"
)

var (
	ErrSessionBusy = errors.New("agent: session is busy")
	ErrRunDone     = errors.New("agent: run is done")
)

// titleRunes 는 SessionInfo.Title 의 최대 길이다 (AG-FR-29).
const titleRunes = 80

const skippedText = "skipped: the user sent a new message"

type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (Decision, error)
}

type ApprovalRequest struct {
	SessionID, RunID string
	Call             contract.ToolCall
	Rule             string // 맞은 ask 규칙, 없으면 ""
	Target           string // AG-FR-14 의 대상
}

// Decision.Kind 는 "allow" | "allow_session" | "deny" 다 (AG-FR-18).
type Decision struct {
	Kind   string
	Reason string
}

type SessionInfo struct {
	ID       string
	Title    string
	Updated  time.Time
	Messages int
}

// SessionKey 는 임의의 키를 결정적인 세션 ID 로 바꾼다 (AG-FR-28).
func SessionKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "k-" + hex.EncodeToString(sum[:8])
}

// Run 은 FollowUp 연쇄를 포함한 실행 하나다 (AG-FR-22~26).
type Run struct {
	a        *Agent
	sid      string
	config   json.RawMessage // 시작할 때의 설정 (AG-FR-34: 이미 시작한 run 은 그대로)
	ctx      context.Context
	cancel   context.CancelFunc
	steers   atomic.Int64
	finished chan struct{}
	settle   core.Settle

	mu    sync.Mutex
	cur   *core.Run
	queue []contract.Message
	done  bool

	pmu      sync.Mutex
	panicErr error
}

type runKey struct{}

func runOf(ctx context.Context) *Run {
	r, _ := ctx.Value(runKey{}).(*Run)
	return r
}

func (r *Run) SessionID() string { return r.sid }

func (r *Run) Steer(msg contract.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return ErrRunDone
	}
	r.steers.Add(1)
	r.cur.Input(msg)
	return nil
}

func (r *Run) FollowUp(msg contract.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return ErrRunDone
	}
	r.queue = append(r.queue, msg)
	return nil
}

func (r *Run) Abort() {
	r.cancel()
	r.mu.Lock()
	r.queue = nil
	r.mu.Unlock()
}

func (r *Run) Wait() core.Settle {
	<-r.finished
	return r.settle
}

// observer 는 Observer panic 을 이 run 에 귀속시킨다 (AG-FR-26).
type runObserver struct {
	r     *Run
	inner core.Observer
}

func (o runObserver) Observe(ev core.ObserverEvent) {
	defer func() {
		if p := recover(); p != nil {
			o.r.pmu.Lock()
			if o.r.panicErr == nil {
				o.r.panicErr = fmt.Errorf("%w: observer: %v", core.ErrPanic, p)
			}
			o.r.pmu.Unlock()
			o.r.cancel()
		}
	}()
	o.inner.Observe(ev)
}

func (a *Agent) request(r *Run, sid string, input []contract.Message) harness.RunRequest {
	req := harness.RunRequest{SessionID: sid, Input: input, Config: r.config}
	if a.observer != nil {
		req.Observer = runObserver{r: r, inner: a.observer}
	}
	return req
}

// Start 는 AG-FR-10, 22 다.
func (a *Agent) Start(ctx context.Context, sessionID string, input []contract.Message) (*Run, error) {
	cfg, err := a.runConfig()
	if err != nil {
		return nil, err
	}
	if sessionID != "" && !a.lock(sessionID) {
		return nil, ErrSessionBusy
	}
	rctx, cancel := context.WithCancel(ctx)
	r := &Run{a: a, config: cfg, cancel: cancel, finished: make(chan struct{})}
	r.ctx = context.WithValue(rctx, runKey{}, r)
	cr, id, err := a.h.Start(r.ctx, a.request(r, sessionID, input))
	if err != nil {
		cancel()
		if sessionID != "" {
			a.unlock(sessionID)
		}
		return nil, err
	}
	if sessionID == "" {
		a.lock(id)
	}
	r.sid, r.cur = id, cr
	go r.loop()
	return r, nil
}

// loop 는 AG-FR-25 의 FollowUp 연쇄다.
func (r *Run) loop() {
	for {
		r.mu.Lock()
		cur := r.cur
		r.mu.Unlock()
		s := cur.Wait()
		r.pmu.Lock()
		if r.panicErr != nil {
			s = core.Settle{Reason: core.SettleFailed, Err: r.panicErr}
		}
		r.pmu.Unlock()
		r.mu.Lock()
		ok := s.Reason == core.SettleCompleted || s.Reason == core.SettleStopped
		if ok && len(r.queue) > 0 && r.ctx.Err() == nil {
			msgs := r.queue
			r.queue = nil
			r.steers.Store(0)
			cr, _, err := r.a.h.Start(r.ctx, r.a.request(r, r.sid, msgs))
			if err == nil {
				r.cur = cr
				r.mu.Unlock()
				continue
			}
			s = core.Settle{Reason: core.SettleFailed, Err: err}
		}
		r.done, r.queue = true, nil
		r.mu.Unlock()
		r.a.unlock(r.sid)
		r.cancel()
		r.settle = s
		close(r.finished)
		return
	}
}

func (a *Agent) lock(sid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy[sid] {
		return false
	}
	a.busy[sid] = true
	return true
}

func (a *Agent) unlock(sid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.busy, sid)
}

func (a *Agent) sessionAllowed(sid string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for k := range a.allowed[sid] {
		out[k] = true
	}
	return out
}

func (a *Agent) allowSession(sid, key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowed[sid] == nil {
		a.allowed[sid] = map[string]bool{}
	}
	a.allowed[sid][key] = true
}

// target 은 AG-FR-14 의 대상이다.
func (a *Agent) target(call contract.ToolCall) (string, bool) {
	var args struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	json.Unmarshal(call.Args, &args)
	switch call.Name {
	case "bash":
		return args.Command, true
	case "read", "write", "edit":
		p, err := tools.Resolve(a.env, args.Path)
		if err != nil {
			return "", true
		}
		return p, true
	}
	return "", false
}

func errorResult(text string) *contract.ToolResult {
	return &contract.ToolResult{Status: contract.StatusError, Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

func (a *Agent) emitApproval(in harness.GateInput, rule, target, decision, reason string) {
	c := in.Call
	a.h.Emit(harness.Event{Kind: harness.EventApproval, RunID: in.RunID, SessionID: in.SessionID, Call: &c,
		Approval: &harness.Approval{Rule: rule, Target: target, Decision: decision, Reason: reason}})
}

// gate 는 Steer 건너뛰기(AG-FR-24)와 승인(AG-FR-17~20)이다.
func (a *Agent) gate(ctx context.Context, in harness.GateInput) *contract.ToolResult {
	if r := runOf(ctx); r != nil && r.steers.Load() > int64(in.InboxConsumed) {
		return errorResult(skippedText)
	}
	target, hasTarget := a.target(in.Call)
	v, rule := a.rules.decide(in.Call.Name, target, hasTarget, a.sessionAllowed(in.SessionID))
	switch {
	case v == allow:
		return nil
	case v == deny:
		a.emitApproval(in, rule, target, "rule_deny", "")
		return errorResult("denied by rule " + rule)
	case a.approver == nil:
		return nil
	}
	d, err := a.approver.Approve(ctx, ApprovalRequest{SessionID: in.SessionID, RunID: in.RunID, Call: in.Call, Rule: rule, Target: target})
	if err != nil {
		if ctx.Err() != nil {
			return &contract.ToolResult{Status: contract.StatusAborted, Content: []contract.Block{{Kind: contract.BlockText, Text: "aborted"}}}
		}
		d = Decision{Kind: "deny", Reason: err.Error()}
	}
	switch d.Kind {
	case "allow":
		a.emitApproval(in, rule, target, "allow", d.Reason)
		return nil
	case "allow_session":
		a.allowSession(in.SessionID, sessionRule(in.Call.Name, target, hasTarget))
		a.emitApproval(in, rule, target, "allow_session", d.Reason)
		return nil
	}
	a.emitApproval(in, rule, target, "deny", d.Reason)
	return errorResult("denied: " + d.Reason)
}

// Sessions 는 AG-FR-29 다.
func (a *Agent) Sessions(ctx context.Context) ([]SessionInfo, error) {
	entries, err := a.env.ListDir(ctx, a.sessionDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	store, err := session.New(a.env, a.sessionDir)
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	for _, ent := range entries {
		id, ok := strings.CutSuffix(ent.Name, ".jsonl")
		if ent.IsDir || !ok || session.CheckID(id) != nil {
			continue
		}
		s, err := store.Load(ctx, id)
		if err != nil {
			continue
		}
		info := SessionInfo{ID: id, Messages: len(s.Messages)}
		for _, m := range s.Messages {
			if m.Timestamp.After(info.Updated) {
				info.Updated = m.Timestamp
			}
			if info.Title == "" && m.Role == contract.RoleUser {
				info.Title = title(m)
			}
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

func title(m contract.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Kind == contract.BlockText {
			parts = append(parts, b.Text)
		}
	}
	r := []rune(strings.Join(parts, " "))
	if len(r) > titleRunes {
		r = r[:titleRunes]
	}
	return string(r)
}
