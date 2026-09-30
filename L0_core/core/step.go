package core

import "slices"

// Step 은 루프의 한 단계다. 값은 3.1 표의 번호와 같다 (CORE-FR-01).
type Step int

const (
	StepRunStart Step = iota + 1
	StepTurnStart
	StepContextMaintain
	StepContextInject
	StepContextProject
	StepRequestBuild
	StepRequestFinalize
	StepModelSelect
	StepModelCall
	StepStreamEvent
	StepStreamEnd
	StepCallError
	StepResponseClassify
	StepResponseCommit
	StepToolsPlan
	StepToolBefore
	StepToolExecute
	StepToolAfter
	StepToolsCommit
	StepTurnFinish
	StepRunBeforeSettle
	StepRunSettled
)

const stepCount = int(StepRunSettled)

// Unit 은 step 이 반복되는 범위다.
type Unit string

const (
	UnitRun   Unit = "run"
	UnitTurn  Unit = "turn"
	UnitCall  Unit = "call"
	UnitEvent Unit = "event"
	UnitTool  Unit = "tool"
)

// SignalKind 는 제어 약속어의 종류다 (CORE-FR-04).
type SignalKind string

const (
	SigNext         SignalKind = "next"
	SigRecall       SignalKind = "recall"
	SigReselect     SignalKind = "reselect"
	SigRebuild      SignalKind = "rebuild"
	SigRestartTurn  SignalKind = "restart_turn"
	SigContinueTurn SignalKind = "continue_turn"
	SigSkipTools    SignalKind = "skip_tools"
	SigSettle       SignalKind = "settle"
	SigStop         SignalKind = "stop"
	SigFail         SignalKind = "fail"
	SigRewrite      SignalKind = "rewrite"
	SigInjectInput  SignalKind = "inject_input"
	SigStartTool    SignalKind = "start_tool"
)

type stepRow struct {
	name    string
	unit    Unit
	next    Step // 0 = 단순 목적지 없음 (3.3)
	allowed string
}

// allowed 는 3.4 표의 한 행이다. 열 순서는 signalOrder 와 같다.
func stepTable(s Step) (stepRow, bool) {
	switch s {
	case StepRunStart:
		return stepRow{"run.start", UnitRun, StepTurnStart, "●.......●●●.."}, true
	case StepTurnStart:
		return stepRow{"turn.start", UnitTurn, StepContextMaintain, "●......●●●..."}, true
	case StepContextMaintain:
		return stepRow{"context.maintain", UnitTurn, StepContextInject, "●.......●●●.."}, true
	case StepContextInject:
		return stepRow{"context.inject", UnitTurn, StepContextProject, "●........●..."}, true
	case StepContextProject:
		return stepRow{"context.project", UnitTurn, StepRequestBuild, "●........●..."}, true
	case StepRequestBuild:
		return stepRow{"request.build", UnitCall, StepRequestFinalize, "●........●..."}, true
	case StepRequestFinalize:
		return stepRow{"request.finalize", UnitCall, StepModelSelect, "●........●..."}, true
	case StepModelSelect:
		return stepRow{"model.select", UnitCall, StepModelCall, "●..●....●●..."}, true
	case StepModelCall:
		return stepRow{"model.call", UnitCall, 0, "●........●..."}, true
	case StepStreamEvent:
		return stepRow{"stream.event", UnitEvent, 0, "●●●.●...●●.●●"}, true
	case StepStreamEnd:
		return stepRow{"stream.end", UnitCall, StepResponseClassify, "●●..●....●..."}, true
	case StepCallError:
		return stepRow{"call.error", UnitCall, 0, ".●●●●...●●●.."}, true
	case StepResponseClassify:
		return stepRow{"response.classify", UnitTurn, StepResponseCommit, "●●●●●...●●●.."}, true
	case StepResponseCommit:
		return stepRow{"response.commit", UnitTurn, StepToolsPlan, "●........●..."}, true
	case StepToolsPlan:
		return stepRow{"tools.plan", UnitTurn, 0, "●.....●..●..."}, true
	case StepToolBefore:
		return stepRow{"tool.before", UnitTool, StepToolExecute, "●........●..."}, true
	case StepToolExecute:
		return stepRow{"tool.execute", UnitTool, StepToolAfter, "●........●..."}, true
	case StepToolAfter:
		return stepRow{"tool.after", UnitTool, 0, "●........●..."}, true
	case StepToolsCommit:
		return stepRow{"tools.commit", UnitTurn, StepTurnFinish, "●........●..."}, true
	case StepTurnFinish:
		return stepRow{"turn.finish", UnitTurn, 0, ".....●.●●●●●."}, true
	case StepRunBeforeSettle:
		return stepRow{"run.beforeSettle", UnitRun, StepRunSettled, "●....●..●●.●."}, true
	case StepRunSettled:
		return stepRow{"run.settled", UnitRun, 0, "............."}, true
	}
	return stepRow{}, false
}

// SignalKinds 는 13개 신호를 3.2 표 순서로 돌려준다.
func SignalKinds() []SignalKind {
	return []SignalKind{SigNext, SigRecall, SigReselect, SigRebuild, SigRestartTurn, SigContinueTurn,
		SigSkipTools, SigSettle, SigStop, SigFail, SigRewrite, SigInjectInput, SigStartTool}
}

// Steps 는 22개 step 을 순서대로 돌려준다.
func Steps() []Step {
	out := make([]Step, stepCount)
	for i := range out {
		out[i] = Step(i + 1)
	}
	return out
}

func (s Step) Valid() bool {
	_, ok := stepTable(s)
	return ok
}

func (s Step) String() string {
	if r, ok := stepTable(s); ok {
		return r.name
	}
	return "step(invalid)"
}

func (s Step) Unit() Unit {
	r, _ := stepTable(s)
	return r.unit
}

// NextOf 는 3.3 표의 단순 목적지를 돌려준다. core 가 따로 처리하는 step 은 false.
func NextOf(s Step) (Step, bool) {
	r, ok := stepTable(s)
	return r.next, ok && r.next != 0
}

// Allowed 는 3.4 허용 표다 (CORE-FR-09, 10).
func Allowed(s Step, k SignalKind) bool {
	r, ok := stepTable(s)
	if !ok {
		return false
	}
	row := []rune(r.allowed)
	for i, kind := range SignalKinds() {
		if kind == k {
			return row[i] == '●'
		}
	}
	return false
}

// AllowedSignals 는 step 에 허용된 신호를 3.2 표 순서로 돌려준다.
func AllowedSignals(s Step) []SignalKind {
	var out []SignalKind
	for _, k := range SignalKinds() {
		if Allowed(s, k) {
			out = append(out, k)
		}
	}
	return out
}

func (k SignalKind) Valid() bool { return slices.Contains(SignalKinds(), k) }

// IsTransition 은 전이 신호인지 알려 준다 (R1). 효과 신호는 rewrite, inject_input, start_tool.
func (k SignalKind) IsTransition() bool {
	return k.Valid() && k != SigRewrite && k != SigInjectInput && k != SigStartTool
}
