package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

const (
	grepLimit   = 100
	findLimit   = 1000
	lsLimit     = 500
	grepLineMax = 500
)

// searchRoot 는 HN-FR-43 의 시작 경로다. name 은 문구에 쓰는 인자 그대로의 경로다.
func searchRoot(ctx context.Context, e env.Env, name string) (string, env.FileInfo, error) {
	abs, err := Resolve(e, name)
	if err != nil {
		return "", env.FileInfo{}, err
	}
	fi, err := e.Stat(ctx, abs)
	if errors.Is(err, fs.ErrNotExist) {
		return "", env.FileInfo{}, fmt.Errorf("Path not found: %s", name)
	}
	if err != nil {
		return "", env.FileInfo{}, fmt.Errorf("Cannot access %s: %v", name, err)
	}
	return abs, fi, nil
}

// visitFunc 는 순회한 항목 하나를 받는다. rel 은 시작 디렉터리 기준 상대 경로다. true 를 돌려주면 순회를 멈춘다.
type visitFunc func(rel, abs string, fi env.FileInfo) bool

// walk 는 HN-FR-43·44 의 깊이 우선 순회다.
func walk(ctx context.Context, e env.Env, root string, respect bool, visit visitFunc) error {
	var rules []ignoreRule
	if respect {
		rules = ancestorRules(ctx, e, root)
	}
	_, err := walkDir(ctx, e, root, "", rules, respect, visit, true)
	return err
}

func walkDir(ctx context.Context, e env.Env, abs, rel string, rules []ignoreRule, respect bool, visit visitFunc, top bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	entries, err := e.ListDir(ctx, abs)
	if err != nil {
		if top {
			return true, err
		}
		return false, nil
	}
	if respect {
		rules = append(rules[:len(rules):len(rules)], readIgnore(ctx, e, abs)...)
	}
	for _, fi := range entries {
		if fi.IsDir && fi.Name == ".git" {
			continue
		}
		childAbs, childRel := path.Join(abs, fi.Name), path.Join(rel, fi.Name)
		if respect && ignored(rules, childAbs, fi.IsDir) {
			continue
		}
		if visit(childRel, childAbs, fi) {
			return true, nil
		}
		if fi.IsDir && !fi.Symlink {
			if stop, err := walkDir(ctx, e, childAbs, childRel, rules, respect, visit, false); stop || err != nil {
				return stop, err
			}
		}
	}
	return false, nil
}

// ancestorRules 는 git 루트(.git 을 가진 가장 가까운 조상)부터 root 의 부모까지의 .gitignore 규칙이다 (HN-FR-44).
func ancestorRules(ctx context.Context, e env.Env, root string) []ignoreRule {
	var dirs []string
	for d := root; ; {
		if _, err := e.Stat(ctx, path.Join(d, ".git")); err == nil {
			break
		}
		if d == "/" {
			return nil
		}
		d = path.Dir(d)
		dirs = append(dirs, d)
	}
	var rules []ignoreRule
	for i := len(dirs) - 1; i >= 0; i-- {
		rules = append(rules, readIgnore(ctx, e, dirs[i])...)
	}
	return rules
}

type ignoreRule struct {
	base            string
	segs            []string
	dirOnly, negate bool
}

func readIgnore(ctx context.Context, e env.Env, dir string) []ignoreRule {
	data, err := e.ReadFile(ctx, path.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	return parseIgnore(dir, string(data))
}

// parseIgnore 는 HN-FR-44 의 gitignore 문법이다. 잘못된 패턴 줄은 버린다.
func parseIgnore(base, content string) []ignoreRule {
	var rules []ignoreRule
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(strings.TrimSuffix(line, "\r"), " ")
		if line == "" || line[0] == '#' {
			continue
		}
		r := ignoreRule{base: base}
		switch line[0] {
		case '!':
			r.negate, line = true, line[1:]
		case '\\':
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly, line = true, strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		if strings.Contains(line, "/") {
			r.segs = strings.Split(strings.TrimPrefix(line, "/"), "/")
		} else {
			r.segs = []string{"**", line}
		}
		if validSegs(r.segs) {
			rules = append(rules, r)
		}
	}
	return rules
}

// ignored 는 맞는 규칙 중 마지막 것으로 정한다.
func ignored(rules []ignoreRule, abs string, isDir bool) bool {
	out := false
	for _, r := range rules {
		rel := strings.TrimPrefix(abs, strings.TrimSuffix(r.base, "/")+"/")
		if (!r.dirOnly || isDir) && matchSegs(r.segs, strings.Split(rel, "/")) {
			out = !r.negate
		}
	}
	return out
}

// pathGlob 은 HN-FR-45 의 glob 이다.
type pathGlob struct {
	segs     []string
	fullPath bool
}

func compileGlob(p string) (pathGlob, error) {
	g := pathGlob{segs: []string{p}}
	if strings.Contains(p, "/") {
		g.fullPath = true
		if strings.HasPrefix(p, "/") {
			g.segs = strings.Split(p[1:], "/")
		} else {
			g.segs = strings.Split("**/"+p, "/")
		}
	}
	if !validSegs(g.segs) {
		return pathGlob{}, fmt.Errorf("Invalid glob pattern: %s", p)
	}
	return g, nil
}

func (g pathGlob) match(rel string) bool {
	if g.fullPath {
		return matchSegs(g.segs, strings.Split(rel, "/"))
	}
	ok, _ := path.Match(g.segs[0], path.Base(rel))
	return ok
}

func validSegs(segs []string) bool {
	for _, s := range segs {
		if _, err := path.Match(s, ""); err != nil {
			return false
		}
	}
	return true
}

// matchSegs 는 경로 단계별 매칭이다. "**" 는 0개 이상의 단계다.
func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// headBytes 는 앞에서부터 온전한 줄을 maxBytes 안에서 담는다 (줄 수 상한 없음).
func headBytes(lines []string) (string, bool) {
	size := 0
	for i, l := range lines {
		add := len(l)
		if i > 0 {
			add++
		}
		if size+add > maxBytes {
			return strings.Join(lines[:i], "\n"), true
		}
		size += add
	}
	return strings.Join(lines, "\n"), false
}

// withNotices 는 pi 식 안내 `\n\n[a. b]` 를 붙인다.
func withNotices(out string, notices []string) string {
	if len(notices) == 0 {
		return out
	}
	return out + "\n\n[" + strings.Join(notices, ". ") + "]"
}

// limitArg 는 limit 인자다. 없으면 def, 1 미만이면 1.
func limitArg(p *int, def int) int {
	if p == nil {
		return def
	}
	return max(*p, 1)
}
