// Command inject-example 는 완성된 에이전트에 사용자 세트를 끼우는 예시다 (examples/inject SPEC, R22).
// 기본 세트 위에 세트 하나를 겹쳐 매 턴 규칙을 덧붙이고 `rm -rf` 가 든 bash 호출을 막는다.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// Rule 은 매 턴 덧붙이는 지시다 (EX-FR-02).
const Rule = "End every reply with the line: -- checked by example"

// ruleID 는 규칙 메시지의 ID 다. 임시 메시지라 저장·편집 대상이 아니므로 고정이다.
const ruleID = "example-rule"

// Sets 는 기본 세트 위에 Example 을 겹친다.
func Sets(d sets.Deps) []harness.SetEntry {
	return append(harness.DefaultSets(d), harness.SetEntry{Set: Example(d), On: true})
}

// Example 은 context.inject 와 tool.before 만 채운 세트다 (EX-FR-01~03).
func Example(d sets.Deps) inject.FunctionSet {
	return inject.FunctionSet{
		Name: "example",
		ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
			m := contract.Message{ID: ruleID, Role: contract.RoleUser, Timestamp: d.Now(),
				Content: []contract.Block{{Kind: contract.BlockText, Text: Rule}}}
			return []contract.Message{m}, nil, nil
		},
		ToolBefore: func(ctx *inject.ToolBeforeCtx, call contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
			var args struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(call.Args, &args)
			if call.Name == "bash" && strings.Contains(args.Command, "rm -rf") {
				return core.BeforeOut{Call: call, ShortCircuit: &contract.ToolResult{CallID: call.ID, Status: contract.StatusError,
					Content: []contract.Block{{Kind: contract.BlockText, Text: "blocked by example: rm -rf"}}}}, nil, nil
			}
			return ctx.Next(call)
		},
	}
}

func newID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWith(ctx, args, stdout, stderr, nil)
}

func runWith(ctx context.Context, args []string, stdout, stderr io.Writer, mod func(*agent.Options)) int {
	fl := flag.NewFlagSet("inject-example", flag.ContinueOnError)
	fl.SetOutput(stderr)
	model := fl.String("model", "", "provider/id")
	cwd := fl.String("cwd", "", "working directory")
	config := fl.String("config", "", "config file")
	if err := fl.Parse(args); err != nil {
		return 1
	}
	prompt := strings.TrimSpace(strings.Join(fl.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(stderr, "inject-example: empty prompt")
		return 1
	}
	var wrote, endsNL = false, true
	o := agent.Options{Cwd: *cwd, ConfigPath: *config, Model: *model, Sets: Sets,
		OnEvent: func(ev harness.Event) {
			switch {
			case ev.Kind == harness.EventMessageUpdate && ev.Delta.Kind == contract.EventTextDelta && ev.Delta.Delta != "":
				io.WriteString(stdout, ev.Delta.Delta)
				wrote, endsNL = true, strings.HasSuffix(ev.Delta.Delta, "\n")
			case ev.Kind == harness.EventToolEnd:
				fmt.Fprintf(stderr, "[tool] %s %s\n", ev.Call.Name, ev.Result.Status)
			}
		}}
	if mod != nil {
		mod(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		fmt.Fprintf(stderr, "inject-example: %v\n", err)
		return 1
	}
	in := contract.Message{ID: newID(), Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockText, Text: prompt}}}
	r, err := a.Start(ctx, "", []contract.Message{in})
	if err != nil {
		fmt.Fprintf(stderr, "inject-example: %v\n", err)
		return 1
	}
	s := r.Wait()
	if wrote && !endsNL {
		io.WriteString(stdout, "\n")
	}
	switch s.Reason {
	case core.SettleCompleted, core.SettleStopped:
		return 0
	case core.SettleAborted:
		return 130
	}
	fmt.Fprintf(stderr, "inject-example: %s: %v\n", s.Reason, s.Err)
	return 1
}
