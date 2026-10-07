package agent

import (
	"context"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// Modes 는 등록한 모드다 (AG-FR-49).
func (a *Agent) Modes() []harness.Mode { return a.h.Modes() }

// ModeStatus 는 세션의 모드 상태다 (AG-FR-49).
func (a *Agent) ModeStatus(ctx context.Context, sessionID string) (harness.ModeStatus, error) {
	return a.h.ModeStatus(ctx, sessionID)
}

// EnterMode 는 사용자의 진입 예약이다. 세션 잠금을 잡지 않는다 (AG-FR-49).
func (a *Agent) EnterMode(ctx context.Context, sessionID, name, arg string) error {
	return a.h.RequestMode(ctx, sessionID, harness.ModeRequest{To: name, Arg: arg, Origin: harness.ModeUser})
}

// ExitMode 는 사용자의 강제 이탈 예약이다 (AG-FR-49).
func (a *Agent) ExitMode(ctx context.Context, sessionID string) error {
	return a.h.RequestMode(ctx, sessionID, harness.ModeRequest{Origin: harness.ModeUser})
}

// Complete 는 역할별 보조 모델의 단발 호출이다 (AG-FR-50).
func (a *Agent) Complete(ctx context.Context, role string, system []contract.Block, msgs []contract.Message) (contract.Message, error) {
	cfg, err := a.runConfig()
	if err != nil {
		return contract.Message{}, err
	}
	return a.h.Complete(ctx, cfg, role, system, msgs)
}
