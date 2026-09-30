// Package stepspec 는 inject 코드 생성의 단일 진실 공급원(StepSpec)이다 (IN-FR-01).
// 허용 신호는 여기 두지 않고 core 의 AllowedSignals 를 읽는다 (IN-FR-02).
package stepspec

import "github.com/Biisairo/dynamic_react/L0_core/core"

// Group 은 조회 그룹이다 (inject 3.4).
type Group string

const (
	Flow    Group = "Flow"
	Conv    Group = "Conv"
	Request Group = "Request"
	Stream  Group = "Stream"
	Output  Group = "Output"
	Tools   Group = "Tools"
	Env     Group = "Env"
	State   Group = "State"
)

// Groups 는 그룹의 표 순서다.
func Groups() []Group { return []Group{Flow, Conv, Request, Stream, Output, Tools, Env, State} }

// Param 은 ctx 다음에 오는 함수 인자다. Field 는 core 입력 구조체의 필드 이름 (없으면 입력 전체).
type Param struct {
	Name, Type, Field string
}

// Row 는 step 하나다.
type Row struct {
	Step   core.Step
	Name   string  // CamelCase 이름
	Input  string  // core 가 넘기는 입력의 Go 타입 ("" = 없음)
	Params []Param // 함수 인자 (ctx 제외)
	Output string  // 출력 Go 타입 ("" = 없음)
	Groups []Group
	// NoSignals 면 함수가 신호를 반환하지 않는다 (22).
	NoSignals bool
}

const (
	msgs    = "[]contract.Message"
	request = "contract.Request"
)

var (
	g1  = []Group{Flow, Env, State}
	g2  = []Group{Flow, Conv, Output, Env, State}
	g3  = []Group{Flow, Conv, Request, Output, Env, State}
	g9  = []Group{Flow, Request, Env, State}
	g10 = []Group{Flow, Request, Stream, Tools, Env, State}
	g12 = []Group{Flow, Conv, Request, Stream, Output, Env, State}
	g15 = []Group{Flow, Conv, Request, Output, Tools, Env, State}
	g16 = []Group{Flow, Tools, Env, State}
	g19 = []Group{Flow, Conv, Output, Tools, Env, State}
)

func whole(name, typ string) []Param { return []Param{{Name: name, Type: typ}} }

// Rows 는 22개 step 의 표다 (core CORE-FR-39, inject 3.4).
func Rows() []Row {
	return []Row{
		{core.StepRunStart, "RunStart", "core.RunInput", whole("in", "core.RunInput"), "core.RunInit", g1, false},
		{core.StepTurnStart, "TurnStart", "", nil, msgs, g2, false},
		{core.StepContextMaintain, "ContextMaintain", "", nil, "", g3, false},
		{core.StepContextInject, "ContextInject", "", nil, msgs, g3, false},
		{core.StepContextProject, "ContextProject", "", nil, msgs, g3, false},
		{core.StepRequestBuild, "RequestBuild", "", nil, request, g3, false},
		{core.StepRequestFinalize, "RequestFinalize", request, whole("req", request), request, g3, false},
		{core.StepModelSelect, "ModelSelect", request, whole("req", request), "contract.ModelInfo", g3, false},
		{core.StepModelCall, "ModelCall", request, whole("req", request), "core.CallStream", g9, false},
		{core.StepStreamEvent, "StreamEvent", "contract.StreamEvent", whole("ev", "contract.StreamEvent"), "", g10, false},
		{core.StepStreamEnd, "StreamEnd", "contract.Message", whole("msg", "contract.Message"), "core.StreamEndOut", g10, false},
		{core.StepCallError, "CallError", "contract.CallError", whole("ce", "contract.CallError"), "", g12, false},
		{core.StepResponseClassify, "ResponseClassify", "", nil, "", g3, false},
		{core.StepResponseCommit, "ResponseCommit", "", nil, "", g3, false},
		{core.StepToolsPlan, "ToolsPlan", "[]contract.ToolCall", whole("calls", "[]contract.ToolCall"), "core.Plan", g15, false},
		{core.StepToolBefore, "ToolBefore", "contract.ToolCall", whole("call", "contract.ToolCall"), "core.BeforeOut", g16, false},
		{core.StepToolExecute, "ToolExecute", "contract.ToolCall", whole("call", "contract.ToolCall"), "contract.ToolResult", g16, false},
		{core.StepToolAfter, "ToolAfter", "core.ToolAfterInput", []Param{
			{Name: "call", Type: "contract.ToolCall", Field: "Call"},
			{Name: "res", Type: "contract.ToolResult", Field: "Result"},
		}, "contract.ToolResult", g16, false},
		{core.StepToolsCommit, "ToolsCommit", "core.ToolsCommitInput", []Param{
			{Name: "calls", Type: "[]contract.ToolCall", Field: "Calls"},
			{Name: "results", Type: "[]contract.ToolResult", Field: "Results"},
		}, msgs, g19, false},
		{core.StepTurnFinish, "TurnFinish", "", nil, "", g15, false},
		{core.StepRunBeforeSettle, "RunBeforeSettle", "", nil, "", g15, false},
		{core.StepRunSettled, "RunSettled", "core.Settle", whole("s", "core.Settle"), "", g15, true},
	}
}

// Combinator 는 조합 함수 종류다 (inject 3.7).
type Combinator string

const (
	FirstDecisive Combinator = "FirstDecisive"
	ByKind        Combinator = "ByKind"
	Pipe          Combinator = "Pipe"
	Concat        Combinator = "Concat"
	Wrap          Combinator = "Wrap"
)

// Combinators 는 step 에 적용할 수 있는 조합이다. Wrap 은 모든 step.
func Combinators(s core.Step) []Combinator {
	switch s {
	case core.StepContextMaintain, core.StepStreamEvent, core.StepCallError,
		core.StepResponseClassify, core.StepTurnFinish, core.StepRunBeforeSettle:
		return []Combinator{FirstDecisive, ByKind, Wrap}
	case core.StepRequestFinalize, core.StepToolAfter:
		return []Combinator{Pipe, Wrap}
	case core.StepTurnStart, core.StepContextInject:
		return []Combinator{Concat, Wrap}
	}
	return []Combinator{Wrap}
}
