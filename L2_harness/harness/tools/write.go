package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

var writeSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},` +
	`"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`)

func writeTool(e env.Env, q *queue) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "write", InputSchema: writeSchema,
			Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."},
		Run: func(ctx context.Context, call contract.ToolCall) contract.ToolResult {
			var a struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := decode(call, &a); err != nil {
				return errResult(call.ID, err.Error())
			}
			abs, err := resolve(e, a.Path)
			if err != nil {
				return errResult(call.ID, err.Error())
			}
			defer q.lock(abs)()
			if err := e.WriteFile(ctx, abs, []byte(a.Content)); err != nil {
				return errResult(call.ID, fmt.Sprintf("Cannot write %s: %v", display(e, abs), err))
			}
			return okResult(call.ID, textBlock(fmt.Sprintf("Successfully wrote %d bytes to %s", len(a.Content), display(e, abs))))
		},
	}
}
