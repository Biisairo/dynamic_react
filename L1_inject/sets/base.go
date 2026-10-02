package sets

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// base 네임스페이스의 상태 키 (ST-FR-43).
const (
	keyInput         = "input"
	keyInboxConsumed = "inbox.consumed"
	keyEditsSaved    = "edits.saved"
)

const (
	maxResultLines = 2000
	maxResultBytes = 50 * 1024
)

// Base 는 L2·L3 가 기대는 계약(세션 로드·저장, 입력, 편집 적용, 도구 실행과 감쌀 수 있는 16·18 통과)을 채운 세트다 (3.0).
func Base(d Deps) inject.FunctionSet {
	d = d.filled()
	return inject.FunctionSet{
		Name:           "base",
		Description:    "session load/save, input, edits, tool execution",
		RunStart:       baseRunStart(d),
		TurnStart:      baseTurnStart(d),
		ContextProject: baseContextProject(d),
		ResponseCommit: baseResponseCommit(d),
		ToolBefore: func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			return core.BeforeOut{Call: call}, nil, nil
		},
		ToolExecute: baseToolExecute(d),
		ToolAfter: func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			return res, nil, nil
		},
		ToolsCommit: baseToolsCommit(d),
	}
}

// Require 는 동작에 필수인 step(요청 조립·모델 호출·응답 확정·실패·순차 계획·R2 판정·짝 복구)과 빈 step 을 채운 세트다 (3.0).
// run.start 는 아래 층(base)의 결과에 짝 복구를 덧붙인다.
func Require(d Deps) inject.FunctionSet {
	d = d.filled()
	return inject.FunctionSet{
		Name:        "require",
		Description: "request, model call, failure, sequential plan, turn loop (R2), pair repair",
		RunStart:    requireRunStart(d),
		ContextMaintain: func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
			return nil, nil
		},
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			return nil, nil, nil
		},
		RequestBuild:    requireRequestBuild(d),
		RequestFinalize: requireRequestFinalize(d),
		ModelSelect:     requireModelSelect(d),
		ModelCall:       requireModelCall(d),
		StreamEvent: func(ctx *inject.StreamEventCtx, ev contract.StreamEvent) ([]inject.StreamEventSignal, error) {
			return nil, nil
		},
		StreamEnd: requireStreamEnd,
		CallError: func(ctx *inject.CallErrorCtx, ce contract.CallError) ([]inject.CallErrorSignal, error) {
			return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
		},
		ResponseClassify: func(ctx *inject.ResponseClassifyCtx) ([]inject.ResponseClassifySignal, error) { return nil, nil },
		ToolsPlan:        requireToolsPlan,
		TurnFinish:       requireTurnFinish,
		RunBeforeSettle:  func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) { return nil, nil },
		RunSettled:       func(ctx *inject.RunSettledCtx, s core.Settle) error { return nil },
	}
}

// Cache 는 아래 층의 요청에 캐시 힌트(system 마지막 블록, 마지막 메시지)를 붙인다 (3.0).
func Cache(d Deps) inject.FunctionSet {
	return inject.FunctionSet{
		Name:        "cache",
		Description: "cache hints on the last system block and last message",
		RequestFinalize: func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
			req, sigs, err := ctx.Next(req)
			if err != nil {
				return req, sigs, err
			}
			var hints []contract.CacheHint
			if len(req.System) > 0 {
				hints = append(hints, contract.CacheHint{Target: contract.CacheSystem, Index: len(req.System) - 1})
			}
			if len(req.Messages) > 0 {
				hints = append(hints, contract.CacheHint{Target: contract.CacheMessage, Index: len(req.Messages) - 1})
			}
			req.Options.CacheHints = hints
			return req, sigs, nil
		},
	}
}

// Guard 는 등록되지 않은 도구와 스키마에 맞지 않는 인자를 error 결과로 막고, 통과하면 아래 층을 부른다 (ST-FR-38).
func Guard(d Deps) inject.FunctionSet {
	d = d.filled()
	return inject.FunctionSet{
		Name:        "guard",
		Description: "reject unknown tools and arguments that do not match the schema",
		ToolBefore: func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			t, ok := d.Tools.Get(call.Name)
			if !ok {
				return core.BeforeOut{Call: call, ShortCircuit: errorResult(call.ID, "unknown tool: "+call.Name)}, nil, nil
			}
			if err := CheckArgs(t.Spec.InputSchema, call.Args); err != nil {
				return core.BeforeOut{Call: call, ShortCircuit: errorResult(call.ID, err.Error())}, nil, nil
			}
			return ctx.Next(call)
		},
	}
}

// Truncate 는 아래 층의 결과에 Tool.Truncate 를, 없으면 텍스트를 앞 2000줄 / 50KB 로 자른다 (3.0).
func Truncate(d Deps) inject.FunctionSet {
	d = d.filled()
	return inject.FunctionSet{
		Name:        "truncate",
		Description: "truncate tool results (tool's own rule, else 2000 lines / 50KB)",
		ToolAfter: func(ctx *inject.ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []inject.ToolAfterSignal, error) {
			res, sigs, err := ctx.Next(call, res)
			if err != nil {
				return res, sigs, err
			}
			if t, ok := d.Tools.Get(call.Name); ok && t.Truncate != nil {
				return t.Truncate(res), sigs, nil
			}
			content := make([]contract.Block, len(res.Content))
			for i, b := range res.Content {
				if b.Kind == contract.BlockText {
					b.Text = truncateText(b.Text)
				}
				content[i] = b
			}
			res.Content = content
			return res, sigs, nil
		},
	}
}

// baseRunStart 는 Store 에서 History 와 편집을 복원하고 초기 입력을 보관한다.
func baseRunStart(d Deps) inject.RunStartFn {
	return func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
		if _, err := ParseConfig(ctx.Env().Config()); err != nil {
			return core.RunInit{}, nil, err
		}
		sess, err := d.Store.Load(ctx.Context(), ctx.Flow().SessionID())
		if err != nil {
			return core.RunInit{}, nil, err
		}
		var sigs []inject.RunStartSignal
		for _, e := range sess.Edits {
			sigs = append(sigs, ctx.Rewrite(e))
		}
		st := ctx.State()
		if err := st.Set(keyEditsSaved, len(sess.Edits)); err != nil {
			return core.RunInit{}, nil, err
		}
		if err := st.Set(keyInboxConsumed, 0); err != nil {
			return core.RunInit{}, nil, err
		}
		st.Publish(keyInboxConsumed)
		st.Delete(keyInput)
		if len(in.Input) > 0 {
			if err := st.Set(keyInput, in.Input); err != nil {
				return core.RunInit{}, nil, err
			}
		}
		return core.RunInit{History: sess.Messages}, sigs, nil
	}
}

// requireRunStart 는 아래 층이 복원한 History 의 마지막 assistant 메시지의 짝 없는 호출을 복구한다 (ST-FR-40).
func requireRunStart(d Deps) inject.RunStartFn {
	return func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
		init, sigs, err := ctx.Next(in)
		if err != nil {
			return init, sigs, err
		}
		if aborted := abortLastCalls(d, init.History); len(aborted) > 0 {
			if err := d.Store.AppendMessages(ctx.Context(), ctx.Flow().SessionID(), aborted); err != nil {
				return core.RunInit{}, nil, err
			}
			init.History = append(init.History, aborted...)
		}
		return init, sigs, nil
	}
}

// abortLastCalls 는 마지막 assistant 메시지 뒤가 그 결과뿐일 때, 결과 없는 호출의 aborted 결과를 만든다.
func abortLastCalls(d Deps, history []contract.Message) []contract.Message {
	last := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == contract.RoleAssistant {
			last = i
			break
		}
	}
	if last < 0 {
		return nil
	}
	answered := map[string]bool{}
	for _, m := range history[last+1:] {
		if m.Role != contract.RoleToolResult {
			return nil
		}
		answered[m.Result.CallID] = true
	}
	var out []contract.Message
	for _, c := range calls(history[last]) {
		if !answered[c.ID] {
			out = append(out, abortedResult(d, c.ID))
		}
	}
	return out
}

func calls(m contract.Message) []contract.ToolCall {
	var out []contract.ToolCall
	for _, b := range m.Content {
		if b.Kind == contract.BlockToolCall && b.ToolCall != nil {
			out = append(out, *b.ToolCall)
		}
	}
	return out
}

func abortedResult(d Deps, callID string) contract.Message {
	return contract.Message{ID: d.NewID(), Role: contract.RoleToolResult, Timestamp: d.Now(),
		Result: &contract.ToolResult{CallID: callID, Status: contract.StatusAborted, Content: []contract.Block{textBlock("aborted")}}}
}

func textBlock(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

// baseTurnStart 는 첫 턴에 초기 입력을, 이후 턴에 입력함의 소비하지 않은 메시지를 준다.
func baseTurnStart(d Deps) inject.TurnStartFn {
	return func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		st := ctx.State()
		msgs, ok := getJSON[[]contract.Message](st, keyInput)
		if ok {
			st.Delete(keyInput)
		} else {
			consumed, _ := getJSON[int](st, keyInboxConsumed)
			if inbox := ctx.Conv().Inbox(); consumed < len(inbox) {
				msgs = inbox[consumed:]
				if err := st.Set(keyInboxConsumed, len(inbox)); err != nil {
					return nil, nil, err
				}
				st.Publish(keyInboxConsumed)
			}
		}
		if len(msgs) == 0 {
			if ctx.Flow().TurnIndex() == 1 {
				return nil, []inject.TurnStartSignal{ctx.Settle()}, nil
			}
			return nil, nil, nil
		}
		if err := d.Store.AppendMessages(ctx.Context(), ctx.Flow().SessionID(), msgs); err != nil {
			return nil, nil, err
		}
		return msgs, nil, nil
	}
}

func getJSON[T any](st inject.State, key string) (T, bool) {
	var v T
	raw, ok := st.Get(key)
	if !ok || json.Unmarshal(raw, &v) != nil {
		return v, false
	}
	return v, true
}

// baseContextProject 는 Transcript 에 편집을 적용하고 Ephemeral 을 붙인다. 아직 기록하지 않은 편집을 Store 에 기록한다.
func baseContextProject(d Deps) inject.ContextProjectFn {
	return func(ctx *inject.ContextProjectCtx) ([]contract.Message, []inject.ContextProjectSignal, error) {
		edits := ctx.Conv().Edits()
		proj := append(ApplyEdits(ctx.Conv().Transcript(), edits), ctx.Conv().Ephemeral()...)
		st := ctx.State()
		saved, _ := getJSON[int](st, keyEditsSaved)
		if saved < len(edits) {
			if err := d.Store.AppendEdits(ctx.Context(), ctx.Flow().SessionID(), edits[saved:]); err != nil {
				return nil, nil, err
			}
			if err := st.Set(keyEditsSaved, len(edits)); err != nil {
				return nil, nil, err
			}
		}
		return proj, nil, nil
	}
}

// requireRequestBuild 는 System·Tools·Messages·옵션으로 요청을 만든다.
func requireRequestBuild(d Deps) inject.RequestBuildFn {
	return func(ctx *inject.RequestBuildCtx) (contract.Request, []inject.RequestBuildSignal, error) {
		cfg, err := ParseConfig(ctx.Env().Config())
		if err != nil {
			return contract.Request{}, nil, err
		}
		maxOut := cfg.MaxOutputTokens
		if maxOut == 0 {
			maxOut = ctx.Request().Model().MaxOutputTokens
		}
		return contract.Request{
			System:   append([]contract.Block(nil), d.System...),
			Messages: ctx.Conv().Projection(),
			Tools:    toolSpecs(d.Tools, cfg.Tools),
			Options:  contract.Options{MaxOutputTokens: maxOut, Thinking: cfg.Thinking},
		}, nil, nil
	}
}

// toolSpecs 는 names 의 spec 이다. names 가 비면 레지스트리 전부, 없는 이름은 무시한다.
func toolSpecs(r ToolRegistry, names []string) []contract.ToolSpec {
	if len(names) == 0 {
		names = r.Names()
	}
	var out []contract.ToolSpec
	for _, n := range names {
		if t, ok := r.Get(n); ok {
			out = append(out, t.Spec)
		}
	}
	return out
}

// requireRequestFinalize 는 결과 없는 호출 바로 뒤에 aborted 결과를 끼운다.
func requireRequestFinalize(d Deps) inject.RequestFinalizeFn {
	return func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
		answered := map[string]bool{}
		for _, m := range req.Messages {
			if m.Role == contract.RoleToolResult && m.Result != nil {
				answered[m.Result.CallID] = true
			}
		}
		var msgs []contract.Message
		for _, m := range req.Messages {
			msgs = append(msgs, m)
			if m.Role != contract.RoleAssistant {
				continue
			}
			for _, c := range calls(m) {
				if !answered[c.ID] {
					msgs = append(msgs, abortedResult(d, c.ID))
				}
			}
		}
		req.Messages = msgs
		return req, nil, nil
	}
}

// requireModelSelect 는 설정의 모델을 조회한다. 모르면 ContextWindow 0 인 ModelInfo 다.
func requireModelSelect(d Deps) inject.ModelSelectFn {
	return func(ctx *inject.ModelSelectCtx, req contract.Request) (contract.ModelInfo, []inject.ModelSelectSignal, error) {
		cfg, err := ParseConfig(ctx.Env().Config())
		if err != nil {
			return contract.ModelInfo{}, nil, err
		}
		if info, ok := d.Model.Lookup(ctx.Context(), cfg.Model.Provider, cfg.Model.ID); ok {
			return info, nil, nil
		}
		return contract.ModelInfo{Ref: contract.ModelRef{Provider: cfg.Model.Provider, ID: cfg.Model.ID}}, nil, nil
	}
}

// requireModelCall 은 ModelCaller 의 이벤트 시퀀스를 CallStream 으로 감싼다 (R9).
// ctx 는 dispatch 뒤 폐기되므로 context 를 여기서 꺼내 넘긴다.
func requireModelCall(d Deps) inject.ModelCallFn {
	return func(ctx *inject.ModelCallCtx, req contract.Request) (core.CallStream, []inject.ModelCallSignal, error) {
		c := ctx.Context()
		return core.CallStream{Events: d.Model.Call(c, req)}, nil, nil
	}
}

// requireStreamEnd 는 응답을 그대로 두고 선행 실행을 모두 버린다.
func requireStreamEnd(ctx *inject.StreamEndCtx, msg contract.Message) (core.StreamEndOut, []inject.StreamEndSignal, error) {
	var discard []string
	for _, s := range ctx.Tools().Speculative() {
		discard = append(discard, s.CallID)
	}
	return core.StreamEndOut{Response: msg, Discard: discard}, nil, nil
}

// baseResponseCommit 는 응답을 Store 에 기록한다.
func baseResponseCommit(d Deps) inject.ResponseCommitFn {
	return func(ctx *inject.ResponseCommitCtx) ([]inject.ResponseCommitSignal, error) {
		resp := ctx.Output().Response()
		return nil, d.Store.AppendMessages(ctx.Context(), ctx.Flow().SessionID(), []contract.Message{resp})
	}
}

// requireToolsPlan 은 호출마다 그룹 하나로 순차 실행한다.
func requireToolsPlan(ctx *inject.ToolsPlanCtx, cs []contract.ToolCall) (core.Plan, []inject.ToolsPlanSignal, error) {
	var p core.Plan
	for _, c := range cs {
		p.Groups = append(p.Groups, core.Group{Calls: []contract.ToolCall{c}, MaxParallel: 1})
	}
	return p, nil, nil
}

func errorResult(callID, msg string) *contract.ToolResult {
	return &contract.ToolResult{CallID: callID, Status: contract.StatusError, Content: []contract.Block{textBlock(msg)}}
}

// baseToolExecute 는 도구를 실행한다. panic 은 error 결과로 바꾼다 (ST-FR-11).
func baseToolExecute(d Deps) inject.ToolExecuteFn {
	return func(ctx *inject.ToolExecuteCtx, call contract.ToolCall) (contract.ToolResult, []inject.ToolExecuteSignal, error) {
		t, ok := d.Tools.Get(call.Name)
		if !ok || t.Run == nil {
			return *errorResult(call.ID, "unknown tool: "+call.Name), nil, nil
		}
		r := runTool(ctx, t, call)
		r.CallID = call.ID
		return r, nil, nil
	}
}

func runTool(ctx *inject.ToolExecuteCtx, t Tool, call contract.ToolCall) (r contract.ToolResult) {
	c := ctx.Context()
	defer func() {
		if p := recover(); p != nil {
			r = *errorResult(call.ID, fmt.Sprintf("tool %s panicked: %v", call.Name, p))
		}
	}()
	return t.Run(c, call)
}

func truncateText(s string) string {
	out, cut := s, false
	lines := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		lines++
	}
	if lines > maxResultLines {
		idx := 0
		for range maxResultLines {
			idx += strings.IndexByte(s[idx:], '\n') + 1
		}
		out, cut = s[:idx], true
	}
	if len(out) > maxResultBytes {
		end := maxResultBytes
		for end > 0 && !utf8.RuneStart(out[end]) {
			end--
		}
		out, cut = out[:end], true
	}
	if !cut {
		return s
	}
	return fmt.Sprintf("%s\n[output truncated: showing %d of %d bytes]", out, len(out), len(s))
}

// baseToolsCommit 은 결과마다 tool_result 메시지를 만들어 Store 에 기록한다.
func baseToolsCommit(d Deps) inject.ToolsCommitFn {
	return func(ctx *inject.ToolsCommitCtx, cs []contract.ToolCall, results []contract.ToolResult) ([]contract.Message, []inject.ToolsCommitSignal, error) {
		msgs := make([]contract.Message, len(results))
		for i := range results {
			r := results[i]
			msgs[i] = contract.Message{ID: d.NewID(), Role: contract.RoleToolResult, Timestamp: d.Now(), Result: &r}
		}
		if len(msgs) > 0 {
			if err := d.Store.AppendMessages(ctx.Context(), ctx.Flow().SessionID(), msgs); err != nil {
				return nil, nil, err
			}
		}
		return msgs, nil, nil
	}
}

// requireTurnFinish 는 이번 턴에 도구 호출이 있었으면 continue_turn, 없으면 settle 이다 (R2).
func requireTurnFinish(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
	if len(ctx.Tools().Calls()) > 0 {
		return []inject.TurnFinishSignal{ctx.ContinueTurn()}, nil
	}
	return []inject.TurnFinishSignal{ctx.Settle()}, nil
}
