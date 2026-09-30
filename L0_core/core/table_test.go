package core_test

import (
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
)

// 3.1 step 목록과 단위.
var specSteps = []struct {
	name string
	unit core.Unit
}{
	{"run.start", core.UnitRun}, {"turn.start", core.UnitTurn}, {"context.maintain", core.UnitTurn},
	{"context.inject", core.UnitTurn}, {"context.project", core.UnitTurn}, {"request.build", core.UnitCall},
	{"request.finalize", core.UnitCall}, {"model.select", core.UnitCall}, {"model.call", core.UnitCall},
	{"stream.event", core.UnitEvent}, {"stream.end", core.UnitCall}, {"call.error", core.UnitCall},
	{"response.classify", core.UnitTurn}, {"response.commit", core.UnitTurn}, {"tools.plan", core.UnitTurn},
	{"tool.before", core.UnitTool}, {"tool.execute", core.UnitTool}, {"tool.after", core.UnitTool},
	{"tools.commit", core.UnitTurn}, {"turn.finish", core.UnitTurn}, {"run.beforeSettle", core.UnitRun},
	{"run.settled", core.UnitRun},
}

// CORE-FR-01
func TestStepTable(t *testing.T) {
	steps := core.Steps()
	if len(steps) != 22 {
		t.Fatalf("len(Steps()) = %d", len(steps))
	}
	for i, s := range steps {
		if int(s) != i+1 {
			t.Errorf("Steps()[%d] = %d, want %d", i, s, i+1)
		}
		if s.String() != specSteps[i].name {
			t.Errorf("step %d name %q, want %q", i+1, s, specSteps[i].name)
		}
		if s.Unit() != specSteps[i].unit {
			t.Errorf("step %s unit %q, want %q", s, s.Unit(), specSteps[i].unit)
		}
		if !s.Valid() {
			t.Errorf("step %s not valid", s)
		}
	}
	for _, bad := range []core.Step{0, 23, -1} {
		if bad.Valid() {
			t.Errorf("Step(%d).Valid() = true", bad)
		}
	}
	steps[0] = 99
	if core.Steps()[0] != core.StepRunStart {
		t.Error("Steps() exposes shared slice")
	}
}

// CORE-FR-04
var specSignals = []struct {
	kind       core.SignalKind
	name       string
	transition bool
}{
	{core.SigNext, "next", true}, {core.SigRecall, "recall", true}, {core.SigReselect, "reselect", true},
	{core.SigRebuild, "rebuild", true}, {core.SigRestartTurn, "restart_turn", true},
	{core.SigContinueTurn, "continue_turn", true}, {core.SigSkipTools, "skip_tools", true},
	{core.SigSettle, "settle", true}, {core.SigStop, "stop", true}, {core.SigFail, "fail", true},
	{core.SigRewrite, "rewrite", false}, {core.SigInjectInput, "inject_input", false}, {core.SigStartTool, "start_tool", false},
}

func TestSignalTable(t *testing.T) {
	kinds := core.SignalKinds()
	if len(kinds) != len(specSignals) {
		t.Fatalf("len(SignalKinds()) = %d", len(kinds))
	}
	for i, s := range specSignals {
		if kinds[i] != s.kind || string(s.kind) != s.name {
			t.Errorf("signal %d = %q, want %q", i, kinds[i], s.name)
		}
		if s.kind.IsTransition() != s.transition {
			t.Errorf("%s.IsTransition() = %v", s.kind, !s.transition)
		}
		if !s.kind.Valid() {
			t.Errorf("%s not valid", s.kind)
		}
	}
	if core.SignalKind("jump").Valid() {
		t.Error("unknown signal valid")
	}
}

// CORE-FR-08: 3.3 의 단순 목적지. 9·10·12·15~18·20·22 는 core 가 따로 처리하거나 목적지가 없다.
func TestNextTable(t *testing.T) {
	want := map[core.Step]core.Step{
		core.StepRunStart: core.StepTurnStart, core.StepTurnStart: core.StepContextMaintain,
		core.StepContextMaintain: core.StepContextInject, core.StepContextInject: core.StepContextProject,
		core.StepContextProject: core.StepRequestBuild, core.StepRequestBuild: core.StepRequestFinalize,
		core.StepRequestFinalize: core.StepModelSelect, core.StepModelSelect: core.StepModelCall,
		core.StepStreamEnd: core.StepResponseClassify, core.StepResponseClassify: core.StepResponseCommit,
		core.StepResponseCommit: core.StepToolsPlan, core.StepToolBefore: core.StepToolExecute,
		core.StepToolExecute: core.StepToolAfter, core.StepToolsCommit: core.StepTurnFinish,
		core.StepRunBeforeSettle: core.StepRunSettled,
	}
	for _, s := range core.Steps() {
		got, ok := core.NextOf(s)
		w, wok := want[s]
		if ok != wok || got != w {
			t.Errorf("NextOf(%s) = %s,%v want %s,%v", s, got, ok, w, wok)
		}
	}
}

// CORE-FR-09, 10: 3.4 허용 표를 그대로 옮긴 것. 열 순서 N RC RS RB RT CT ST SE SP F W II SO.
var specAllowed = []string{
	"●.......●●●..", // 1 run.start
	"●......●●●...", // 2 turn.start
	"●.......●●●..", // 3 context.maintain
	"●........●...", // 4 context.inject
	"●........●...", // 5 context.project
	"●........●...", // 6 request.build
	"●........●...", // 7 request.finalize
	"●..●....●●...", // 8 model.select
	"●........●...", // 9 model.call
	"●●●.●...●●.●●", // 10 stream.event
	"●●..●....●...", // 11 stream.end
	".●●●●...●●●..", // 12 call.error
	"●●●●●...●●●..", // 13 response.classify
	"●........●...", // 14 response.commit
	"●.....●..●...", // 15 tools.plan
	"●........●...", // 16 tool.before
	"●........●...", // 17 tool.execute
	"●........●...", // 18 tool.after
	"●........●...", // 19 tools.commit
	".....●.●●●●●.", // 20 turn.finish
	"●....●..●●.●.", // 21 run.beforeSettle
	".............", // 22 run.settled
}

func TestAllowedTable(t *testing.T) {
	kinds := core.SignalKinds()
	for i, s := range core.Steps() {
		row := []rune(specAllowed[i])
		var want []core.SignalKind
		for j, k := range kinds {
			w := row[j] == '●'
			if core.Allowed(s, k) != w {
				t.Errorf("Allowed(%s, %s) = %v", s, k, !w)
			}
			if w {
				want = append(want, k)
			}
		}
		got := core.AllowedSignals(s)
		if strings.Join(kindNames(got), ",") != strings.Join(kindNames(want), ",") {
			t.Errorf("AllowedSignals(%s) = %v, want %v", s, got, want)
		}
	}
	if core.Allowed(0, core.SigNext) || core.Allowed(core.StepRunStart, "jump") {
		t.Error("invalid step or kind allowed")
	}
}

func kindNames(ks []core.SignalKind) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = string(k)
	}
	return out
}
