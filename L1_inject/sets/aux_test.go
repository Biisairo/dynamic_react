package sets_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// ST-FR-01 (R39): aux 는 하네스 키라 형식이 틀려도 건너뛴다. 다른 모르는 키는 에러.
func TestAuxConfigSkipped(t *testing.T) {
	for name, raw := range map[string]string{
		"valid":       `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"q","id":"j","maxOutputTokens":9,"thinking":"low"}}}`,
		"empty role":  `{"model":{"provider":"p","id":"m"},"aux":{"":{"provider":"q","id":"j"}}}`,
		"empty id":    `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"q"}}}`,
		"unknown key": `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"x":1}}}`,
		"not object":  `{"model":{"provider":"p","id":"m"},"aux":[1]}`,
	} {
		if _, err := sets.ParseConfig(json.RawMessage(raw)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := sets.ParseConfig(json.RawMessage(`{"model":{"provider":"p","id":"m"},"bogus":1}`)); err == nil {
		t.Error("unknown top-level key accepted")
	}
}

// ST-FR-63
func TestAuxOf(t *testing.T) {
	_, err := sets.AuxOf(sets.Deps{}).Complete(context.Background(), nil, "judge", nil, nil)
	if !errors.Is(err, sets.ErrNoAux) || err.Error() != "sets: aux model not available" {
		t.Fatalf("no aux: %v", err)
	}
	mine := &fixedAux{reply: contract.Message{ID: "r"}}
	got, err := sets.AuxOf(sets.Deps{Aux: mine}).Complete(context.Background(), json.RawMessage(`{}`), "judge", nil, nil)
	if err != nil || got.ID != "r" || mine.role != "judge" {
		t.Fatalf("AuxOf did not return Deps.Aux: %+v %v %q", got, err, mine.role)
	}
}

type fixedAux struct {
	reply contract.Message
	role  string
}

func (f *fixedAux) Complete(ctx context.Context, config json.RawMessage, role string, system []contract.Block, msgs []contract.Message) (contract.Message, error) {
	f.role = role
	return f.reply, nil
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
