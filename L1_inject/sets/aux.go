package sets

import (
	"context"
	"errors"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// AuxModel 은 역할의 보조 모델이다. 설정에 없으면 본 모델이다 (ST-FR-55).
func AuxModel(cfg Config, role string) ConfigModel {
	if m, ok := cfg.Aux[role]; ok {
		return m
	}
	return cfg.Model
}

// Complete 는 모델을 한 번 불러 done 메시지를 돌려준다. 도구를 실행하지 않고 재시도하지 않는다 (ST-FR-56).
func Complete(ctx context.Context, m ModelCaller, req contract.Request) (contract.Message, error) {
	for ev, err := range m.Call(ctx, req) {
		if err != nil {
			return contract.Message{}, err
		}
		switch ev.Kind {
		case contract.EventDone:
			if ev.Message == nil {
				return contract.Message{}, errors.New("sets: complete: done without message")
			}
			return *ev.Message, nil
		case contract.EventError:
			if ev.Error == nil {
				return contract.Message{}, errors.New("sets: complete: error event without error")
			}
			ce := *ev.Error
			return contract.Message{}, &ce
		}
	}
	if err := ctx.Err(); err != nil {
		return contract.Message{}, err
	}
	return contract.Message{}, errors.New("sets: complete: stream ended without done")
}

// ModeState 는 세션의 모드다. Active "" = 기본 모드 (ST-FR-57).
type ModeState struct {
	Active, Arg string
}

// ModeSwitcher 는 세트가 모드를 읽고 이탈을 요청하는 통로다. 진입 수단은 없다 (ST-FR-57, 58).
type ModeSwitcher interface {
	Current(ctx context.Context, sessionID string) (ModeState, error)
	Exit(ctx context.Context, sessionID, reason string) error
}

var ErrNoModes = errors.New("sets: modes not available")

type noModes struct{}

func (noModes) Current(ctx context.Context, sessionID string) (ModeState, error) {
	return ModeState{}, nil
}

func (noModes) Exit(ctx context.Context, sessionID, reason string) error { return ErrNoModes }

// ModesOf 는 d.Modes 이고, nil 이면 "모드 없음" 구현이다 (ST-FR-57).
func ModesOf(d Deps) ModeSwitcher {
	if d.Modes == nil {
		return noModes{}
	}
	return d.Modes
}
