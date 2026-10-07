package inject

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// Tracer 는 dispatch 마다 기록을 받는다 (IN-FR-42).
type Tracer interface {
	Trace(rec TraceRecord)
}

type TraceRecord struct {
	Seq              int
	Step             core.Step
	Set              string
	Duration         time.Duration
	Signals          []core.Signal
	Dropped          []core.Signal
	Err              error
	StateKeysChanged []string
}

type BindOptions struct {
	StateStore StateStore
	Tracer     Tracer
	Runs       *RunTable // nil → Dispatcher 전용 표 (IN-FR-66)
}

// RunTable 은 RunID → run 상태 표다. 같은 표로 Bind 한 Dispatcher 들은 run 상태를 공유한다 (IN-FR-64).
type RunTable struct {
	mu   sync.Mutex
	runs map[string]*runState
}

func NewRunTable() *RunTable { return &RunTable{runs: map[string]*runState{}} }

func (t *RunTable) get(id string) *runState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.runs[id]
}

func (t *RunTable) put(id string, r *runState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runs[id] = r
}

func (t *RunTable) drop(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.runs, id)
}

// Dispatcher 는 바인딩된 함수로 core.Dispatcher 를 구현한다. 불변이며 여러 Kernel 이 공유해도 된다 (IN-FR-20, 41).
type Dispatcher struct {
	sets   []FunctionSet
	stacks map[core.Step][]int // step → 그 step 을 채운 sets 의 위치, 아래 층부터
	opts   BindOptions
	runs   *RunTable
}

// Bind 는 step 마다 그 step 을 채운 세트들을 순서대로 쌓는다. 뒤의 세트가 위 층이다 (IN-FR-19).
func Bind(sets ...FunctionSet) (*Dispatcher, error) {
	return BindWith(BindOptions{}, sets...)
}

func BindWith(opts BindOptions, sets ...FunctionSet) (*Dispatcher, error) {
	seen := map[string]bool{}
	d := &Dispatcher{sets: append([]FunctionSet(nil), sets...), stacks: map[core.Step][]int{}, opts: opts, runs: opts.Runs}
	if d.runs == nil {
		d.runs = NewRunTable()
	}
	for i, s := range d.sets {
		if s.Name == "" {
			return nil, fmt.Errorf("%w: set without name", ErrBind)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("%w: duplicate set name %q", ErrBind, s.Name)
		}
		seen[s.Name] = true
		for _, st := range filled(&s) {
			d.stacks[st] = append(d.stacks[st], i)
		}
	}
	var missing []string
	for _, st := range core.Steps() {
		if len(d.stacks[st]) == 0 {
			missing = append(missing, st.String())
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing steps: %s", ErrBind, strings.Join(missing, ", "))
	}
	return d, nil
}

// Binding 은 step → 가장 위 층 세트 이름의 복사본이다 (IN-FR-21).
func (d *Dispatcher) Binding() map[core.Step]string {
	out := make(map[core.Step]string, len(d.stacks))
	for st := range d.stacks {
		out[st] = d.top(st).Name
	}
	return out
}

// top 은 step 의 가장 위 층 세트다. 바인딩되지 않은 step 이면 nil.
func (d *Dispatcher) top(st core.Step) *FunctionSet {
	stack := d.stacks[st]
	if len(stack) == 0 {
		return nil
	}
	return &d.sets[stack[len(stack)-1]]
}

func (d *Dispatcher) Dispatch(ctx context.Context, step core.Step, input any, view core.View) (core.Result, error) {
	return d.DispatchSource(ctx, step, input, view)
}

// DispatchSource 는 dispatch 한 번이다 (IN-FR-40 의 [1]~[8]).
func (d *Dispatcher) DispatchSource(ctx context.Context, step core.Step, input any, view ViewSource) (core.Result, error) {
	began := time.Now()
	fs := d.top(step) // [1]
	rec := TraceRecord{Seq: view.Seq(), Step: step}
	if fs != nil {
		rec.Set = fs.Name
	}
	res, err := d.dispatch(ctx, step, input, view, fs, &rec)
	if step == core.StepRunSettled {
		d.runs.drop(view.RunID())
	}
	rec.Duration, rec.Err = time.Since(began), err
	if d.opts.Tracer != nil { // [7]
		d.opts.Tracer.Trace(rec)
	}
	return res, err // [8]
}

func (d *Dispatcher) dispatch(ctx context.Context, step core.Step, input any, view ViewSource, fs *FunctionSet, rec *TraceRecord) (core.Result, error) {
	if fs == nil {
		return core.Result{}, fmt.Errorf("%w: step %s not bound", ErrBind, step)
	}
	run, err := d.runFor(ctx, step, view)
	if err != nil {
		return core.Result{}, err
	}
	inv := &invocation{} // [2]
	c := &call{run: run}
	b := &base{inv: inv, ctx: ctx, view: view, d: d, st: c.state(fs.Name), step: step, layer: len(d.stacks[step]) - 1, call: c}
	out, rs, err := d.safeInvoke(fs, b, step, input) // [3]
	inv.expired.Store(true)                          // [4]
	c.mu.Lock()
	rec.Dropped = c.dropped
	c.mu.Unlock()
	if err != nil {
		return core.Result{}, err
	}
	sigs, err := verify(step, inv, rs) // [5]
	rec.Signals = sigs
	if err != nil {
		return core.Result{}, err
	}
	for _, st := range c.states { // [6] 층마다 자기 네임스페이스 (IN-FR-61)
		delta, keys := st.commit()
		rec.StateKeysChanged = append(rec.StateKeysChanged, keys...)
		if d.opts.StateStore != nil && !delta.empty() {
			if err := d.opts.StateStore.Save(ctx, view.SessionID(), st.ns, delta); err != nil {
				return core.Result{}, fmt.Errorf("inject: state save: %w", err)
			}
		}
	}
	return core.Result{Output: out, Signals: sigs, Origin: fs.Name}, nil
}

// runFor 는 run 의 상태를 찾고, run.start 에서는 StateStore 로 복원한다.
func (d *Dispatcher) runFor(ctx context.Context, step core.Step, view ViewSource) (*runState, error) {
	id := view.RunID()
	r := d.runs.get(id)
	if r != nil && step != core.StepRunStart {
		return r, nil
	}
	var loaded map[string]NamespaceState
	if step == core.StepRunStart && d.opts.StateStore != nil {
		var err error
		if loaded, err = d.opts.StateStore.Load(ctx, view.SessionID()); err != nil {
			return nil, fmt.Errorf("inject: state load: %w", err)
		}
	}
	r = newRunState(loaded)
	d.runs.put(id, r)
	return r, nil
}

func (d *Dispatcher) safeInvoke(fs *FunctionSet, b *base, step core.Step, input any) (out any, rs []sig, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, rs, err = nil, nil, fmt.Errorf("%w: %s: %v", ErrPanic, step, p)
		}
	}()
	return invoke(fs, b, step, input)
}

// verify 는 토큰, 허용 표, 배열 규칙을 검사한다 (IN-FR-12, 40).
func verify(step core.Step, inv *invocation, rs []sig) ([]core.Signal, error) {
	out := make([]core.Signal, len(rs))
	transitions := 0
	for i, r := range rs {
		if r.inv != inv {
			return nil, fmt.Errorf("%w: %s at %d", ErrForeignSignal, r.s.Kind, i)
		}
		if !core.Allowed(step, r.s.Kind) {
			return nil, fmt.Errorf("%w: %s not allowed at %s", ErrSignalOrder, r.s.Kind, step)
		}
		if r.s.Kind.IsTransition() {
			transitions++
			if i != len(rs)-1 {
				return nil, fmt.Errorf("%w: transition %s must be last", ErrSignalOrder, r.s.Kind)
			}
		}
		out[i] = r.s
	}
	if transitions > 1 {
		return nil, fmt.Errorf("%w: more than one transition", ErrSignalOrder)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
