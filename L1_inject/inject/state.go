package inject

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// StateStore 는 함수 세트 상태의 저장·복원이다 (IN-FR-26, 51).
type StateStore interface {
	Load(ctx context.Context, sessionID string) (map[string]NamespaceState, error)
	Save(ctx context.Context, sessionID, ns string, d StateDelta) error
}

type NamespaceState struct {
	Values    map[string]json.RawMessage
	Published []string
}

type StateDelta struct {
	Set     map[string]json.RawMessage
	Delete  []string
	Publish []string
}

func (d StateDelta) empty() bool { return len(d.Set) == 0 && len(d.Delete) == 0 && len(d.Publish) == 0 }

type nsState struct {
	values    map[string]json.RawMessage
	published map[string]bool
}

// runState 는 run 하나의 커밋된 상태다.
type runState struct {
	mu sync.Mutex
	ns map[string]*nsState
}

func newRunState(loaded map[string]NamespaceState) *runState {
	r := &runState{ns: map[string]*nsState{}}
	for name, s := range loaded {
		n := r.get(name)
		for k, v := range s.Values {
			n.values[k] = clone(v)
		}
		for _, k := range s.Published {
			n.published[k] = true
		}
	}
	return r
}

// get 은 r.mu 를 잡은 상태에서 부른다.
func (r *runState) get(ns string) *nsState {
	n := r.ns[ns]
	if n == nil {
		n = &nsState{values: map[string]json.RawMessage{}, published: map[string]bool{}}
		r.ns[ns] = n
	}
	return n
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// stateCtx 는 dispatch 한 번의 상태 쓰기 버퍼다 (IN-FR-24).
type stateCtx struct {
	ns  string
	run *runState

	mu  sync.Mutex
	set map[string]json.RawMessage
	del map[string]bool
	pub map[string]bool
}

func newStateCtx(ns string, run *runState) *stateCtx {
	return &stateCtx{ns: ns, run: run, set: map[string]json.RawMessage{}, del: map[string]bool{}, pub: map[string]bool{}}
}

// State 는 함수가 바인딩된 세트의 네임스페이스다 (IN-FR-23).
type State struct{ b *base }

// Get 은 이 dispatch 의 쓰기를 먼저 보고, 없으면 커밋된 값을 본다.
func (q State) Get(key string) (json.RawMessage, bool) {
	q.b.check()
	s := q.b.st
	s.mu.Lock()
	if s.del[key] {
		s.mu.Unlock()
		return nil, false
	}
	if v, ok := s.set[key]; ok {
		s.mu.Unlock()
		return clone(v), true
	}
	s.mu.Unlock()
	s.run.mu.Lock()
	defer s.run.mu.Unlock()
	v, ok := s.run.get(s.ns).values[key]
	return clone(v), ok
}

// Set 은 v 를 JSON 으로 직렬화해 버퍼에 쓴다.
func (q State) Set(key string, v any) error {
	q.b.check()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s := q.b.st
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.del, key)
	s.set[key] = b
	return nil
}

func (q State) Delete(key string) {
	q.b.check()
	s := q.b.st
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.set, key)
	s.del[key] = true
}

// Publish 는 key 를 다른 세트가 읽을 수 있게 표시한다 (IN-FR-25).
func (q State) Publish(key string) {
	q.b.check()
	s := q.b.st
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pub[key] = true
}

// Read 는 ns 네임스페이스의 공개 키를 읽는다. 자기 네임스페이스면 Get 과 같다.
func (q State) Read(ns, key string) (json.RawMessage, bool) {
	q.b.check()
	s := q.b.st
	if ns == s.ns {
		return q.Get(key)
	}
	s.run.mu.Lock()
	defer s.run.mu.Unlock()
	n := s.run.ns[ns]
	if n == nil || !n.published[key] {
		return nil, false
	}
	v, ok := n.values[key]
	return clone(v), ok
}

// commit 은 버퍼를 run 상태에 반영하고 delta 와 바뀐 키를 돌려준다.
func (s *stateCtx) commit() (StateDelta, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := StateDelta{}
	var keys []string
	s.run.mu.Lock()
	n := s.run.get(s.ns)
	for k, v := range s.set {
		if d.Set == nil {
			d.Set = map[string]json.RawMessage{}
		}
		n.values[k] = v
		d.Set[k] = clone(v)
		keys = append(keys, k)
	}
	for k := range s.del {
		delete(n.values, k)
		d.Delete = append(d.Delete, k)
		keys = append(keys, k)
	}
	for k := range s.pub {
		n.published[k] = true
		d.Publish = append(d.Publish, k)
	}
	s.run.mu.Unlock()
	sort.Strings(keys)
	sort.Strings(d.Delete)
	sort.Strings(d.Publish)
	return d, keys
}
