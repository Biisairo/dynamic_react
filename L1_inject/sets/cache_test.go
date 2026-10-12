package sets_test

import (
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

func msgs(ids ...string) []contract.Message {
	var out []contract.Message
	for _, id := range ids {
		out = append(out, user(id, id))
	}
	return out
}

// ST-FR-60
func TestStableLen(t *testing.T) {
	cases := []struct {
		name      string
		msgs, eph []string
		want      int
	}{
		{"no tail", []string{"u1", "a1", "u2"}, []string{"x"}, 3},
		{"tail 1", []string{"u1", "a1", "e1"}, []string{"e1"}, 2},
		{"tail 2", []string{"u1", "e1", "e2"}, []string{"e1", "e2"}, 1},
		{"middle is stable", []string{"u1", "e1", "u2", "e2"}, []string{"e1", "e2"}, 3},
		{"all tail", []string{"e1", "e2"}, []string{"e1", "e2"}, 0},
		{"empty msgs", nil, []string{"e1"}, 0},
		{"empty ephemeral", []string{"u1", "u2"}, nil, 2},
	}
	for _, c := range cases {
		if got := sets.StableLen(msgs(c.msgs...), msgs(c.eph...)); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// ST-FR-61
func TestCacheHintBeforeTail(t *testing.T) {
	d := deps(&fakeModel{})
	finalize := func(m []contract.Message, eph []contract.Message) []contract.CacheHint {
		t.Helper()
		req := contract.Request{System: []contract.Block{text("sys")}, Messages: m}
		res, err := stackStep(t, core.StepRequestFinalize, req, injecttest.Facts{Ephemeral: eph}, stack(d, false)...)
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(contract.Request).Options.CacheHints
	}
	sys := contract.CacheHint{Target: contract.CacheSystem, Index: 0}

	h := finalize(msgs("u1", "a1", "rule"), msgs("rule"))
	if len(h) != 2 || h[0] != sys || h[1] != (contract.CacheHint{Target: contract.CacheMessage, Index: 1}) {
		t.Fatalf("tail 1: %+v", h)
	}
	h = finalize(msgs("rule"), msgs("rule"))
	if len(h) != 1 || h[0] != sys {
		t.Fatalf("all tail: %+v", h)
	}
	h = finalize(msgs("u1", "a1"), nil)
	if len(h) != 2 || h[1] != (contract.CacheHint{Target: contract.CacheMessage, Index: 1}) {
		t.Fatalf("no ephemeral: %+v", h)
	}
}
