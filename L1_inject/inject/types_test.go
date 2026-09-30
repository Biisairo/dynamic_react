package inject_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// inject 3.4 의 step 별 그룹 표. 열 순서 Flow Conv Request Stream Output Tools Env State.
var specGroups = []string{
	"●.....●●",                                                             // 1
	"●●..●.●●",                                                             // 2
	"●●●.●.●●", "●●●.●.●●", "●●●.●.●●", "●●●.●.●●", "●●●.●.●●", "●●●.●.●●", // 3~8
	"●.●...●●",             // 9
	"●.●●.●●●", "●.●●.●●●", // 10, 11
	"●●●●●.●●",             // 12
	"●●●.●.●●", "●●●.●.●●", // 13, 14
	"●●●.●●●●",                         // 15
	"●....●●●", "●....●●●", "●....●●●", // 16~18
	"●●..●●●●",                         // 19
	"●●●.●●●●", "●●●.●●●●", "●●●.●●●●", // 20~22
}

var groupNames = []string{"Flow", "Conv", "Request", "Stream", "Output", "Tools", "Env", "State"}

var ctorNames = map[core.SignalKind]string{
	core.SigNext: "Next", core.SigRecall: "Recall", core.SigReselect: "Reselect", core.SigRebuild: "Rebuild",
	core.SigRestartTurn: "RestartTurn", core.SigContinueTurn: "ContinueTurn", core.SigSkipTools: "SkipTools",
	core.SigSettle: "Settle", core.SigStop: "Stop", core.SigFail: "Fail", core.SigRewrite: "Rewrite",
	core.SigInjectInput: "InjectInput", core.SigStartTool: "StartTool",
}

// ctxOf 는 FunctionSet 의 step 필드 함수 타입과 그 ctx 타입이다.
func ctxOf(t *testing.T, i int) (reflect.Type, reflect.Type) {
	fs := reflect.TypeOf(inject.FunctionSet{})
	f := fs.Field(i + 1) // 0 = Name
	fn := f.Type
	if fn.Kind() != reflect.Func || fn.In(0).Kind() != reflect.Pointer {
		t.Fatalf("field %s is not a step function", f.Name)
	}
	return fn, fn.In(0)
}

// IN-FR-05, 08, 14, 18, 44, 45: 생성된 타입이 core 3.1·3.4 와 inject 3.4 를 따른다.
func TestGeneratedTypes(t *testing.T) {
	fs := reflect.TypeOf(inject.FunctionSet{})
	if fs.NumField() != 23 || fs.Field(0).Name != "Name" {
		t.Fatalf("FunctionSet has %d fields", fs.NumField())
	}
	for i, st := range core.Steps() {
		fn, ctx := ctxOf(t, i)
		field := fs.Field(i + 1).Name
		if ctx.Elem().Name() != field+"Ctx" || fn.Name() != field+"Fn" {
			t.Errorf("%s: field %s ctx %s fn %s", st, field, ctx.Elem().Name(), fn.Name())
		}
		// 그룹 접근자
		row := []rune(specGroups[i])
		for j, g := range groupNames {
			_, has := ctx.MethodByName(g)
			if has != (row[j] == '●') {
				t.Errorf("%s: group %s present=%v", st, g, has)
			}
		}
		// 신호 생성자
		allowed := map[string]bool{}
		for _, k := range core.AllowedSignals(st) {
			allowed[ctorNames[k]] = true
		}
		for _, name := range ctorNames {
			_, has := ctx.MethodByName(name)
			if has != allowed[name] {
				t.Errorf("%s: ctor %s present=%v", st, name, has)
			}
		}
		// 결과: 마지막은 error, 신호 슬라이스는 그 step 전용 타입
		last := fn.Out(fn.NumOut() - 1)
		if last.String() != "error" {
			t.Errorf("%s: last result %s", st, last)
		}
		if st == core.StepRunSettled {
			if fn.NumOut() != 1 {
				t.Errorf("run.settled returns %d values", fn.NumOut())
			}
			continue
		}
		sig := fn.Out(fn.NumOut() - 2)
		if sig.Kind() != reflect.Slice || sig.Elem().Name() != field+"Signal" || sig.Elem().Kind() != reflect.Interface {
			t.Errorf("%s: signal result %s", st, sig)
		}
		if _, ok := ctx.MethodByName("Dropped"); !ok {
			t.Errorf("%s: no Dropped", st)
		}
	}
}

// 조회 그룹 메서드 목록 (3.4).
func TestGroupMethods(t *testing.T) {
	want := map[reflect.Type]string{
		reflect.TypeOf(inject.Flow{}):    "CallIndex Count Elapsed RunID Seq SessionID Signals Step TurnIndex",
		reflect.TypeOf(inject.Conv{}):    "Edits Ephemeral History Inbox Projection Transcript TurnAppended",
		reflect.TypeOf(inject.Request{}): "Current Model",
		reflect.TypeOf(inject.Stream{}):  "Draft Event",
		reflect.TypeOf(inject.Output{}):  "CallError LastUsage Response",
		reflect.TypeOf(inject.Tools{}):   "Calls Plan Results Speculative",
		reflect.TypeOf(inject.Env{}):     "Binding Config ContractVersion",
		reflect.TypeOf(inject.State{}):   "Delete Get Publish Read Set",
	}
	for typ, names := range want {
		var got []string
		for i := range typ.NumMethod() {
			got = append(got, typ.Method(i).Name)
		}
		if strings.Join(got, " ") != names {
			t.Errorf("%s methods: %v, want %s", typ.Name(), got, names)
		}
	}
}
