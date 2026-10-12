// Package sets 는 22 step 의 기본 함수 세트(base)와 기본 정책 세트(resilient)를 제공한다.
// 세트는 자기 루프의 제어만 갖고, 하네스 기능(도구·저장소 구현, 프롬프트, 요약기, 보조 모델)은 interface 로만 받는다 (R13, R39).
package sets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"iter"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// ModelCaller 는 모델 호출과 모델 정보 조회다. adapters.Client 가 그대로 만족한다 (ST-FR-32).
type ModelCaller interface {
	Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error]
	Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool)
}

// Tool 은 도구 하나다. Truncate 가 nil 이면 18 의 기본 절단 규칙을 쓴다 (ST-FR-33).
type Tool struct {
	Spec     contract.ToolSpec
	Run      func(ctx context.Context, call contract.ToolCall) contract.ToolResult
	Truncate func(contract.ToolResult) contract.ToolResult
}

type ToolRegistry interface {
	Get(name string) (Tool, bool)
	Names() []string
}

// Session 은 Store 가 보관한 세션 하나다.
type Session struct {
	Messages []contract.Message
	Edits    []contract.ContextEdit
	State    map[string]inject.NamespaceState
}

// Store 는 세션 저장소다. 메시지·편집은 append-only 다 (ST-FR-34).
type Store interface {
	Load(ctx context.Context, sessionID string) (Session, error)
	AppendMessages(ctx context.Context, sessionID string, msgs []contract.Message) error
	AppendEdits(ctx context.Context, sessionID string, edits []contract.ContextEdit) error
	SaveState(ctx context.Context, sessionID, ns string, d inject.StateDelta) error
}

// noTools 는 nil Tools 의 "없음" 구현이다 (ST-FR-33).
type noTools struct{}

func (noTools) Get(string) (Tool, bool) { return Tool{}, false }
func (noTools) Names() []string         { return nil }

// noStore 는 nil Store 의 "없음" 구현이다: 빈 세션을 읽고 쓰기는 버린다 (ST-FR-34).
type noStore struct{}

func (noStore) Load(context.Context, string) (Session, error) { return Session{}, nil }
func (noStore) AppendMessages(context.Context, string, []contract.Message) error {
	return nil
}
func (noStore) AppendEdits(context.Context, string, []contract.ContextEdit) error {
	return nil
}
func (noStore) SaveState(context.Context, string, string, inject.StateDelta) error { return nil }

// SummaryRequest 는 압축 구간의 요약 요청이다. Previous 는 이전 압축의 요약 텍스트다 (ST-FR-24).
type SummaryRequest struct {
	Model     contract.ModelRef
	Messages  []contract.Message
	Previous  string
	MaxTokens int
}

// Summarizer 는 요약기다. 요약 프롬프트는 구현(L2_harness)의 몫이다 (ST-FR-36).
type Summarizer interface {
	Summarize(ctx context.Context, r SummaryRequest) (string, error)
}

// Deps 는 세트가 받는 의존이다. Model 외의 nil 은 기본값을 쓴다 (ST-FR-32~37).
type Deps struct {
	Model      ModelCaller
	Tools      ToolRegistry
	Store      Store
	System     []contract.Block
	Summarizer Summarizer
	Sleep      func(ctx context.Context, d time.Duration) error
	Now        func() time.Time
	NewID      func() string
	Modes      ModeSwitcher // nil → 모드 없음 (ST-FR-57, ModesOf)
	Aux        AuxCaller    // nil → 보조 모델 없음 (ST-FR-63, AuxOf)
}

// filled 는 nil 을 기본값으로 채운 복사본이다 (ST-FR-42).
func (d Deps) filled() Deps {
	if d.Tools == nil {
		d.Tools = noTools{}
	}
	if d.Store == nil {
		d.Store = noStore{}
	}
	if d.Sleep == nil {
		d.Sleep = sleep
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = randomID
	}
	d.Modes = ModesOf(d)
	d.Aux = AuxOf(d)
	return d
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
