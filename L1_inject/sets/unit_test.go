package sets_test

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// ST-FR-01, 02
func TestParseConfig(t *testing.T) {
	c, err := sets.ParseConfig(json.RawMessage(`{"model":{"provider":"p","id":"m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Retry.MaxAttempts != 3 || c.Retry.BaseDelayMs != 2000 || c.Retry.MaxDelayMs != 60000 ||
		!c.Compaction.Enabled || c.Compaction.KeepRecentTokens != 20000 || c.Compaction.ReserveTokens != 0 {
		t.Fatalf("defaults %+v", c)
	}
	c, _ = sets.ParseConfig(json.RawMessage(`{"model":{"provider":"p","id":"m"},"compaction":{"enabled":false,"keepRecentTokens":5},"retry":{"maxAttempts":1},"tools":["a"],"thinking":"high","maxOutputTokens":9}`))
	if c.Compaction.Enabled || c.Compaction.KeepRecentTokens != 5 || c.Retry.MaxAttempts != 1 || c.Tools[0] != "a" || c.Thinking != "high" || c.MaxOutputTokens != 9 {
		t.Fatalf("overrides %+v", c)
	}
	for name, bad := range map[string]string{
		"unknown key":   `{"model":{"provider":"p","id":"m"},"sessionDir":"x"}`,
		"no model":      `{}`,
		"no model id":   `{"model":{"provider":"p"}}`,
		"api key field": `{"model":{"provider":"p","id":"m","apiKey":"sk"}}`,
		"not json":      `{`,
		"empty":         ``,
	} {
		if _, err := sets.ParseConfig(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// ST-FR-20, 21
func TestEstimate(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"abcd":    1,
		"abcde":   2,
		"안녕하세요":   5,
		"hi 안녕":   2 + 1, // 한글 2자 + "hi " 3바이트 → (3+3)/4 = 1
		"😀":       1,     // 비한중일 4바이트 → (4+3)/4
		"日本語テキスト": 7,
	}
	for s, want := range cases {
		if got := sets.EstimateText(s); got != want {
			t.Errorf("EstimateText(%q) = %d, want %d", s, got, want)
		}
	}
	img := contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: make([]byte, 99999)}}
	pd := &contract.ProviderData{Provider: "p", Model: "m", Payload: make([]byte, 99999)}
	m := contract.Message{ID: "a", Role: contract.RoleAssistant, Timestamp: ts, Content: []contract.Block{
		text("abcd"), {Kind: contract.BlockThinking, Text: "abcd", ProviderData: pd}, img,
		toolCall("c", "read", `{"p":1}`), {Kind: contract.BlockOpaque, Opaque: &contract.Opaque{Provider: "p", Kind: "k", Payload: make([]byte, 9999)}},
	}}
	want := 1 + 1 + 1200 + sets.EstimateText("read") + sets.EstimateText(`{"p":1}`)
	if got := sets.EstimateMessage(m); got != want {
		t.Fatalf("EstimateMessage = %d, want %d", got, want)
	}
	r := resultMsg("r", "c", "abcdabcd")
	if sets.EstimateMessage(r) != 2 {
		t.Fatalf("result estimate %d", sets.EstimateMessage(r))
	}
}

// ST-FR-22
func TestEstimateContext(t *testing.T) {
	later := ts.Add(time.Minute)
	a := assistant("a1", contract.Usage{Input: 1000, Output: 100}, contract.StopStop, text("x"))
	a.Timestamp = later
	proj := []contract.Message{user("u1", "abcd"), a, user("u2", "abcdabcd")}
	sys := []contract.Block{text("abcdabcdabcd")}
	tools := []contract.ToolSpec{{Name: "t", InputSchema: json.RawMessage(`{}`)}}
	// 편집 없음 → usage 신뢰
	if got := sets.EstimateContext(proj, nil, sys, tools); got != 1100+2 {
		t.Fatalf("trusted = %d", got)
	}
	// 응답보다 뒤의 편집 → 전부 추정
	edit := contract.ContextEdit{ID: "e", Kind: contract.EditHide, Timestamp: later.Add(time.Second), Target: "zz"}
	full := sets.EstimateMessage(proj[0]) + sets.EstimateMessage(proj[1]) + sets.EstimateMessage(proj[2]) + 3 + sets.EstimateText("t") + sets.EstimateText("{}")
	if got := sets.EstimateContext(proj, []contract.ContextEdit{edit}, sys, tools); got != full {
		t.Fatalf("untrusted = %d, want %d", got, full)
	}
	// 응답과 같은 시각의 편집 → "뒤" 가 아니므로 전부 추정
	edit.Timestamp = later
	if got := sets.EstimateContext(proj, []contract.ContextEdit{edit}, sys, tools); got != full {
		t.Fatalf("edit at usage time = %d, want %d", got, full)
	}
	// 응답보다 앞의 편집 → 신뢰
	edit.Timestamp = ts
	if got := sets.EstimateContext(proj, []contract.ContextEdit{edit}, sys, tools); got != 1102 {
		t.Fatalf("edit before usage = %d", got)
	}
}

// ST-FR-38
func TestCheckArgs(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","required":["a"],"properties":{"a":{"type":"string"},"n":{"type":"integer"},"f":{"type":"number"},"b":{"type":"boolean"},"l":{"type":"array"},"o":{"type":"object"},"z":{"type":"null"},"any":{}}}`)
	ok := []string{`{"a":"x"}`, `{"a":"x","n":3,"f":1.5,"b":true,"l":[],"o":{},"z":null,"any":1,"extra":1}`, `{"a":"x","n":3.0}`}
	bad := []string{`{}`, `{"a":1}`, `{"a":"x","n":1.5}`, `{"a":"x","f":"1"}`, `{"a":"x","b":0}`, `{"a":"x","l":{}}`, `{"a":"x","o":[]}`, `[]`, `{"a":"x","z":1}`}
	for _, a := range ok {
		if err := sets.CheckArgs(schema, json.RawMessage(a)); err != nil {
			t.Errorf("%s rejected: %v", a, err)
		}
	}
	for _, a := range bad {
		if err := sets.CheckArgs(schema, json.RawMessage(a)); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
	if err := sets.CheckArgs(json.RawMessage(`{"type":["string","null"]}`), json.RawMessage(`{}`)); err != nil {
		t.Errorf("non-object schema type list: %v", err)
	}
}

// ST-FR-33, 34, 42: nil Tools → 도구 없음, nil Store → 빈 세션을 읽고 저장하지 않음 (다음 run 에 남지 않음).
func TestNilToolsAndStore(t *testing.T) {
	m := &fakeModel{script: func(n int, req contract.Request) []contract.StreamEvent {
		return doneEv(assistant(fmt.Sprint("a", n), contract.Usage{}, contract.StopStop, text("ok")))
	}}
	d := deps(m)
	d.Tools, d.Store = nil, nil
	for i, in := range []string{"first", "second"} {
		if s := agent(t, d, false, cfgJSON, user(fmt.Sprint("u", i), in)); s.Reason != core.SettleCompleted {
			t.Fatalf("run %d %+v", i, s)
		}
	}
	reqs := m.requests()
	if len(reqs) != 2 || len(reqs[1].Tools) != 0 || len(reqs[1].Messages) != 1 || reqs[1].Messages[0].Content[0].Text != "second" {
		t.Fatalf("second request %+v", reqs[len(reqs)-1])
	}
}

// ST-NFR-04
func TestImportsOnlyAllowed(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	allowed := map[string]bool{
		"github.com/Biisairo/dynamic_react/L1_inject/sets": true, "github.com/Biisairo/dynamic_react/L0_core/contract": true,
		"github.com/Biisairo/dynamic_react/L0_core/core": true, "github.com/Biisairo/dynamic_react/L1_inject/inject": true,
	}
	for _, p := range strings.Fields(string(out)) {
		if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") && !allowed[p] {
			t.Errorf("forbidden dependency %s", p)
		}
	}
}

func TestReserveTokens(t *testing.T) {
	cfg := func(r int) sets.Config {
		var c sets.Config
		c.Compaction.ReserveTokens = r
		return c
	}
	cases := []struct {
		name       string
		reserve, w int
		want       int
	}{
		{"configured wins", 5000, 200000, 5000},
		{"floor 16384", 0, 100000, 16384},
		{"15 percent", 0, 200000, 30000},
		{"15 percent floor division", 0, 110001, 16500},
		{"zero window", 0, 0, 16384},
	}
	for _, c := range cases {
		if got := sets.ReserveTokens(cfg(c.reserve), c.w); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
