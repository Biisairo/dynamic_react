package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

var errNoTerminal = errors.New("stream ended without terminal event")

func unknownCallError(err error) contract.CallError {
	return contract.CallError{Class: contract.ClassUnknown, Message: err.Error()}
}

func (r *runner) toCallError(ce contract.CallError) (Step, *Settle) {
	r.mu.Lock()
	r.st.callErr = &ce
	r.mu.Unlock()
	return StepCallError, nil
}

// modelCall 은 9 를 dispatch 하고 스트림을 소비한다 (CORE-FR-15~18, 43).
func (r *runner) modelCall() (Step, *Settle) {
	r.mu.Lock()
	r.st.call++
	r.st.draft = draft{}
	r.st.event, r.st.response, r.st.callErr = nil, nil, nil
	req := *r.st.request
	r.mu.Unlock()

	callCtx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	o := r.do(callCtx, StepModelCall, req)
	if o.end != nil {
		return 0, o.end
	}
	if o.goErr != nil {
		return r.toCallError(unknownCallError(o.goErr))
	}
	cs := o.out.(CallStream)
	r.mu.Lock()
	r.live = cs.Live
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.live = nil
		r.mu.Unlock()
	}()
	return r.consume(cs)
}

func (r *runner) consume(cs CallStream) (Step, *Settle) {
	var next Step
	var end *Settle
	var iterErr error
	func() {
		defer func() {
			if p := recover(); p != nil {
				iterErr = fmt.Errorf("%w: %v", ErrPanic, p)
			}
		}()
		for ev, err := range cs.Events {
			if err != nil {
				iterErr = err
				return
			}
			if next, end = r.onEvent(ev); next != 0 || end != nil {
				return
			}
		}
	}()
	switch {
	case end != nil:
		return 0, end
	case r.ctx.Err() != nil:
		return 0, r.aborted()
	case next != 0:
		return next, nil
	case iterErr != nil:
		return r.toCallError(unknownCallError(iterErr))
	}
	return r.toCallError(unknownCallError(errNoTerminal))
}

// onEvent 는 이벤트 하나에 10 을 dispatch 한다. 0, nil 이면 다음 이벤트로 간다.
func (r *runner) onEvent(ev contract.StreamEvent) (Step, *Settle) {
	r.mu.Lock()
	r.st.event = &ev
	r.st.draft.apply(ev)
	r.mu.Unlock()
	r.observe(ObserverEvent{Kind: ObsStreamEvent, Stream: &ev})

	o := r.do(r.ctx, StepStreamEvent, ev)
	if o.end != nil {
		return 0, o.end
	}
	if o.tr.Kind != SigNext {
		return r.route(o.tr, 0), nil
	}
	switch ev.Kind {
	case contract.EventDone:
		if ev.Message == nil {
			return r.toCallError(unknownCallError(errors.New("done event without message")))
		}
		r.doneMsg = *ev.Message
		return StepStreamEnd, nil
	case contract.EventError:
		if ev.Error == nil {
			return r.toCallError(unknownCallError(errors.New("error event without error")))
		}
		return r.toCallError(*ev.Error)
	}
	return 0, nil
}

// draft 는 스트림 이벤트로 조립하는 부분 응답이다 (CORE-FR-46).
// 스냅샷이 블록 슬라이스를 복사하므로, ToolCall 은 바꿀 때마다 새로 만든다.
type draft struct {
	blocks []contract.Block
	pos    map[int]int
}

func (d *draft) block(index int, kind contract.BlockKind) *contract.Block {
	if d.pos == nil {
		d.pos = map[int]int{}
	}
	i, ok := d.pos[index]
	if !ok {
		i = len(d.blocks)
		d.pos[index] = i
		d.blocks = append(d.blocks, contract.Block{Kind: kind})
	}
	return &d.blocks[i]
}

func (d *draft) apply(ev contract.StreamEvent) {
	switch ev.Kind {
	case contract.EventTextDelta:
		d.block(ev.Index, contract.BlockText).Text += ev.Delta
	case contract.EventThinkingDelta:
		d.block(ev.Index, contract.BlockThinking).Text += ev.Delta
	case contract.EventToolCallStart, contract.EventToolCallEnd:
		if ev.ToolCall != nil {
			c := *ev.ToolCall
			d.block(ev.Index, contract.BlockToolCall).ToolCall = &c
		}
	case contract.EventToolCallDelta:
		b := d.block(ev.Index, contract.BlockToolCall)
		var c contract.ToolCall
		if b.ToolCall != nil {
			c = *b.ToolCall
		}
		c.Args = append(append([]byte(nil), c.Args...), ev.Delta...)
		b.ToolCall = &c
	}
}
