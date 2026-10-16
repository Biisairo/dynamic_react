// dr 은 대화형 터미널 진입점이다 (L3_ui/cmd/dr/SPEC.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L3_ui/surfaces/terminal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// hooks 는 테스트가 바꿔 끼우는 부분이다 (DR-C2). nil 이면 실제 구현.
type hooks struct {
	isTerminal   func(fd int) bool
	open         func(*os.File) (func() error, func() int, error)
	agentOptions func(*agent.Options)
}

func run(ctx context.Context, args []string, stdin, stdout *os.File, stderr io.Writer) int {
	return runWith(ctx, args, stdin, stdout, stderr, hooks{})
}

func runWith(ctx context.Context, args []string, stdin, stdout *os.File, stderr io.Writer, h hooks) int {
	if h.isTerminal == nil {
		h.isTerminal = terminal.IsTerminal
	}
	if h.open == nil {
		h.open = terminal.Open
	}
	fs := flag.NewFlagSet("dr", flag.ContinueOnError)
	fs.SetOutput(stderr)
	model := fs.String("model", "", "provider/id")
	cwd := fs.String("cwd", "", "작업 디렉터리")
	config := fs.String("config", "", "설정 파일")
	session := fs.String("session", "", "이어갈 세션 ID")
	cont := fs.Bool("c", false, "가장 최근 세션 이어가기")
	pick := fs.Bool("r", false, "세션 목록에서 고르기")
	noColor := fs.Bool("no-color", false, "색을 쓰지 않음")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exclusive := 0
	for _, b := range []bool{*session != "", *cont, *pick} {
		if b {
			exclusive++
		}
	}
	if fs.NArg() > 0 || exclusive > 1 {
		fmt.Fprintln(stderr, "dr: 위치 인자는 받지 않고, -session·-c·-r 은 하나만 쓸 수 있습니다")
		fs.Usage()
		return 2
	}
	if !h.isTerminal(int(stdin.Fd())) || !h.isTerminal(int(stdout.Fd())) {
		fmt.Fprintln(stderr, "dr: 대화형 터미널이 필요합니다 (비대화형은 dr-print)")
		return 2
	}
	restore, width, err := h.open(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "dr: %v\n", err)
		return 1
	}
	defer func() { _ = restore() }()
	t := terminal.New(terminal.Options{In: stdin, Out: stdout, Width: width, Color: !*noColor})
	o := agent.Options{Model: *model, Cwd: *cwd, ConfigPath: *config, Approver: t.Approver(), OnEvent: t.OnEvent}
	if h.agentOptions != nil {
		h.agentOptions(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		_ = restore()
		fmt.Fprintf(stderr, "dr: %v\n", err)
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
	err = t.Run(ctx, a, terminal.Start{Session: *session, Continue: *cont, Pick: *pick})
	_ = restore()
	switch {
	case err == nil:
		return 0
	case ctx.Err() != nil:
		return 130
	}
	fmt.Fprintf(stderr, "dr: %v\n", err)
	return 1
}
