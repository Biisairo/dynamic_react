package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

var bashSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"command":{"type":"string","description":"Bash command to execute"},` +
	`"timeout":{"type":"number","description":"Timeout in seconds (optional, no default timeout)"}},"required":["command"]}`)

const (
	maxTimeoutSeconds = math.MaxInt32 / 1000
	spillAt           = 2 * maxBytes // 이 크기를 넘으면 전체 출력을 임시 파일로 흘린다
	tailKeep          = 2 * maxBytes // spill 뒤 메모리에 남기는 끝부분
)

// Bash 는 bash 도구다. 자체 절단을 하므로 Truncate 는 항등이다 (HN-FR-07).
func Bash(e env.Env) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "bash", InputSchema: bashSchema, Description: fmt.Sprintf(
			"Execute a bash command in the current working directory. Returns combined stdout and stderr. "+
				"Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, full output is saved to a temp file. "+
				"Optionally provide a timeout in seconds.", maxLines, maxBytes/1024)},
		Run:      func(ctx context.Context, call contract.ToolCall) contract.ToolResult { return runBash(ctx, e, call) },
		Truncate: identity,
	}
}

func runBash(ctx context.Context, e env.Env, call contract.ToolCall) contract.ToolResult {
	var a struct {
		Command string   `json:"command"`
		Timeout *float64 `json:"timeout"`
	}
	if err := decode(call, &a); err != nil {
		return errResult(call.ID, err.Error())
	}
	var timeout time.Duration
	if a.Timeout != nil {
		t := *a.Timeout
		if math.IsNaN(t) || math.IsInf(t, 0) || t <= 0 || t > maxTimeoutSeconds {
			return errResult(call.ID, fmt.Sprintf("Invalid timeout: must be a finite number of seconds between 0 and %d", maxTimeoutSeconds))
		}
		timeout = time.Duration(t * float64(time.Second))
	}
	c := &capture{ctx: ctx, fs: e}
	if fn, ok := ctx.Value(progressKey{}).(func(callID, chunk string)); ok {
		c.progress = func(chunk string) { fn(call.ID, chunk) }
	}
	res, execErr := e.Exec(ctx, env.ExecRequest{Command: a.Command, Dir: e.Cwd(), Timeout: timeout}, c)
	text := c.finish()

	var status string
	switch {
	case execErr != nil && ctx.Err() != nil:
		status = "Command aborted"
	case execErr != nil:
		status = execErr.Error()
	case res.TimedOut:
		status = fmt.Sprintf("Command timed out after %s seconds", strconv.FormatFloat(*a.Timeout, 'f', -1, 64))
	case res.ExitCode != 0:
		status = fmt.Sprintf("Command exited with code %d", res.ExitCode)
	}
	if status != "" {
		if text != "" {
			status = text + "\n\n" + status
		}
		return errResult(call.ID, status)
	}
	if text == "" {
		text = "(no output)"
	}
	return okResult(call.ID, textBlock(text))
}

// capture 는 합친 출력을 받아 끝부분을 남기고, 크면 전체를 임시 파일로 흘린다.
type capture struct {
	ctx context.Context
	fs  env.FileSystem

	mu       sync.Mutex
	buf      []byte // spill 전: 전체, spill 뒤: 끝부분
	cut      bool   // buf 앞이 잘렸음
	newlines int
	total    int
	sinceNL  int // 마지막 개행 뒤의 바이트 수
	spill    io.WriteCloser
	path     string
	spillErr error
	progress func(chunk string) // HN-FR-33
	pending  []byte             // progress 로 아직 넘기지 않은 끊긴 문자
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += len(p)
	c.newlines += bytes.Count(p, []byte{'\n'})
	if i := bytes.LastIndexByte(p, '\n'); i >= 0 {
		c.sinceNL = len(p) - i - 1
	} else {
		c.sinceNL += len(p)
	}
	if c.spill != nil {
		c.spill.Write(p)
	}
	c.buf = append(c.buf, p...)
	if c.progress != nil {
		data := append(c.pending, p...)
		k := completeUpTo(data)
		if k > 0 {
			c.progress(string(data[:k]))
		}
		c.pending = append([]byte(nil), data[k:]...)
	}
	if c.spill == nil && c.spillErr == nil && len(c.buf) > spillAt {
		c.openSpill()
	}
	if c.spill != nil && len(c.buf) > tailKeep {
		c.buf = append([]byte(nil), c.buf[len(c.buf)-tailKeep:]...)
		c.cut = true
	}
	return len(p), nil
}

// openSpill 은 c.mu 를 잡은 상태에서 부른다. 지금까지의 전체 출력(buf)을 먼저 쓴다.
func (c *capture) openSpill() {
	c.path, c.spill, c.spillErr = c.fs.CreateTemp(c.ctx, "dr-bash-*.log")
	if c.spillErr != nil {
		c.spill = nil
		return
	}
	c.spill.Write(c.buf)
}

// finish 는 모델에 줄 출력이다. 잘렸으면 끝 2000줄 / 50KB 와 전체 출력 경로 안내 (pi bash).
func (c *capture) finish() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.progress != nil && len(c.pending) > 0 {
		c.progress(string(c.pending))
		c.pending = nil
	}
	if c.total == 0 {
		return ""
	}
	totalLines := c.newlines
	if c.buf[len(c.buf)-1] != '\n' {
		totalLines++
	}
	body := string(c.buf)
	if c.cut {
		if i := strings.IndexByte(body, '\n'); i >= 0 && i < len(body)-1 {
			body = body[i+1:]
		}
	}
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	t := truncateTail(lines)
	if !t.truncated && !c.cut {
		if c.spill != nil {
			c.spill.Close()
		}
		return string(c.buf)
	}
	if c.spill == nil && c.spillErr == nil {
		c.openSpill()
	}
	where := c.path
	if c.spill != nil {
		c.spill.Close()
	} else {
		where = fmt.Sprintf("(unavailable: %v)", c.spillErr)
	}
	start := totalLines - t.outputLines + 1
	var note string
	switch {
	case t.partial:
		size := c.sinceNL
		if size == 0 {
			size = len(lines[len(lines)-1])
		}
		note = fmt.Sprintf("[Showing last %s of line %d (line is %s). Full output: %s]", formatSize(len(t.content)), totalLines, formatSize(size), where)
	case t.byBytes:
		note = fmt.Sprintf("[Showing lines %d-%d of %d (%s limit). Full output: %s]", start, totalLines, totalLines, formatSize(maxBytes), where)
	default:
		note = fmt.Sprintf("[Showing lines %d-%d of %d. Full output: %s]", start, totalLines, totalLines, where)
	}
	return t.content + "\n\n" + note
}

type progressKey struct{}

// WithProgress 는 bash 가 출력 조각을 넘길 fn 을 ctx 에 싣는다 (HN-FR-33).
func WithProgress(ctx context.Context, fn func(callID, chunk string)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// completeUpTo 는 b 의 앞에서 끊기지 않은 UTF-8 문자까지의 길이다.
func completeUpTo(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return len(b)
			}
			return i
		}
	}
	return len(b)
}
