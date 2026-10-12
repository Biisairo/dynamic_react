package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

type enumCase struct {
	name    string
	valid   []string
	decode  func([]byte) error
	encode  func(string) ([]byte, error)
	isValid func(string) bool
}

func enumOf[T ~string](name string, valid ...T) enumCase {
	vs := make([]string, len(valid))
	for i, v := range valid {
		vs[i] = string(v)
	}
	return enumCase{
		name:  name,
		valid: vs,
		decode: func(b []byte) error {
			var v T
			return json.Unmarshal(b, &v)
		},
		encode: func(s string) ([]byte, error) { return json.Marshal(T(s)) },
		isValid: func(s string) bool {
			return any(T(s)).(interface{ Valid() bool }).Valid()
		},
	}
}

// CT-FR-02, 10, 14, 20, 21, 24, 28, 30, 33 과 CacheHint.Target 의 닫힌 값 목록.
var enums = []enumCase{
	enumOf("Role", contract.RoleSystem, contract.RoleUser, contract.RoleAssistant, contract.RoleToolResult),
	enumOf("BlockKind", contract.BlockText, contract.BlockImage, contract.BlockThinking, contract.BlockToolCall, contract.BlockOpaque),
	enumOf("ToolStatus", contract.StatusOK, contract.StatusError, contract.StatusAborted, contract.StatusSkipped),
	enumOf("StreamEventKind", contract.EventStart, contract.EventTextDelta, contract.EventThinkingDelta,
		contract.EventToolCallStart, contract.EventToolCallDelta, contract.EventToolCallEnd,
		contract.EventUsage, contract.EventDone, contract.EventError),
	enumOf("StopKind", contract.StopStop, contract.StopLength, contract.StopToolUse, contract.StopError, contract.StopAborted),
	enumOf("ErrorClass", contract.ClassOverflow, contract.ClassRateLimit, contract.ClassOverloaded, contract.ClassAuth,
		contract.ClassPayloadTooLarge, contract.ClassTransientNetwork, contract.ClassInvalidRequest,
		contract.ClassPermanent, contract.ClassUnknown),
	enumOf("EditKind", contract.EditReplaceRange, contract.EditHide, contract.EditTruncateAfter),
	enumOf("ToolChoiceMode", contract.ToolChoiceAuto, contract.ToolChoiceNone, contract.ToolChoiceRequired, contract.ToolChoiceTool),
	enumOf("CacheTarget", contract.CacheSystem, contract.CacheMessage),
}

var enumWire = map[string][]string{
	"Role":            {"system", "user", "assistant", "tool_result"},
	"BlockKind":       {"text", "image", "thinking", "tool_call", "opaque"},
	"ToolStatus":      {"ok", "error", "aborted", "skipped"},
	"StreamEventKind": {"start", "text_delta", "thinking_delta", "toolcall_start", "toolcall_delta", "toolcall_end", "usage", "done", "error"},
	"StopKind":        {"stop", "length", "tool_use", "error", "aborted"},
	"ErrorClass":      {"overflow", "rate_limit", "overloaded", "auth", "payload_too_large", "transient_network", "invalid_request", "permanent", "unknown"},
	"EditKind":        {"replace_range", "hide", "truncate_after"},
	"ToolChoiceMode":  {"auto", "none", "required", "tool"},
	"CacheTarget":     {"system", "message"},
}

func TestEnumWireValues(t *testing.T) {
	for _, e := range enums {
		want := enumWire[e.name]
		if len(want) != len(e.valid) {
			t.Fatalf("%s: %d values, want %d", e.name, len(e.valid), len(want))
		}
		for i := range want {
			if e.valid[i] != want[i] {
				t.Errorf("%s[%d] = %q, want %q", e.name, i, e.valid[i], want[i])
			}
		}
	}
}

func TestEnumRoundTrip(t *testing.T) {
	for _, e := range enums {
		for _, v := range e.valid {
			if !e.isValid(v) {
				t.Errorf("%s(%q).Valid() = false", e.name, v)
			}
			b, err := e.encode(v)
			if err != nil {
				t.Fatalf("%s: marshal %q: %v", e.name, v, err)
			}
			if err := e.decode(b); err != nil {
				t.Errorf("%s: unmarshal %s: %v", e.name, b, err)
			}
		}
	}
}

func TestEnumRejectsUnknown(t *testing.T) {
	for _, e := range enums {
		for _, bad := range []string{"", "bogus", "TEXT"} {
			if e.isValid(bad) {
				t.Errorf("%s(%q).Valid() = true", e.name, bad)
			}
			if _, err := e.encode(bad); err == nil {
				t.Errorf("%s: marshal %q succeeded", e.name, bad)
			}
			b, _ := json.Marshal(bad)
			if err := e.decode(b); err == nil {
				t.Errorf("%s: unmarshal %s succeeded", e.name, b)
			}
		}
		if err := e.decode([]byte(`1`)); err == nil {
			t.Errorf("%s: unmarshal number succeeded", e.name)
		}
	}
}

func TestUnknownEnumInsideStruct(t *testing.T) {
	var m contract.Message
	err := json.Unmarshal([]byte(`{"id":"m1","role":"robot","timestamp":"2026-09-30T12:00:00Z"}`), &m)
	if err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestContractVersion(t *testing.T) {
	if contract.ContractVersion != 2 {
		t.Fatalf("ContractVersion = %d, want 2", contract.ContractVersion)
	}
}
