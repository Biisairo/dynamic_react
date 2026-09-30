package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func TestToolSpecValidate(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	runValidate(t, []validateCase{
		{"ok", contract.ToolSpec{Name: "read", Description: "d", InputSchema: schema}, true},
		{"no description", contract.ToolSpec{Name: "read", InputSchema: schema}, true},
		{"no name", contract.ToolSpec{InputSchema: schema}, false},
		{"no schema", contract.ToolSpec{Name: "read"}, false},
		{"schema not object", contract.ToolSpec{Name: "read", InputSchema: json.RawMessage(`[]`)}, false},
		{"schema not json", contract.ToolSpec{Name: "read", InputSchema: json.RawMessage(`{`)}, false},
		{"zero", contract.ToolSpec{}, false},
	})
}

func TestToolCallValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"ok", call("c1", "read", `{"path":"a"}`), true},
		{"empty object", call("c1", "read", `{}`), true},
		{"object with spaces", call("c1", "read", ` { } `), true},
		{"no id", call("", "read", `{}`), false},
		{"no name", call("c1", "", `{}`), false},
		{"no args", contract.ToolCall{ID: "c1", Name: "read"}, false},
		{"args string", call("c1", "read", `"{}"`), false},
		{"args array", call("c1", "read", `[]`), false},
		{"args null", call("c1", "read", `null`), false},
		{"args broken", call("c1", "read", `{"a":`), false},
		{"zero", contract.ToolCall{}, false},
	})
}

func TestToolResultValidate(t *testing.T) {
	img := contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}
	tc := call("c1", "read", `{}`)
	runValidate(t, []validateCase{
		{"ok", contract.ToolResult{CallID: "c1", Status: contract.StatusOK, Content: []contract.Block{textBlock("x")}}, true},
		{"image", contract.ToolResult{CallID: "c1", Status: contract.StatusError, Content: []contract.Block{img}}, true},
		{"empty content", contract.ToolResult{CallID: "c1", Status: contract.StatusAborted}, true},
		{"no call id", contract.ToolResult{Status: contract.StatusOK}, false},
		{"bad status", contract.ToolResult{CallID: "c1", Status: "done"}, false},
		{"thinking block", contract.ToolResult{CallID: "c1", Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockThinking, Text: "x"}}}, false},
		{"tool_call block", contract.ToolResult{CallID: "c1", Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockToolCall, ToolCall: &tc}}}, false},
		{"invalid image", contract.ToolResult{CallID: "c1", Status: contract.StatusOK, Content: []contract.Block{{Kind: contract.BlockImage}}}, false},
		{"zero", contract.ToolResult{}, false},
	})
}

// CT-FR-09, 36
func TestArgsDigest(t *testing.T) {
	d := func(args string) string { return call("c1", "t", args).ArgsDigest() }

	same := [][2]string{
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`},
		{`{"a":1,"b":2}`, "{ \"a\" : 1 ,\n \"b\" : 2 }"},
		{`{"o":{"x":1,"y":[1,{"q":1,"p":2}]}}`, `{"o":{"y":[1,{"p":2,"q":1}],"x":1}}`},
		{`{}`, ` { } `},
	}
	for _, p := range same {
		if d(p[0]) != d(p[1]) {
			t.Errorf("digest differs: %s vs %s", p[0], p[1])
		}
	}

	diff := [][2]string{
		{`{"a":1}`, `{"a":2}`},
		{`{"a":1}`, `{"b":1}`},
		{`{"a":1}`, `{"a":1.0}`},
		{`{"a":[1,2]}`, `{"a":[2,1]}`},
		{`{"a":"1"}`, `{"a":1}`},
		{`{"a":null}`, `{}`},
	}
	for _, p := range diff {
		if d(p[0]) == d(p[1]) {
			t.Errorf("digest equal: %s vs %s", p[0], p[1])
		}
	}

	if got := d(`{"a":1}`); len(got) != 64 {
		t.Errorf("digest %q is not sha256 hex", got)
	}
	if a, b := d(`{"a":1}`), d(`{"a":1}`); a != b {
		t.Error("digest not deterministic")
	}
	if d(`not json`) == d(`also not json`) {
		t.Error("invalid args must hash raw bytes")
	}
	if a, b := d(`not json`), d(`not json`); a != b {
		t.Error("invalid args digest not deterministic")
	}
	if call("c1", "t", `{"a":1}`).ArgsDigest() != call("c2", "other", `{"a":1}`).ArgsDigest() {
		t.Error("digest must depend on args only")
	}
}
