package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func baseRequest() contract.Request {
	return contract.Request{
		System:   []contract.Block{textBlock("sys")},
		Messages: []contract.Message{userMsg("m1", "hi")},
		Tools:    []contract.ToolSpec{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}
}

func TestRequestValidate(t *testing.T) {
	mod := func(f func(*contract.Request)) contract.Request { r := baseRequest(); f(&r); return r }
	img := contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}
	runValidate(t, []validateCase{
		{"ok", baseRequest(), true},
		{"model not checked", mod(func(r *contract.Request) { r.Model = contract.ModelRef{Provider: "p"} }), true},
		{"no system", mod(func(r *contract.Request) { r.System = nil }), true},
		{"no tools", mod(func(r *contract.Request) { r.Tools = nil }), true},
		{"cache system last", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheSystem, Index: 0}}
		}), true},
		{"cache message last", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheMessage, Index: 0}}
		}), true},
		{"zero", contract.Request{}, false},
		{"no messages", mod(func(r *contract.Request) { r.Messages = nil }), false},
		{"invalid message", mod(func(r *contract.Request) { r.Messages = []contract.Message{{}} }), false},
		{"system image", mod(func(r *contract.Request) { r.System = []contract.Block{img} }), false},
		{"system invalid", mod(func(r *contract.Request) { r.System = []contract.Block{{}} }), false},
		{"invalid tool", mod(func(r *contract.Request) { r.Tools = []contract.ToolSpec{{Name: "x"}} }), false},
		{"duplicate tool", mod(func(r *contract.Request) { r.Tools = append(r.Tools, r.Tools[0]) }), false},
		{"cache system out of range", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheSystem, Index: 1}}
		}), false},
		{"cache message out of range", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheMessage, Index: 1}}
		}), false},
		{"cache negative", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: contract.CacheMessage, Index: -1}}
		}), false},
		{"cache bad target", mod(func(r *contract.Request) {
			r.Options.CacheHints = []contract.CacheHint{{Target: "tools", Index: 0}}
		}), false},
		{"invalid options", mod(func(r *contract.Request) { r.Options.MaxOutputTokens = -1 }), false},
	})
}

func TestOptionsValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"zero", contract.Options{}, true},
		{"max tokens", contract.Options{MaxOutputTokens: 1024, Thinking: "high"}, true},
		{"extra", contract.Options{Extra: map[string]json.RawMessage{"top_k": json.RawMessage(`5`)}}, true},
		{"choice auto", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceAuto}}, true},
		{"choice none", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceNone}}, true},
		{"choice required", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceRequired}}, true},
		{"choice tool", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceTool, Name: "read"}}, true},
		{"negative max tokens", contract.Options{MaxOutputTokens: -1}, false},
		{"choice tool without name", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceTool}}, false},
		{"choice auto with name", contract.Options{ToolChoice: &contract.ToolChoice{Mode: contract.ToolChoiceAuto, Name: "read"}}, false},
		{"choice bad mode", contract.Options{ToolChoice: &contract.ToolChoice{Mode: "any"}}, false},
	})
}

func TestModelValidate(t *testing.T) {
	ref := contract.ModelRef{Provider: "anthropic", ID: "claude-sonnet-4-5", API: "anthropic-messages"}
	runValidate(t, []validateCase{
		{"ref ok", ref, true},
		{"ref no provider", contract.ModelRef{ID: "x", API: "a"}, false},
		{"ref no id", contract.ModelRef{Provider: "p", API: "a"}, false},
		{"ref no api", contract.ModelRef{Provider: "p", ID: "x"}, false},
		{"info ok", contract.ModelInfo{Ref: ref, ContextWindow: 200000, MaxOutputTokens: 64000, Capabilities: contract.Capabilities{Thinking: true}}, true},
		{"info unknown window", contract.ModelInfo{Ref: ref}, true},
		{"info bad ref", contract.ModelInfo{}, false},
		{"info negative window", contract.ModelInfo{Ref: ref, ContextWindow: -1}, false},
		{"info negative max output", contract.ModelInfo{Ref: ref, MaxOutputTokens: -1}, false},
	})
}
