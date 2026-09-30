package contract_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func fullRequest() contract.Request {
	pd := &contract.ProviderData{Provider: "anthropic", Model: "claude-sonnet-4-5", Payload: []byte("sig")}
	tc := call("c1", "read", `{"path":"a.go"}`)
	asst := assistantMsg("a1")
	asst.Content = []contract.Block{
		{Kind: contract.BlockThinking, Text: "plan", ProviderData: pd},
		{Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Provider: "anthropic", Kind: "redacted_thinking", Payload: []byte{0, 1, 2}}},
		textBlock("reading"),
		{Kind: contract.BlockToolCall, ToolCall: &tc},
	}
	asst.Usage = &contract.Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Reasoning: 1, ContextTokens: 10}
	asst.StopReason = &contract.StopReason{Kind: contract.StopToolUse, Raw: "tool_use"}
	res := resultMsg("r1", "c1")
	res.Result.Content = append(res.Result.Content, contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{9, 8}}})
	return contract.Request{
		Model:    contract.ModelRef{Provider: "anthropic", ID: "claude-sonnet-4-5", API: "anthropic-messages"},
		System:   []contract.Block{textBlock("你好 system")},
		Messages: []contract.Message{userMsg("m1", "hi <b>&"), asst, res},
		Tools:    []contract.ToolSpec{{Name: "read", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}},
		Options: contract.Options{
			MaxOutputTokens: 8192,
			Thinking:        "high",
			ToolChoice:      &contract.ToolChoice{Mode: contract.ToolChoiceTool, Name: "read"},
			CacheHints:      []contract.CacheHint{{Target: contract.CacheSystem, Index: 0}, {Target: contract.CacheMessage, Index: 2}},
			Extra:           map[string]json.RawMessage{"z": json.RawMessage(`1`), "a": json.RawMessage(`{"k":true}`), "m": json.RawMessage(`"s"`)},
		},
	}
}

func roundTrip[T any](t *testing.T, v T) {
	t.Helper()
	b1, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back T
	if err := json.Unmarshal(b1, &back); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b1)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round trip mismatch\nin:  %#v\nout: %#v", v, back)
	}
	b2, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("bytes differ after round trip\n%s\n%s", b1, b2)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	req := fullRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}
	t.Run("request", func(t *testing.T) { roundTrip(t, req) })
	t.Run("stream events", func(t *testing.T) {
		evs := []contract.StreamEvent{
			{Kind: contract.EventStart},
			{Kind: contract.EventTextDelta, Index: 2, Delta: "x"},
			{Kind: contract.EventToolCallEnd, Index: 3, ToolCall: ptr(call("c1", "read", `{"path":"a"}`))},
			{Kind: contract.EventUsage, Usage: &contract.Usage{Input: 5}},
			doneEvent(req.Messages[1]),
			{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassRateLimit, Message: "m", RetryAfter: 1500 * time.Millisecond, Status: 429, Raw: "{}"}},
		}
		roundTrip(t, evs)
	})
	t.Run("model info", func(t *testing.T) {
		roundTrip(t, contract.ModelInfo{
			Ref:             req.Model,
			ContextWindow:   200000,
			MaxOutputTokens: 64000,
			Capabilities: contract.Capabilities{LiveInput: true, MidConvoSystem: true, ToolChanges: true, ImagesInToolResult: true,
				NativeCompaction: true, ParallelToolCalls: true, Thinking: true, Vision: true},
		})
	})
	t.Run("edits", func(t *testing.T) {
		roundTrip(t, []contract.ContextEdit{
			{ID: "e1", Kind: contract.EditReplaceRange, Timestamp: ts, From: "m1", To: "a1", Messages: []contract.Message{userMsg("s1", "sum")}},
			{ID: "e2", Kind: contract.EditHide, Timestamp: ts, Target: "m1"},
		})
	})
}

// CT-C2
func TestJSONDeterministic(t *testing.T) {
	first, err := json.Marshal(fullRequest())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		b, err := json.Marshal(fullRequest())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, b) {
			t.Fatalf("iteration %d: bytes differ", i)
		}
	}
}

// CT-FR-29, 31
func TestJSONShape(t *testing.T) {
	b, err := json.Marshal(fullRequest())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`"maxOutputTokens":8192`,
		`"toolChoice":{"mode":"tool","name":"read"}`,
		`"cacheHints":[{"target":"system","index":0},{"target":"message","index":2}]`,
		`"inputSchema":{`,
		`"providerData":{"provider":"anthropic","model":"claude-sonnet-4-5","payload":"c2ln"}`,
		`"stopReason":{"kind":"tool_use","raw":"tool_use"}`,
		`"timestamp":"2026-09-30T12:00:00Z"`,
		`"role":"tool_result"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}

	ub, _ := json.Marshal(userMsg("m1", "x"))
	for _, absent := range []string{"result", "usage", "stopReason", "image", "toolCall", "opaque", "providerData"} {
		if strings.Contains(string(ub), `"`+absent+`"`) {
			t.Errorf("empty optional field %q emitted: %s", absent, ub)
		}
	}

	eb, _ := json.Marshal(contract.CallError{Class: contract.ClassRateLimit, RetryAfter: 2 * time.Second})
	if !strings.Contains(string(eb), `"retryAfter":2000000000`) {
		t.Errorf("retryAfter not nanoseconds: %s", eb)
	}
}

func TestMarshalRejectsInvalidEnum(t *testing.T) {
	if _, err := json.Marshal(contract.Message{ID: "m1", Timestamp: ts}); err == nil {
		t.Fatal("marshal with empty role succeeded")
	}
}
