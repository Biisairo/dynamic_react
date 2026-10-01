// Command dr-print 는 완성된 에이전트를 프롬프트 하나로 실행하는 검증용 CLI 다 (dr-print SPEC).
package main

import (
	"bytes"
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
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
)

// maxArgsBytes 는 [tool] 줄에 보이는 인자 JSON 의 최대 길이다 (DP-FR-06).
const maxArgsBytes = 200

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWith(ctx, args, stdin, stdout, stderr, nil)
}

// runWith 는 run 이다. agentOptions 는 테스트가 agent.Options 를 고치는 통로다 (SPEC 4.1).
func runWith(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, agentOptions func(*agent.Options)) int {
	flags := flag.NewFlagSet("dr-print", flag.ContinueOnError)
	flags.SetOutput(stderr)
	model := flags.String("model", "", "provider/id (overrides config agent.model)")
	sessionID := flags.String("session", "", "session id to continue")
	cwd := flags.String("cwd", "", "working directory")
	config := flags.String("config", "", "config file")
	jsonOut := flags.Bool("json", false, "print observer events as JSON lines")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: dr-print [-model provider/id] [-session id] [-cwd dir] [-config file] [-json] [prompt ...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	prompt := strings.Join(flags.Args(), " ")
	if flags.NArg() == 0 {
		b, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "dr-print: stdin: %v\n", err)
			return 1
		}
		prompt = string(b)
	}
	if prompt = strings.TrimSpace(prompt); prompt == "" {
		fmt.Fprintln(stderr, "dr-print: empty prompt")
		return 1
	}

	p := newPrinter(*jsonOut, stdout, stderr)
	o := agent.Options{Cwd: *cwd, ConfigPath: *config, Model: *model, Observer: p}
	if agentOptions != nil {
		agentOptions(&o)
	}
	a, err := agent.New(ctx, o)
	if err != nil {
		fmt.Fprintf(stderr, "dr-print: %v\n", err)
		return 1
	}
	r, err := a.Start(ctx, *sessionID, []contract.Message{userMessage(prompt)})
	if err != nil {
		fmt.Fprintf(stderr, "dr-print: %v\n", err)
		return 1
	}
	p.release("session: " + r.SessionID() + "\n")
	settle := r.Wait()
	p.finish()
	if settle.Reason == core.SettleFailed || settle.Reason == core.SettleGuard {
		fmt.Fprintf(stderr, "dr-print: %s: %v\n", settle.Reason, settle.Err)
	}
	return exitCode(settle.Reason)
}

// exitCode 는 DP-FR-08 이다.
func exitCode(reason core.SettleReason) int {
	switch reason {
	case core.SettleCompleted, core.SettleStopped:
		return 0
	case core.SettleGuard:
		return 2
	case core.SettleAborted:
		return 130
	}
	return 1
}

// userMessage 는 DP-FR-03 이다.
func userMessage(text string) contract.Message {
	var b [16]byte
	rand.Read(b[:])
	return contract.Message{ID: hex.EncodeToString(b[:]), Role: contract.RoleUser, Timestamp: time.Now(),
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

// printer 는 Observer 이벤트를 출력한다. session 줄이 stderr 의 첫 줄이 되도록 release 전까지 stderr 를 모아 둔다.
type printer struct {
	mu     sync.Mutex
	json   bool
	stdout io.Writer
	stderr io.Writer
	held   *bytes.Buffer
	last   byte
	wrote  bool
}

func newPrinter(jsonOut bool, stdout, stderr io.Writer) *printer {
	return &printer{json: jsonOut, stdout: stdout, stderr: stderr, held: &bytes.Buffer{}}
}

func (p *printer) errLine(line string) {
	if p.held != nil {
		p.held.WriteString(line)
		return
	}
	io.WriteString(p.stderr, line)
}

func (p *printer) release(first string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	io.WriteString(p.stderr, first)
	p.stderr.Write(p.held.Bytes())
	p.held = nil
}

// finish 는 stdout 텍스트가 줄바꿈으로 끝나게 한다 (DP-FR-06).
func (p *printer) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held != nil {
		p.stderr.Write(p.held.Bytes())
		p.held = nil
	}
	if p.wrote && p.last != '\n' {
		io.WriteString(p.stdout, "\n")
	}
}

func (p *printer) Observe(ev core.ObserverEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.json {
		b, _ := json.Marshal(toJSON(ev))
		p.stdout.Write(append(b, '\n'))
		return
	}
	if ev.Kind != core.ObsStreamEvent || ev.Stream == nil {
		return
	}
	s := ev.Stream
	switch {
	case s.Kind == contract.EventTextDelta && s.Delta != "":
		io.WriteString(p.stdout, s.Delta)
		p.last, p.wrote = s.Delta[len(s.Delta)-1], true
	case s.Kind == contract.EventToolCallEnd && s.ToolCall != nil:
		p.errLine("[tool] " + s.ToolCall.Name + " " + truncate(string(s.ToolCall.Args), maxArgsBytes) + "\n")
	case s.Kind == contract.EventError && s.Error != nil:
		p.errLine(fmt.Sprintf("[error] %s: %s\n", s.Error.Class, s.Error.Message))
	}
}

// truncate 는 s 를 n byte 이하로 자르고 "…" 를 붙인다. UTF-8 문자 중간에서 자르지 않는다.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// jsonEvent 는 -json 한 줄이다 (SPEC 3.1).
type jsonEvent struct {
	Kind       core.ObserverEventKind `json:"kind"`
	RunID      string                 `json:"runId,omitempty"`
	Step       string                 `json:"step,omitempty"`
	Seq        int                    `json:"seq,omitempty"`
	Unit       core.Unit              `json:"unit,omitempty"`
	DurationMs int64                  `json:"durationMs,omitempty"`
	Err        string                 `json:"err,omitempty"`
	Signals    []jsonSignal           `json:"signals,omitempty"`
	Stream     *contract.StreamEvent  `json:"stream,omitempty"`
	Settle     *jsonSettle            `json:"settle,omitempty"`
}

type jsonSignal struct {
	Kind   core.SignalKind `json:"kind"`
	Reason string          `json:"reason,omitempty"`
	Err    string          `json:"err,omitempty"`
}

type jsonSettle struct {
	Reason core.SettleReason `json:"reason"`
	Err    string            `json:"err,omitempty"`
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func toJSON(ev core.ObserverEvent) jsonEvent {
	j := jsonEvent{Kind: ev.Kind, RunID: ev.RunID, Seq: ev.Seq, Unit: ev.Unit,
		DurationMs: ev.Duration.Milliseconds(), Err: errText(ev.Err), Stream: ev.Stream}
	if ev.Step.Valid() {
		j.Step = ev.Step.String()
	}
	for _, s := range ev.Signals {
		j.Signals = append(j.Signals, jsonSignal{Kind: s.Kind, Reason: s.Reason, Err: errText(s.Err)})
	}
	if ev.Settle != nil {
		j.Settle = &jsonSettle{Reason: ev.Settle.Reason, Err: errText(ev.Settle.Err)}
	}
	return j
}
