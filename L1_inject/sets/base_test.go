package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// filledSteps 는 세트가 채운 step 필드 이름이다.
func filledSteps(s inject.FunctionSet) string {
	v := reflect.ValueOf(s)
	var out []string
	for i := 2; i < v.NumField(); i++ { // 0 = Name, 1 = Description
		if !v.Field(i).IsNil() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	return strings.Join(out, " ")
}

// ST-FR-03, 51: 세트별 채운 step, Description, base·require 가 22 step 을 채움
func TestSetDecomposition(t *testing.T) {
	d := deps(&fakeModel{})
	for _, c := range []struct {
		set        inject.FunctionSet
		name, want string
	}{
		{sets.Base(d), "base", "RunStart TurnStart ContextProject ResponseCommit ToolBefore ToolExecute ToolAfter ToolsCommit"},
		{sets.Require(d), "require", "RunStart ContextMaintain ContextInject RequestBuild RequestFinalize ModelSelect ModelCall StreamEvent StreamEnd CallError ResponseClassify ToolsPlan TurnFinish RunBeforeSettle RunSettled"},
		{sets.Cache(d), "cache", "RequestFinalize"},
		{sets.Guard(d), "guard", "ToolBefore"},
		{sets.Truncate(d), "truncate", "ToolAfter"},
		{sets.Resilient(d), "resilient", "ContextMaintain CallError ResponseClassify RunBeforeSettle"},
	} {
		if c.set.Name != c.name || c.set.Description == "" || filledSteps(c.set) != c.want {
			t.Errorf("%s: name %q description %q steps %s", c.name, c.set.Name, c.set.Description, filledSteps(c.set))
		}
	}
	v, _ := sets.Verify(d, sets.VerifyOptions{Check: func(context.Context) (sets.CheckResult, error) { return sets.CheckResult{OK: true}, nil }})
	if v.Name != "verify" || v.Description == "" || filledSteps(v) != "TurnFinish" {
		t.Errorf("verify %q %q %s", v.Name, v.Description, filledSteps(v))
	}
	if _, err := inject.Bind(sets.Base(d), sets.Require(d)); err != nil {
		t.Fatal(err)
	}
	for _, only := range []inject.FunctionSet{sets.Base(d), sets.Require(d)} {
		if _, err := inject.Bind(only); !errors.Is(err, inject.ErrBind) {
			t.Errorf("%s alone: %v", only.Name, err)
		}
	}
}

// 1 run.start: 복원, 편집 rewrite, 짝 복구 (ST-FR-40), 초기 입력 보관
func TestRunStart(t *testing.T) {
	d := deps(&fakeModel{})
	ctx := context.Background()
	call := toolCall("c1", "echo", `{"s":"x"}`)
	d.Store.AppendMessages(ctx, "s1", []contract.Message{
		user("u0", "old"),
		assistant("a0", contract.Usage{}, contract.StopToolUse, text("t"), toolCall("c0", "echo", `{"s":"y"}`)),
		resultMsg("r0", "c0", "done"),
		assistant("a1", contract.Usage{}, contract.StopToolUse, call, toolCall("c2", "echo", `{"s":"z"}`)),
		resultMsg("r2", "c2", "done"),
	})
	edit := contract.ContextEdit{ID: "e1", Kind: contract.EditHide, Timestamp: ts, Target: "u0"}
	d.Store.AppendEdits(ctx, "s1", []contract.ContextEdit{edit})
	res, err := stackStep(t, core.StepRunStart,
		core.RunInput{SessionID: "s1", Input: []contract.Message{user("u9", "new")}}, injecttest.Facts{}, stack(d, false)...)
	if err != nil {
		t.Fatal(err)
	}
	h := res.Output.(core.RunInit).History
	if len(h) != 6 || h[5].Role != contract.RoleToolResult || h[5].Result.CallID != "c1" || h[5].Result.Status != contract.StatusAborted {
		t.Fatalf("history %+v", h)
	}
	if kinds(res.Signals) != "[rewrite]" || res.Signals[0].Edit.ID != "e1" {
		t.Fatalf("signals %s", kinds(res.Signals))
	}
	sess, _ := d.Store.Load(ctx, "s1")
	if len(sess.Messages) != 6 {
		t.Fatalf("aborted result not stored: %d", len(sess.Messages))
	}
}

func TestRunStartStoreError(t *testing.T) {
	d := deps(&fakeModel{})
	d.Store = failingStore{}
	_, err := stackStep(t, core.StepRunStart, core.RunInput{SessionID: "s1"}, injecttest.Facts{}, stack(d, false)...)
	if err == nil {
		t.Fatal("store error ignored")
	}
}

type failingStore struct{}

func (failingStore) Load(context.Context, string) (sets.Session, error) {
	return sets.Session{}, errors.New("disk")
}
func (failingStore) AppendMessages(context.Context, string, []contract.Message) error {
	return errors.New("disk")
}
func (failingStore) AppendEdits(context.Context, string, []contract.ContextEdit) error {
	return errors.New("disk")
}
func (failingStore) SaveState(context.Context, string, string, inject.StateDelta) error {
	return errors.New("disk")
}

// 5 context.project: 편집 적용
func TestProjection(t *testing.T) {
	tr := []contract.Message{user("u1", "a"), user("u2", "b"), user("u3", "c"), user("u4", "d"), user("u5", "e")}
	sum := user("sum", "summary")
	edits := []contract.ContextEdit{
		{ID: "e1", Kind: contract.EditReplaceRange, Timestamp: ts, From: "u1", To: "u2", Messages: []contract.Message{sum}},
		{ID: "e2", Kind: contract.EditHide, Timestamp: ts, Target: "u3"},
		{ID: "e3", Kind: contract.EditTruncateAfter, Timestamp: ts, Target: "u4"},
		{ID: "e4", Kind: contract.EditHide, Timestamp: ts, Target: "missing"},
	}
	got := sets.ApplyEdits(tr, edits)
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "sum,u4" {
		t.Fatalf("projection %v", ids)
	}
	d := deps(&fakeModel{})
	res, _ := stackStep(t, core.StepContextProject, nil,
		injecttest.Facts{Transcript: tr, Edits: edits[:1], Ephemeral: []contract.Message{user("eph", "x")}}, stack(d, false)...)
	out := res.Output.([]contract.Message)
	if len(out) != 5 || out[0].ID != "sum" || out[4].ID != "eph" {
		t.Fatalf("project %+v", out)
	}
	sess, _ := d.Store.Load(context.Background(), "s1")
	if len(sess.Edits) != 1 {
		t.Fatal("new edit not stored")
	}
}

// 6 request.build, 7 request.finalize
func TestRequestBuildAndFinalize(t *testing.T) {
	d := deps(&fakeModel{}, echoTool(nil), sets.Tool{Spec: contract.ToolSpec{Name: "other", InputSchema: json.RawMessage(`{}`)}})
	proj := []contract.Message{user("u1", "a")}
	res, _ := stackStep(t, core.StepRequestBuild, nil,
		injecttest.Facts{Projection: proj, Config: json.RawMessage(`{"model":{"provider":"p","id":"m"},"tools":["echo"],"thinking":"high"}`),
			Model: &contract.ModelInfo{MaxOutputTokens: 777}}, stack(d, false)...)
	req := res.Output.(contract.Request)
	if req.System[0].Text != "sys" || len(req.Tools) != 1 || req.Tools[0].Name != "echo" || req.Options.MaxOutputTokens != 777 ||
		req.Options.Thinking != "high" || req.Messages[0].ID != "u1" {
		t.Fatalf("request %+v", req)
	}
	// finalize: 짝 복구 + 캐시 힌트
	req.Messages = []contract.Message{
		user("u1", "a"),
		assistant("a1", contract.Usage{}, contract.StopToolUse, toolCall("c1", "echo", `{}`), toolCall("c2", "echo", `{}`)),
		resultMsg("r2", "c2", "ok"),
		user("u2", "b"),
	}
	res, _ = stackStep(t, core.StepRequestFinalize, req, injecttest.Facts{}, stack(d, false)...)
	fin := res.Output.(contract.Request)
	if len(fin.Messages) != 5 || fin.Messages[2].Result == nil || fin.Messages[2].Result.CallID != "c1" || fin.Messages[2].Result.Status != contract.StatusAborted {
		t.Fatalf("pair repair %+v", fin.Messages)
	}
	h := fin.Options.CacheHints
	if len(h) != 2 || h[0] != (contract.CacheHint{Target: contract.CacheSystem, Index: 0}) || h[1] != (contract.CacheHint{Target: contract.CacheMessage, Index: 4}) {
		t.Fatalf("hints %+v", h)
	}
	if err := fin.Validate(); err != nil {
		t.Fatalf("invalid request: %v", err)
	}
}

// 8 model.select
func TestModelSelect(t *testing.T) {
	info := contract.ModelInfo{Ref: contract.ModelRef{Provider: "p", ID: "m", API: "x"}, ContextWindow: 100}
	d := deps(&fakeModel{info: map[string]contract.ModelInfo{"p/m": info}})
	res, _ := stackStep(t, core.StepModelSelect, contract.Request{}, injecttest.Facts{}, stack(d, false)...)
	if res.Output.(contract.ModelInfo) != info {
		t.Fatalf("select %+v", res.Output)
	}
	res, _ = stackStep(t, core.StepModelSelect, contract.Request{}, injecttest.Facts{Config: json.RawMessage(`{"model":{"provider":"p","id":"unknown"}}`)}, stack(d, false)...)
	if got := res.Output.(contract.ModelInfo); got.ContextWindow != 0 || got.Ref.ID != "unknown" || got.Ref.Provider != "p" {
		t.Fatalf("unknown model %+v", got)
	}
}

// 16, 17, 18
func TestToolSteps(t *testing.T) {
	var runs []string
	big := sets.Tool{Spec: contract.ToolSpec{Name: "big", InputSchema: json.RawMessage(`{}`)},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult {
			return contract.ToolResult{CallID: c.ID, Status: contract.StatusOK, Content: []contract.Block{text(strings.Repeat("line\n", 3000))}}
		}}
	boom := sets.Tool{Spec: contract.ToolSpec{Name: "boom", InputSchema: json.RawMessage(`{}`)},
		Run: func(ctx context.Context, c contract.ToolCall) contract.ToolResult { panic("tool bug") }}
	own := sets.Tool{Spec: contract.ToolSpec{Name: "own", InputSchema: json.RawMessage(`{}`)},
		Truncate: func(r contract.ToolResult) contract.ToolResult {
			r.Content = []contract.Block{text("custom")}
			return r
		}}
	d := deps(&fakeModel{}, echoTool(&runs), big, boom, own)
	do := func(st core.Step, input any, f injecttest.Facts) (core.Result, error) {
		return stackStep(t, st, input, f, stack(d, false)...)
	}
	before := func(name, args string) core.BeforeOut {
		res, err := do(core.StepToolBefore, *toolCall("c", name, args).ToolCall, injecttest.Facts{})
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(core.BeforeOut)
	}
	if o := before("echo", `{"s":"x"}`); o.ShortCircuit != nil {
		t.Fatalf("valid blocked %+v", o.ShortCircuit)
	}
	if o := before("nope", `{}`); o.ShortCircuit == nil || o.ShortCircuit.Status != contract.StatusError || !strings.Contains(o.ShortCircuit.Content[0].Text, "unknown tool: nope") {
		t.Fatalf("unknown %+v", o)
	}
	if o := before("echo", `{}`); o.ShortCircuit == nil || o.ShortCircuit.Status != contract.StatusError {
		t.Fatalf("schema %+v", o)
	}
	res, _ := do(core.StepToolExecute, *toolCall("c", "echo", `{"s":"hi"}`).ToolCall, injecttest.Facts{})
	if r := res.Output.(contract.ToolResult); r.Content[0].Text != "echo:hi" || runs[0] != "hi" {
		t.Fatalf("execute %+v", r)
	}
	res, err := do(core.StepToolExecute, *toolCall("c", "boom", `{}`).ToolCall, injecttest.Facts{})
	if err != nil || res.Output.(contract.ToolResult).Status != contract.StatusError {
		t.Fatalf("panic not converted: %v %+v", err, res.Output)
	}
	bigRes := contract.ToolResult{CallID: "c", Status: contract.StatusOK, Content: []contract.Block{text(strings.Repeat("line\n", 3000))}}
	res, _ = do(core.StepToolAfter, core.ToolAfterInput{Call: *toolCall("c", "big", `{}`).ToolCall, Result: bigRes}, injecttest.Facts{})
	txt := res.Output.(contract.ToolResult).Content[0].Text
	if n := strings.Count(txt, "line\n"); n != 2000 || !strings.Contains(txt, "truncated") {
		t.Fatalf("truncate lines=%d tail=%q", n, txt[len(txt)-80:])
	}
	wide := contract.ToolResult{CallID: "c", Status: contract.StatusOK, Content: []contract.Block{text(strings.Repeat("x", 60*1024))}}
	res, _ = do(core.StepToolAfter, core.ToolAfterInput{Call: *toolCall("c", "big", `{}`).ToolCall, Result: wide}, injecttest.Facts{})
	if l := len(res.Output.(contract.ToolResult).Content[0].Text); l > 50*1024+200 {
		t.Fatalf("byte limit %d", l)
	}
	res, _ = do(core.StepToolAfter, core.ToolAfterInput{Call: *toolCall("c", "own", `{}`).ToolCall, Result: bigRes}, injecttest.Facts{})
	if res.Output.(contract.ToolResult).Content[0].Text != "custom" {
		t.Fatal("tool Truncate not used")
	}
}

// 19, 20
func TestCommitAndFinish(t *testing.T) {
	d := deps(&fakeModel{})
	do := func(st core.Step, input any, f injecttest.Facts) (core.Result, error) {
		return stackStep(t, st, input, f, stack(d, false)...)
	}
	results := []contract.ToolResult{{CallID: "c1", Status: contract.StatusOK}, {CallID: "c2", Status: contract.StatusError}}
	res, _ := do(core.StepToolsCommit, core.ToolsCommitInput{Calls: []contract.ToolCall{{ID: "c1"}, {ID: "c2"}}, Results: results}, injecttest.Facts{})
	ms := res.Output.([]contract.Message)
	if len(ms) != 2 || ms[1].Result.CallID != "c2" || ms[0].ID == ms[1].ID || ms[0].Validate() != nil {
		t.Fatalf("commit %+v", ms)
	}
	if sess, _ := d.Store.Load(context.Background(), "s1"); len(sess.Messages) != 2 {
		t.Fatal("results not stored")
	}
	res, _ = do(core.StepTurnFinish, nil, injecttest.Facts{ToolCalls: []contract.ToolCall{{ID: "c1"}}})
	if kinds(res.Signals) != "[continue_turn]" {
		t.Fatalf("with tools %s", kinds(res.Signals))
	}
	res, _ = do(core.StepTurnFinish, nil, injecttest.Facts{})
	if kinds(res.Signals) != "[settle]" {
		t.Fatalf("without tools %s", kinds(res.Signals))
	}
}

// ST-FR-51: 정책 세트를 빼면 그 동작만 빠진다.
func TestPolicySetsOff(t *testing.T) {
	big := sets.Tool{Spec: contract.ToolSpec{Name: "big", InputSchema: json.RawMessage(`{}`)}}
	d := deps(&fakeModel{}, echoTool(nil), big)
	req := contract.Request{System: []contract.Block{text("sys")}, Messages: []contract.Message{
		user("u1", "a"),
		assistant("a1", contract.Usage{}, contract.StopToolUse, toolCall("c1", "echo", `{}`)),
	}}
	res, err := stackStep(t, core.StepRequestFinalize, req, injecttest.Facts{}, sets.Base(d), sets.Require(d), sets.Guard(d), sets.Truncate(d))
	if fin := res.Output.(contract.Request); err != nil || len(fin.Messages) != 3 || fin.Messages[2].Result.Status != contract.StatusAborted || fin.Options.CacheHints != nil {
		t.Fatalf("no cache: %v %+v", err, fin)
	}
	noGuard := []inject.FunctionSet{sets.Base(d), sets.Require(d), sets.Cache(d), sets.Truncate(d)}
	res, err = stackStep(t, core.StepToolBefore, *toolCall("c", "nope", `{}`).ToolCall, injecttest.Facts{}, noGuard...)
	if o := res.Output.(core.BeforeOut); err != nil || o.ShortCircuit != nil || o.Call.Name != "nope" {
		t.Fatalf("no guard before: %v %+v", err, o)
	}
	res, err = stackStep(t, core.StepToolExecute, *toolCall("c", "nope", `{}`).ToolCall, injecttest.Facts{}, noGuard...)
	if r := res.Output.(contract.ToolResult); err != nil || r.Status != contract.StatusError || r.Content[0].Text != "unknown tool: nope" {
		t.Fatalf("no guard execute: %v %+v", err, r)
	}
	long := strings.Repeat("line\n", 3000)
	in := core.ToolAfterInput{Call: *toolCall("c", "big", `{}`).ToolCall, Result: contract.ToolResult{CallID: "c", Status: contract.StatusOK, Content: []contract.Block{text(long)}}}
	res, err = stackStep(t, core.StepToolAfter, in, injecttest.Facts{}, sets.Base(d), sets.Require(d), sets.Cache(d), sets.Guard(d))
	if err != nil || res.Output.(contract.ToolResult).Content[0].Text != long {
		t.Fatalf("no truncate: %v", err)
	}
}

// ST-FR-51, inject IN-FR-62: 아래 층이 필요한 세트를 아래 층 없이 쌓으면 그 step 이 ErrNoNext 로 실패한다.
func TestPolicySetsNeedLower(t *testing.T) {
	d := deps(&fakeModel{}, echoTool(nil))
	for _, c := range []struct {
		set   inject.FunctionSet
		st    core.Step
		input any
	}{
		{sets.Require(d), core.StepRunStart, core.RunInput{SessionID: "s1"}},
		{sets.Cache(d), core.StepRequestFinalize, contract.Request{}},
		{sets.Guard(d), core.StepToolBefore, *toolCall("c", "echo", `{"s":"x"}`).ToolCall},
		{sets.Truncate(d), core.StepToolAfter, core.ToolAfterInput{Call: *toolCall("c", "echo", `{}`).ToolCall}},
	} {
		if _, err := stackStep(t, c.st, c.input, injecttest.Facts{}, c.set); !errors.Is(err, inject.ErrNoNext) {
			t.Errorf("%s at %s: %v", c.set.Name, c.st, err)
		}
	}
}

type nsRecorder struct {
	inject.StateStore
	mu sync.Mutex
	ns map[string]bool
}

func (r *nsRecorder) Save(ctx context.Context, sid, ns string, d inject.StateDelta) error {
	r.mu.Lock()
	r.ns[ns] = true
	r.mu.Unlock()
	return r.StateStore.Save(ctx, sid, ns, d)
}

// ST-FR-53: require·cache·guard·truncate 는 상태를 쓰지 않는다. base 의 키는 base 네임스페이스에.
func TestPolicySetsNoState(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		if n == 0 {
			return doneEv(assistant("a0", contract.Usage{Output: 1}, contract.StopToolUse, toolCall("c1", "echo", `{"s":"x"}`)))
		}
		return doneEv(assistant("a1", contract.Usage{Output: 1}, contract.StopStop, text("ok")))
	}}
	d := deps(m, echoTool(nil))
	rec := &nsRecorder{StateStore: sets.StateStoreOf(d.Store), ns: map[string]bool{}}
	disp, err := inject.BindWith(inject.BindOptions{StateStore: rec}, stack(d, false)...)
	if err != nil {
		t.Fatal(err)
	}
	s := core.New(disp, core.Options{}).Start(context.Background(), core.RunInput{SessionID: "s1", Input: []contract.Message{user("u1", "go")}, Config: json.RawMessage(cfgJSON)}).Wait()
	if s.Reason != core.SettleCompleted || len(rec.ns) != 1 || !rec.ns["base"] {
		t.Fatalf("settle %s namespaces %v", s.Reason, rec.ns)
	}
}
