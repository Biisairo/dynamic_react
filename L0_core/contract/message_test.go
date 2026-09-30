package contract_test

import (
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func TestBlockValidate(t *testing.T) {
	pd := &contract.ProviderData{Provider: "anthropic", Model: "m", Payload: []byte("sig")}
	tc := call("c1", "read", `{}`)
	runValidate(t, []validateCase{
		{"text", textBlock("x"), true},
		{"text empty allowed", textBlock(""), true},
		{"text with provider data", contract.Block{Kind: contract.BlockText, Text: "x", ProviderData: pd}, true},
		{"image", contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}, true},
		{"image missing", contract.Block{Kind: contract.BlockImage}, false},
		{"image no media type", contract.Block{Kind: contract.BlockImage, Image: &contract.Image{Data: []byte{1}}}, false},
		{"image no data", contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png"}}, false},
		{"thinking", contract.Block{Kind: contract.BlockThinking, Text: "hmm", ProviderData: pd}, true},
		{"tool_call", contract.Block{Kind: contract.BlockToolCall, ToolCall: &tc}, true},
		{"tool_call missing", contract.Block{Kind: contract.BlockToolCall}, false},
		{"tool_call invalid", contract.Block{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: "c1"}}, false},
		{"opaque", contract.Block{Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Provider: "anthropic", Kind: "redacted_thinking", Payload: []byte("x")}}, true},
		{"opaque missing", contract.Block{Kind: contract.BlockOpaque}, false},
		{"opaque no kind", contract.Block{Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Provider: "p"}}, false},
		{"opaque no provider", contract.Block{Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Kind: "k"}}, false},
		{"zero", contract.Block{}, false},
		{"unknown kind", contract.Block{Kind: "video"}, false},
		{"text with foreign image", contract.Block{Kind: contract.BlockText, Text: "x", Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}, false},
		{"text with foreign tool call", contract.Block{Kind: contract.BlockText, ToolCall: &tc}, false},
		{"image with foreign text", contract.Block{Kind: contract.BlockImage, Text: "x", Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}, false},
		{"tool_call with foreign opaque", contract.Block{Kind: contract.BlockToolCall, ToolCall: &tc, Opaque: &contract.Opaque{Provider: "p", Kind: "k"}}, false},
		{"bad provider data", contract.Block{Kind: contract.BlockText, ProviderData: &contract.ProviderData{Provider: "p"}}, false},
	})
}

func TestProviderDataValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"ok", contract.ProviderData{Provider: "p", Model: "m"}, true},
		{"with payload", contract.ProviderData{Provider: "p", Model: "m", Payload: []byte("x")}, true},
		{"no provider", contract.ProviderData{Model: "m"}, false},
		{"no model", contract.ProviderData{Provider: "p"}, false},
		{"zero", contract.ProviderData{}, false},
	})
}

func TestMessageValidate(t *testing.T) {
	withRole := func(m contract.Message, r contract.Role) contract.Message { m.Role = r; return m }
	userWithUsage := userMsg("m1", "x")
	userWithUsage.Usage = &contract.Usage{}
	userWithStop := userMsg("m1", "x")
	userWithStop.StopReason = &contract.StopReason{Kind: contract.StopStop}
	resultWithContent := resultMsg("m1", "c1")
	resultWithContent.Content = []contract.Block{textBlock("x")}
	resultNoResult := resultMsg("m1", "c1")
	resultNoResult.Result = nil
	userWithResult := userMsg("m1", "x")
	userWithResult.Result = &contract.ToolResult{CallID: "c1", Status: contract.StatusOK}
	noID := userMsg("", "x")
	noTime := userMsg("m1", "x")
	noTime.Timestamp = time.Time{}
	badBlock := userMsg("m1", "x")
	badBlock.Content = []contract.Block{{Kind: "nope"}}
	badResult := resultMsg("m1", "")
	assistantBadUsage := assistantMsg("a1")
	assistantBadUsage.Usage = &contract.Usage{Input: -1}
	assistantBadStop := assistantMsg("a1")
	assistantBadStop.StopReason = &contract.StopReason{Kind: "halt"}
	assistantBare := assistantMsg("a1")
	assistantBare.Usage, assistantBare.StopReason = nil, nil

	runValidate(t, []validateCase{
		{"user", userMsg("m1", "x"), true},
		{"user empty content", contract.Message{ID: "m1", Role: contract.RoleUser, Timestamp: ts}, true},
		{"system", withRole(userMsg("m1", "x"), contract.RoleSystem), true},
		{"assistant with usage and stop", assistantMsg("a1"), true},
		{"assistant without usage and stop", assistantBare, true},
		{"tool_result", resultMsg("m1", "c1"), true},
		{"zero", contract.Message{}, false},
		{"no id", noID, false},
		{"no timestamp", noTime, false},
		{"unknown role", withRole(userMsg("m1", "x"), "robot"), false},
		{"tool_result with content", resultWithContent, false},
		{"tool_result without result", resultNoResult, false},
		{"user with result", userWithResult, false},
		{"user with usage", userWithUsage, false},
		{"user with stop reason", userWithStop, false},
		{"invalid block", badBlock, false},
		{"invalid result", badResult, false},
		{"assistant invalid usage", assistantBadUsage, false},
		{"assistant invalid stop reason", assistantBadStop, false},
	})
}
