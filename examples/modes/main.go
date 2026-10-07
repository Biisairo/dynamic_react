// Command modes-example 는 R27 모드 메커니즘으로 goal·plan 모드를 만드는 예시다 (examples/modes SPEC).
// /mode goal <목표> 로 판정 루프를, /mode plan <과제> 로 쓰기 도구를 막은 계획 턴을 연다.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L3_ui/surfaces/terminal"
)

const defaultGoalTurns = 20

// config 는 플래그에서 정한 세트·모드 구성이다 (MX-FR-08, 09).
type config struct {
	goalTurns int
	check     func(ctx context.Context) (sets.CheckResult, error) // nil = 품질 게이트 없음
	attempts  int
}

func (c config) sets(d sets.Deps) []harness.SetEntry {
	out := append(harness.DefaultSets(d),
		harness.SetEntry{Set: GoalSet(d, c.goalTurns)},
		harness.SetEntry{Set: PlanSet(d)})
	if c.check != nil {
		v, err := sets.Verify(d, sets.VerifyOptions{Check: c.check, MaxAttempts: c.attempts})
		if err != nil {
			panic(err)
		}
		out = append(out, harness.SetEntry{Set: v})
	}
	return out
}

func (c config) modes() []harness.Mode {
	goal := []string{"goal"}
	if c.check != nil {
		goal = []string{"verify", "goal"}
	}
	return []harness.Mode{
		{Name: "goal", Description: "keep working until a judge says the goal is met (arg = goal)", Push: goal},
		{Name: "plan", Description: "plan only; write/edit/bash are blocked (arg = task)", Push: []string{"plan"}},
	}
}

// shellCheck 은 command 를 dir 에서 /bin/sh 로 실행한다. 0 이 아닌 종료는 실패 보고다.
func shellCheck(dir, command string) func(ctx context.Context) (sets.CheckResult, error) {
	return func(ctx context.Context) (sets.CheckResult, error) {
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &out
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) && ctx.Err() == nil {
			return sets.CheckResult{Report: out.String()}, nil
		}
		if err != nil {
			return sets.CheckResult{}, err
		}
		return sets.CheckResult{OK: true, Report: out.String()}, nil
	}
}

// hooks 는 테스트가 바꿔 넣는 것들이다 (examples/verify 와 같음).
type hooks struct {
	stdin, stdout *os.File
	isTerminal    func(fd int) bool
	open          func(*os.File) (func() error, func() int, error)
	agentOptions  func(*agent.Options)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runWith(ctx, os.Args[1:], os.Stderr, hooks{stdin: os.Stdin, stdout: os.Stdout})
	stop()
	os.Exit(code)
}

func runWith(ctx context.Context, args []string, stderr io.Writer, h hooks) int {
	fl := flag.NewFlagSet("modes-example", flag.ContinueOnError)
	fl.SetOutput(stderr)
	check := fl.String("check", "", "shell command run before the goal judge (quality gate)")
	attempts := fl.Int("attempts", 0, "verification attempts before the run fails (0 = default 3)")
	goalTurns := fl.Int("goal-turns", defaultGoalTurns, "goal turn budget")
	model := fl.String("model", "", "provider/id")
	cwd := fl.String("cwd", "", "working directory")
	cfgPath := fl.String("config", "", "config file")
	noColor := fl.Bool("no-color", false, "no ANSI colors")
	if err := fl.Parse(args); err != nil {
		return 1
	}
	if fl.NArg() > 0 {
		fmt.Fprintln(stderr, "modes-example: takes no prompt arguments")
		return 2
	}
	c := config{goalTurns: *goalTurns, attempts: *attempts}
	if *check != "" {
		dir := *cwd
		if dir == "" {
			dir, _ = os.Getwd()
		}
		c.check = shellCheck(dir, *check)
	}
	if h.isTerminal == nil {
		h.isTerminal = terminal.IsTerminal
	}
	if h.open == nil {
		h.open = terminal.Open
	}
	if h.stdin == nil || h.stdout == nil || !h.isTerminal(int(h.stdin.Fd())) || !h.isTerminal(int(h.stdout.Fd())) {
		fmt.Fprintln(stderr, "modes-example: needs an interactive terminal")
		return 2
	}
	restore, width, err := h.open(h.stdin)
	if err != nil {
		fmt.Fprintf(stderr, "modes-example: %v\n", err)
		return 1
	}
	defer restore()
	t := terminal.New(terminal.Options{In: h.stdin, Out: h.stdout, Width: width, Color: !*noColor})
	o := agent.Options{Cwd: *cwd, ConfigPath: *cfgPath, Model: *model, Sets: c.sets, Modes: c.modes(),
		Approver: t.Approver(), OnEvent: t.OnEvent}
	if h.agentOptions != nil {
		h.agentOptions(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		restore()
		fmt.Fprintf(stderr, "modes-example: %v\n", err)
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
	fmt.Fprintf(stderr, "modes-example: %v\n", err)
	return 1
}
