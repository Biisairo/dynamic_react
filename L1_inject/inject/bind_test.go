package inject_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// IN-FR-19, 21
func TestBindOverlay(t *testing.T) {
	over := inject.FunctionSet{Name: "over",
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			return []inject.TurnFinishSignal{ctx.Stop()}, nil
		},
	}
	empty := inject.FunctionSet{Name: "empty"}
	d := mustBind(t, workingSet("base"), over, empty)
	b := d.Binding()
	if len(b) != 22 {
		t.Fatalf("binding size %d", len(b))
	}
	for _, s := range core.Steps() {
		want := "base"
		if s == core.StepTurnFinish {
			want = "over"
		}
		if b[s] != want {
			t.Errorf("%s bound to %q", s, b[s])
		}
	}
	b[core.StepRunStart] = "mutated"
	if d.Binding()[core.StepRunStart] != "base" {
		t.Fatal("Binding() exposes internal map")
	}
	if s := runCore(t, d); s.Reason != core.SettleStopped {
		t.Fatalf("overlay not applied: %s", s.Reason)
	}
}

func TestBindMissing(t *testing.T) {
	set := workingSet("base")
	set.ToolAfter, set.RunSettled = nil, nil
	_, err := inject.Bind(set)
	if !errors.Is(err, inject.ErrBind) || !strings.Contains(err.Error(), "tool.after, run.settled") {
		t.Fatalf("err = %v", err)
	}
	if _, err := inject.Bind(); !errors.Is(err, inject.ErrBind) {
		t.Fatal("empty bind accepted")
	}
}

func TestBindNames(t *testing.T) {
	if _, err := inject.Bind(workingSet("a"), workingSet("a")); !errors.Is(err, inject.ErrBind) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := inject.Bind(workingSet("a"), inject.FunctionSet{}); !errors.Is(err, inject.ErrBind) {
		t.Fatalf("empty name: %v", err)
	}
}

func TestEnvBinding(t *testing.T) {
	var got map[core.Step]string
	set := workingSet("base")
	set.RunBeforeSettle = func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
		got = ctx.Env().Binding()
		if ctx.Env().ContractVersion() != 2 || string(ctx.Env().Config()) != `{"a":1}` {
			t.Errorf("env %d %s", ctx.Env().ContractVersion(), ctx.Env().Config())
		}
		return nil, nil
	}
	runCore(t, mustBind(t, set))
	if got[core.StepRunBeforeSettle] != "base" {
		t.Fatalf("binding %v", got)
	}
}
