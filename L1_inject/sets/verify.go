package sets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// CheckResult 는 검증 한 번의 결과다 (3.2a).
type CheckResult struct {
	OK     bool
	Report string
}

// VerifyOptions 는 verify 세트의 설정이다 (3.2a).
type VerifyOptions struct {
	Check       func(ctx context.Context) (CheckResult, error)
	MaxAttempts int
	Tools       []string
}

// ErrVerifyFailed 는 검증이 MaxAttempts 번 실패해 run 이 끝났다는 뜻이다 (ST-FR-48).
var ErrVerifyFailed = errors.New("verify: checks still failing")

const (
	verifyAttempts = 3
	keyVerify      = "progress"
	reportMaxLines = 2000
	reportMaxBytes = 50 * 1024
)

var verifyTools = []string{"write", "edit", "bash"}

type verifyState struct {
	Run        string `json:"run"`
	Checked    int    `json:"checked"`
	Failures   int    `json:"failures"`
	LastFailed bool   `json:"lastFailed"`
}

// Verify 는 turn.finish 만 채운 자기검증 세트다 (ST-FR-46~50).
func Verify(d Deps, o VerifyOptions) (inject.FunctionSet, error) {
	if o.Check == nil {
		return inject.FunctionSet{}, errors.New("sets: verify: Check is required")
	}
	d = d.filled()
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = verifyAttempts
	}
	if o.Tools == nil {
		o.Tools = verifyTools
	}
	return inject.FunctionSet{Name: "verify", Description: "run checks after changes before settling", TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
		sigs, err := ctx.Next()
		if err != nil {
			return nil, err
		}
		rest := slices.DeleteFunc(slices.Clone(sigs), func(s inject.TurnFinishSignal) bool { return s.Kind() == core.SigSettle })
		if len(rest) == len(sigs) {
			return sigs, nil
		}
		st, conv := ctx.State(), ctx.Conv()
		vs, _ := getJSON[verifyState](st, keyVerify)
		if vs.Run != ctx.Flow().RunID() {
			vs = verifyState{Run: ctx.Flow().RunID(), Checked: len(conv.History())}
		}
		transcript := conv.Transcript()
		if !vs.LastFailed && !changed(transcript[min(vs.Checked, len(transcript)):], transcript, o.Tools) {
			return sigs, nil
		}
		vs.Checked = len(transcript)
		res, err := o.Check(ctx.Context())
		if err != nil {
			return []inject.TurnFinishSignal{ctx.Fail(fmt.Errorf("verify: check: %w", err))}, nil
		}
		vs.LastFailed = !res.OK
		if res.OK {
			return sigs, st.Set(keyVerify, vs)
		}
		vs.Failures++
		if err := st.Set(keyVerify, vs); err != nil {
			return nil, err
		}
		if vs.Failures >= o.MaxAttempts {
			return []inject.TurnFinishSignal{ctx.Fail(fmt.Errorf("%w after %d attempts: %s", ErrVerifyFailed, vs.Failures, firstLine(res.Report)))}, nil
		}
		msg := contract.Message{ID: d.NewID(), Role: contract.RoleUser, Timestamp: d.Now(), Content: []contract.Block{{Kind: contract.BlockText,
			Text: fmt.Sprintf("Verification failed (attempt %d of %d). Fix the problems below, then finish.\n\n%s", vs.Failures, o.MaxAttempts, reportTail(res.Report))}}}
		return append(rest, ctx.InjectInput(msg), ctx.ContinueTurn()), nil
	}}, nil
}

// changed 는 msgs 에 tools 에 든 도구의 ok 결과가 있는지 본다. 호출 이름은 transcript 의 tool_call 에서 찾는다.
func changed(msgs, transcript []contract.Message, tools []string) bool {
	names := map[string]string{}
	for _, m := range transcript {
		for _, b := range m.Content {
			if b.Kind == contract.BlockToolCall && b.ToolCall != nil {
				names[b.ToolCall.ID] = b.ToolCall.Name
			}
		}
	}
	for _, m := range msgs {
		if m.Result != nil && m.Result.Status == contract.StatusOK && slices.Contains(tools, names[m.Result.CallID]) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// reportTail 은 보고의 끝 2000줄 / 50KB 다 (ST-FR-49).
func reportTail(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "(no output)"
	}
	lines := strings.Split(s, "\n")
	n, size := 0, 0
	for i := len(lines) - 1; i >= 0 && n < reportMaxLines; i-- {
		add := len(lines[i])
		if n > 0 {
			add++
		}
		if size+add > reportMaxBytes {
			if n == 0 {
				return "[earlier output truncated]\n" + lastBytes(lines[i], reportMaxBytes)
			}
			break
		}
		n, size = n+1, size+add
	}
	if n == len(lines) {
		return s
	}
	return "[earlier output truncated]\n" + strings.Join(lines[len(lines)-n:], "\n")
}

// lastBytes 는 s 의 끝 n 바이트 이하를 UTF-8 경계에서 자른다.
func lastBytes(s string, n int) string {
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
