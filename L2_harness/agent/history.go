package agent

import (
	"context"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
)

// History 는 세션의 투영 중 원본 메시지다 (AG-FR-30).
type History struct {
	Messages []contract.Message
	Omitted  int
}

// History 는 AG-FR-30 이다. 잠금을 잡지 않는다.
func (a *Agent) History(ctx context.Context, sessionID string) (History, error) {
	if err := session.CheckID(sessionID); err != nil {
		return History{}, err
	}
	s, err := a.h.Store().Load(ctx, sessionID)
	if err != nil {
		return History{}, err
	}
	orig := make(map[string]bool, len(s.Messages))
	for _, m := range s.Messages {
		orig[m.ID] = true
	}
	var out History
	for _, m := range sets.ApplyEdits(s.Messages, s.Edits) {
		if orig[m.ID] {
			out.Messages = append(out.Messages, m)
		}
	}
	out.Omitted = len(s.Messages) - len(out.Messages)
	return out, nil
}

// Compact 는 AG-FR-31 이다. 빈 ID·잘못된 ID 는 harness 의 ID 검사가 거부한다.
func (a *Agent) Compact(ctx context.Context, sessionID string) (bool, error) {
	cfg, err := a.runConfig()
	if err != nil {
		return false, err
	}
	if !a.lock(sessionID) {
		return false, ErrSessionBusy
	}
	defer a.unlock(sessionID)
	return a.h.Compact(ctx, sessionID, cfg)
}
