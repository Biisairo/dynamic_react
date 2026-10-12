package agent

import (
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// Stats 는 AG-FR-52 다.
func (a *Agent) Stats(sessionID string) (harness.Stats, bool) { return a.h.Stats(sessionID) }

// ToolNames 는 모델에 보내는 도구 이름이다 (AG-FR-53).
func (a *Agent) ToolNames() []string {
	registry := a.h.Tools().Names()
	a.smu.Lock()
	cfg, err := sets.ParseConfig(a.config)
	a.smu.Unlock()
	if err != nil || len(cfg.Tools) == 0 {
		return registry
	}
	want := map[string]bool{}
	for _, n := range cfg.Tools {
		want[n] = true
	}
	var out []string
	for _, n := range registry {
		if want[n] {
			out = append(out, n)
		}
	}
	return out
}
