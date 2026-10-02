package inject_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// IN-NFR-03: 봉인·범위 강제는 컴파일 에러여야 한다. 코드 조각마다 임시 모듈을 만들어 go build 가 실패하는지 본다.
var mustNotCompile = map[string]string{
	"next not allowed at turn.finish": `
func f(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
	return []inject.TurnFinishSignal{ctx.NextSignal()}, nil
}`,
	"recall not allowed at request.build": `
func f(ctx *inject.RequestBuildCtx) (contract.Request, []inject.RequestBuildSignal, error) {
	return contract.Request{}, []inject.RequestBuildSignal{ctx.Recall()}, nil
}`,
	"signal of another step": `
func f(a *inject.ContextMaintainCtx, ctx *inject.TurnFinishCtx) []inject.TurnFinishSignal {
	return []inject.TurnFinishSignal{a.Stop()}
}`,
	"stream group absent at turn.finish": `
func f(ctx *inject.TurnFinishCtx) { _ = ctx.Stream() }`,
	"tools group absent at run.start": `
func f(ctx *inject.RunStartCtx) { _ = ctx.Tools() }`,
	"no signals at run.settled": `
func f(ctx *inject.RunSettledCtx) { _ = ctx.Stop() }`,
	"user type cannot implement signal": `
type fake struct{}
func (fake) Kind() core.SignalKind { return core.SigSettle }
func (f fake) WithReason(string) inject.TurnFinishSignal { return f }
var _ inject.TurnFinishSignal = fake{}`,
	"pipe not applicable to turn.finish": `
var _ = combine.TurnFinish.Pipe`,
	"first-decisive not applicable to request.build": `
var _ = combine.RequestBuild.FirstDecisive`,
	"concat not applicable to context.project": `
var _ = combine.ContextProject.Concat`,
	"combinator of another step": `
var _ inject.TurnFinishFn = combine.ContextMaintain.FirstDecisive()`,
	"wrong function signature": `
var _ = inject.FunctionSet{Name: "x", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.StreamEventSignal, error) { return nil, nil }}`,
}

const compileOK = `
func f(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
	_ = ctx.Output().LastUsage()
	return []inject.TurnFinishSignal{ctx.Settle().WithReason("done")}, nil
}`

func TestCompileFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("builds temporary modules")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not found")
	}
	root, _ := filepath.Abs("../..")
	build := func(t *testing.T, body string) (string, error) {
		dir := t.TempDir()
		mod := "module probe\n\ngo 1.23\n\nrequire (\n" +
			"\tgithub.com/Biisairo/dynamic_react/L0_core/contract v0.0.0\n" +
			"\tgithub.com/Biisairo/dynamic_react/L0_core/core v0.0.0\n" +
			"\tgithub.com/Biisairo/dynamic_react/L1_inject/inject v0.0.0\n)\n\n" +
			"replace github.com/Biisairo/dynamic_react/L0_core/contract => " + filepath.Join(root, "L0_core/contract") + "\n" +
			"replace github.com/Biisairo/dynamic_react/L0_core/core => " + filepath.Join(root, "L0_core/core") + "\n" +
			"replace github.com/Biisairo/dynamic_react/L1_inject/inject => " + filepath.Join(root, "L1_inject/inject") + "\n"
		src := "package probe\n\nimport (\n" +
			"\t\"github.com/Biisairo/dynamic_react/L0_core/contract\"\n" +
			"\t\"github.com/Biisairo/dynamic_react/L0_core/core\"\n" +
			"\t\"github.com/Biisairo/dynamic_react/L1_inject/inject\"\n" +
			"\t\"github.com/Biisairo/dynamic_react/L1_inject/inject/combine\"\n)\n\n" +
			"var _ contract.Message\nvar _ core.Step\nvar _ = combine.TurnFinish\n" + body + "\n"
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
		os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644)
		cmd := exec.Command(gobin, "build", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	t.Run("control compiles", func(t *testing.T) {
		if out, err := build(t, compileOK); err != nil {
			t.Fatalf("control failed to build: %v\n%s", err, out)
		}
	})
	for name, body := range mustNotCompile {
		t.Run(name, func(t *testing.T) {
			out, err := build(t, body)
			if err == nil {
				t.Fatal("compiled")
			}
			if !strings.Contains(out, "p.go") {
				t.Fatalf("failed for another reason:\n%s", out)
			}
		})
	}
}
