// Package sets 는 22 step 의 기본 함수 세트(base)와 기본 정책 세트(resilient)를 제공한다.
// 하네스 기능(도구 구현, 세션 파일, 프롬프트, 요약기)은 interface 로만 받는다 (R13).
package sets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"iter"
	"sort"
	"sync"
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

// Registry 는 등록 순서를 유지하는 맵 레지스트리다. 동시 사용에 안전하다.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	order []string
}

func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.Add(t)
	}
	return r
}

// Add 는 도구를 등록한다. 같은 이름이 있으면 그 자리에서 덮는다 (ST-FR-10).
func (r *Registry) Add(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Spec.Name]; !ok {
		r.order = append(r.order, t.Spec.Name)
	}
	r.tools[t.Spec.Name] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
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

// MemoryStore 는 프로세스 메모리 Store 다 (ST-FR-39).
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]*memSession
}

type memSession struct {
	messages []contract.Message
	edits    []contract.ContextEdit
	state    map[string]*memNS
}

type memNS struct {
	values    map[string]json.RawMessage
	published map[string]bool
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{sessions: map[string]*memSession{}} }

// get 은 s.mu 를 잡은 상태에서 부른다.
func (s *MemoryStore) get(id string) *memSession {
	m := s.sessions[id]
	if m == nil {
		m = &memSession{state: map[string]*memNS{}}
		s.sessions[id] = m
	}
	return m
}

// Load 는 저장된 세션의 복사본을 돌려준다.
func (s *MemoryStore) Load(ctx context.Context, sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.get(sessionID)
	out := Session{
		Messages: append([]contract.Message(nil), m.messages...),
		Edits:    append([]contract.ContextEdit(nil), m.edits...),
		State:    map[string]inject.NamespaceState{},
	}
	for name, n := range m.state {
		ns := inject.NamespaceState{Values: map[string]json.RawMessage{}}
		for k, v := range n.values {
			ns.Values[k] = append(json.RawMessage(nil), v...)
		}
		for k := range n.published {
			ns.Published = append(ns.Published, k)
		}
		sort.Strings(ns.Published)
		out.State[name] = ns
	}
	return out, nil
}

func (s *MemoryStore) AppendMessages(ctx context.Context, sessionID string, msgs []contract.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.get(sessionID)
	m.messages = append(m.messages, msgs...)
	return nil
}

func (s *MemoryStore) AppendEdits(ctx context.Context, sessionID string, edits []contract.ContextEdit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.get(sessionID)
	m.edits = append(m.edits, edits...)
	return nil
}

func (s *MemoryStore) SaveState(ctx context.Context, sessionID, ns string, d inject.StateDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.get(sessionID)
	n := m.state[ns]
	if n == nil {
		n = &memNS{values: map[string]json.RawMessage{}, published: map[string]bool{}}
		m.state[ns] = n
	}
	for k, v := range d.Set {
		n.values[k] = append(json.RawMessage(nil), v...)
	}
	for _, k := range d.Delete {
		delete(n.values, k)
	}
	for _, k := range d.Publish {
		n.published[k] = true
	}
	return nil
}

// StateStoreOf 는 Store 를 inject 의 StateStore 로 감싼다 (ST-FR-15). nil 이면 nil.
func StateStoreOf(s Store) inject.StateStore {
	if s == nil {
		return nil
	}
	return stateStore{s}
}

type stateStore struct{ s Store }

func (x stateStore) Load(ctx context.Context, sessionID string) (map[string]inject.NamespaceState, error) {
	sess, err := x.s.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.State, nil
}

func (x stateStore) Save(ctx context.Context, sessionID, ns string, d inject.StateDelta) error {
	return x.s.SaveState(ctx, sessionID, ns, d)
}

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
}

// filled 는 nil 을 기본값으로 채운 복사본이다 (ST-FR-42).
func (d Deps) filled() Deps {
	if d.Tools == nil {
		d.Tools = NewRegistry()
	}
	if d.Store == nil {
		d.Store = NewMemoryStore()
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
