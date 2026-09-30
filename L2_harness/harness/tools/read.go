package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

var readSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},` +
	`"offset":{"type":"integer","description":"Line number to start reading from (1-indexed)"},` +
	`"limit":{"type":"integer","description":"Maximum number of lines to read"}},"required":["path"]}`)

// Read 는 read 도구다. 자체 절단을 하므로 Truncate 는 항등이다 (HN-FR-07).
func Read(e env.Env) sets.Tool {
	return sets.Tool{
		Spec: contract.ToolSpec{Name: "read", InputSchema: readSchema, Description: fmt.Sprintf(
			"Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are sent as attachments. "+
				"For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files. "+
				"When you need the full file, continue with offset until complete.", maxLines, maxBytes/1024)},
		Run:      func(ctx context.Context, call contract.ToolCall) contract.ToolResult { return runRead(ctx, e, call) },
		Truncate: identity,
	}
}

func runRead(ctx context.Context, e env.Env, call contract.ToolCall) contract.ToolResult {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := decode(call, &a); err != nil {
		return errResult(call.ID, err.Error())
	}
	abs, err := resolve(e, a.Path)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	data, err := e.ReadFile(ctx, abs)
	if errors.Is(err, fs.ErrNotExist) {
		return errResult(call.ID, "File not found: "+a.Path)
	}
	if err != nil {
		return errResult(call.ID, fmt.Sprintf("Cannot read %s: %v", a.Path, err))
	}
	if mt := imageType(data); mt != "" {
		return okResult(call.ID, textBlock("Read image file ["+mt+"]"),
			contract.Block{Kind: contract.BlockImage, Image: &contract.Image{MediaType: mt, Data: data}})
	}
	text, err := readText(string(data), a.Path, a.Offset, a.Limit)
	if err != nil {
		return errResult(call.ID, err.Error())
	}
	return okResult(call.ID, textBlock(text))
}

// readText 는 pi read 도구의 텍스트 선택·절단·안내 문구다.
func readText(content, name string, offset int, limit *int) (string, error) {
	all := strings.Split(content, "\n")
	start := max(offset-1, 0)
	if start >= len(all) {
		return "", fmt.Errorf("Offset %d is beyond end of file (%d lines total)", offset, len(all))
	}
	end := len(all)
	if limit != nil {
		end = min(start+max(*limit, 0), len(all))
	}
	t := truncateHead(all[start:end])
	first := start + 1
	switch {
	case t.firstLineTooLong:
		return fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			first, formatSize(len(all[start])), formatSize(maxBytes), first, name, maxBytes), nil
	case t.truncated:
		last := first + t.outputLines - 1
		note := fmt.Sprintf("[Showing lines %d-%d of %d. Use offset=%d to continue.]", first, last, len(all), last+1)
		if t.byBytes {
			note = fmt.Sprintf("[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", first, last, len(all), formatSize(maxBytes), last+1)
		}
		return t.content + "\n\n" + note, nil
	case limit != nil && end < len(all):
		return fmt.Sprintf("%s\n\n[%d more lines in file. Use offset=%d to continue.]", t.content, len(all)-end, end+1), nil
	}
	return t.content, nil
}

// imageType 은 내용 시그니처로 판별한 지원 이미지의 MediaType 이다. 아니면 "".
func imageType(b []byte) string {
	s := string(b[:min(len(b), 12)])
	switch {
	case strings.HasPrefix(s, "\x89PNG\r\n\x1a\n"):
		return "image/png"
	case strings.HasPrefix(s, "\xff\xd8\xff"):
		return "image/jpeg"
	case strings.HasPrefix(s, "GIF87a"), strings.HasPrefix(s, "GIF89a"):
		return "image/gif"
	case len(s) == 12 && s[:4] == "RIFF" && s[8:] == "WEBP":
		return "image/webp"
	}
	return ""
}
