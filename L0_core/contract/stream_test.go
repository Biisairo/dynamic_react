package contract_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func TestUsage(t *testing.T) {
	u := contract.Usage{Input: 100, Output: 40, CacheRead: 1000, CacheWrite: 10, Reasoning: 30}
	if got := u.Prompt(); got != 1110 {
		t.Errorf("Prompt() = %d, want 1110", got)
	}
	if got := u.Total(); got != 1150 {
		t.Errorf("Total() = %d, want 1150", got)
	}
	if (contract.Usage{}).Total() != 0 {
		t.Error("zero usage Total() != 0")
	}
	runValidate(t, []validateCase{
		{"ok", u, true},
		{"zero", contract.Usage{}, true},
		{"reasoning equals output", contract.Usage{Output: 5, Reasoning: 5}, true},
		{"context tokens", contract.Usage{ContextTokens: 7}, true},
		{"negative input", contract.Usage{Input: -1}, false},
		{"negative output", contract.Usage{Output: -1}, false},
		{"negative cache read", contract.Usage{CacheRead: -1}, false},
		{"negative cache write", contract.Usage{CacheWrite: -1}, false},
		{"negative reasoning", contract.Usage{Reasoning: -1}, false},
		{"negative context", contract.Usage{ContextTokens: -1}, false},
		{"reasoning exceeds output", contract.Usage{Output: 5, Reasoning: 6}, false},
	})
}

func TestStopReasonValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"ok", contract.StopReason{Kind: contract.StopLength, Raw: "max_tokens"}, true},
		{"no raw", contract.StopReason{Kind: contract.StopStop}, true},
		{"zero", contract.StopReason{}, false},
		{"bad kind", contract.StopReason{Kind: "halt"}, false},
	})
}

func TestCallError(t *testing.T) {
	e := &contract.CallError{Class: contract.ClassRateLimit, Message: "slow down", RetryAfter: 2 * time.Second, Status: 429}
	var err error = e
	if got := err.Error(); got != "rate_limit: slow down" {
		t.Errorf("Error() = %q", got)
	}
	runValidate(t, []validateCase{
		{"ok", *e, true},
		{"no message", contract.CallError{Class: contract.ClassUnknown}, true},
		{"zero", contract.CallError{}, false},
		{"bad class", contract.CallError{Class: "teapot"}, false},
		{"negative retry", contract.CallError{Class: contract.ClassRateLimit, RetryAfter: -time.Second}, false},
	})
}

func doneEvent(msg contract.Message) contract.StreamEvent {
	return contract.StreamEvent{Kind: contract.EventDone, Message: &msg}
}

func TestStreamEventValidate(t *testing.T) {
	tc := call("c1", "read", `{"path":"a"}`)
	partial := contract.ToolCall{ID: "c1", Name: "read"}
	errEv := &contract.CallError{Class: contract.ClassOverflow, Message: "too long"}
	bareAssistant := assistantMsg("a1")
	bareAssistant.Usage = nil
	noStop := assistantMsg("a1")
	noStop.StopReason = nil
	runValidate(t, []validateCase{
		{"start", contract.StreamEvent{Kind: contract.EventStart}, true},
		{"text", contract.StreamEvent{Kind: contract.EventTextDelta, Index: 1, Delta: "x"}, true},
		{"thinking", contract.StreamEvent{Kind: contract.EventThinkingDelta, Delta: "x"}, true},
		{"toolcall start", contract.StreamEvent{Kind: contract.EventToolCallStart, ToolCall: &partial}, true},
		{"toolcall delta", contract.StreamEvent{Kind: contract.EventToolCallDelta, Delta: `{"pa`}, true},
		{"toolcall end", contract.StreamEvent{Kind: contract.EventToolCallEnd, ToolCall: &tc}, true},
		{"usage", contract.StreamEvent{Kind: contract.EventUsage, Usage: &contract.Usage{Input: 1}}, true},
		{"done", doneEvent(assistantMsg("a1")), true},
		{"error", contract.StreamEvent{Kind: contract.EventError, Error: errEv}, true},
		{"zero", contract.StreamEvent{}, false},
		{"bad kind", contract.StreamEvent{Kind: "ping"}, false},
		{"negative index", contract.StreamEvent{Kind: contract.EventTextDelta, Index: -1, Delta: "x"}, false},
		{"text no delta", contract.StreamEvent{Kind: contract.EventTextDelta}, false},
		{"thinking no delta", contract.StreamEvent{Kind: contract.EventThinkingDelta}, false},
		{"toolcall delta no delta", contract.StreamEvent{Kind: contract.EventToolCallDelta}, false},
		{"toolcall start no call", contract.StreamEvent{Kind: contract.EventToolCallStart}, false},
		{"toolcall start no name", contract.StreamEvent{Kind: contract.EventToolCallStart, ToolCall: &contract.ToolCall{ID: "c1"}}, false},
		{"toolcall end partial", contract.StreamEvent{Kind: contract.EventToolCallEnd, ToolCall: &partial}, false},
		{"usage missing", contract.StreamEvent{Kind: contract.EventUsage}, false},
		{"done missing message", contract.StreamEvent{Kind: contract.EventDone}, false},
		{"done user message", doneEvent(userMsg("m1", "x")), false},
		{"done without usage", doneEvent(bareAssistant), false},
		{"done without stop reason", doneEvent(noStop), false},
		{"error missing", contract.StreamEvent{Kind: contract.EventError}, false},
		{"error invalid", contract.StreamEvent{Kind: contract.EventError, Error: &contract.CallError{}}, false},
		{"start with foreign delta", contract.StreamEvent{Kind: contract.EventStart, Delta: "x"}, false},
		{"text with foreign usage", contract.StreamEvent{Kind: contract.EventTextDelta, Delta: "x", Usage: &contract.Usage{}}, false},
		{"done with foreign error", contract.StreamEvent{Kind: contract.EventDone, Message: ptr(assistantMsg("a1")), Error: errEv}, false},
	})
}

func ptr[T any](v T) *T { return &v }

// CT-FR-16, 35
func TestValidateStream(t *testing.T) {
	start := contract.StreamEvent{Kind: contract.EventStart}
	text := contract.StreamEvent{Kind: contract.EventTextDelta, Delta: "hi"}
	done := doneEvent(assistantMsg("a1"))
	fail := contract.StreamEvent{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassUnknown, Message: "aborted"}}

	cases := []struct {
		name   string
		events []contract.StreamEvent
		ok     bool
	}{
		{"done", []contract.StreamEvent{start, text, done}, true},
		{"only done", []contract.StreamEvent{done}, true},
		{"error", []contract.StreamEvent{start, text, fail}, true},
		{"only error", []contract.StreamEvent{fail}, true},
		{"empty", nil, false},
		{"no terminal", []contract.StreamEvent{start, text}, false},
		{"two dones", []contract.StreamEvent{done, done}, false},
		{"done then error", []contract.StreamEvent{start, done, fail}, false},
		{"terminal not last", []contract.StreamEvent{start, done, text}, false},
		{"invalid event", []contract.StreamEvent{start, {Kind: contract.EventTextDelta}, done}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := contract.ValidateStream(c.events)
			if c.ok != (err == nil) {
				t.Fatalf("ok=%v, err=%v", c.ok, err)
			}
			if err != nil && !strings.Contains(err.Error(), "contract") {
				t.Fatalf("error lacks package prefix: %v", err)
			}
		})
	}
}
