package harness

import (
	"errors"
	"fmt"

	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// SetEntry 는 등록 항목이다: 세트와 기본 켜짐 (3.6c).
type SetEntry struct {
	Set inject.FunctionSet
	On  bool
}

// SetState 는 배치의 한 칸이다 (HN-FR-51).
type SetState struct {
	Name, Description string
	On                bool
}

// ErrSetLayout 은 Arrange 의 배치가 등록된 세트와 맞지 않는다는 뜻이다 (HN-FR-53).
var ErrSetLayout = errors.New("harness: invalid set layout")

// DefaultSets 는 기본 구성(프리셋)이다. harness 가 갖는 유일한 순서 지식이다 (HN-FR-39).
func DefaultSets(d sets.Deps) []SetEntry {
	repeat, _ := sets.Repeat(d, sets.RepeatOptions{}) // 기본 옵션은 에러가 없다 (HN-FR-39)
	return []SetEntry{
		{Set: sets.Base(d), On: true},
		{Set: sets.Require(d), On: true},
		{Set: sets.Cache(d), On: true},
		{Set: sets.Guard(d), On: true},
		{Set: sets.Truncate(d), On: true},
		{Set: sets.Resilient(d), On: true},
		{Set: repeat, On: true},
	}
}

// checkEntries 는 등록 목록의 이름이 비거나 겹치지 않는지 본다 (HN-FR-38 ⑤).
func checkEntries(reg []SetEntry) error {
	seen := map[string]bool{}
	for _, e := range reg {
		if e.Set.Name == "" {
			return errors.New("harness: sets: set without name")
		}
		if e.Set.Name == ModeNamespace {
			return fmt.Errorf("harness: sets: set name %q is reserved", ModeNamespace)
		}
		if seen[e.Set.Name] {
			return fmt.Errorf("harness: sets: duplicate set name %q", e.Set.Name)
		}
		seen[e.Set.Name] = true
	}
	return nil
}

// initialLayout 은 want 의 등록된 이름을 앞에, 나머지 등록 세트를 등록 순서·기본 켜짐으로 뒤에 둔다 (HN-FR-52).
func initialLayout(reg []SetEntry, want []SetState) []SetState {
	known := map[string]SetEntry{}
	for _, e := range reg {
		known[e.Set.Name] = e
	}
	placed := map[string]bool{}
	var out []SetState
	for _, s := range want {
		e, ok := known[s.Name]
		if !ok || placed[s.Name] {
			continue
		}
		placed[s.Name] = true
		out = append(out, SetState{Name: s.Name, Description: e.Set.Description, On: s.On})
	}
	for _, e := range reg {
		if !placed[e.Set.Name] {
			out = append(out, SetState{Name: e.Set.Name, Description: e.Set.Description, On: e.On})
		}
	}
	return out
}

// bind 는 켜진 세트를 배치 순서대로 래핑해 Dispatcher 를 만든다 (HN-FR-38 ⑤, 41).
func (a *Agent) bind(layout []SetState) (*inject.Dispatcher, error) {
	byName := map[string]inject.FunctionSet{}
	for _, e := range a.reg {
		byName[e.Set.Name] = e.Set
	}
	var fs []inject.FunctionSet
	for _, s := range layout {
		if s.On {
			fs = append(fs, byName[s.Name])
		}
	}
	if a.sf.active() {
		fs = a.sf.wrap(fs)
	}
	d, err := inject.BindWith(inject.BindOptions{StateStore: stateStoreOf(a.store), Runs: a.runs}, fs...)
	if err != nil {
		return nil, fmt.Errorf("harness: sets: %w", err)
	}
	return d, nil
}

// Sets 는 지금 배치의 복사본이다 (HN-FR-51).
func (a *Agent) Sets() []SetState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]SetState(nil), a.layout...)
}

// Arrange 는 배치를 바꾼다. 다음 Start 부터 쓰이고, 실패하면 배치는 그대로다 (HN-FR-53).
func (a *Agent) Arrange(layout []SetState) error {
	desc := map[string]string{}
	for _, e := range a.reg {
		desc[e.Set.Name] = e.Set.Description
	}
	if len(layout) != len(a.reg) {
		return fmt.Errorf("%w: %d sets, %d registered", ErrSetLayout, len(layout), len(a.reg))
	}
	next := make([]SetState, len(layout))
	seen := map[string]bool{}
	for i, s := range layout {
		d, ok := desc[s.Name]
		switch {
		case !ok:
			return fmt.Errorf("%w: unknown set %q", ErrSetLayout, s.Name)
		case seen[s.Name]:
			return fmt.Errorf("%w: duplicate set %q", ErrSetLayout, s.Name)
		}
		seen[s.Name] = true
		next[i] = SetState{Name: s.Name, Description: d, On: s.On}
	}
	b, err := a.bindAll(next)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.layout, a.bundle = next, b
	return nil
}
