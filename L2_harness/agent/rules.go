package agent

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Permissions 는 승인 규칙 목록이다 (AG-FR-12).
type Permissions struct {
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

type verdict int

const (
	allow verdict = iota
	ask
	deny
)

func (v verdict) String() string { return [...]string{"allow", "ask", "deny"}[v] }

var ruleSyntax = regexp.MustCompile(`^([A-Za-z0-9_.-]+)(?:\((.+)\))?$`)

// patternTools 는 패턴 규칙을 쓸 수 있는 도구와 대상 종류다 (AG-FR-13, 14).
var patternTools = map[string]bool{"bash": false, "read": true, "write": true, "edit": true, "grep": true, "find": true, "ls": true} // true = 경로

// compoundOps 는 `*` allow 규칙이 허용하지 않는 셸 연결자다 (AG-FR-16).
var compoundOps = []string{";", "&&", "|", "`", "$(", "\n"}

type rule struct {
	text, tool string
	pattern    string // 경로 규칙이면 절대 경로 패턴
	hasPattern bool
	isPath     bool
}

type ruleSet struct{ allow, ask, deny []rule }

func compileRules(p Permissions, cwd, home string) (*ruleSet, error) {
	rs := &ruleSet{}
	for _, l := range []struct {
		src []string
		dst *[]rule
	}{{p.Allow, &rs.allow}, {p.Ask, &rs.ask}, {p.Deny, &rs.deny}} {
		for _, text := range l.src {
			r, err := compileRule(text, cwd, home)
			if err != nil {
				return nil, err
			}
			*l.dst = append(*l.dst, r)
		}
	}
	return rs, nil
}

func compileRule(text, cwd, home string) (rule, error) {
	m := ruleSyntax.FindStringSubmatch(text)
	if m == nil {
		return rule{}, fmt.Errorf("permission rule %q: want name or name(pattern)", text)
	}
	r := rule{text: text, tool: m[1], pattern: m[2], hasPattern: m[2] != ""}
	if !r.hasPattern {
		return r, nil
	}
	isPath, ok := patternTools[r.tool]
	if !ok {
		return rule{}, fmt.Errorf("permission rule %q: patterns are only for bash, read, write, edit, grep, find, ls", text)
	}
	r.isPath = isPath
	if isPath {
		switch {
		case r.pattern == "~" || strings.HasPrefix(r.pattern, "~/"):
			if home == "" {
				return rule{}, fmt.Errorf("permission rule %q: home directory unknown", text)
			}
			r.pattern = path.Join(home, r.pattern[1:])
		case !path.IsAbs(r.pattern):
			r.pattern = path.Join(cwd, r.pattern)
		default:
			r.pattern = path.Clean(r.pattern)
		}
	}
	return r, nil
}

// decide 는 AG-FR-17 이다. session 은 이 세션에서 allow_session 으로 허용한 규칙 문자열이다.
func (rs *ruleSet) decide(tool, target string, hasTarget bool, session map[string]bool) (verdict, string) {
	for _, r := range rs.deny {
		if r.match(tool, target, false) {
			return deny, r.text
		}
	}
	if session[sessionRule(tool, target, hasTarget)] {
		return allow, ""
	}
	for _, r := range rs.ask {
		if r.match(tool, target, false) {
			return ask, r.text
		}
	}
	for _, r := range rs.allow {
		if r.match(tool, target, true) {
			return allow, r.text
		}
	}
	return ask, ""
}

// sessionRule 은 AG-FR-19 의 정확 일치 규칙 문자열이다.
func sessionRule(tool, target string, hasTarget bool) string {
	if !hasTarget {
		return tool
	}
	return tool + "(" + target + ")"
}

func (r rule) match(tool, target string, isAllow bool) bool {
	switch {
	case r.tool != tool:
		return false
	case !r.hasPattern:
		return true
	case r.isPath:
		return globMatch(strings.Split(r.pattern, "/"), strings.Split(path.Clean(target), "/"))
	}
	if isAllow && strings.Contains(r.pattern, "*") {
		for _, op := range compoundOps {
			if strings.Contains(target, op) {
				return false
			}
		}
	}
	return wildMatch(r.pattern, target)
}

// globMatch 는 경로 단계 단위 glob 이다. `**` 는 0개 이상의 단계, 나머지 단계는 wildMatch (AG-FR-15).
func globMatch(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if globMatch(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	return len(segs) > 0 && wildMatch(pat[0], segs[0]) && globMatch(pat[1:], segs[1:])
}

// wildMatch 는 `*` 만 특수 문자인 전체 일치다.
func wildMatch(pat, s string) bool {
	star, mark, i, j := -1, 0, 0, 0
	for j < len(s) {
		switch {
		case i < len(pat) && pat[i] == '*':
			star, mark = i, j
			i++
		case i < len(pat) && pat[i] == s[j]:
			i, j = i+1, j+1
		case star >= 0:
			mark++
			i, j = star+1, mark
		default:
			return false
		}
	}
	for i < len(pat) && pat[i] == '*' {
		i++
	}
	return i == len(pat)
}
