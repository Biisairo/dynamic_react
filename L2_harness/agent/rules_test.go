package agent

import "testing"

// AG-FR-13~17: 규칙 문법·대상·매칭·복합 명령 방어·우선순위.
func TestRules(t *testing.T) {
	rs, err := compileRules(Permissions{
		Allow: []string{"read", "mytool", "bash(go test *)", "bash(make build && make test)", "edit(src/**)", "write(~/notes/*.md)"},
		Ask:   []string{"bash(git push *)", "bash(go test ./risky/*)"},
		Deny:  []string{"write(/etc/**)", "bash(rm -rf *)"},
	}, "/w", "/h")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		tool, target string
		hasTarget    bool
		want         verdict
		rule         string
	}{
		{"read", "/anything", true, allow, "read"},
		{"mytool", "", false, allow, "mytool"},
		{"other", "", false, ask, ""},
		{"bash", "go test ./...", true, allow, "bash(go test *)"},
		{"bash", "go test ", true, allow, "bash(go test *)"},
		{"bash", "go test ./... && rm x", true, ask, ""},
		{"bash", "go test a; b", true, ask, ""},
		{"bash", "go test a || b", true, ask, ""},
		{"bash", "go test a | b", true, ask, ""},
		{"bash", "go test `b`", true, ask, ""},
		{"bash", "go test $(b)", true, ask, ""},
		{"bash", "go test a\nb", true, ask, ""},
		{"bash", "make build && make test", true, allow, "bash(make build && make test)"},
		{"bash", "make build", true, ask, ""},
		{"bash", "git push origin main", true, ask, "bash(git push *)"},
		{"bash", "go test ./risky/x", true, ask, "bash(go test ./risky/*)"},
		{"bash", "rm -rf /", true, deny, "bash(rm -rf *)"},
		{"bash", "rm -rf / && echo", true, deny, "bash(rm -rf *)"},
		{"bash", "ls", true, ask, ""},
		{"edit", "/w/src/a/b.go", true, allow, "edit(src/**)"},
		{"edit", "/w/src", true, allow, "edit(src/**)"},
		{"edit", "/w/srcx/a.go", true, ask, ""},
		{"edit", "/w/other.go", true, ask, ""},
		{"write", "/h/notes/a.md", true, allow, "write(~/notes/*.md)"},
		{"write", "/h/notes/sub/a.md", true, ask, ""},
		{"write", "/h/notes/a.txt", true, ask, ""},
		{"write", "/etc/passwd", true, deny, "write(/etc/**)"},
		{"write", "/etc/a/b/c", true, deny, "write(/etc/**)"},
	}
	for _, c := range cases {
		v, rule := rs.decide(c.tool, c.target, c.hasTarget, nil)
		if v != c.want || rule != c.rule {
			t.Errorf("%s %q: got %v %q, want %v %q", c.tool, c.target, v, rule, c.want, c.rule)
		}
	}
	session := map[string]bool{"bash(git push origin main)": true, "bash(rm -rf /)": true, "other": true}
	for _, c := range []struct {
		tool, target string
		hasTarget    bool
		want         verdict
	}{
		{"bash", "git push origin main", true, allow},
		{"bash", "git push origin dev", true, ask},
		{"bash", "rm -rf /", true, deny},
		{"other", "", false, allow},
	} {
		if v, _ := rs.decide(c.tool, c.target, c.hasTarget, session); v != c.want {
			t.Errorf("session %s %q: got %v, want %v", c.tool, c.target, v, c.want)
		}
	}
}

func TestRuleSyntax(t *testing.T) {
	for _, bad := range []string{"", "bash(", "bash()", "(x)", "mytool(x)", "read(a)b", "ba sh"} {
		if _, err := compileRules(Permissions{Allow: []string{bad}}, "/w", "/h"); err == nil {
			t.Errorf("rule %q accepted", bad)
		}
	}
	if _, err := compileRules(Permissions{Deny: []string{"write(~/x)"}}, "/w", ""); err == nil {
		t.Error("~ pattern without home accepted")
	}
}

func TestSessionKey(t *testing.T) {
	a, b := SessionKey("discord/g/c/t"), SessionKey("discord/g/c/t2")
	if a != SessionKey("discord/g/c/t") || a == b || len(a) != 18 || a[:2] != "k-" {
		t.Fatalf("%q %q", a, b)
	}
}
