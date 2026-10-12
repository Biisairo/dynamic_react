package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// AuxModel 은 역할 하나의 보조 모델이다 (HN-FR-86).
type AuxModel struct {
	Provider        string `json:"provider"`
	ID              string `json:"id"`
	MaxOutputTokens int    `json:"maxOutputTokens"`
	Thinking        string `json:"thinking"`
}

// ParseAux 는 설정 문서의 최상위 aux 만 읽는다. 없으면 빈 맵이다 (HN-FR-86).
func ParseAux(config json.RawMessage) (map[string]AuxModel, error) {
	var doc struct {
		Aux json.RawMessage `json:"aux"`
	}
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, fmt.Errorf("harness: config: aux: %w", err)
	}
	out := map[string]AuxModel{}
	if len(doc.Aux) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Aux))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("harness: config: aux: %w", err)
	}
	for role, m := range out {
		if role == "" || m.Provider == "" || m.ID == "" {
			return nil, fmt.Errorf("harness: config: aux %q: role, provider and id required", role)
		}
		if m.MaxOutputTokens < 0 {
			return nil, fmt.Errorf("harness: config: aux %q: maxOutputTokens must not be negative", role)
		}
	}
	return out, nil
}

// Complete 는 보조 모델 단발 호출이다. run 밖에서도, 세트의 Deps.Aux 로도 부른다 (HN-FR-72, 85).
func (a *Agent) Complete(ctx context.Context, config json.RawMessage, role string,
	system []contract.Block, msgs []contract.Message) (contract.Message, error) {
	cfg, err := sets.ParseConfig(config)
	if err != nil {
		return contract.Message{}, err
	}
	aux, err := ParseAux(config)
	if err != nil {
		return contract.Message{}, err
	}
	m, ok := aux[role]
	if !ok {
		m = AuxModel{Provider: cfg.Model.Provider, ID: cfg.Model.ID}
	}
	ref := contract.ModelRef{Provider: m.Provider, ID: m.ID}
	maxOut := m.MaxOutputTokens
	if maxOut == 0 {
		maxOut = cfg.MaxOutputTokens
	}
	if info, ok := a.model.Lookup(ctx, m.Provider, m.ID); ok {
		ref.API = info.Ref.API
		if maxOut == 0 {
			maxOut = info.MaxOutputTokens
		}
	}
	return completeOnce(ctx, a.model, contract.Request{
		Model:    ref,
		System:   append([]contract.Block(nil), system...),
		Messages: append([]contract.Message(nil), msgs...),
		Options:  contract.Options{MaxOutputTokens: maxOut, Thinking: m.Thinking},
	})
}

// completeOnce 는 모델을 한 번 불러 done 메시지를 돌려준다. 도구를 실행하지 않고 재시도하지 않는다 (HN-FR-87).
func completeOnce(ctx context.Context, m sets.ModelCaller, req contract.Request) (contract.Message, error) {
	for ev, err := range m.Call(ctx, req) {
		if err != nil {
			return contract.Message{}, err
		}
		switch ev.Kind {
		case contract.EventDone:
			if ev.Message == nil {
				return contract.Message{}, errors.New("harness: complete: done without message")
			}
			return *ev.Message, nil
		case contract.EventError:
			if ev.Error == nil {
				return contract.Message{}, errors.New("harness: complete: error event without error")
			}
			ce := *ev.Error
			return contract.Message{}, &ce
		}
	}
	if err := ctx.Err(); err != nil {
		return contract.Message{}, err
	}
	return contract.Message{}, errors.New("harness: complete: stream ended without done")
}

// auxCaller 는 세트용 sets.AuxCaller 다 (HN-FR-85).
type auxCaller struct{ a *Agent }

func (c *auxCaller) Complete(ctx context.Context, config json.RawMessage, role string,
	system []contract.Block, msgs []contract.Message) (contract.Message, error) {
	if c.a == nil {
		return contract.Message{}, sets.ErrNoAux
	}
	return c.a.Complete(ctx, config, role, system, msgs)
}
