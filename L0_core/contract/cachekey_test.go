package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// CT-FR-12 (R33): CacheKey 는 선택 값이고 JSON 으로 왕복한다.
func TestOptionsCacheKey(t *testing.T) {
	o := contract.Options{CacheKey: "s1"}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(o)
	var back contract.Options
	if err := json.Unmarshal(b, &back); err != nil || back.CacheKey != "s1" || !strings.Contains(string(b), `"cacheKey":"s1"`) {
		t.Fatalf("%s %+v %v", b, back, err)
	}
	if b, _ := json.Marshal(contract.Options{}); strings.Contains(string(b), "cacheKey") {
		t.Fatalf("empty key serialized: %s", b)
	}
}
