package harness

import (
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// UsageTotals 는 요청 사용량의 합이다 (3.6g).
type UsageTotals struct {
	Requests    int
	Prompt      int
	CacheRead   int
	Ceiling     int
	CeilingRead int
}

func (t *UsageTotals) add(u contract.Usage, ceiling int, hasCeiling bool) {
	t.Requests++
	t.Prompt += u.Prompt()
	t.CacheRead += u.CacheRead
	if hasCeiling {
		t.Ceiling += ceiling
		t.CeilingRead += min(u.CacheRead, ceiling)
	}
}

// Stats 는 세션 하나의 사용량 집계다 (R30, 3.6g).
type Stats struct {
	Model     contract.ModelRef
	Context   int
	Window    int
	Threshold int
	LastRun   UsageTotals
	Session   UsageTotals
}

type sessionStats struct {
	Stats
	runID string
	last  map[modelKey]int // 모델별 직전 Prompt (HN-FR-81)
}

// statsTable 은 세션 → 집계다. Agent 메모리에만 둔다 (HN-FR-83).
type statsTable struct {
	mu         sync.Mutex
	sess       map[string]*sessionStats
	summarizer bool
}

func newStatsTable(summarizer bool) *statsTable {
	return &statsTable{sess: map[string]*sessionStats{}, summarizer: summarizer}
}

// record 는 요청 하나를 센다 (HN-FR-79~82).
func (t *statsTable) record(sid, runID string, model contract.ModelInfo, cfg sets.Config, u contract.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.sess[sid]
	if s == nil {
		s = &sessionStats{last: map[modelKey]int{}}
		t.sess[sid] = s
	}
	if s.runID != runID {
		s.runID, s.LastRun = runID, UsageTotals{}
	}
	key := modelKey{model.Ref.Provider, model.Ref.ID}
	ceiling, ok := s.last[key]
	s.LastRun.add(u, ceiling, ok)
	s.Session.add(u, ceiling, ok)
	s.last[key] = u.Prompt()
	s.Model, s.Context, s.Window, s.Threshold = model.Ref, u.Prompt()+u.Output, model.ContextWindow, 0
	if cfg.Compaction.Enabled && t.summarizer && model.ContextWindow > 0 {
		s.Threshold = model.ContextWindow - sets.ReserveTokens(cfg, model.ContextWindow)
	}
}

// Stats 는 그 세션의 집계 복사본이다. 센 요청이 없으면 false (HN-FR-83).
func (a *Agent) Stats(sessionID string) (Stats, bool) {
	a.sf.stats.mu.Lock()
	defer a.sf.stats.mu.Unlock()
	s := a.sf.stats.sess[sessionID]
	if s == nil {
		return Stats{}, false
	}
	return s.Stats, true
}
