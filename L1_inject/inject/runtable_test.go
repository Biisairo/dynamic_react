package inject_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

// stateProbe 는 run.start 에서 "k" 를 쓰고 turn.start 에서 읽은 값을 seen 에 남기는 세트다.
func stateProbe(seen *string) inject.FunctionSet {
	set := workingSet("a")
	set.RunStart = func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
		return core.RunInit{}, nil, ctx.State().Set("k", "v")
	}
	set.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		v, ok := ctx.State().Get("k")
		*seen = "<none>"
		if ok {
			*seen = string(v)
		}
		return nil, nil, nil
	}
	return set
}

func dispatchAt(t *testing.T, d *inject.Dispatcher, step core.Step, input any) {
	t.Helper()
	v := injecttest.Facts{RunID: "r1", SessionID: "s1", Step: step}.View()
	if _, err := d.DispatchSource(context.Background(), step, input, v); err != nil {
		t.Fatalf("%s: %v", step, err)
	}
}

// IN-FR-64, 65
func TestRunTableShared(t *testing.T) {
	var seenA, seenB string
	runs := inject.NewRunTable()
	a, err := inject.BindWith(inject.BindOptions{Runs: runs}, stateProbe(&seenA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := inject.BindWith(inject.BindOptions{Runs: runs}, stateProbe(&seenB))
	if err != nil {
		t.Fatal(err)
	}
	dispatchAt(t, a, core.StepRunStart, core.RunInput{SessionID: "s1"})
	dispatchAt(t, b, core.StepTurnStart, nil)
	if seenB != `"v"` {
		t.Fatalf("B after A run.start: got %s, want \"v\"", seenB)
	}
	dispatchAt(t, b, core.StepRunSettled, core.Settle{Reason: core.SettleCompleted})
	dispatchAt(t, a, core.StepTurnStart, nil)
	if seenA != "<none>" {
		t.Fatalf("A after B run.settled: got %s, want empty state", seenA)
	}
}

// IN-FR-66
func TestRunTableNilIsPrivate(t *testing.T) {
	var seenA, seenB string
	a, err := inject.BindWith(inject.BindOptions{}, stateProbe(&seenA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := inject.BindWith(inject.BindOptions{}, stateProbe(&seenB))
	if err != nil {
		t.Fatal(err)
	}
	dispatchAt(t, a, core.StepRunStart, core.RunInput{SessionID: "s1"})
	dispatchAt(t, b, core.StepTurnStart, nil)
	if seenB != "<none>" {
		t.Fatalf("private tables: B got %s", seenB)
	}
	dispatchAt(t, a, core.StepTurnStart, nil)
	if seenA != `"v"` {
		t.Fatalf("A own table: got %s", seenA)
	}
}

// IN-FR-64, 66: 같은 표를 쓰는 두 Dispatcher 로 동시 run 을 번갈아 보낸다.
func TestRunTableConcurrent(t *testing.T) {
	runs := inject.NewRunTable()
	var mu sync.Mutex
	got := map[string]string{}
	mk := func() *inject.Dispatcher {
		set := workingSet("a")
		set.RunStart = func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
			return core.RunInit{}, nil, ctx.State().Set("sid", in.SessionID)
		}
		set.RunBeforeSettle = func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
			v, _ := ctx.State().Get("sid")
			mu.Lock()
			got[ctx.Flow().SessionID()] = string(v)
			mu.Unlock()
			return nil, nil
		}
		d, err := inject.BindWith(inject.BindOptions{Runs: runs}, set)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b := mk(), mk()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sw := alternating{a: a, b: b}
			core.New(&sw, core.Options{}).Start(context.Background(), core.RunInput{SessionID: fmt.Sprint(i)}).Wait()
		}(i)
	}
	wg.Wait()
	for i := range 20 {
		if got[fmt.Sprint(i)] != fmt.Sprintf(`"%d"`, i) {
			t.Fatalf("run %d saw %s", i, got[fmt.Sprint(i)])
		}
	}
}

// alternating 은 dispatch 마다 a·b 를 번갈아 쓰는 Dispatcher 다.
type alternating struct {
	mu   sync.Mutex
	n    int
	a, b *inject.Dispatcher
}

func (s *alternating) Dispatch(ctx context.Context, step core.Step, input any, view core.View) (core.Result, error) {
	s.mu.Lock()
	d := s.a
	if s.n%2 == 1 {
		d = s.b
	}
	s.n++
	s.mu.Unlock()
	return d.Dispatch(ctx, step, input, view)
}
