package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/internal/stepspec"
)

// IN-FR-02: StepSpec 이 core 와 다르면 생성이 실패한다.
func TestCheckRejectsMismatch(t *testing.T) {
	if err := Check(stepspec.Rows()); err != nil {
		t.Fatalf("spec rows rejected: %v", err)
	}
	mut := map[string]func([]stepspec.Row) []stepspec.Row{
		"missing row": func(r []stepspec.Row) []stepspec.Row { return r[:21] },
		"swapped": func(r []stepspec.Row) []stepspec.Row {
			r[3], r[4] = r[4], r[3]
			return r
		},
		"renamed": func(r []stepspec.Row) []stepspec.Row {
			r[0].Name = "Start"
			return r
		},
		"no-signal flag": func(r []stepspec.Row) []stepspec.Row {
			r[19].NoSignals = true
			return r
		},
		"wrong step": func(r []stepspec.Row) []stepspec.Row {
			r[0].Step = core.StepRunSettled
			return r
		},
	}
	for name, f := range mut {
		t.Run(name, func(t *testing.T) {
			if _, err := Generate(f(stepspec.Rows())); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// IN-FR-04: 커밋된 생성 코드가 StepSpec 과 동기화돼 있다.
func TestGeneratedUpToDate(t *testing.T) {
	out, err := Generate(stepspec.Rows())
	if err != nil {
		t.Fatal(err)
	}
	for path, src := range out {
		disk, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(disk, src) {
			t.Errorf("%s is stale: run go generate", path)
		}
	}
}

func TestCamel(t *testing.T) {
	for in, want := range map[string]string{"run.beforeSettle": "RunBeforeSettle", "tools.commit": "ToolsCommit", "restart.turn": "RestartTurn"} {
		if got := camel(in); got != want {
			t.Errorf("camel(%q) = %q", in, got)
		}
	}
}
