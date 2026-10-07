package main

import (
	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

var planBlocked = map[string]bool{"write": true, "edit": true, "bash": true}

const planRule = "[plan mode] Plan only — do not implement. Investigate with read-only tools and reply with a step-by-step plan " +
	"(goal, approach, tasks with exact file paths, verification commands, risks)."

// PlanSet 은 plan 모드의 세트다: 계획 지시, 쓰기 도구 차단, run 끝에서 이탈 (MX-FR-05~07).
func PlanSet(d sets.Deps) inject.FunctionSet {
	modes := sets.ModesOf(d)
	return inject.FunctionSet{Name: "plan", Description: "plan mode: plan only, write/edit/bash blocked",
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			msgs, sigs, err := ctx.Next()
			if err != nil {
				return msgs, sigs, err
			}
			rule := planRule
			if st, err := modes.Current(ctx.Context(), ctx.Flow().SessionID()); err == nil && st.Arg != "" {
				rule += "\nTask: " + st.Arg
			}
			return append(msgs, userMessage(d, rule)), sigs, nil
		},
		RequestFinalize: func(ctx *inject.RequestFinalizeCtx, req contract.Request) (contract.Request, []inject.RequestFinalizeSignal, error) {
			req, sigs, err := ctx.Next(req)
			var tools []contract.ToolSpec
			for _, t := range req.Tools {
				if !planBlocked[t.Name] {
					tools = append(tools, t)
				}
			}
			req.Tools = tools
			return req, sigs, err
		},
		ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			if planBlocked[c.Name] {
				return core.BeforeOut{Call: c, ShortCircuit: &contract.ToolResult{CallID: c.ID, Status: contract.StatusError,
					Content: []contract.Block{{Kind: contract.BlockText, Text: "blocked in plan mode"}}}}, nil, nil
			}
			return ctx.Next(c)
		},
		TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
			sigs, err := ctx.Next()
			if err != nil || !hasSettle(sigs) {
				return sigs, err
			}
			return sigs, modes.Exit(ctx.Context(), ctx.Flow().SessionID(), "plan done")
		}}
}
