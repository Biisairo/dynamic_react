package harness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/storetest"
)

func tool(name, desc string) sets.Tool {
	return sets.Tool{Spec: contract.ToolSpec{Name: name, Description: desc, InputSchema: json.RawMessage(`{}`)}}
}

// HN-FR-84 (sets ST-FR-41 에서 옮김): 등록 순서, 같은 이름은 그 자리에서 덮음.
func TestRegistry(t *testing.T) {
	r := newRegistry()
	r.add(tool("echo", "v1"))
	r.add(tool("b", ""))
	r.add(tool("echo", "v2"))
	if strings.Join(r.Names(), ",") != "echo,b" {
		t.Fatalf("names %v", r.Names())
	}
	if tl, _ := r.Get("echo"); tl.Spec.Description != "v2" {
		t.Fatal("replace")
	}
	if _, ok := r.Get("zz"); ok {
		t.Fatal("unknown found")
	}
	names := r.Names()
	names[0] = "x"
	if r.Names()[0] != "echo" {
		t.Fatal("Names shares storage")
	}
}

// HN-FR-84 (sets ST-FR-15 에서 옮김): Store 의 상태로 복원하고 SaveState 로 저장.
func TestStateStore(t *testing.T) {
	ctx := context.Background()
	s := storetest.New()
	s.SaveState(ctx, "x", "base", inject.StateDelta{Set: map[string]json.RawMessage{"k": json.RawMessage(`1`)}})
	st := stateStoreOf(s)
	loaded, err := st.Load(ctx, "x")
	if err != nil || string(loaded["base"].Values["k"]) != "1" {
		t.Fatalf("load %+v %v", loaded, err)
	}
	st.Save(ctx, "x", "other", inject.StateDelta{Set: map[string]json.RawMessage{"q": json.RawMessage(`true`)}})
	if sess, _ := s.Load(ctx, "x"); string(sess.State["other"].Values["q"]) != "true" {
		t.Fatal("save")
	}
}
