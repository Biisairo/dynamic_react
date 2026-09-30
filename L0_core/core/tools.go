package core

import (
	"context"
	"slices"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

type specKey struct{ id, digest string }

// handle 은 선행 실행 하나다 (CORE-FR-25). call·result·end 는 done 이 닫힌 뒤에만 읽는다.
type handle struct {
	key    specKey
	cancel context.CancelFunc
	done   chan struct{}
	call   contract.ToolCall
	result contract.ToolResult
	end    *Settle
}

func (h *handle) isDone() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

func (r *runner) startSpeculative(c contract.ToolCall) string {
	key := specKey{c.ID, c.ArgsDigest()}
	r.mu.Lock()
	if _, ok := r.spec[key]; ok {
		r.mu.Unlock()
		return ResultDuplicate
	}
	ctx, cancel := context.WithCancel(r.ctx)
	h := &handle{key: key, cancel: cancel, done: make(chan struct{})}
	r.spec[key] = h
	r.specWG.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.specWG.Done()
		defer close(h.done)
		h.call, h.result, h.end = r.beforeExecute(ctx, c)
	}()
	return ResultApplied
}

// cancelSpeculative 는 모든 핸들을 취소하고 끝나기를 기다린다 (CORE-FR-27).
func (r *runner) cancelSpeculative() {
	r.mu.Lock()
	hs := make([]*handle, 0, len(r.spec))
	for _, h := range r.spec {
		hs = append(hs, h)
	}
	r.spec = map[specKey]*handle{}
	r.mu.Unlock()
	stop(hs)
}

func stop(hs []*handle) {
	for _, h := range hs {
		h.cancel()
	}
	for _, h := range hs {
		<-h.done
	}
}

// takeHandle 은 호출 c 의 핸들을 꺼낸다. 재사용할 수 있는 핸들이면 돌려주고, 같은 CallID 의 나머지는 취소한다 (CORE-FR-26).
func (r *runner) takeHandle(c contract.ToolCall) *handle {
	key := specKey{c.ID, c.ArgsDigest()}
	r.mu.Lock()
	var reuse *handle
	var drop []*handle
	for k, h := range r.spec {
		if k.id != c.ID {
			continue
		}
		delete(r.spec, k)
		if k == key && !slices.Contains(r.discard, c.ID) {
			reuse = h
		} else {
			drop = append(drop, h)
		}
	}
	r.mu.Unlock()
	stop(drop)
	return reuse
}

// beforeExecute 는 16 → 17 을 dispatch 한다. ShortCircuit 이면 17 을 건너뛴다 (CORE-FR-21).
func (r *runner) beforeExecute(ctx context.Context, c contract.ToolCall) (contract.ToolCall, contract.ToolResult, *Settle) {
	o := r.do(ctx, StepToolBefore, c)
	if o.end != nil {
		return c, contract.ToolResult{}, o.end
	}
	bo := o.out.(BeforeOut)
	if bo.ShortCircuit != nil {
		return bo.Call, *bo.ShortCircuit, nil
	}
	o = r.do(ctx, StepToolExecute, bo.Call)
	if o.end != nil {
		return bo.Call, contract.ToolResult{}, o.end
	}
	return bo.Call, o.out.(contract.ToolResult), nil
}

// executeTools 는 Plan 의 그룹을 순서대로 실행하고, 결과를 Plan 순서로 모은다 (CORE-FR-20~24).
func (r *runner) executeTools(plan Plan) (ToolsCommitInput, *Settle) {
	defer r.cancelSpeculative()
	n := 0
	for _, g := range plan.Groups {
		n += len(g.Calls)
	}
	in := ToolsCommitInput{Calls: make([]contract.ToolCall, n), Results: make([]contract.ToolResult, n)}
	base := 0
	for _, g := range plan.Groups {
		if end := r.runGroup(g, base, &in); end != nil {
			return ToolsCommitInput{}, end
		}
		base += len(g.Calls)
	}
	return in, nil
}

func (r *runner) runGroup(g Group, base int, in *ToolsCommitInput) *Settle {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	limit := max(g.MaxParallel, 1)
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	var once sync.Once
	var first *Settle
	for i, c := range g.Calls {
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int, c contract.ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()
			call, res, end := r.runTool(ctx, base+i, c)
			if end != nil {
				once.Do(func() {
					first = end
					cancel()
				})
				return
			}
			in.Calls[base+i], in.Results[base+i] = call, res
		}(i, c)
	}
	wg.Wait()
	if r.ctx.Err() != nil {
		return r.aborted()
	}
	return first
}

func (r *runner) runTool(ctx context.Context, idx int, c contract.ToolCall) (contract.ToolCall, contract.ToolResult, *Settle) {
	var call contract.ToolCall
	var res contract.ToolResult
	if h := r.takeHandle(c); h != nil {
		select {
		case <-h.done:
		case <-ctx.Done():
			stop([]*handle{h})
			return c, res, &Settle{Reason: SettleAborted, Err: ctx.Err()}
		}
		if h.end != nil {
			return c, res, h.end
		}
		call, res = h.call, h.result
	} else {
		var end *Settle
		if call, res, end = r.beforeExecute(ctx, c); end != nil {
			return c, res, end
		}
	}
	r.mu.Lock()
	r.st.toolCalls[idx] = call
	r.mu.Unlock()

	o := r.do(ctx, StepToolAfter, ToolAfterInput{Call: call, Result: res})
	if o.end != nil {
		return call, res, o.end
	}
	res = o.out.(contract.ToolResult)
	r.mu.Lock()
	r.st.toolResults = append(r.st.toolResults, res)
	r.mu.Unlock()
	return call, res, nil
}
