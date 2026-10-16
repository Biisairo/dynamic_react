package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// setEntry 는 설정 sets 의 한 칸이다 (AG-FR-44).
type setEntry struct {
	Name string `json:"name"`
	On   *bool  `json:"on"`
}

// parseSets 는 설정 sets 를 harness 배치로 바꾼다. 없으면 nil (AG-FR-44).
func parseSets(raw json.RawMessage) ([]harness.SetState, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries []setEntry
	if err := decodeStrict(raw, &entries); err != nil {
		return nil, err
	}
	out := make([]harness.SetState, len(entries))
	for i, e := range entries {
		if e.Name == "" || e.On == nil {
			return nil, errors.New(`each entry needs "name" and "on"`)
		}
		out[i] = harness.SetState{Name: e.Name, On: *e.On}
	}
	return out, nil
}

// Sets 는 harness 의 지금 배치다 (AG-FR-45).
func (a *Agent) Sets() []harness.SetState { return a.h.Sets() }

// ArrangeSets 는 harness 배치를 바꾸고 사용자 설정 파일의 sets 에 저장한다. 저장에 실패하면 배치를 되돌린다 (AG-FR-45).
func (a *Agent) ArrangeSets(ctx context.Context, layout []harness.SetState) error {
	a.smu.Lock()
	defer a.smu.Unlock()
	prev := a.h.Sets()
	if err := a.h.Arrange(layout); err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	saved := make([]setEntry, len(layout))
	for i, s := range layout {
		on := s.On
		saved[i] = setEntry{Name: s.Name, On: &on}
	}
	err := a.saveConfig(ctx, func(top map[string]json.RawMessage) error {
		b, err := json.Marshal(saved)
		top["sets"] = b
		return err
	})
	if err != nil {
		_ = a.h.Arrange(prev)
		return err
	}
	return nil
}
