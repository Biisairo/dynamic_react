package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/envtest"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/session"
)

var ts = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

var _ sets.Store = (*session.Store)(nil)

func user(id, s string) contract.Message {
	return contract.Message{ID: id, Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{{Kind: contract.BlockText, Text: s}}}
}

func open(t *testing.T) (*envtest.Env, *session.Store) {
	t.Helper()
	e := envtest.New("/w", "/h")
	s, err := session.New(e, "/h/sessions")
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

// HN-FR-11, 12
func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	e, s := open(t)
	if sess, err := s.Load(ctx, "s1"); err != nil || len(sess.Messages) != 0 || sess.State == nil {
		t.Fatalf("empty %+v %v", sess, err)
	}
	img := contract.Message{ID: "i", Role: contract.RoleUser, Timestamp: ts, Content: []contract.Block{{Kind: contract.BlockImage, Image: &contract.Image{MediaType: "image/png", Data: []byte{1, 2, 3}}}}}
	s.AppendMessages(ctx, "s1", []contract.Message{user("u1", "a"), img})
	edit := contract.ContextEdit{ID: "e1", Kind: contract.EditHide, Timestamp: ts, Target: "u1"}
	s.AppendEdits(ctx, "s1", []contract.ContextEdit{edit})
	s.SaveState(ctx, "s1", "base", inject.StateDelta{Set: map[string]json.RawMessage{"k": json.RawMessage(`1`), "d": json.RawMessage(`"x"`)}, Publish: []string{"k"}})
	s.SaveState(ctx, "s1", "base", inject.StateDelta{Delete: []string{"d"}})

	if n := e.AppendCount("/h/sessions/s1.jsonl"); n != 4 {
		t.Fatalf("one AppendFile per call, got %d", n)
	}
	raw, _ := e.File("/h/sessions/s1.jsonl")
	if lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n"); len(lines) != 5 || !strings.HasPrefix(lines[0], `{"type":"message"`) ||
		!strings.HasPrefix(lines[2], `{"type":"edit"`) || !strings.HasPrefix(lines[3], `{"type":"state"`) {
		t.Fatalf("file:\n%s", raw)
	}

	// 새 Store (재시작) 로 읽는다
	s2, _ := session.New(e, "/h/sessions")
	sess, err := s2.Load(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content[0].Image.Data[2] != 3 || !sess.Messages[0].Timestamp.Equal(ts) {
		t.Fatalf("messages %+v", sess.Messages)
	}
	if len(sess.Edits) != 1 || sess.Edits[0].Target != "u1" {
		t.Fatalf("edits %+v", sess.Edits)
	}
	ns := sess.State["base"]
	if string(ns.Values["k"]) != "1" || ns.Values["d"] != nil || len(ns.Published) != 1 || ns.Published[0] != "k" {
		t.Fatalf("state %+v", ns)
	}
	if err := s.AppendMessages(ctx, "s1", nil); err != nil || e.AppendCount("/h/sessions/s1.jsonl") != 4 {
		t.Fatal("empty append must not write")
	}
}

// HN-FR-13
func TestBrokenLines(t *testing.T) {
	ctx := context.Background()
	e, s := open(t)
	s.AppendMessages(ctx, "s1", []contract.Message{user("u1", "a")})
	good, _ := e.File("/h/sessions/s1.jsonl")

	e.SetFile("/h/sessions/s1.jsonl", good+`{"type":"mess`)
	sess, err := s.Load(ctx, "s1")
	if err != nil || len(sess.Messages) != 1 {
		t.Fatalf("broken tail: %v %+v", err, sess)
	}
	if c, _ := e.File("/h/sessions/s1.jsonl"); c != good {
		t.Fatalf("not repaired: %q", c)
	}
	s.AppendMessages(ctx, "s1", []contract.Message{user("u2", "b")})
	if sess, err := s.Load(ctx, "s1"); err != nil || len(sess.Messages) != 2 {
		t.Fatalf("after repair: %v %d", err, len(sess.Messages))
	}

	line := strings.TrimSuffix(good, "\n")
	e.SetFile("/h/sessions/s2.jsonl", good+line) // 온전하지만 개행 없는 마지막 줄
	if sess, err := s.Load(ctx, "s2"); err != nil || len(sess.Messages) != 2 {
		t.Fatalf("unterminated valid line: %v %+v", err, sess)
	}
	if c, _ := e.File("/h/sessions/s2.jsonl"); c != good+good {
		t.Fatalf("newline not added: %q", c)
	}

	e.SetFile("/h/sessions/s3.jsonl", "{broken\n"+good)
	if _, err := s.Load(ctx, "s3"); err == nil {
		t.Fatal("broken middle line accepted")
	}
	e.SetFile("/h/sessions/s4.jsonl", `{"type":"usage"}`+"\n")
	if _, err := s.Load(ctx, "s4"); err == nil {
		t.Fatal("unknown type accepted")
	}
	e.SetFile("/h/sessions/s5.jsonl", `{"type":"message"}`+"\n")
	if _, err := s.Load(ctx, "s5"); err == nil {
		t.Fatal("message line without message accepted")
	}
}

// HN-FR-14
func TestSessionID(t *testing.T) {
	ctx := context.Background()
	_, s := open(t)
	for _, bad := range []string{"", ".", "..", "a/b", "../x", "a b", strings.Repeat("x", 129)} {
		if _, err := s.Load(ctx, bad); err == nil {
			t.Errorf("Load accepted %q", bad)
		}
		if err := s.AppendMessages(ctx, bad, []contract.Message{user("u", "x")}); err == nil {
			t.Errorf("Append accepted %q", bad)
		}
	}
	for _, good := range []string{"a", "A.b_c-1", strings.Repeat("x", 128)} {
		if err := session.CheckID(good); err != nil {
			t.Errorf("rejected %q: %v", good, err)
		}
	}
	id := session.NewID(ts, strings.NewReader("\x01\x02\x03\x04"))
	if id != "20260930-120000-01020304" || session.CheckID(id) != nil {
		t.Fatalf("NewID %q", id)
	}
	if !regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{8}$`).MatchString(session.NewID(ts, strings.NewReader(""))) {
		t.Fatal("NewID with short entropy")
	}
}

func TestNewAndErrors(t *testing.T) {
	ctx := context.Background()
	e := envtest.New("/w", "/h")
	if _, err := session.New(e, "rel/dir"); err == nil {
		t.Fatal("relative dir accepted")
	}
	s, _ := session.New(e, "/h/sessions")
	e.FailOn("/h/sessions/s1.jsonl", errors.New("disk"))
	if _, err := s.Load(ctx, "s1"); err == nil {
		t.Fatal("read error ignored")
	}
	if err := s.AppendEdits(ctx, "s1", []contract.ContextEdit{{ID: "e", Kind: contract.EditHide, Timestamp: ts, Target: "x"}}); err == nil {
		t.Fatal("append error ignored")
	}
}

// HN-FR-15
func TestDefaultDir(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if got := session.DefaultDir(getenv, "/home/u"); got != "/home/u/.dynamic_react/sessions" {
		t.Fatalf("default %q", got)
	}
	env["DR_SESSION_DIR"] = "/data/s"
	if got := session.DefaultDir(getenv, "/home/u"); got != "/data/s" {
		t.Fatalf("env %q", got)
	}
}

// HN-FR-16
func TestConcurrentAppend(t *testing.T) {
	ctx := context.Background()
	_, s := open(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.AppendMessages(ctx, "s1", []contract.Message{user(fmt.Sprint("u", i), "x")})
		}(i)
	}
	wg.Wait()
	if sess, err := s.Load(ctx, "s1"); err != nil || len(sess.Messages) != 20 {
		t.Fatalf("%v %d", err, len(sess.Messages))
	}
}
