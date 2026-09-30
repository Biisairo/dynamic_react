package inject

import (
	"sync"
	"sync/atomic"

	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// DefaultFanoutBuffer 는 NewFanout 의 buffer 가 0 이하일 때 쓰는 sink 별 버퍼 크기다.
const DefaultFanoutBuffer = 256

type Sink struct {
	Name     string
	Observer core.Observer
}

// Fanout 은 core.Observer 를 구현해 여러 sink 에 비동기로 나눠 준다 (IN-FR-43).
// sink 별 버퍼가 차면 그 sink 의 이벤트를 버리고 센다. 루프를 막지 않는다.
type Fanout struct {
	mu     sync.RWMutex
	closed bool
	qs     []*sinkQueue
	wg     sync.WaitGroup
}

type sinkQueue struct {
	name    string
	ch      chan core.ObserverEvent
	dropped atomic.Int64
}

func NewFanout(buffer int, sinks ...Sink) *Fanout {
	if buffer <= 0 {
		buffer = DefaultFanoutBuffer
	}
	f := &Fanout{}
	for _, s := range sinks {
		q := &sinkQueue{name: s.Name, ch: make(chan core.ObserverEvent, buffer)}
		f.qs = append(f.qs, q)
		f.wg.Add(1)
		go func(obs core.Observer) {
			defer f.wg.Done()
			for ev := range q.ch {
				obs.Observe(ev)
			}
		}(s.Observer)
	}
	return f
}

func (f *Fanout) Observe(ev core.ObserverEvent) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, q := range f.qs {
		if f.closed {
			q.dropped.Add(1)
			continue
		}
		select {
		case q.ch <- ev:
		default:
			q.dropped.Add(1)
		}
	}
}

// Dropped 는 이름이 name 인 sink 에서 버린 이벤트 수다.
func (f *Fanout) Dropped(name string) int64 {
	var n int64
	for _, q := range f.qs {
		if q.name == name {
			n += q.dropped.Load()
		}
	}
	return n
}

// Close 는 새 이벤트를 받지 않고, 버퍼에 남은 이벤트를 모두 전달한 뒤 돌아온다. 여러 번 불러도 된다.
func (f *Fanout) Close() {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		for _, q := range f.qs {
			close(q.ch)
		}
	}
	f.mu.Unlock()
	f.wg.Wait()
}
