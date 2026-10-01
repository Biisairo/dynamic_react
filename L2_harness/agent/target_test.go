package agent

import (
	"encoding/json"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/osenv"
)

// AG-FR-14: 도구별 대상. grep·find·ls 는 path 가 없으면 cwd (R23).
func TestTarget(t *testing.T) {
	e, err := osenv.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	cwd := e.Cwd()
	a := &Agent{env: e}
	cases := []struct {
		name, args, want string
		has              bool
	}{
		{"bash", `{"command":"ls -la"}`, "ls -la", true},
		{"bash", `{}`, "", true},
		{"read", `{"path":"a.txt"}`, cwd + "/a.txt", true},
		{"read", `{}`, "", true},
		{"grep", `{"pattern":"x","path":"src"}`, cwd + "/src", true},
		{"grep", `{"pattern":"x"}`, cwd, true},
		{"find", `{"pattern":"*.go","path":""}`, cwd, true},
		{"find", `{"pattern":"*.go","path":"/etc"}`, "/etc", true},
		{"ls", `{}`, cwd, true},
		{"ls", `{"path":"sub/.."}`, cwd, true},
		{"ls", `not json`, cwd, true},
		{"mytool", `{"path":"a"}`, "", false},
	}
	for _, c := range cases {
		got, has := a.target(contract.ToolCall{Name: c.name, Args: json.RawMessage(c.args)})
		if got != c.want || has != c.has {
			t.Errorf("%s %s: got %q %v, want %q %v", c.name, c.args, got, has, c.want, c.has)
		}
	}
}
