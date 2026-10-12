package sets

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// AuxCaller 는 세트가 보조 모델을 부르는 통로다. 모델·옵션 결정과 호출은 하네스의 몫이다 (ST-FR-63, R39).
type AuxCaller interface {
	Complete(ctx context.Context, config json.RawMessage, role string, system []contract.Block, msgs []contract.Message) (contract.Message, error)
}

var ErrNoAux = errors.New("sets: aux model not available")

type noAux struct{}

func (noAux) Complete(context.Context, json.RawMessage, string, []contract.Block, []contract.Message) (contract.Message, error) {
	return contract.Message{}, ErrNoAux
}

// AuxOf 는 d.Aux 이고, nil 이면 ErrNoAux 를 내는 구현이다 (ST-FR-63).
func AuxOf(d Deps) AuxCaller {
	if d.Aux == nil {
		return noAux{}
	}
	return d.Aux
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
