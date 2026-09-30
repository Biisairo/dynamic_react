package contract_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

type validator interface{ Validate() error }

type validateCase struct {
	name  string
	value validator
	ok    bool
}

func runValidate(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.value.Validate()
			if c.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !c.ok {
				if err == nil {
					t.Fatal("want invalid, got nil")
				}
				if !errors.Is(err, contract.ErrInvalid) {
					t.Fatalf("error does not wrap ErrInvalid: %v", err)
				}
			}
		})
	}
}

func textBlock(s string) contract.Block {
	return contract.Block{Kind: contract.BlockText, Text: s}
}

func userMsg(id, text string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Content: []contract.Block{textBlock(text)}, Timestamp: ts}
}

func call(id, name, args string) contract.ToolCall {
	return contract.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func assistantMsg(id string) contract.Message {
	return contract.Message{
		ID:         id,
		Role:       contract.RoleAssistant,
		Content:    []contract.Block{textBlock("hi")},
		Usage:      &contract.Usage{Input: 10, Output: 5},
		StopReason: &contract.StopReason{Kind: contract.StopStop, Raw: "end_turn"},
		Timestamp:  ts,
	}
}

func resultMsg(id, callID string) contract.Message {
	return contract.Message{
		ID:   id,
		Role: contract.RoleToolResult,
		Result: &contract.ToolResult{
			CallID:  callID,
			Status:  contract.StatusOK,
			Content: []contract.Block{textBlock("out")},
		},
		Timestamp: ts,
	}
}
