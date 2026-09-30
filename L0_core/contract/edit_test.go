package contract_test

import (
	"testing"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

func TestContextEditValidate(t *testing.T) {
	summary := userMsg("s1", "summary")
	replace := contract.ContextEdit{ID: "e1", Kind: contract.EditReplaceRange, Timestamp: ts, From: "m1", To: "m9", Messages: []contract.Message{summary}}
	hide := contract.ContextEdit{ID: "e2", Kind: contract.EditHide, Timestamp: ts, Target: "m3"}
	trunc := contract.ContextEdit{ID: "e3", Kind: contract.EditTruncateAfter, Timestamp: ts, Target: "m3"}
	mod := func(e contract.ContextEdit, f func(*contract.ContextEdit)) contract.ContextEdit { f(&e); return e }

	runValidate(t, []validateCase{
		{"replace", replace, true},
		{"replace same message", mod(replace, func(e *contract.ContextEdit) { e.To = "m1" }), true},
		{"replace no messages", mod(replace, func(e *contract.ContextEdit) { e.Messages = nil }), true},
		{"hide", hide, true},
		{"truncate", trunc, true},
		{"zero", contract.ContextEdit{}, false},
		{"no id", mod(hide, func(e *contract.ContextEdit) { e.ID = "" }), false},
		{"no timestamp", mod(hide, func(e *contract.ContextEdit) { e.Timestamp = time.Time{} }), false},
		{"bad kind", mod(hide, func(e *contract.ContextEdit) { e.Kind = "delete" }), false},
		{"replace no from", mod(replace, func(e *contract.ContextEdit) { e.From = "" }), false},
		{"replace no to", mod(replace, func(e *contract.ContextEdit) { e.To = "" }), false},
		{"replace invalid message", mod(replace, func(e *contract.ContextEdit) { e.Messages = []contract.Message{{}} }), false},
		{"replace with target", mod(replace, func(e *contract.ContextEdit) { e.Target = "m1" }), false},
		{"hide no target", mod(hide, func(e *contract.ContextEdit) { e.Target = "" }), false},
		{"hide with from", mod(hide, func(e *contract.ContextEdit) { e.From = "m1" }), false},
		{"hide with to", mod(hide, func(e *contract.ContextEdit) { e.To = "m1" }), false},
		{"hide with messages", mod(hide, func(e *contract.ContextEdit) { e.Messages = []contract.Message{summary} }), false},
		{"truncate no target", mod(trunc, func(e *contract.ContextEdit) { e.Target = "" }), false},
		{"truncate with messages", mod(trunc, func(e *contract.ContextEdit) { e.Messages = []contract.Message{summary} }), false},
	})
}
