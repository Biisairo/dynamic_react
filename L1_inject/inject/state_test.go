package inject_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
)

type memStore struct {
	mu     sync.Mutex
	loaded map[string]inject.NamespaceState
	saves  []string // "ns:set=..;del=..;pub=.."
	log    *[]string
	err    error
}

func (m *memStore) Load(ctx context.Context, sid string) (map[string]inject.NamespaceState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.log != nil {
		*m.log = append(*m.log, "load")
	}
	return m.loaded, m.err
}

func (m *memStore) Save(ctx context.Context, sid, ns string, d inject.StateDelta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := ""
	for k, v := range d.Set {
		keys += k + "=" + string(v)
	}
	m.saves = append(m.saves, fmt.Sprintf("%s:%s del%v pub%v", ns, keys, d.Delete, d.Publish))
	if m.log != nil {
		*m.log = append(*m.log, "save")
	}
	return nil
}

type tracer struct {
	mu   sync.Mutex
	recs []inject.TraceRecord
	log  *[]string
}

func (tr *tracer) Trace(r inject.TraceRecord) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.recs = append(tr.recs, r)
	if tr.log != nil {
		*tr.log = append(*tr.log, "trace")
	}
}

// 두 세트가 같은 run 에서 상태를 쓰고 읽는다: 네임스페이스 격리, publish, 버퍼 (IN-FR-23~25).
func TestStateNamespaces(t *testing.T) {
	var seen []string
	a := workingSet("a")
	a.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		st := ctx.State()
		st.Set("count", 1)
		st.Set("secret", "x")
		st.Publish("count")
		v, _ := st.Get("count") // 같은 dispatch 의 쓰기가 보인다
		seen = append(seen, "a.get="+string(v))
		return []contract.Message{userMsg("u1", "hi")}, nil, nil
	}
	b := inject.FunctionSet{Name: "b",
		ContextMaintain: func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
			st := ctx.State()
			_, own := st.Get("count")
			pub, okPub := st.Read("a", "count")
			_, okSecret := st.Read("a", "secret")
			seen = append(seen, fmt.Sprintf("b.own=%v pub=%s/%v secret=%v", own, pub, okPub, okSecret))
			return nil, nil
		},
	}
	runCore(t, mustBind(t, a, b))
	want := []string{"a.get=1", "b.own=false pub=1/true secret=false"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("seen %v", seen)
	}
}

func TestStateDeleteAndOverwrite(t *testing.T) {
	var got []string
	set := workingSet("a")
	set.TurnStart = func(ctx *inject.TurnStartCtx) ([]contract.Message, []inject.TurnStartSignal, error) {
		ctx.State().Set("k", "v1")
		ctx.State().Set("gone", 1)
		return []contract.Message{userMsg("u1", "hi")}, nil, nil
	}
	set.ContextMaintain = func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
		ctx.State().Delete("gone")
		ctx.State().Set("k", "v2")
		_, ok := ctx.State().Get("gone")
		got = append(got, fmt.Sprint(ok))
		return nil, nil
	}
	set.ContextInject = func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
		v, _ := ctx.State().Get("k")
		_, ok := ctx.State().Get("gone")
		got = append(got, string(v), fmt.Sprint(ok))
		return nil, nil, nil
	}
	runCore(t, mustBind(t, set))
	if fmt.Sprint(got) != `[false "v2" false]` {
		t.Fatalf("got %v", got)
	}
}

func TestStateSetRejectsUnserializable(t *testing.T) {
	set := workingSet("a")
	var err error
	set.ContextMaintain = func(ctx *inject.ContextMaintainCtx) ([]inject.ContextMaintainSignal, error) {
		err = ctx.State().Set("ch", make(chan int))
		return nil, nil
	}
	runCore(t, mustBind(t, set))
	if err == nil {
		t.Fatal("chan accepted")
	}
}

// IN-FR-24: 실패한 dispatch 의 쓰기는 반영되지 않는다.
func TestStateNotCommittedOnFailure(t *testing.T) {
	cases := map[string]func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error){
		"error": func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			ctx.State().Set("k", 1)
			return nil, errors.New("x")
		},
		"panic": func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			ctx.State().Set("k", 1)
			panic("x")
		},
		"signal rule": func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			ctx.State().Set("k", 1)
			return []inject.TurnFinishSignal{ctx.Stop(), ctx.Settle()}, nil
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			st := &memStore{}
			var after bool
			set := injecttest.Fill(inject.FunctionSet{Name: "a", TurnFinish: fn,
				RunBeforeSettle: func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
					_, after = ctx.State().Get("k")
					return nil, nil
				}})
			d, _ := inject.BindWith(inject.BindOptions{StateStore: st}, set)
			v := facts(core.StepTurnFinish)
			d.DispatchSource(context.Background(), core.StepTurnFinish, nil, v.View())
			v.Step = core.StepRunBeforeSettle
			d.DispatchSource(context.Background(), core.StepRunBeforeSettle, nil, v.View())
			if after || len(st.saves) != 0 {
				t.Fatalf("committed: after=%v saves=%v", after, st.saves)
			}
		})
	}
}

// IN-FR-26, 51: Load 는 run.start 에서 함수 전에, Save 는 반영 직후, 트레이스는 그 뒤 (IN-FR-40).
func TestStateStoreAndOrder(t *testing.T) {
	var log []string
	st := &memStore{log: &log, loaded: map[string]inject.NamespaceState{
		"a":     {Values: map[string]json.RawMessage{"restored": json.RawMessage(`"yes"`)}},
		"other": {Values: map[string]json.RawMessage{"p": json.RawMessage(`7`)}, Published: []string{"p"}},
	}}
	tr := &tracer{log: &log}
	var restored, other string
	set := workingSet("a")
	set.RunStart = func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
		log = append(log, "fn")
		v, _ := ctx.State().Get("restored")
		p, _ := ctx.State().Read("other", "p")
		restored, other = string(v), string(p)
		ctx.State().Set("n", 1)
		ctx.State().Publish("n")
		return core.RunInit{}, nil, nil
	}
	d, err := inject.BindWith(inject.BindOptions{StateStore: st, Tracer: tr}, set)
	if err != nil {
		t.Fatal(err)
	}
	runCore(t, d)
	if restored != `"yes"` || other != "7" {
		t.Fatalf("restored %s other %s", restored, other)
	}
	if fmt.Sprint(log[:4]) != "[load fn save trace]" {
		t.Fatalf("order %v", log[:4])
	}
	if len(st.saves) != 1 || st.saves[0] != "a:n=1 del[] pub[n]" {
		t.Fatalf("saves %v", st.saves)
	}
	loads := 0
	for _, l := range log {
		if l == "load" {
			loads++
		}
	}
	if loads != 1 {
		t.Fatalf("loads %d", loads)
	}
	if tr.recs[0].StateKeysChanged[0] != "n" {
		t.Fatalf("trace keys %v", tr.recs[0].StateKeysChanged)
	}
}

func TestStateLoadError(t *testing.T) {
	d, _ := inject.BindWith(inject.BindOptions{StateStore: &memStore{err: errors.New("disk")}}, workingSet("a"))
	if s := runCore(t, d); s.Reason != core.SettleFailed {
		t.Fatalf("reason %s", s.Reason)
	}
}

// 동시 run 의 상태는 섞이지 않는다.
func TestStatePerRun(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	set := workingSet("a")
	set.RunStart = func(ctx *inject.RunStartCtx, in core.RunInput) (core.RunInit, []inject.RunStartSignal, error) {
		ctx.State().Set("sid", in.SessionID)
		return core.RunInit{}, nil, nil
	}
	set.RunBeforeSettle = func(ctx *inject.RunBeforeSettleCtx) ([]inject.RunBeforeSettleSignal, error) {
		v, _ := ctx.State().Get("sid")
		mu.Lock()
		got[ctx.Flow().SessionID()] = string(v)
		mu.Unlock()
		return nil, nil
	}
	k := core.New(mustBind(t, set), core.Options{})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k.Start(context.Background(), core.RunInput{SessionID: fmt.Sprint(i)}).Wait()
		}(i)
	}
	wg.Wait()
	for i := range 20 {
		if got[fmt.Sprint(i)] != fmt.Sprintf(`"%d"`, i) {
			t.Fatalf("run %d saw %s", i, got[fmt.Sprint(i)])
		}
	}
}

// IN-FR-42, 49
func TestTraceRecord(t *testing.T) {
	tr := &tracer{}
	set := injecttest.Fill(inject.FunctionSet{Name: "a", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		ctx.Dropped(ctx.Stop().WithReason("loser"))
		return []inject.TurnFinishSignal{ctx.Settle()}, nil
	}})
	d, _ := inject.BindWith(inject.BindOptions{Tracer: tr}, set)
	f := facts(core.StepTurnFinish)
	f.Seq = 42
	d.DispatchSource(context.Background(), core.StepTurnFinish, nil, f.View())
	r := tr.recs[0]
	if r.Seq != 42 || r.Step != core.StepTurnFinish || r.Set != "a" || r.Duration <= 0 || r.Err != nil ||
		len(r.Signals) != 1 || r.Signals[0].Kind != core.SigSettle ||
		len(r.Dropped) != 1 || r.Dropped[0].Reason != "loser" {
		t.Fatalf("record %+v", r)
	}
	d.DispatchSource(context.Background(), core.StepToolExecute, contract.ToolCall{}, f.View())
	if tr.recs[1].Err == nil {
		t.Fatal("error not traced")
	}
}
