package storetest_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/storetest"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func user(id, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{{Kind: contract.BlockText, Text: s}}}
}

// HN-FR-04a (sets ST-FR-39 에서 옮김): 재생, 상태 삭제·공개, 복사본.
func TestMemoryStore(t *testing.T) {
	ctx := context.Background()
	s := storetest.New()
	if sess, err := s.Load(ctx, "x"); err != nil || len(sess.Messages) != 0 {
		t.Fatalf("empty %+v %v", sess, err)
	}
	s.AppendMessages(ctx, "x", []contract.Message{user("u1", "a")})
	s.AppendMessages(ctx, "x", []contract.Message{user("u2", "b")})
	edit := contract.ContextEdit{ID: "e", Kind: contract.EditHide, Timestamp: ts, Target: "u1"}
	s.AppendEdits(ctx, "x", []contract.ContextEdit{edit})
	s.SaveState(ctx, "x", "base", inject.StateDelta{Set: map[string]json.RawMessage{"k": json.RawMessage(`1`), "d": json.RawMessage(`2`)}, Publish: []string{"k"}})
	s.SaveState(ctx, "x", "base", inject.StateDelta{Delete: []string{"d"}})
	sess, _ := s.Load(ctx, "x")
	if len(sess.Messages) != 2 || sess.Messages[1].ID != "u2" || len(sess.Edits) != 1 {
		t.Fatalf("session %+v", sess)
	}
	ns := sess.State["base"]
	if string(ns.Values["k"]) != "1" || ns.Values["d"] != nil || len(ns.Published) != 1 || ns.Published[0] != "k" {
		t.Fatalf("state %+v", ns)
	}
	sess.Messages[0].ID = "mutated"
	ns.Values["k"][0] = '9'
	again, _ := s.Load(ctx, "x")
	if again.Messages[0].ID != "u1" || string(again.State["base"].Values["k"]) != "1" {
		t.Fatal("Load shares storage")
	}
	if other, _ := s.Load(ctx, "y"); len(other.Messages) != 0 || len(other.State) != 0 {
		t.Fatalf("sessions mixed %+v", other)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.AppendMessages(ctx, "c", []contract.Message{user("m", "x")})
		}()
	}
	wg.Wait()
	if c, _ := s.Load(ctx, "c"); len(c.Messages) != 8 {
		t.Fatalf("concurrent appends %d", len(c.Messages))
	}
}
