package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// choiceConfig 는 키가 필요 없는 "test"(모델 a01..a12)와 키가 필요한 "keyed"(모델 k)를 등록한 설정이다.
// agent 가 "" 이면 모델이 없다.
func choiceConfig(url, agent string) string {
	var models []string
	for i := 1; i <= 12; i++ {
		models = append(models, fmt.Sprintf(`"test/a%02d":{}`, i))
	}
	return `{"agent":{` + agent + `},` +
		`"providers":{"test":{"api":"openai-chat","baseUrl":"` + url + `"},` +
		`"keyed":{"api":"openai-chat","baseUrl":"` + url + `","requiresKey":true}},` +
		`"models":{` + strings.Join(models, ",") + `,"keyed/k":{}}}`
}

// region 은 화면에서 마지막 status 줄부터 끝까지다 (다시 그리는 영역).
func region(s, status string) string {
	i := strings.LastIndex(s, status)
	if i < 0 {
		return ""
	}
	return s[i:]
}

func requestModel(t *testing.T, body string) string {
	t.Helper()
	var req struct{ Model string }
	json.Unmarshal([]byte(body), &req)
	return req.Model
}

// TM-FR-48~50: 모델 없이 시작 → 선택 화면, 필터, 10개 상한, 창 이동, Enter 저장
func TestRunModelPick(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), choiceConfig(l.URL, ""), Start{})
	h.wait("⚠ 모델이 설정되지 않았습니다")
	const status = "모델 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소"
	h.waitFn("full list", func(s string) bool {
		r := region(s, status)
		return strings.HasPrefix(r, status+"\n▶ test/a01\n  test/a02\n") && strings.Contains(r, "  test/a10\n  … +2개\n모델:") &&
			!strings.Contains(r, "a11") && !strings.Contains(r, "keyed")
	})
	h.typ("A1")
	h.waitFn("filter a1", func(s string) bool {
		return strings.HasSuffix(region(s, status), status+"\n▶ test/a10\n  test/a11\n  test/a12\n모델: A1")
	})
	h.typ(" 2")
	h.waitFn("filter and", func(s string) bool {
		return strings.HasSuffix(region(s, status), status+"\n▶ test/a12\n모델: A1 2")
	})
	h.typ("zz")
	h.waitFn("no match", func(s string) bool {
		return strings.HasSuffix(region(s, status), status+"\n  (일치하는 모델 없음)\n모델: A1 2zz")
	})
	h.typ("\r")
	h.typ(strings.Repeat("\x7f", 6))
	h.waitFn("cleared", func(s string) bool { return strings.HasSuffix(region(s, status), "  … +2개\n모델:") })
	h.typ("\x1b[A")
	h.typ(strings.Repeat("\x1b[B", 11))
	h.typ("\x1b[B")
	h.waitFn("window moved", func(s string) bool {
		r := region(s, status)
		return strings.HasPrefix(r, status+"\n  test/a03\n") && strings.Contains(r, "▶ test/a12\n  … +2개\n모델:")
	})
	for range 10 {
		h.typ("\x1b[A")
	}
	h.waitFn("window back", func(s string) bool {
		r := region(s, status)
		return strings.HasPrefix(r, status+"\n▶ test/a02\n  test/a03\n") && strings.Contains(r, "  test/a11\n  … +2개")
	})
	h.typ("a")
	h.waitFn("highlight reset", func(s string) bool { return strings.HasPrefix(region(s, status), status+"\n▶ test/a01\n") })
	h.typ("\x7f\x1b[B\x1b[B\r")
	h.wait("── 모델 test/a03 (저장됨)")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	b, _ := os.ReadFile(filepath.Join(e.home, ".dynamic_react", "config.json"))
	if !strings.Contains(string(b), `"id": "a03"`) {
		t.Fatalf("config %s", b)
	}
	h.typ("hi\r")
	h.wait("── completed")
	if requestModel(t, l.body(0)) != "a03" {
		t.Fatalf("model %s", l.body(0))
	}
}

// TM-FR-16, 47a, 50: 취소 뒤 전송 에러, /model 인자, 실행 중 거부
func TestRunModelCommand(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), choiceConfig(l.URL, ""), Start{})
	h.wait("모델:")
	h.typ("\x1b")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("hi\r")
	h.wait("✗ no model configured (/model)")
	h.typ("/model bad\r")
	h.wait(`✗ agent: model "bad": want provider/id`)
	h.typ("/model keyed/k\r")
	h.wait(`✗ missing API key for "keyed"`)
	h.typ("/model\r")
	h.wait("모델:")
	h.typ("\x03")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("/model test/a02\r")
	h.wait("── 모델 test/a02 (저장됨)")
	gate := l.block()
	h.typ("hi\r")
	h.wait("● 실행 중")
	h.typ("/model test/a01\r")
	h.wait("✗ 실행 중에는 모델을 바꿀 수 없습니다")
	h.typ("/login keyed\r")
	h.wait("✗ 실행 중에는 키를 바꿀 수 없습니다")
	h.typ("/logout\r")
	h.waitCount("✗ 실행 중에는 키를 바꿀 수 없습니다", 2)
	gate <- struct{}{}
	h.wait("── completed")
	if requestModel(t, l.body(0)) != "a02" {
		t.Fatalf("model %s", l.body(0))
	}
	h.typ("/help\r")
	h.wait("/model [provider/id] · /login [provider] · /logout [provider]")
}

// TM-FR-48, 52, 53: 시작 시 키 없음 → 키 입력, 가림, 저장, /login·/logout
func TestRunLogin(t *testing.T) {
	const secret = "sk-secret-123"
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), choiceConfig(l.URL, `"model":{"provider":"keyed","id":"k"}`), Start{})
	h.wait(`⚠ missing API key for "keyed"`)
	const status = "키 입력 · Enter 저장 · Esc 취소"
	h.waitFn("key prompt", func(s string) bool { return strings.HasSuffix(s, status+"\nkeyed 키:") })
	h.typ("abc")
	h.waitFn("masked", func(s string) bool { return strings.HasSuffix(s, "\nkeyed 키: •••") })
	h.typ("\x1b")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("hi\r")
	h.wait(`✗ missing API key for "keyed" (/login)`)
	h.typ("/login nope\r")
	h.wait(`✗ unknown provider "nope"`)
	h.typ("/login keyed\r")
	h.waitFn("key prompt", func(s string) bool { return strings.HasSuffix(s, "\nkeyed 키:") })
	h.typ("\r")
	h.typ("  \r")
	h.wait("✗ empty API key")
	h.waitFn("still key prompt", func(s string) bool { return strings.HasSuffix(s, "\nkeyed 키:") })
	h.typ(secret + "\r")
	h.wait("── 키 저장됨 keyed")
	if n := strings.Count(h.scr.text(), "✗ empty API key"); n != 1 {
		t.Fatalf("empty Enter: %d error lines", n)
	}
	h.typ("hi\r")
	h.wait("── completed")
	if got := l.header(0).Get("Authorization"); got != "Bearer "+secret {
		t.Fatalf("Authorization %q", got)
	}
	h.typ("\x1b[A")
	h.waitFn("history is not the key", func(s string) bool { return strings.HasSuffix(s, "\n> hi") })
	h.typ("\x03")

	const pstatus = "provider 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소"
	h.typ("/login\r")
	h.waitFn("provider list", func(s string) bool {
		r := region(s, pstatus)
		return strings.Contains(r, "\n▶ ant-ling\n  anthropic\n") && strings.Contains(r, "  keyed (저장됨)\n") &&
			!strings.Contains(r, "  test\n") && strings.HasSuffix(r, "provider:")
	})
	h.typ("test")
	h.waitFn("no key needed", func(s string) bool {
		return strings.HasSuffix(region(s, pstatus), pstatus+"\n  (일치하는 provider 없음)\nprovider: test")
	})
	h.typ("\x7f\x7f\x7f\x7fkeyed")
	h.waitFn("filtered", func(s string) bool {
		return strings.HasSuffix(region(s, pstatus), pstatus+"\n▶ keyed (저장됨)\nprovider: keyed")
	})
	h.typ("\r")
	h.waitFn("key prompt", func(s string) bool { return strings.HasSuffix(s, "\nkeyed 키:") })
	h.typ("\x03")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })

	h.typ("/logout\r")
	h.waitFn("logout list", func(s string) bool {
		return strings.HasSuffix(region(s, pstatus), pstatus+"\n▶ keyed (저장됨)\nprovider:")
	})
	h.typ("\r")
	h.wait("── 키 삭제됨 keyed")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("/logout\r")
	h.wait("✗ 저장된 키가 없습니다")
	h.typ("/logout keyed\r")
	h.wait(`✗ no stored API key for "keyed"`)
	h.typ("hi\r")
	h.waitCount(`✗ missing API key for "keyed" (/login)`, 2)
	h.typ("/quit\r")
	if err := h.exit(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.scr.bytes(), secret) {
		t.Fatal("key echoed to the terminal")
	}
}

// TM-FR-48: Pick 이 있으면 세션 선택이 끝난 뒤 모델 선택
func TestRunModelAfterPick(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	h := e.start(t, context.Background(), choiceConfig(l.URL, ""), Start{Pick: true})
	h.wait(strings.TrimSpace(promptPick))
	if strings.Contains(h.scr.text(), "⚠") {
		t.Fatal("model warning before pick")
	}
	h.typ("\r")
	h.wait("── 새 세션\n⚠ 모델이 설정되지 않았습니다")
	h.wait("모델:")
	h.typ("\x1b")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	h.typ("/resume\r")
	h.wait(strings.TrimSpace(promptPick))
	h.typ("\x1b")
	h.waitCount("── 새 세션", 2)
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
	if n := strings.Count(h.scr.text(), "⚠"); n != 1 {
		t.Fatalf("model warning %d times", n)
	}
}

// TM-FR-49: 메모가 폭을 넘으면 줄을 자른다
func TestChooseLinesTruncate(t *testing.T) {
	term := New(Options{Width: func() int { return 14 }, Color: true})
	term.ch = &chooser{kind: chLogin, items: []choice{{label: "abc", note: savedNote}, {label: "abcdefgh", note: savedNote}}}
	got := term.chooseLines()
	want := []string{"▶ abc\x1b[2m (저장됨)\x1b[0m", "  abcdefgh (…"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("%q", got)
	}
}

// TM-FR-49: 쓸 수 있는 모델이 없으면 /login 안내
func TestRunNoModels(t *testing.T) {
	e := newEnv(t)
	l := newLLM(t, textReply("ok"))
	cfg := `{"providers":{"keyed":{"api":"openai-chat","baseUrl":"` + l.URL + `","requiresKey":true}},"models":{"keyed/k":{}}}`
	h := e.start(t, context.Background(), cfg, Start{})
	h.waitFn("empty list", func(s string) bool {
		return strings.HasSuffix(s, "모델 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소\n  (쓸 수 있는 모델 없음 — /login)\n모델:")
	})
	h.typ("\r\x1b")
	h.waitFn("idle", func(s string) bool { return strings.HasSuffix(s, "\n>") })
}

// TM-FR-49, 53: 목록이 있으면 커서는 입력 줄에, 키 입력은 가려진다
func TestChooseView(t *testing.T) {
	term := New(Options{Width: func() int { return 80 }})
	term.st, term.ch = stChoose, &chooser{kind: chModel, items: []choice{{label: "p/a"}, {label: "p/b"}}}
	term.ed.insert("p")
	v := term.view()
	if v.status != statusModel || len(v.lines) != 3 || v.lines[2] != promptModel+"p" || v.line != 2 || v.col != len([]rune(promptModel))+1 {
		t.Fatalf("%+v", v)
	}
	term.st, term.ch, term.keyFor = stKey, nil, "x"
	term.secret.insert("ab")
	if v := term.view(); v.status != statusKey || len(v.lines) != 1 || v.lines[0] != "x 키: ••" || v.line != 0 || v.col != len([]rune("x 키: "))+2 {
		t.Fatalf("%+v", v)
	}
}
