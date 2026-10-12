package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

func layoutStr(l []harness.SetState) string {
	var out []string
	for _, s := range l {
		mark := "-"
		if s.On {
			mark = "+"
		}
		out = append(out, mark+s.Name)
	}
	return strings.Join(out, " ")
}

func layoutOf(spec string) []harness.SetState {
	var out []harness.SetState
	for _, f := range strings.Fields(spec) {
		out = append(out, harness.SetState{Name: f[1:], On: f[0] == '+'})
	}
	return out
}

const allOn = "+base +require +cache +guard +truncate +resilient"

// AG-FR-02, 44: sets 형식 에러는 New 에러
func TestConfigSetsFormat(t *testing.T) {
	for name, v := range map[string]string{
		"not array":   `1`,
		"empty name":  `[{"name":"","on":true}]`,
		"missing on":  `[{"name":"guard"}]`,
		"unknown key": `[{"name":"guard","on":true,"x":1}]`,
		"on type":     `[{"name":"guard","on":"yes"}]`,
	} {
		f := setup(t)
		f.writeConfig(t, testConfig("http://127.0.0.1:1", `,"sets":`+v))
		_, err := agent.New(context.Background(), f.opts())
		if err == nil || !strings.Contains(err.Error(), "agent: config: sets:") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// AG-FR-44: 저장된 배치가 harness 에 반영된다 (모르는 이름 무시, 빠진 세트는 뒤에). 배치가 Bind 에 실패하면 New 에러.
func TestConfigSetsLayout(t *testing.T) {
	f := setup(t)
	f.writeConfig(t, testConfig("http://127.0.0.1:1", `,"sets":[{"name":"guard","on":false},{"name":"ghost","on":true},{"name":"base","on":true}]`))
	a := mkAgent(t, f.opts())
	if got := layoutStr(a.Sets()); got != "-guard +base +require +cache +truncate +resilient" {
		t.Fatalf("layout %s", got)
	}
	if got := layoutStr(a.Harness().Sets()); got != layoutStr(a.Sets()) {
		t.Fatalf("harness layout %s", got)
	}
	f.writeConfig(t, testConfig("http://127.0.0.1:1", `,"sets":[{"name":"require","on":false}]`))
	if _, err := agent.New(context.Background(), f.opts()); err == nil || !strings.Contains(err.Error(), "agent: harness: sets:") {
		t.Fatalf("bad layout accepted: %v", err)
	}
}

// AG-FR-45: 성공하면 파일의 sets 가 새 배치 전체, 다른 키 보존, 다음 New 가 그 배치
func TestArrangeSetsSaves(t *testing.T) {
	f := setup(t)
	p := f.writeConfig(t, testConfig("http://127.0.0.1:1", `,"permissions":{"allow":["read"]}`))
	a := mkAgent(t, f.opts())
	want := "+base +require +cache -guard +resilient +truncate"
	if err := a.ArrangeSets(context.Background(), layoutOf(want)); err != nil {
		t.Fatal(err)
	}
	if got := layoutStr(a.Sets()); got != want {
		t.Fatalf("layout %s", got)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readFile(t, p)), &top); err != nil {
		t.Fatal(err)
	}
	var orig map[string]json.RawMessage
	json.Unmarshal([]byte(testConfig("http://127.0.0.1:1", `,"permissions":{"allow":["read"]}`)), &orig)
	for k, v := range orig {
		var x, y any
		json.Unmarshal(v, &x)
		json.Unmarshal(top[k], &y)
		if !reflectEqual(x, y) {
			t.Errorf("key %s changed: %s → %s", k, v, top[k])
		}
	}
	if string(compact(t, top["sets"])) != `[{"name":"base","on":true},{"name":"require","on":true},{"name":"cache","on":true},{"name":"guard","on":false},{"name":"resilient","on":true},{"name":"truncate","on":true}]` {
		t.Fatalf("saved sets %s", top["sets"])
	}
	if got := layoutStr(mkAgent(t, f.opts()).Sets()); got != want {
		t.Fatalf("reloaded %s", got)
	}
}

func reflectEqual(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}

func compact(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return b
}

// AG-FR-45: harness 가 거부하면 파일을 건드리지 않는다
func TestArrangeSetsRejected(t *testing.T) {
	f := setup(t)
	p := f.writeConfig(t, testConfig("http://127.0.0.1:1", ""))
	before := readFile(t, p)
	a := mkAgent(t, f.opts())
	for _, l := range []string{"+base -require +cache +guard +truncate +resilient", "+base +require"} {
		if err := a.ArrangeSets(context.Background(), layoutOf(l)); err == nil {
			t.Errorf("%s accepted", l)
		}
	}
	if err := a.ArrangeSets(context.Background(), layoutOf("+base +require")); !errors.Is(err, harness.ErrSetLayout) {
		t.Fatalf("not ErrSetLayout: %v", err)
	}
	if readFile(t, p) != before || layoutStr(a.Sets()) != allOn {
		t.Fatalf("changed: %s %s", readFile(t, p), layoutStr(a.Sets()))
	}
}

// AG-FR-45: 파일 쓰기 실패면 에러이고 harness 배치를 되돌린다
func TestArrangeSetsWriteFailure(t *testing.T) {
	f := setup(t)
	a := mkAgent(t, f.opts())
	dir := filepath.Join(f.home, ".dynamic_react")
	os.MkdirAll(dir, 0o500)
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := a.ArrangeSets(context.Background(), layoutOf("+base +require +cache -guard +truncate +resilient")); err == nil {
		t.Fatal("write into read-only dir succeeded")
	}
	if got := layoutStr(a.Sets()); got != allOn {
		t.Fatalf("not rolled back: %s", got)
	}
	if got := layoutStr(a.Harness().Sets()); got != allOn {
		t.Fatalf("harness not rolled back: %s", got)
	}
}

// AG-FR-46: 동시 호출 (-race)
func TestArrangeSetsConcurrent(t *testing.T) {
	f := setup(t)
	f.writeConfig(t, testConfig("http://127.0.0.1:1", ""))
	a := mkAgent(t, f.opts())
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := allOn
			if i%2 == 0 {
				l = "+base +require +cache -guard +truncate +resilient"
			}
			if err := a.ArrangeSets(context.Background(), layoutOf(l)); err != nil {
				t.Error(err)
			}
			_ = a.Sets()
			_ = a.SetModel(context.Background(), "test/m")
		}()
	}
	wg.Wait()
}
