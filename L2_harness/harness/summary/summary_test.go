package summary_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/summary"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

type fakeModel struct {
	req    contract.Request
	events []contract.StreamEvent
	err    error
}

func (m *fakeModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	m.req = req
	return func(yield func(contract.StreamEvent, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(contract.StreamEvent{}, err)
			return
		}
		for _, e := range m.events {
			if !yield(e, nil) {
				return
			}
		}
		if m.err != nil {
			yield(contract.StreamEvent{}, m.err)
		}
	}
}

func (m *fakeModel) Lookup(context.Context, string, string) (contract.ModelInfo, bool) {
	return contract.ModelInfo{}, false
}

func done(stop contract.StopKind, texts ...string) []contract.StreamEvent {
	var blocks []contract.Block
	for _, t := range texts {
		blocks = append(blocks, contract.Block{Kind: contract.BlockText, Text: t})
	}
	m := contract.Message{ID: "r", Role: contract.RoleAssistant, Timestamp: ts, Content: blocks,
		Usage: &contract.Usage{Output: 1}, StopReason: &contract.StopReason{Kind: stop}}
	return []contract.StreamEvent{{Kind: contract.EventStart}, {Kind: contract.EventDone, Message: &m}}
}

func text(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

var conversation = []contract.Message{
	{ID: "u1", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{text("hello")}},
	{ID: "a1", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{
		{Kind: contract.BlockThinking, Text: "hmm", ProviderData: &contract.ProviderData{Provider: "p", Model: "m", Payload: []byte("sig")}},
		text("hi"),
		{Kind: contract.BlockToolCall, ToolCall: &contract.ToolCall{ID: "c1", Name: "read", Args: json.RawMessage(`{"path":"a","limit":5}`)}},
		{Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Provider: "p", Kind: "k"}},
	}},
	{ID: "r1", Role: contract.RoleToolResult, Timestamp: ts, Result: &contract.ToolResult{CallID: "c1", Status: contract.StatusOK,
		Content: []contract.Block{text(strings.Repeat("x", 2500))}}},
	{ID: "u2", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{text("look"), {Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1}}}}},
}

var serialized = "[User]: hello\n\n[Assistant thinking]: hmm\n\n[Assistant]: hi\n\n[Assistant tool calls]: read(path=\"a\", limit=5)\n\n" +
	"[Tool result]: " + strings.Repeat("x", 2000) + "\n\n[... 500 more characters truncated]\n\n[User]: look\n[image]"

var _ sets.Summarizer = (*summary.Summarizer)(nil)

// HN-FR-22, 23, 24
func TestSummarize(t *testing.T) {
	m := &fakeModel{events: done(contract.StopStop, "## Goal", "- x")}
	s := summary.New(m, func() time.Time { return ts })
	ref := contract.ModelRef{Provider: "p", ID: "m", API: "a"}
	out, err := s.Summarize(context.Background(), sets.SummaryRequest{Model: ref, Messages: conversation, MaxTokens: 1234})
	if err != nil || out != "## Goal\n- x" {
		t.Fatalf("%q %v", out, err)
	}
	r := m.req
	if r.Model != ref || r.Options.MaxOutputTokens != 1234 || len(r.Tools) != 0 || len(r.System) != 1 || r.System[0].Text != summary.SystemPrompt {
		t.Fatalf("request %+v", r)
	}
	if len(r.Messages) != 1 || r.Messages[0].Role != contract.RoleUser || !r.Messages[0].Timestamp.Equal(ts) {
		t.Fatalf("messages %+v", r.Messages)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	want := "<conversation>\n" + serialized + "\n</conversation>\n\n" + summary.Prompt
	if got := r.Messages[0].Content[0].Text; got != want {
		t.Fatalf("prompt:\n%s\n---want:\n%s", got, want)
	}

	s.Summarize(context.Background(), sets.SummaryRequest{Model: ref, Messages: conversation[:1], Previous: "OLD", MaxTokens: 10})
	want = "<conversation>\n[User]: hello\n</conversation>\n\n<previous-summary>\nOLD\n</previous-summary>\n\n" + summary.UpdatePrompt
	if got := m.req.Messages[0].Content[0].Text; got != want {
		t.Fatalf("update prompt:\n%s", got)
	}
}

// HN-FR-24, 25
func TestSummarizeFailures(t *testing.T) {
	cases := map[string]*fakeModel{
		"error event":  {events: []contract.StreamEvent{{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassOverloaded, Message: "busy"}}}},
		"iterator err": {err: errors.New("net")},
		"no done":      {events: []contract.StreamEvent{{Kind: contract.EventStart}}},
		"length":       {events: done(contract.StopLength, "partial")},
		"error stop":   {events: done(contract.StopError, "x")},
		"aborted":      {events: done(contract.StopAborted, "x")},
		"empty":        {events: done(contract.StopStop, "  \n")},
	}
	for name, m := range cases {
		if _, err := summary.New(m, nil).Summarize(context.Background(), sets.SummaryRequest{Messages: conversation[:1]}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &fakeModel{events: done(contract.StopStop, "ok")}
	if _, err := summary.New(m, nil).Summarize(ctx, sets.SummaryRequest{Messages: conversation[:1]}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}
