package inject_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

type countSink struct {
	n     atomic.Int64
	block chan struct{}
	mu    sync.Mutex
	seqs  []int
}

func (s *countSink) Observe(ev core.ObserverEvent) {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	s.seqs = append(s.seqs, ev.Seq)
	s.mu.Unlock()
	s.n.Add(1)
}

// IN-FR-43: 느린 sink 는 루프를 막지 않고 버린 수를 센다. 빠른 sink 는 모두 받는다.
func TestFanoutSlowSink(t *testing.T) {
	slow := &countSink{block: make(chan struct{})}
	fast := &countSink{}
	f := inject.NewFanout(4, inject.Sink{Name: "slow", Observer: slow}, inject.Sink{Name: "fast", Observer: fast})
	start := time.Now()
	for i := range 100 {
		f.Observe(core.ObserverEvent{Kind: core.ObsStepStarted, Seq: i})
	}
	if time.Since(start) > time.Second {
		t.Fatal("Observe blocked")
	}
	close(slow.block)
	f.Close()
	if got := slow.n.Load() + f.Dropped("slow"); got != 100 {
		t.Fatalf("slow delivered+dropped = %d", got)
	}
	if f.Dropped("slow") == 0 {
		t.Fatal("slow sink dropped nothing")
	}
	if fast.n.Load()+f.Dropped("fast") != 100 {
		t.Fatal("fast accounting")
	}
	// 순서는 보존된다.
	for i := 1; i < len(fast.seqs); i++ {
		if fast.seqs[i] <= fast.seqs[i-1] {
			t.Fatal("order not preserved")
		}
	}
	if f.Dropped("nobody") != 0 {
		t.Fatal("unknown sink")
	}
}

func TestFanoutCloseDrainsAndStops(t *testing.T) {
	s := &countSink{}
	f := inject.NewFanout(0, inject.Sink{Name: "s", Observer: s})
	for range 10 {
		f.Observe(core.ObserverEvent{})
	}
	f.Close()
	if s.n.Load() != 10 {
		t.Fatalf("drained %d", s.n.Load())
	}
	f.Observe(core.ObserverEvent{}) // Close 뒤 Observe 는 버린다 (panic 없음)
	f.Close()
	if f.Dropped("s") != 1 {
		t.Fatalf("dropped after close %d", f.Dropped("s"))
	}
}

func TestFanoutAsCoreObserver(t *testing.T) {
	s := &countSink{}
	f := inject.NewFanout(1024, inject.Sink{Name: "s", Observer: s})
	var _ core.Observer = f
	d := mustBind(t, workingSet("w"))
	core.New(d, core.Options{Observer: f}).Start(context.Background(), core.RunInput{}).Wait()
	f.Close()
	if s.n.Load() == 0 {
		t.Fatal("no events")
	}
}
