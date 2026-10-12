package harness

import (
	"context"
	"sync"

	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// registry 는 등록 순서를 지키는 sets.ToolRegistry 다. 동시 사용에 안전하다 (HN-FR-84).
type registry struct {
	mu    sync.RWMutex
	tools map[string]sets.Tool
	order []string
}

func newRegistry() *registry { return &registry{tools: map[string]sets.Tool{}} }

// add 는 도구를 등록한다. 같은 이름이 있으면 그 자리에서 덮는다.
func (r *registry) add(t sets.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Spec.Name]; !ok {
		r.order = append(r.order, t.Spec.Name)
	}
	r.tools[t.Spec.Name] = t
}

func (r *registry) Get(name string) (sets.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// stateStoreOf 는 Store 를 inject 의 StateStore 로 감싼다 (HN-FR-84).
func stateStoreOf(s sets.Store) inject.StateStore { return stateStore{s} }

type stateStore struct{ s sets.Store }

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
