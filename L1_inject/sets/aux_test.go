package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// ST-FR-54, 55
func TestAuxConfig(t *testing.T) {
	c, err := sets.ParseConfig(json.RawMessage(`{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"q","id":"j"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := sets.AuxModel(c, "judge"); got != (sets.ConfigModel{Provider: "q", ID: "j"}) {
		t.Fatalf("aux judge = %+v", got)
	}
	if got := sets.AuxModel(c, "other"); got != (sets.ConfigModel{Provider: "p", ID: "m"}) {
		t.Fatalf("fallback = %+v", got)
	}
	for name, bad := range map[string]string{
		"empty role":     `{"model":{"provider":"p","id":"m"},"aux":{"":{"provider":"q","id":"j"}}}`,
		"empty provider": `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"","id":"j"}}}`,
		"empty id":       `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"q"}}}`,
		"unknown key":    `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"q","id":"j","x":1}}}`,
	} {
		if _, err := sets.ParseConfig(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// ST-FR-56
func TestComplete(t *testing.T) {
	want := assistant("a1", contract.Usage{}, contract.StopStop, text("verdict"))
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent { return doneEv(want) }}
	req := contract.Request{Model: contract.ModelRef{Provider: "p", ID: "m"}, Messages: []contract.Message{user("u1", "q")}}
	got, err := sets.Complete(context.Background(), m, req)
	if err != nil || got.ID != "a1" || got.Content[0].Text != "verdict" {
		t.Fatalf("done: %+v %v", got, err)
	}
	if r := m.requests(); len(r) != 1 || r[0].Model.ID != "m" || len(r[0].Tools) != 0 {
		t.Fatalf("request %+v", r)
	}

	m = &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return errEv(contract.CallError{Class: contract.ClassRateLimit, Message: "slow"})
	}}
	_, err = sets.Complete(context.Background(), m, req)
	var ce *contract.CallError
	if !errors.As(err, &ce) || ce.Class != contract.ClassRateLimit {
		t.Fatalf("error event: %v", err)
	}
	if len(m.requests()) != 1 {
		t.Fatal("Complete must not retry")
	}

	m = &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return []contract.StreamEvent{{Kind: contract.EventStart}}
	}}
	if _, err := sets.Complete(context.Background(), m, req); err == nil {
		t.Fatal("stream without done accepted")
	}

	boom := errors.New("boom")
	if _, err := sets.Complete(context.Background(), errModel{boom}, req); !errors.Is(err, boom) {
		t.Fatalf("iterator error: %v", err)
	}
}

type errModel struct{ err error }

func (e errModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	return func(yield func(contract.StreamEvent, error) bool) { yield(contract.StreamEvent{}, e.err) }
}

func (e errModel) Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool) {
	return contract.ModelInfo{}, false
}

// ST-FR-57
func TestModesOf(t *testing.T) {
	none := sets.ModesOf(sets.Deps{})
	st, err := none.Current(context.Background(), "s1")
	if err != nil || st != (sets.ModeState{}) {
		t.Fatalf("Current = %+v %v", st, err)
	}
	if err := none.Exit(context.Background(), "s1", "done"); !errors.Is(err, sets.ErrNoModes) {
		t.Fatalf("Exit = %v", err)
	}
	mine := fixedModes{sets.ModeState{Active: "goal", Arg: "x"}}
	got, _ := sets.ModesOf(sets.Deps{Modes: mine}).Current(context.Background(), "s1")
	if got.Active != "goal" || got.Arg != "x" {
		t.Fatalf("ModesOf did not return Deps.Modes: %+v", got)
	}
}

type fixedModes struct{ st sets.ModeState }

func (f fixedModes) Current(ctx context.Context, sid string) (sets.ModeState, error) {
	return f.st, nil
}
func (f fixedModes) Exit(ctx context.Context, sid, reason string) error { return nil }
