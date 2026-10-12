// Package storetest 는 테스트용 메모리 저장소다 (HN-FR-04a, R39).
package storetest

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// MemoryStore 는 프로세스 메모리 sets.Store 다 (HN-FR-04a).
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

// New 는 빈 MemoryStore 다.
func New() *MemoryStore { return &MemoryStore{sessions: map[string]*memSession{}} }

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
func (s *MemoryStore) Load(ctx context.Context, sessionID string) (sets.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.get(sessionID)
	out := sets.Session{
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
