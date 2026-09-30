package inject_test

import (
	"context"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
)

// IN-NFR-01: in-process dispatch 의 inject 자체 오버헤드. 목표 20µs 이하.
func BenchmarkDispatch(b *testing.B) {
	d, err := inject.Bind(workingSet("w"))
	if err != nil {
		b.Fatal(err)
	}
	v := facts(core.StepTurnFinish).View()
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		if _, err := d.DispatchSource(ctx, core.StepTurnFinish, nil, v); err != nil {
			b.Fatal(err)
		}
	}
}
