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
}

// Dispatcher 는 바인딩된 함수로 core.Dispatcher 를 구현한다. 불변이며 여러 Kernel 이 공유해도 된다 (IN-FR-20, 41).
type Dispatcher struct {
	fns   FunctionSet
	owner map[core.Step]string
	opts  BindOptions

	mu   sync.Mutex
	runs map[string]*runState
}

// Bind 는 세트를 순서대로 겹쳐 step 마다 nil 이 아닌 마지막 함수를 고른다 (IN-FR-19).
func Bind(sets ...FunctionSet) (*Dispatcher, error) {
	return BindWith(BindOptions{}, sets...)
}

func BindWith(opts BindOptions, sets ...FunctionSet) (*Dispatcher, error) {
	seen := map[string]bool{}
	d := &Dispatcher{owner: map[core.Step]string{}, opts: opts, runs: map[string]*runState{}}
	for _, s := range sets {
		if s.Name == "" {
			return nil, fmt.Errorf("%w: set without name", ErrBind)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("%w: duplicate set name %q", ErrBind, s.Name)
		}
		seen[s.Name] = true
		overlay(&d.fns, s, d.owner)
	}
	var missing []string
	for _, st := range core.Steps() {
		if d.owner[st] == "" {
			missing = append(missing, st.String())
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing steps: %s", ErrBind, strings.Join(missing, ", "))
	}
	return d, nil
}

// Binding 은 step → 세트 이름의 복사본이다.
func (d *Dispatcher) Binding() map[core.Step]string {
	out := make(map[core.Step]string, len(d.owner))
	for k, v := range d.owner {
		out[k] = v
	}
	return out
}

func (d *Dispatcher) Dispatch(ctx context.Context, step core.Step, input any, view core.View) (core.Result, error) {
	return d.DispatchSource(ctx, step, input, view)
}

// DispatchSource 는 dispatch 한 번이다 (IN-FR-40 의 [1]~[8]).
func (d *Dispatcher) DispatchSource(ctx context.Context, step core.Step, input any, view ViewSource) (core.Result, error) {
	began := time.Now()
	ns := d.owner[step] // [1]
	rec := TraceRecord{Seq: view.Seq(), Step: step, Set: ns}
	res, err := d.dispatch(ctx, step, input, view, ns, &rec)
	if step == core.StepRunSettled {
		d.mu.Lock()
		delete(d.runs, view.RunID())
		d.mu.Unlock()
	}
	rec.Duration, rec.Err = time.Since(began), err
	if d.opts.Tracer != nil { // [7]
		d.opts.Tracer.Trace(rec)
	}
	return res, err // [8]
}

func (d *Dispatcher) dispatch(ctx context.Context, step core.Step, input any, view ViewSource, ns string, rec *TraceRecord) (core.Result, error) {
	if ns == "" {
		return core.Result{}, fmt.Errorf("%w: step %s not bound", ErrBind, step)
	}
	run, err := d.runFor(ctx, step, view)
	if err != nil {
		return core.Result{}, err
	}
	inv := &invocation{} // [2]
	b := &base{inv: inv, ctx: ctx, view: view, d: d, st: newStateCtx(ns, run)}
	out, rs, err := d.safeInvoke(b, step, input) // [3]
	inv.expired.Store(true)                      // [4]
	b.mu.Lock()
	rec.Dropped = b.dropped
	b.mu.Unlock()
	if err != nil {
		return core.Result{}, err
	}
	sigs, err := verify(step, inv, rs) // [5]
	rec.Signals = sigs
	if err != nil {
		return core.Result{}, err
	}
	delta, keys := b.st.commit() // [6]
	rec.StateKeysChanged = keys
	if d.opts.StateStore != nil && !delta.empty() {
		if err := d.opts.StateStore.Save(ctx, view.SessionID(), ns, delta); err != nil {
			return core.Result{}, fmt.Errorf("inject: state save: %w", err)
		}
	}
	return core.Result{Output: out, Signals: sigs, Origin: ns}, nil
}

// runFor 는 run 의 상태를 찾고, run.start 에서는 StateStore 로 복원한다.
func (d *Dispatcher) runFor(ctx context.Context, step core.Step, view ViewSource) (*runState, error) {
	id := view.RunID()
	d.mu.Lock()
	r := d.runs[id]
	d.mu.Unlock()
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
	d.mu.Lock()
	d.runs[id] = r
	d.mu.Unlock()
	return r, nil
}

func (d *Dispatcher) safeInvoke(b *base, step core.Step, input any) (out any, rs []sig, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, rs, err = nil, nil, fmt.Errorf("%w: %s: %v", ErrPanic, step, p)
		}
	}()
	return invoke(&d.fns, b, step, input)
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
