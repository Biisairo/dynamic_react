// Package tools 는 도구 7종(read, bash, edit, write, grep, find, ls)의 로직이다. 파일·프로세스는 env.Env 로만 접근한다 (HN-C1, 3.2).
package tools

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

const (
	maxLines = 2000
	maxBytes = 50 * 1024
)

var snippets = map[string]string{
	"read":  "Read file contents",
	"bash":  "Execute bash commands (ls, grep, find, etc.)",
	"edit":  "Make precise file edits with exact text replacement, including multiple disjoint edits in one call",
	"write": "Create or overwrite files",
	"grep":  "Search file contents for patterns",
	"find":  "Find files by glob pattern",
	"ls":    "List directory contents",
}

// Snippet 은 시스템 프롬프트에 쓰는 도구 한 줄 설명이다. 모르는 이름은 "" (HN-FR-09).
func Snippet(name string) string { return snippets[name] }

// All 은 read, bash, edit, write, grep, find, ls 순서의 도구다. write·edit 가 변경 큐 하나를 공유한다 (HN-FR-10).
func All(e env.Env) []sets.Tool {
	q := newQueue()
	return []sets.Tool{Read(e), Bash(e), editTool(e, q), writeTool(e, q), Grep(e), Find(e), Ls(e)}
}

// Write 는 write 도구다. 자신만의 변경 큐를 쓴다.
func Write(e env.Env) sets.Tool { return writeTool(e, newQueue()) }

// Edit 는 edit 도구다. 자신만의 변경 큐를 쓴다.
func Edit(e env.Env) sets.Tool { return editTool(e, newQueue()) }

func identity(r contract.ToolResult) contract.ToolResult { return r }

// queue 는 파일 경로별 변경 잠금이다 (pi file-mutation-queue).
type queue struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newQueue() *queue { return &queue{locks: map[string]*sync.Mutex{}} }

func (q *queue) lock(p string) func() {
	q.mu.Lock()
	l := q.locks[p]
	if l == nil {
		l = &sync.Mutex{}
		q.locks[p] = l
	}
	q.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// Resolve 는 인자 경로를 절대 경로로 푼다 (HN-FR-05).
func Resolve(e env.Env, p string) (string, error) {
	switch {
	case p == "":
		return "", fmt.Errorf("path is required")
	case p == "~" || strings.HasPrefix(p, "~/"):
		if e.Home() == "" {
			return "", fmt.Errorf("cannot resolve %s: home directory unknown", p)
		}
		return path.Join(e.Home(), p[1:]), nil
	case path.IsAbs(p):
		return path.Clean(p), nil
	}
	return path.Join(e.Cwd(), p), nil
}

// display 는 결과 문구의 경로다. cwd 안이면 상대 경로, 밖이면 절대 경로.
func display(e env.Env, abs string) string {
	cwd := e.Cwd()
	if abs == cwd {
		return "."
	}
	prefix := strings.TrimSuffix(cwd, "/") + "/"
	if strings.HasPrefix(abs, prefix) {
		return abs[len(prefix):]
	}
	return abs
}

func textBlock(s string) contract.Block { return contract.Block{Kind: contract.BlockText, Text: s} }

func okResult(id string, blocks ...contract.Block) contract.ToolResult {
	return contract.ToolResult{CallID: id, Status: contract.StatusOK, Content: blocks}
}

func errResult(id, msg string) contract.ToolResult {
	return contract.ToolResult{CallID: id, Status: contract.StatusError, Content: []contract.Block{textBlock(msg)}}
}

func decode(call contract.ToolCall, v any) error {
	if err := json.Unmarshal(call.Args, v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// formatSize 는 pi formatSize 와 같은 표기다.
func formatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}
