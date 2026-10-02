// Command verify-example 는 완성된 에이전트에 자기검증 세트를 끼우는 예시다 (examples/verify SPEC, R24).
// 모델이 파일을 바꾼 뒤 끝내려 하면 셸 명령으로 검증하고, 실패하면 출력을 모델에 돌려준다.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	"github.com/Biisairo/dynamic_react/L3_ui/surfaces/terminal"
)

// ShellCheck 은 command 를 dir 에서 /bin/sh 로 실행하는 Check 다 (VX-FR-01).
func ShellCheck(dir, command string, log io.Writer) func(ctx context.Context) (sets.CheckResult, error) {
	return func(ctx context.Context) (sets.CheckResult, error) {
		fmt.Fprintf(log, "[verify] %s\n", command)
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &out
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			fmt.Fprintf(log, "[verify] failed (exit %d)\n", exit.ExitCode())
			return sets.CheckResult{Report: out.String()}, nil
		}
		if err != nil {
			return sets.CheckResult{}, err
		}
		fmt.Fprintln(log, "[verify] ok")
		return sets.CheckResult{OK: true, Report: out.String()}, nil
	}
}

func newID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// hooks 는 테스트가 바꿔 넣는 것들이다. stdin·stdout 은 -i 의 TTY 다 (VX-FR-05).
type hooks struct {
	stdin, stdout *os.File
	isTerminal    func(fd int) bool
	open          func(*os.File) (func() error, func() int, error)
	agentOptions  func(*agent.Options)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWith(ctx, args, stdout, stderr, hooks{stdin: os.Stdin, stdout: os.Stdout})
}

func runWith(ctx context.Context, args []string, stdout, stderr io.Writer, h hooks) int {
	fl := flag.NewFlagSet("verify-example", flag.ContinueOnError)
	fl.SetOutput(stderr)
	check := fl.String("check", "", "shell command that verifies the work")
	attempts := fl.Int("attempts", 0, "verification attempts before the run fails (0 = default 3)")
	model := fl.String("model", "", "provider/id")
	cwd := fl.String("cwd", "", "working directory")
	config := fl.String("config", "", "config file")
	interactive := fl.Bool("i", false, "interactive terminal")
	noColor := fl.Bool("no-color", false, "no ANSI colors (-i)")
	if err := fl.Parse(args); err != nil {
		return 1
	}
	if strings.TrimSpace(*check) == "" {
		fmt.Fprintln(stderr, "verify-example: -check is required")
		return 1
	}
	verifySets := func(log io.Writer) func(d sets.Deps) []inject.FunctionSet {
		opts := sets.VerifyOptions{Check: ShellCheck(*cwd, *check, log), MaxAttempts: *attempts}
		return func(d sets.Deps) []inject.FunctionSet {
			v, err := sets.Verify(d, opts)
			if err != nil {
				panic(err)
			}
			return append(harness.DefaultSets(d), v)
		}
	}
	if *interactive {
		if fl.NArg() > 0 {
			fmt.Fprintln(stderr, "verify-example: -i takes no prompt arguments")
			return 2
		}
		o := agent.Options{Cwd: *cwd, ConfigPath: *config, Model: *model, Sets: verifySets(io.Discard)}
		return runInteractive(ctx, o, !*noColor, stderr, h)
	}
	prompt := strings.TrimSpace(strings.Join(fl.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(stderr, "verify-example: empty prompt")
		return 1
	}
	var wrote, endsNL = false, true
	o := agent.Options{Cwd: *cwd, ConfigPath: *config, Model: *model, Sets: verifySets(stderr),
		OnEvent: func(ev harness.Event) {
			switch {
			case ev.Kind == harness.EventMessageUpdate && ev.Delta.Kind == contract.EventTextDelta && ev.Delta.Delta != "":
				io.WriteString(stdout, ev.Delta.Delta)
				wrote, endsNL = true, strings.HasSuffix(ev.Delta.Delta, "\n")
			case ev.Kind == harness.EventMessageEnd && wrote && !endsNL:
				io.WriteString(stdout, "\n")
				endsNL = true
			case ev.Kind == harness.EventToolEnd:
				fmt.Fprintf(stderr, "[tool] %s %s\n", ev.Call.Name, ev.Result.Status)
			}
		}}
	if h.agentOptions != nil {
		h.agentOptions(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		fmt.Fprintf(stderr, "verify-example: %v\n", err)
		return 1
	}
	in := contract.Message{ID: newID(), Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockText, Text: prompt}}}
	r, err := a.Start(ctx, "", []contract.Message{in})
	if err != nil {
		fmt.Fprintf(stderr, "verify-example: %v\n", err)
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
	fmt.Fprintf(stderr, "verify-example: %s: %v\n", s.Reason, s.Err)
	return 1
}

// runInteractive 는 dr 과 같은 대화형 터미널에 verify 세트를 끼운다 (VX-FR-05).
func runInteractive(ctx context.Context, o agent.Options, color bool, stderr io.Writer, h hooks) int {
	if h.isTerminal == nil {
		h.isTerminal = terminal.IsTerminal
	}
	if h.open == nil {
		h.open = terminal.Open
	}
	if h.stdin == nil || h.stdout == nil || !h.isTerminal(int(h.stdin.Fd())) || !h.isTerminal(int(h.stdout.Fd())) {
		fmt.Fprintln(stderr, "verify-example: -i needs an interactive terminal")
		return 2
	}
	restore, width, err := h.open(h.stdin)
	if err != nil {
		fmt.Fprintf(stderr, "verify-example: %v\n", err)
		return 1
	}
	defer restore()
	t := terminal.New(terminal.Options{In: h.stdin, Out: h.stdout, Width: width, Color: color})
	o.Approver, o.OnEvent = t.Approver(), t.OnEvent
	if h.agentOptions != nil {
		h.agentOptions(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		restore()
		fmt.Fprintf(stderr, "verify-example: %v\n", err)
		return 1
	}
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer func() {
		signal.Stop(winch)
		close(winch)
	}()
	go func() {
		for range winch {
			t.Resize()
		}
	}()
	err = t.Run(ctx, a, terminal.Start{})
	restore()
	switch {
	case err == nil:
		return 0
	case ctx.Err() != nil:
		return 130
	}
	fmt.Fprintf(stderr, "verify-example: %v\n", err)
	return 1
}
