package sets

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// resilient 네임스페이스의 상태 키. run 을 넘어 복원되므로 RunID 를 함께 둔다 (ST-FR-44).
const (
	keyRetry             = "retry"
	keyRecovered         = "recovered"
	keyCompaction        = "compaction"
	keyCompactionBlocked = "compaction.blocked"
)

const (
	minReserveTokens      = 16384
	maxInvalidCompactions = 2
	baseNamespace         = "base"
)

type retryState struct {
	Run    string                      `json:"run"`
	Turn   int                         `json:"turn"`
	Counts map[contract.ErrorClass]int `json:"counts"`
}

type turnMark struct {
	Run  string `json:"run"`
	Turn int    `json:"turn"`
}

type compactionState struct {
	Run     string `json:"run"`
	Pending bool   `json:"pending"`
	Invalid int    `json:"invalid"`
	Blocked bool   `json:"blocked"`
}

// Resilient 는 재시도·overflow 복구·임계치 압축·follow-up 세트다. 3·12·13·21 만 채운다 (ST-FR-07).
func Resilient(d Deps) inject.FunctionSet {
	d = d.filled()
	return inject.FunctionSet{
		Name:             "resilient",
		ContextMaintain:  ResilientContextMaintain(d),
		CallError:        ResilientCallError(d),
		ResponseClassify: ResilientResponseClassify(d),
		RunBeforeSettle:  ResilientRunBeforeSettle(d),
	}
}

func reserveTokens(cfg Config, window int) int {
	if cfg.Compaction.ReserveTokens > 0 {
		return cfg.Compaction.ReserveTokens
	}
	return max(minReserveTokens, window*15/100)
}

func canCompact(d Deps, cfg Config) bool { return cfg.Compaction.Enabled && d.Summarizer != nil }

// compactConv 는 Transcript 에 편집을 적용한 투영을 압축한다 (ST-FR-05 의 투영 규칙).
func compactConv(c context.Context, d Deps, cfg Config, model contract.ModelInfo, conv inject.Conv) (contract.ContextEdit, bool, error) {
	proj := ApplyEdits(conv.Transcript(), conv.Edits())
	return Compact(c, d.Summarizer, model.Ref, proj, cfg.Compaction.KeepRecentTokens,
		reserveTokens(cfg, model.ContextWindow), model.MaxOutputTokens, d.Now(), d.NewID(), d.NewID())
}

func recoveredThisTurn(st inject.State, fl inject.Flow) bool {
	m, ok := getJSON[turnMark](st, keyRecovered)
	return ok && m.Run == fl.RunID() && m.Turn == fl.TurnIndex()
}

func markRecovered(st inject.State, fl inject.Flow) error {
	return st.Set(keyRecovered, turnMark{Run: fl.RunID(), Turn: fl.TurnIndex()})
}

// ResilientContextMaintain 은 투영 추정이 W − reserve 를 넘으면 압축한다 (ST-FR-05).
// 압축 뒤 응답이 여전히 임계치 이상이면 무효 압축으로 세고, 연속 2회면 이 run 에서 멈춘다 (ST-FR-06).
func ResilientContextMaintain(d Deps) inject.ContextMaintainFn {
	d = d.filled()
	return func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
		cfg, err := ParseConfig(ctx.Env().Config())
		if err != nil {
			return nil, err
		}
		model := ctx.Request().Model()
		if !canCompact(d, cfg) || model.ContextWindow <= 0 {
			return nil, nil
		}
		threshold := model.ContextWindow - reserveTokens(cfg, model.ContextWindow)
		st, run := ctx.State(), ctx.Flow().RunID()
		cs, _ := getJSON[compactionState](st, keyCompaction)
		if cs.Run != run {
			cs = compactionState{Run: run}
			st.Delete(keyCompactionBlocked)
		}
		before := cs
		if cs.Pending {
			cs.Pending = false
			if ctx.Output().LastUsage().Prompt() >= threshold {
				cs.Invalid++
			} else {
				cs.Invalid = 0
			}
			if cs.Invalid >= maxInvalidCompactions {
				cs.Blocked = true
				if err := st.Set(keyCompactionBlocked, true); err != nil {
					return nil, err
				}
				st.Publish(keyCompactionBlocked)
			}
		}
		var sigs []inject.ContextMaintainSignal
		if !cs.Blocked {
			conv := ctx.Conv()
			edits := conv.Edits()
			used := EstimateContext(ApplyEdits(conv.Transcript(), edits), edits, d.System, toolSpecs(d.Tools, cfg.Tools))
			if used > threshold {
				if edit, ok, err := compactConv(ctx.Context(), d, cfg, model, conv); err == nil && ok {
					sigs = []inject.ContextMaintainSignal{ctx.Rewrite(edit), ctx.Next()}
					cs.Pending = true
				}
			}
		}
		if cs != before {
			if err := st.Set(keyCompaction, cs); err != nil {
				return nil, err
			}
		}
		return sigs, nil
	}
}

// ResilientCallError 는 일시적 에러를 대기 후 재시도하고, overflow 를 턴당 한 번 압축으로 복구한다.
func ResilientCallError(d Deps) inject.CallErrorFn {
	d = d.filled()
	return func(ctx *inject.CallErrorCtx, ce contract.CallError) ([]inject.CallErrorSignal, error) {
		cfg, err := ParseConfig(ctx.Env().Config())
		if err != nil {
			return nil, err
		}
		st, fl := ctx.State(), ctx.Flow()
		switch ce.Class {
		case contract.ClassRateLimit, contract.ClassOverloaded, contract.ClassTransientNetwork:
			rs, _ := getJSON[retryState](st, keyRetry)
			if rs.Run != fl.RunID() || rs.Turn != fl.TurnIndex() || rs.Counts == nil {
				rs = retryState{Run: fl.RunID(), Turn: fl.TurnIndex(), Counts: map[contract.ErrorClass]int{}}
			}
			n := rs.Counts[ce.Class]
			if n >= cfg.Retry.MaxAttempts {
				return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
			}
			if err := d.Sleep(ctx.Context(), retryDelay(cfg.Retry, ce.RetryAfter, n)); err != nil {
				return []inject.CallErrorSignal{ctx.Fail(err)}, nil
			}
			rs.Counts[ce.Class] = n + 1
			if err := st.Set(keyRetry, rs); err != nil {
				return nil, err
			}
			return []inject.CallErrorSignal{ctx.Recall()}, nil
		case contract.ClassOverflow:
			if !canCompact(d, cfg) || recoveredThisTurn(st, fl) {
				return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
			}
			edit, ok, err := compactConv(ctx.Context(), d, cfg, ctx.Request().Model(), ctx.Conv())
			if err != nil || !ok {
				return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
			}
			if err := markRecovered(st, fl); err != nil {
				return nil, err
			}
			return []inject.CallErrorSignal{ctx.Rewrite(edit), ctx.Rebuild()}, nil
		}
		return []inject.CallErrorSignal{ctx.Fail(&ce)}, nil
	}
}

// retryDelay 는 RetryAfter(상한 maxDelay) 또는 min(baseDelay × 2^n, maxDelay) 다.
func retryDelay(r Retry, retryAfter time.Duration, n int) time.Duration {
	maxDelay := time.Duration(r.MaxDelayMs) * time.Millisecond
	if retryAfter > 0 {
		return min(retryAfter, maxDelay)
	}
	delay := time.Duration(r.BaseDelayMs) * time.Millisecond
	for i := 0; i < n && delay < maxDelay; i++ {
		delay *= 2
	}
	return min(delay, maxDelay)
}

// ResilientResponseClassify 는 회복 가능한 length 와 조용한 overflow 를 압축 후 rebuild 로 복구한다.
func ResilientResponseClassify(d Deps) inject.ResponseClassifyFn {
	d = d.filled()
	return func(ctx *inject.ResponseClassifyCtx) ([]inject.ResponseClassifySignal, error) {
		cfg, err := ParseConfig(ctx.Env().Config())
		if err != nil {
			return nil, err
		}
		resp := ctx.Output().Response()
		st, fl := ctx.State(), ctx.Flow()
		if !canCompact(d, cfg) || resp.StopReason == nil || resp.Usage == nil || recoveredThisTurn(st, fl) {
			return nil, nil
		}
		model, u := ctx.Request().Model(), *resp.Usage
		recoverable := false
		switch resp.StopReason.Kind {
		case contract.StopLength:
			recoverable = u.Output < ctx.Request().Current().Options.MaxOutputTokens
		case contract.StopStop:
			recoverable = model.ContextWindow > 0 && u.Input+u.CacheRead > model.ContextWindow
		}
		if !recoverable {
			return nil, nil
		}
		edit, ok, err := compactConv(ctx.Context(), d, cfg, model, ctx.Conv())
		if err != nil {
			return []inject.ResponseClassifySignal{ctx.Fail(err)}, nil
		}
		if !ok {
			return nil, nil
		}
		if err := markRecovered(st, fl); err != nil {
			return nil, err
		}
		return []inject.ResponseClassifySignal{ctx.Rewrite(edit), ctx.Rebuild()}, nil
	}
}

// ResilientRunBeforeSettle 은 입력함에 base 가 소비하지 않은 메시지가 있으면 턴을 이어간다.
func ResilientRunBeforeSettle(d Deps) inject.RunBeforeSettleFn {
	return func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
		consumed := 0
		if raw, ok := ctx.State().Read(baseNamespace, keyInboxConsumed); ok {
			json.Unmarshal(raw, &consumed)
		}
		if len(ctx.Conv().Inbox()) > consumed {
			return []inject.RunBeforeSettleSignal{ctx.ContinueTurn()}, nil
		}
		return nil, nil
	}
}
