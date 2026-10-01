package harness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
)

// Compact 는 run 밖에서 세션 하나를 즉시 압축한다 (HN-FR-36).
// 같은 세션의 run 과 겹치지 않게 하는 것은 호출자의 몫이다.
func (a *Agent) Compact(ctx context.Context, sessionID string, config json.RawMessage) (bool, error) {
	cfg, err := sets.ParseConfig(config)
	if err != nil {
		return false, err
	}
	if err := session.CheckID(sessionID); err != nil {
		return false, err
	}
	if a.sum == nil {
		return false, errors.New("harness: compaction disabled: no summarizer")
	}
	sess, err := a.store.Load(ctx, sessionID)
	if err != nil {
		return false, err
	}
	proj := sets.ApplyEdits(sess.Messages, sess.Edits)
	info, ok := a.model.Lookup(ctx, cfg.Model.Provider, cfg.Model.ID)
	if !ok {
		info.Ref.Provider, info.Ref.ID = cfg.Model.Provider, cfg.Model.ID
	}
	s := manualSummarizer{a.sf, a.sum, sessionID}
	edit, ok, err := sets.Compact(ctx, s, info.Ref, proj, cfg.Compaction.KeepRecentTokens,
		sets.ReserveTokens(cfg, info.ContextWindow), info.MaxOutputTokens, a.now(), a.newID(), a.newID())
	if err != nil || !ok {
		return false, err
	}
	if err := a.store.AppendEdits(ctx, sessionID, []contract.ContextEdit{edit}); err != nil {
		return false, err
	}
	return true, nil
}

// manualSummarizer 는 run 이 없는 수동 압축에서 compaction 이벤트를 낸다.
type manualSummarizer struct {
	s     *surface
	inner sets.Summarizer
	sid   string
}

func (m manualSummarizer) Summarize(ctx context.Context, r sets.SummaryRequest) (string, error) {
	if err := m.emit(Event{Kind: EventCompaction, SessionID: m.sid, Phase: "start"}); err != nil {
		return "", err
	}
	text, err := m.inner.Summarize(ctx, r)
	if perr := m.emit(Event{Kind: EventCompaction, SessionID: m.sid, Phase: "end", Err: err}); perr != nil {
		return "", perr
	}
	return text, err
}

// emit 은 콜백 panic 을 ErrPanic 에러로 바꾼다 (run 밖이라 dispatch 의 recover 가 없다).
func (m manualSummarizer) emit(ev Event) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: %v", core.ErrPanic, p)
		}
	}()
	m.s.emit(ev)
	return nil
}

func randomID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
