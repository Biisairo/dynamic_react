package core

import (
	"fmt"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func (r *runner) run() Settle {
	r.observe(ObserverEvent{Kind: ObsRunStarted})
	return r.settle(r.loop())
}

// loop 는 run.start 부터 run.beforeSettle 까지 step 을 진행하고 끝난 방식을 돌려준다.
func (r *runner) loop() Settle {
	o := r.do(r.ctx, StepRunStart, r.input)
	if o.end != nil {
		return *o.end
	}
	history := o.out.(RunInit).History
	r.mu.Lock()
	r.st.history = clone(history)
	r.st.transcript = clone(history)
	r.mu.Unlock()

	pc := StepTurnStart
	for {
		var next Step
		var end *Settle
		switch pc {
		case StepTurnStart:
			next, end = r.turnStart()
		case StepContextMaintain:
			next, end = r.simple(StepContextMaintain)
		case StepContextInject:
			next, end = r.messages(StepContextInject, &r.st.ephemeral)
		case StepContextProject:
			next, end = r.messages(StepContextProject, &r.st.projection)
		case StepRequestBuild:
			next, end = r.requestBuild()
		case StepRequestFinalize:
			next, end = r.requestFinalize()
		case StepModelSelect:
			next, end = r.modelSelect()
		case StepModelCall:
			next, end = r.modelCall()
		case StepStreamEnd:
			next, end = r.streamEnd()
		case StepCallError:
			next, end = r.simpleInput(StepCallError, *r.st.callErr)
		case StepResponseClassify:
			next, end = r.simple(StepResponseClassify)
		case StepResponseCommit:
			next, end = r.responseCommit()
		case StepToolsPlan:
			next, end = r.toolsPlan()
		case StepToolsCommit:
			next, end = r.toolsCommit()
		case StepTurnFinish:
			next, end = r.simple(StepTurnFinish)
		case StepRunBeforeSettle:
			next, end = r.simple(StepRunBeforeSettle)
		}
		if end != nil {
			return *end
		}
		if next == StepRunSettled {
			return Settle{Reason: SettleCompleted}
		}
		pc = next
	}
}

// route 는 next 가 아닌 전이의 목적지다 (3.2). next 면 dflt.
func (r *runner) route(tr Signal, dflt Step) Step {
	switch tr.Kind {
	case SigRecall:
		r.cancelSpeculative()
		return StepModelCall
	case SigReselect:
		r.cancelSpeculative()
		return StepModelSelect
	case SigRebuild:
		r.cancelSpeculative()
		return StepContextProject
	case SigRestartTurn:
		r.cancelSpeculative()
		return StepTurnStart
	case SigContinueTurn:
		return StepTurnStart
	case SigSettle:
		return StepRunBeforeSettle
	case SigSkipTools:
		return StepToolsCommit
	}
	return dflt
}

func (r *runner) nextOf(step Step, o outcome) Step {
	n, _ := NextOf(step)
	return r.route(o.tr, n)
}

func (r *runner) simple(step Step) (Step, *Settle) {
	return r.simpleInput(step, nil)
}

func (r *runner) simpleInput(step Step, input any) (Step, *Settle) {
	o := r.do(r.ctx, step, input)
	if o.end != nil {
		return 0, o.end
	}
	return r.nextOf(step, o), nil
}

func (r *runner) messages(step Step, dst *[]contract.Message) (Step, *Settle) {
	o := r.do(r.ctx, step, nil)
	if o.end != nil {
		return 0, o.end
	}
	r.mu.Lock()
	*dst = clone(o.out.([]contract.Message))
	r.mu.Unlock()
	return r.nextOf(step, o), nil
}

func (r *runner) turnStart() (Step, *Settle) {
	r.mu.Lock()
	r.st.turn++
	r.st.call = 0
	r.st.turnAppended, r.st.ephemeral, r.st.projection = nil, nil, nil
	r.st.request, r.st.response, r.st.callErr, r.st.plan, r.st.event = nil, nil, nil, nil, nil
	r.st.draft = draft{}
	r.st.toolCalls, r.st.toolResults = nil, nil
	r.discard = nil
	r.mu.Unlock()

	o := r.do(r.ctx, StepTurnStart, nil)
	if o.end != nil {
		return 0, o.end
	}
	msgs := clone(o.out.([]contract.Message))
	r.mu.Lock()
	r.st.turnAppended = msgs
	r.st.transcript = append(r.st.transcript, msgs...)
	r.mu.Unlock()
	return r.nextOf(StepTurnStart, o), nil
}

func (r *runner) requestBuild() (Step, *Settle) {
	r.mu.Lock()
	r.st.request = nil
	r.mu.Unlock()
	o := r.do(r.ctx, StepRequestBuild, nil)
	if o.end != nil {
		return 0, o.end
	}
	r.built = o.out.(contract.Request)
	return r.nextOf(StepRequestBuild, o), nil
}

func (r *runner) requestFinalize() (Step, *Settle) {
	o := r.do(r.ctx, StepRequestFinalize, r.built)
	if o.end != nil {
		return 0, o.end
	}
	req := o.out.(contract.Request)
	r.mu.Lock()
	r.st.request = &req
	r.mu.Unlock()
	return r.nextOf(StepRequestFinalize, o), nil
}

func (r *runner) modelSelect() (Step, *Settle) {
	o := r.do(r.ctx, StepModelSelect, *r.st.request)
	if o.end != nil {
		return 0, o.end
	}
	info := o.out.(contract.ModelInfo)
	r.mu.Lock()
	req := *r.st.request
	req.Model = info.Ref
	r.st.request = &req
	r.st.model = &info
	r.mu.Unlock()
	return r.nextOf(StepModelSelect, o), nil
}

func (r *runner) streamEnd() (Step, *Settle) {
	o := r.do(r.ctx, StepStreamEnd, r.doneMsg)
	if o.end != nil {
		return 0, o.end
	}
	se := o.out.(StreamEndOut)
	r.mu.Lock()
	r.st.response = &se.Response
	r.discard = clone(se.Discard)
	r.mu.Unlock()
	return r.nextOf(StepStreamEnd, o), nil
}

func (r *runner) responseCommit() (Step, *Settle) {
	o := r.do(r.ctx, StepResponseCommit, nil)
	if o.end != nil {
		return 0, o.end
	}
	r.mu.Lock()
	r.st.transcript = append(r.st.transcript, *r.st.response)
	r.mu.Unlock()
	return r.nextOf(StepResponseCommit, o), nil
}

func toolCallsOf(m contract.Message) []contract.ToolCall {
	calls := []contract.ToolCall{}
	for _, b := range m.Content {
		if b.Kind == contract.BlockToolCall && b.ToolCall != nil {
			calls = append(calls, *b.ToolCall)
		}
	}
	return calls
}

func (r *runner) toolsPlan() (Step, *Settle) {
	calls := toolCallsOf(*r.st.response)
	o := r.do(r.ctx, StepToolsPlan, calls)
	if o.end != nil {
		return 0, o.end
	}
	if o.tr.Kind == SigSkipTools {
		r.commitIn = ToolsCommitInput{Calls: calls, Results: clone(o.tr.Results)}
		return StepToolsCommit, nil
	}
	plan := o.out.(Plan)
	if err := checkPlan(plan, calls); err != nil {
		return 0, failed(StepToolsPlan, err)
	}
	r.mu.Lock()
	r.st.plan = &plan
	for _, g := range plan.Groups {
		r.st.toolCalls = append(r.st.toolCalls, g.Calls...)
	}
	r.mu.Unlock()
	in, end := r.executeTools(plan)
	if end != nil {
		return 0, end
	}
	r.commitIn = in
	return StepToolsCommit, nil
}

// checkPlan 은 CORE-FR-22 를 검사한다.
func checkPlan(plan Plan, calls []contract.ToolCall) error {
	known := make(map[string]bool, len(calls))
	for _, c := range calls {
		known[c.ID] = true
	}
	seen := map[string]bool{}
	for _, g := range plan.Groups {
		for _, c := range g.Calls {
			if !known[c.ID] {
				return fmt.Errorf("%w: unknown call %q", ErrPlan, c.ID)
			}
			if seen[c.ID] {
				return fmt.Errorf("%w: duplicate call %q", ErrPlan, c.ID)
			}
			seen[c.ID] = true
		}
	}
	return nil
}

func (r *runner) toolsCommit() (Step, *Settle) {
	in := r.commitIn
	o := r.do(r.ctx, StepToolsCommit, in)
	if o.end != nil {
		return 0, o.end
	}
	msgs := o.out.([]contract.Message)
	if err := checkCommit(msgs, in.Results); err != nil {
		return 0, failed(StepToolsCommit, err)
	}
	r.mu.Lock()
	r.st.transcript = append(r.st.transcript, msgs...)
	r.mu.Unlock()
	return r.nextOf(StepToolsCommit, o), nil
}

// checkCommit 은 CORE-FR-24a 를 검사한다.
func checkCommit(msgs []contract.Message, results []contract.ToolResult) error {
	if len(msgs) != len(results) {
		return fmt.Errorf("%w: %d messages for %d results", ErrCommit, len(msgs), len(results))
	}
	for i, m := range msgs {
		if m.Role != contract.RoleToolResult || m.Result == nil || m.Result.CallID != results[i].CallID {
			return fmt.Errorf("%w: message %d does not match result %q", ErrCommit, i, results[i].CallID)
		}
		if err := m.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrCommit, err)
		}
	}
	return nil
}
