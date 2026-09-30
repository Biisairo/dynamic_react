package inject

import (
	"encoding/json"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// 조회 결과의 깊은 복사 (IN-FR-17, 48). 함수가 결과를 고쳐도 스냅샷은 바뀌지 않는다.

func ptrClone[T any](p *T, f func(T) T) *T {
	if p == nil {
		return nil
	}
	v := f(*p)
	return &v
}

func same[T any](v T) T { return v }

func cloneSlice[T any](s []T, f func(T) T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

func cloneProviderData(p contract.ProviderData) contract.ProviderData {
	p.Payload = clone(p.Payload)
	return p
}

func cloneBlock(b contract.Block) contract.Block {
	b.Image = ptrClone(b.Image, func(i contract.Image) contract.Image { i.Data = clone(i.Data); return i })
	b.ToolCall = ptrClone(b.ToolCall, cloneToolCall)
	b.Opaque = ptrClone(b.Opaque, func(o contract.Opaque) contract.Opaque { o.Payload = clone(o.Payload); return o })
	b.ProviderData = ptrClone(b.ProviderData, cloneProviderData)
	return b
}

func cloneToolCall(c contract.ToolCall) contract.ToolCall {
	c.Args = clone(c.Args)
	return c
}

func cloneResult(r contract.ToolResult) contract.ToolResult {
	r.Content = cloneSlice(r.Content, cloneBlock)
	return r
}

func cloneMessage(m contract.Message) contract.Message {
	m.Content = cloneSlice(m.Content, cloneBlock)
	m.Result = ptrClone(m.Result, cloneResult)
	m.Usage = ptrClone(m.Usage, same)
	m.StopReason = ptrClone(m.StopReason, same)
	return m
}

func cloneMessages(ms []contract.Message) []contract.Message { return cloneSlice(ms, cloneMessage) }

func cloneRequest(r contract.Request) contract.Request {
	r.System = cloneSlice(r.System, cloneBlock)
	r.Messages = cloneMessages(r.Messages)
	r.Tools = cloneSlice(r.Tools, func(t contract.ToolSpec) contract.ToolSpec { t.InputSchema = clone(t.InputSchema); return t })
	o := &r.Options
	o.ToolChoice = ptrClone(o.ToolChoice, same)
	o.CacheHints = cloneSlice(o.CacheHints, same)
	if o.Extra != nil {
		extra := make(map[string]json.RawMessage, len(o.Extra))
		for k, v := range o.Extra {
			extra[k] = clone(v)
		}
		o.Extra = extra
	}
	return r
}

func cloneEvent(e contract.StreamEvent) contract.StreamEvent {
	e.ToolCall = ptrClone(e.ToolCall, cloneToolCall)
	e.Usage = ptrClone(e.Usage, same)
	e.Message = ptrClone(e.Message, cloneMessage)
	e.Error = ptrClone(e.Error, same)
	return e
}

func cloneEdit(e contract.ContextEdit) contract.ContextEdit {
	e.Messages = cloneMessages(e.Messages)
	return e
}

func clonePlan(p core.Plan) core.Plan {
	p.Groups = cloneSlice(p.Groups, func(g core.Group) core.Group {
		g.Calls = cloneSlice(g.Calls, cloneToolCall)
		return g
	})
	return p
}

func cloneSignal(s core.Signal) core.Signal {
	s.Edit = ptrClone(s.Edit, cloneEdit)
	s.Message = ptrClone(s.Message, cloneMessage)
	s.Call = ptrClone(s.Call, cloneToolCall)
	s.Results = cloneSlice(s.Results, cloneResult)
	return s
}

func cloneSignals(ss []core.Signal) []core.Signal { return cloneSlice(ss, cloneSignal) }
