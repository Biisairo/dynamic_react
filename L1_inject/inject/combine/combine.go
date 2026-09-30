// Package combine 은 step 하나에 여러 기능을 합치는 조합 함수다 (inject 3.7, IN-FR-28~30, 54~59).
// step 별 네임스페이스 값(예: combine.TurnFinish)의 메서드로 쓴다. inject 의 공개 API 만 사용한다.
package combine

import (
	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
)

type signal interface{ Kind() core.SignalKind }

// decisive 는 신호가 비어 있지 않고 [next] 하나만이 아닌지 본다 (IN-FR-54).
func decisive[S signal](s []S) bool {
	return len(s) > 0 && !(len(s) == 1 && s[0].Kind() == core.SigNext)
}

func transition[S signal](s []S) core.SignalKind {
	if n := len(s); n > 0 && s[n-1].Kind().IsTransition() {
		return s[n-1].Kind()
	}
	return core.SigNext
}

func rank(order []core.SignalKind, k core.SignalKind) int {
	for i, o := range order {
		if o == k {
			return i
		}
	}
	return len(order)
}

func firstDecisive[S signal](n int, call func(i int) ([]S, error), drop func(...S)) ([]S, error) {
	var skipped []S
	for i := range n {
		s, err := call(i)
		if err != nil {
			return nil, err
		}
		if decisive(s) {
			drop(skipped...)
			return s, nil
		}
		skipped = append(skipped, s...)
	}
	drop(skipped...)
	return nil, nil
}

func byKind[S signal](order []core.SignalKind, n int, call func(i int) ([]S, error), drop func(...S)) ([]S, error) {
	all := make([][]S, n)
	best := -1
	for i := range n {
		s, err := call(i)
		if err != nil {
			return nil, err
		}
		all[i] = s
		if best < 0 || rank(order, transition(s)) < rank(order, transition(all[best])) {
			best = i
		}
	}
	if best < 0 {
		return nil, nil
	}
	for i, s := range all {
		if i != best {
			drop(s...)
		}
	}
	return all[best], nil
}

func pipe[T any, S signal](n int, call func(i int, in T) (T, []S, error), in T) (T, []S, error) {
	var s []S
	for i := range n {
		var err error
		if in, s, err = call(i, in); err != nil {
			return in, nil, err
		}
		if decisive(s) {
			return in, s, nil
		}
	}
	return in, s, nil
}

func concat[S signal](n int, call func(i int) ([]contract.Message, []S, error), drop func(...S)) ([]contract.Message, []S, error) {
	var out []contract.Message
	var chosen []S
	for i := range n {
		ms, s, err := call(i)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, ms...)
		switch {
		case chosen == nil && decisive(s):
			chosen = s
		case len(s) > 0:
			drop(s...)
		}
	}
	return out, chosen, nil
}
