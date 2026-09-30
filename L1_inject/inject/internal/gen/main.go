// gen 은 StepSpec(internal/stepspec)과 core 의 허용 표로 inject 의 step 별 타입을 생성한다 (IN-FR-03).
//
//	go run ./internal/gen [-dir .]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/internal/stepspec"
)

// Files 는 생성 대상 파일(dir 기준 상대 경로)이다.
var files = []struct {
	path string
	tmpl string
}{
	{"zz_generated.go", injectTmpl},
	{filepath.Join("injecttest", "zz_generated.go"), injecttestTmpl},
}

func main() {
	dir := flag.String("dir", ".", "inject package directory")
	flag.Parse()
	out, err := Generate(stepspec.Rows())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	for path, src := range out {
		if err := os.WriteFile(filepath.Join(*dir, path), src, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "gen:", err)
			os.Exit(1)
		}
	}
}

// Check 는 StepSpec 이 core 의 step 목록과 같은지 검사한다 (IN-FR-02).
func Check(rows []stepspec.Row) error {
	steps := core.Steps()
	if len(rows) != len(steps) {
		return fmt.Errorf("stepspec has %d rows, core has %d steps", len(rows), len(steps))
	}
	for i, r := range rows {
		if r.Step != steps[i] {
			return fmt.Errorf("row %d: step %s, core has %s", i, r.Step, steps[i])
		}
		if want := camel(steps[i].String()); r.Name != want {
			return fmt.Errorf("row %d: name %q, want %q", i, r.Name, want)
		}
		if r.NoSignals != (len(core.AllowedSignals(r.Step)) == 0) {
			return fmt.Errorf("row %d: NoSignals %v disagrees with core allowed table", i, r.NoSignals)
		}
	}
	return nil
}

func camel(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		if r == '.' {
			up = true
			continue
		}
		if up {
			r = unicode.ToUpper(r)
			up = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func lowerFirst(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

type ctor struct {
	Method, Params, Payload string
	Kind                    string // core 상수 이름
}

// ctors 는 신호 종류별 ctx 생성자다 (IN-FR-45).
func ctorOf(k core.SignalKind) ctor {
	c := ctor{Method: camel(strings.ReplaceAll(string(k), "_", ".")), Kind: "core.Sig" + camel(strings.ReplaceAll(string(k), "_", "."))}
	switch k {
	case core.SigSkipTools:
		c.Params, c.Payload = "results []contract.ToolResult", "Results: cloneResults(results)"
	case core.SigFail:
		c.Params, c.Payload = "err error", "Err: err"
	case core.SigRewrite:
		c.Params, c.Payload = "edit contract.ContextEdit", "Edit: &edit"
	case core.SigInjectInput:
		c.Params, c.Payload = "msg contract.Message", "Message: &msg"
	case core.SigStartTool:
		c.Params, c.Payload = "call contract.ToolCall", "Call: &call"
	}
	return c
}

type row struct {
	stepspec.Row
	Const, Lower string
	Ctors        []ctor
	FnParams     string // ", in core.RunInput"
	FnArgs       string // ", in" / ", in.Call, in.Result"
	FnResults    string
	StubParams   string // injecttest 용: ", _ core.RunInput"
	StubResults  string // injecttest 용: 신호 타입에 inject. 접두어
	StubReturn   string
}

func (r row) StepConst() string { return r.Const }

func build(rows []stepspec.Row) []row {
	out := make([]row, len(rows))
	for i, r := range rows {
		x := row{Row: r, Const: "core.Step" + r.Name, Lower: lowerFirst(r.Name)}
		for _, k := range core.AllowedSignals(r.Step) {
			x.Ctors = append(x.Ctors, ctorOf(k))
		}
		var params, args, stub []string
		for _, p := range r.Params {
			params = append(params, p.Name+" "+p.Type)
			stub = append(stub, "_ "+p.Type)
			if p.Field == "" {
				args = append(args, "in")
			} else {
				args = append(args, "in."+p.Field)
			}
		}
		if len(params) > 0 {
			x.FnParams = ", " + strings.Join(params, ", ")
			x.FnArgs = ", " + strings.Join(args, ", ")
			x.StubParams = ", " + strings.Join(stub, ", ")
		}
		var res []string
		if r.Output != "" {
			res = append(res, r.Output)
		}
		if !r.NoSignals {
			res = append(res, "[]"+r.Name+"Signal")
		}
		res = append(res, "error")
		x.FnResults = results(res)
		for i, t := range res {
			if strings.HasSuffix(t, "Signal") {
				res[i] = "[]inject." + strings.TrimPrefix(t, "[]")
			}
		}
		x.StubResults = results(res)
		var zeros []string
		if r.Output != "" {
			zeros = append(zeros, zeroOf(r.Output))
		}
		if !r.NoSignals {
			zeros = append(zeros, "nil")
		}
		zeros = append(zeros, "missing("+x.Const+")")
		x.StubReturn = strings.Join(zeros, ", ")
		out[i] = x
	}
	return out
}

func results(res []string) string {
	if len(res) == 1 {
		return res[0]
	}
	return "(" + strings.Join(res, ", ") + ")"
}

func zeroOf(t string) string {
	if strings.HasPrefix(t, "[]") {
		return "nil"
	}
	return t + "{}"
}

// Generate 는 파일 경로 → gofmt 된 소스를 돌려준다.
func Generate(rows []stepspec.Row) (map[string][]byte, error) {
	if err := Check(rows); err != nil {
		return nil, err
	}
	data := struct{ Rows []row }{build(rows)}
	out := map[string][]byte{}
	for _, f := range files {
		t, err := template.New(f.path).Parse(f.tmpl)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return nil, err
		}
		src, err := format.Source(buf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("%s: %w\n%s", f.path, err, buf.Bytes())
		}
		out[f.path] = src
	}
	return out, nil
}

const header = "// Code generated by internal/gen from internal/stepspec; DO NOT EDIT.\n\n"

const injectTmpl = header + `package inject

import (
	"context"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)
{{range .Rows}}
// ---- {{.Step}} ----
{{if not .NoSignals}}
// {{.Name}}Signal 은 {{.Step}} 에서만 낼 수 있는 신호다. ctx 의 생성자로만 만든다.
type {{.Name}}Signal interface {
	Kind() core.SignalKind
	WithReason(reason string) {{.Name}}Signal
	raw() sig
}

type {{.Lower}}Signal struct{ sig }

func (x {{.Lower}}Signal) WithReason(reason string) {{.Name}}Signal {
	x.s.Reason = reason
	return x
}

func (x {{.Lower}}Signal) raw() sig { return x.sig }
{{end}}
// {{.Name}}Ctx 는 {{.Step}} dispatch 한 번의 ctx 다.
type {{.Name}}Ctx struct{ b *base }

func (c *{{.Name}}Ctx) Context() context.Context { return c.b.context() }
{{$r := .}}{{range .Groups}}
func (c *{{$r.Name}}Ctx) {{.}}() {{.}} { return c.b.group{{.}}() }
{{end}}{{range .Ctors}}
func (c *{{$r.Name}}Ctx) {{.Method}}({{.Params}}) {{$r.Name}}Signal {
	return {{$r.Lower}}Signal{c.b.mk(core.Signal{Kind: {{.Kind}}{{if .Payload}}, {{.Payload}}{{end}}})}
}
{{end}}{{if not .NoSignals}}
// Dropped 는 채택하지 않은 신호를 트레이스에 남긴다 (IN-FR-49).
func (c *{{.Name}}Ctx) Dropped(sigs ...{{.Name}}Signal) {
	rs, _ := rawAll(sigs)
	c.b.drop(rs)
}
{{end}}
type {{.Name}}Fn func(ctx *{{.Name}}Ctx{{.FnParams}}) {{.FnResults}}
{{end}}
// FunctionSet 은 이름을 가진 주입 함수 묶음이다 (IN-FR-18).
type FunctionSet struct {
	Name string
{{range .Rows}}	{{.Name}} {{.Name}}Fn
{{end}}}

// overlay 는 src 의 nil 이 아닌 함수로 dst 를 덮고, 덮은 step 의 주인을 기록한다.
func overlay(dst *FunctionSet, src FunctionSet, owner map[core.Step]string) {
{{range .Rows}}	if src.{{.Name}} != nil {
		dst.{{.Name}} = src.{{.Name}}
		owner[{{.Const}}] = src.Name
	}
{{end}}}

// invoke 는 step 의 함수를 부른다.
func invoke(fns *FunctionSet, b *base, step core.Step, input any) (any, []sig, error) {
	switch step {
{{range .Rows}}	case {{.Const}}:
{{if .Input}}		in, ok := input.({{.Input}})
		if !ok {
			return nil, nil, inputErr(step, input)
		}
{{else}}		if input != nil {
			return nil, nil, inputErr(step, input)
		}
{{end}}{{if .NoSignals}}		return nil, nil, fns.{{.Name}}(&{{.Name}}Ctx{b}{{.FnArgs}})
{{else if .Output}}		out, s, err := fns.{{.Name}}(&{{.Name}}Ctx{b}{{.FnArgs}})
		return finish(out, s, err)
{{else}}		s, err := fns.{{.Name}}(&{{.Name}}Ctx{b}{{.FnArgs}})
		return finish(nil, s, err)
{{end}}{{end}}	}
	return nil, nil, inputErr(step, input)
}
`

const injecttestTmpl = header + `package injecttest

import (
	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// Fill 은 비어 있는 step 을 "제공되지 않음" 에러를 내는 함수로 채운다. 단위 테스트에서 Bind 를 통과시키는 용도다.
func Fill(s inject.FunctionSet) inject.FunctionSet {
{{range .Rows}}	if s.{{.Name}} == nil {
		s.{{.Name}} = func(_ *inject.{{.Name}}Ctx{{.StubParams}}) {{.StubResults}} { return {{.StubReturn}} }
	}
{{end}}	return s
}
`
