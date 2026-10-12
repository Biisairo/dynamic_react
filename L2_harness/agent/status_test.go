package agent_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func cachedReply(s string, prompt, cached, completion int) string {
	return sse(`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"`+s+`"},"finish_reason":"stop"}]}`,
		`{"id":"c1","choices":[],"usage":{"prompt_tokens":`+strconv.Itoa(prompt)+`,"completion_tokens":`+strconv.Itoa(completion)+
			`,"prompt_tokens_details":{"cached_tokens":`+strconv.Itoa(cached)+`}}}`)
}

// AG-FR-52: Stats 는 harness 값 그대로, 기록 없음 false.
func TestStats(t *testing.T) {
	f := setup(t)
	l := newLLM(t, cachedReply("ok", 120, 100, 5))
	a := newAgent(t, f, testConfig(l.URL, ""), func(o *agent.Options) { o.OnEvent = func(harness.Event) {} })
	if _, ok := a.Stats("nope"); ok {
		t.Fatal("stats for unknown session")
	}
	_, sid := runOnce(t, a, "", "go")
	st, ok := a.Stats(sid)
	want, _ := a.Harness().Stats(sid)
	if !ok || st != want || st.Session.Requests != 1 || st.Session.Prompt != 120 || st.Session.CacheRead != 100 ||
		st.Context != 125 || st.Window != 100000 || st.Model.ID != "m" {
		t.Fatalf("%+v", st)
	}
}

// AG-FR-53: ToolNames = 상한·설정 tools 적용 뒤, 레지스트리 순서.
func TestToolNames(t *testing.T) {
	f := setup(t)
	l := newLLM(t, textReply("ok"))
	names := func(cfg string, limit []string) string {
		t.Helper()
		f.writeConfig(t, cfg)
		o := f.opts()
		o.Tools = limit
		a, err := agent.New(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(a.ToolNames(), ",")
	}
	base := testConfig(l.URL, "")
	withTools := func(list string) string {
		return strings.Replace(base, `"agent":{`, `"agent":{"tools":`+list+`,`, 1)
	}
	if got := names(base, nil); got != "read,bash,edit,write,grep,find,ls" {
		t.Fatalf("all %s", got)
	}
	if got := names(base, []string{"bash", "read"}); got != "read,bash" {
		t.Fatalf("limit %s", got)
	}
	if got := names(withTools(`["bash","edit"]`), []string{"read", "bash"}); got != "bash" {
		t.Fatalf("intersection %s", got)
	}
	if got := names(withTools(`["edit","read","nope"]`), nil); got != "read,edit" {
		t.Fatalf("config tools %s", got)
	}
}
