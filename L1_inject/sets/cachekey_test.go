package sets_test

import (
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L0_core/core"
	"github.com/Biisairo/dynamic_react/L1_inject/inject/injecttest"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// ST-FR-62: cache 는 CacheKey 를 세션 ID 로 채운다. cache 가 없으면 비어 있다.
func TestCacheKey(t *testing.T) {
	d := deps(&fakeModel{})
	req := contract.Request{Messages: msgs("u1"), Options: contract.Options{CacheKey: "below"}}
	res, err := stackStep(t, core.StepRequestFinalize, req, injecttest.Facts{SessionID: "sess-1", RunID: "r1"}, stack(d, false)...)
	if err != nil || res.Output.(contract.Request).Options.CacheKey != "sess-1" {
		t.Fatalf("%v %+v", err, res.Output)
	}
	res, err = stackStep(t, core.StepRequestFinalize, contract.Request{Messages: msgs("u1")}, injecttest.Facts{SessionID: "sess-1", RunID: "r1"},
		sets.Base(d), sets.Require(d))
	if err != nil || res.Output.(contract.Request).Options.CacheKey != "" {
		t.Fatalf("without cache %v %+v", err, res.Output)
	}
}
