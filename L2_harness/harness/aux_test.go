package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
)

// HN-FR-86: aux 만 읽는다. 없음·null → 빈 맵, 형식 에러는 harness: config: aux 로 시작.
func TestParseAux(t *testing.T) {
	got, err := harness.ParseAux(json.RawMessage(`{"model":{"provider":"p","id":"m"},"other":1,` +
		`"aux":{"judge":{"provider":"q","id":"j","maxOutputTokens":64,"thinking":"low"}}}`))
	if err != nil || len(got) != 1 || got["judge"] != (harness.AuxModel{Provider: "q", ID: "j", MaxOutputTokens: 64, Thinking: "low"}) {
		t.Fatalf("ParseAux %+v %v", got, err)
	}
	for _, raw := range []string{`{"model":{"provider":"p","id":"m"}}`, `{"aux":null}`} {
		if got, err := harness.ParseAux(json.RawMessage(raw)); err != nil || len(got) != 0 {
			t.Errorf("%s: %+v %v", raw, got, err)
		}
	}
	for name, bad := range map[string]string{
		"empty role":     `{"aux":{"":{"provider":"q","id":"j"}}}`,
		"empty provider": `{"aux":{"judge":{"provider":"","id":"j"}}}`,
		"empty id":       `{"aux":{"judge":{"provider":"q"}}}`,
		"negative":       `{"aux":{"judge":{"provider":"q","id":"j","maxOutputTokens":-1}}}`,
		"unknown key":    `{"aux":{"judge":{"provider":"q","id":"j","x":1}}}`,
		"not object":     `{"aux":[1]}`,
		"bad json":       `{`,
	} {
		if _, err := harness.ParseAux(json.RawMessage(bad)); err == nil || !strings.HasPrefix(err.Error(), "harness: config: aux") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

const auxCfg = `{"model":{"provider":"p","id":"m"},"maxOutputTokens":300,"thinking":"low",` +
	`"aux":{"judge":{"provider":"p","id":"small","maxOutputTokens":77,"thinking":"high"},"plain":{"provider":"p","id":"tiny"}}}`

// HN-FR-72 (R40): aux 의 max tokens·thinking, 폴백 aux → 설정 → Lookup, 본 모델 thinking 은 물려받지 않음.
func TestCompleteAuxOptions(t *testing.T) {
	m := textModel()
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {})
	q := []contract.Message{user("u1", "q")}
	for _, role := range []string{"judge", "plain", "other"} {
		if _, err := a.Complete(context.Background(), json.RawMessage(auxCfg), role, nil, q); err != nil {
			t.Fatal(role, err)
		}
	}
	if _, err := a.Complete(context.Background(), json.RawMessage(`{"model":{"provider":"p","id":"m"}}`), "other", nil, q); err != nil {
		t.Fatal(err)
	}
	reqs := m.requests()
	type got struct {
		id, api, thinking string
		max               int
	}
	want := []got{{"small", "x", "high", 77}, {"tiny", "x", "", 300}, {"m", "x", "", 300}, {"m", "x", "", 500}}
	for i, w := range want {
		r := reqs[i]
		if g := (got{r.Model.ID, r.Model.API, r.Options.Thinking, r.Options.MaxOutputTokens}); g != w || len(r.Tools) != 0 {
			t.Errorf("request %d = %+v, want %+v", i, g, w)
		}
	}
	bad := `{"model":{"provider":"p","id":"m"},"aux":{"judge":{"provider":"p"}}}`
	if _, err := a.Complete(context.Background(), json.RawMessage(bad), "judge", nil, q); err == nil || !strings.HasPrefix(err.Error(), "harness: config: aux") {
		t.Fatalf("bad aux: %v", err)
	}
}

type errModel struct {
	*fakeModel
	err error
}

func (e errModel) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	return func(yield func(contract.StreamEvent, error) bool) { yield(contract.StreamEvent{}, e.err) }
}

// HN-FR-87 (sets ST-FR-56 에서 옮김): done, error 이벤트, 값 없는 이벤트, done 없이 끝남, iterator 에러, ctx 취소. 재시도하지 않음.
func TestCompleteStream(t *testing.T) {
	q := []contract.Message{user("u1", "q")}
	run := func(m *fakeModel, ctx context.Context) (contract.Message, error) {
		a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {})
		return a.Complete(ctx, json.RawMessage(cfg), "judge", nil, q)
	}
	script := func(evs ...contract.StreamEvent) *fakeModel {
		return &fakeModel{script: func(int, contract.Request) []contract.StreamEvent { return evs }}
	}
	if got, err := run(textModel(), context.Background()); err != nil || got.Content[0].Text != "ok" {
		t.Fatalf("done %+v %v", got, err)
	}
	m := script(contract.StreamEvent{Kind: contract.EventError, Error: &contract.CallError{Class: contract.ClassRateLimit, Message: "slow"}})
	_, err := run(m, context.Background())
	var ce *contract.CallError
	if !errors.As(err, &ce) || ce.Class != contract.ClassRateLimit || len(m.requests()) != 1 {
		t.Fatalf("error event: %v (%d requests)", err, len(m.requests()))
	}
	if _, err := run(script(contract.StreamEvent{Kind: contract.EventStart}), context.Background()); err == nil || err.Error() != "harness: complete: stream ended without done" {
		t.Fatalf("no done: %v", err)
	}
	for _, ev := range []contract.StreamEvent{{Kind: contract.EventDone}, {Kind: contract.EventError}} {
		if _, err := run(script(ev), context.Background()); err == nil {
			t.Fatalf("%s without value accepted", ev.Kind)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := run(script(), ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
	boom := errors.New("boom")
	a := newAgent(t, envtest.New("/w", "/h"), textModel(), func(o *harness.Options) { o.Model = errModel{textModel(), boom} })
	if _, err := a.Complete(context.Background(), json.RawMessage(cfg), "judge", nil, q); !errors.Is(err, boom) {
		t.Fatalf("iterator error: %v", err)
	}
}

// HN-FR-85: 세트는 sets.AuxOf(d) 로 하네스의 Complete 를 부른다 (같은 규칙).
func TestDepsAux(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return reply(fmt.Sprint("a", n), contract.StopStop, text("ok"))
	}}
	var got contract.Message
	var gotErr error
	probe := func(d sets.Deps) inject.FunctionSet {
		return inject.FunctionSet{Name: "probe", Description: "aux probe",
			TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
				sigs, err := ctx.Next()
				if got.ID == "" && gotErr == nil {
					got, gotErr = sets.AuxOf(d).Complete(ctx.Context(), ctx.Env().Config(), "judge",
						[]contract.Block{text("rules")}, []contract.Message{user("q1", "q")})
				}
				return sigs, err
			}}
	}
	var early error
	a := newAgent(t, envtest.New("/w", "/h"), m, func(o *harness.Options) {
		o.Sets = func(d sets.Deps) []harness.SetEntry {
			_, early = sets.AuxOf(d).Complete(context.Background(), json.RawMessage(auxCfg), "judge", nil, nil)
			return append(harness.DefaultSets(d), harness.SetEntry{Set: probe(d), On: true})
		}
	})
	if !errors.Is(early, sets.ErrNoAux) {
		t.Fatalf("call before New returned: %v", early)
	}
	r, _, err := a.Start(context.Background(), harness.RunRequest{SessionID: "s1", Input: []contract.Message{user("u1", "hi")}, Config: json.RawMessage(auxCfg)})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, r)
	reqs := m.requests()
	if gotErr != nil || got.ID != "a1" || len(reqs) != 2 {
		t.Fatalf("aux call %+v %v (%d requests)", got, gotErr, len(reqs))
	}
	if aux := reqs[1]; aux.Model.ID != "small" || aux.Options.MaxOutputTokens != 77 || aux.Options.Thinking != "high" ||
		len(aux.Tools) != 0 || len(aux.System) != 1 || aux.System[0].Text != "rules" {
		t.Fatalf("aux request %+v", aux)
	}
}
