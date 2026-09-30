package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// ToolSpec 은 모델에 알리는 도구 정의다 (CT-FR-07).
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolCall 은 모델이 요청한 도구 호출이다. Args 는 항상 JSON 객체다 (CT-FR-08).
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResult 는 도구 실행 결과다. Content 는 text·image 블록만 (CT-FR-10).
type ToolResult struct {
	CallID  string     `json:"callId"`
	Status  ToolStatus `json:"status"`
	Content []Block    `json:"content,omitempty"`
}

func (s ToolSpec) Validate() error {
	if s.Name == "" {
		return invalid("toolSpec: name required")
	}
	if !isJSONObject(s.InputSchema) {
		return invalid("toolSpec %q: inputSchema must be a JSON object", s.Name)
	}
	return nil
}

func (c ToolCall) Validate() error {
	if c.ID == "" || c.Name == "" {
		return invalid("toolCall: id and name required")
	}
	if !isJSONObject(c.Args) {
		return invalid("toolCall %q: args must be a JSON object", c.ID)
	}
	return nil
}

// ArgsDigest 는 키 정렬·공백 제거로 정규화한 Args 의 SHA-256 hex 다 (CT-FR-09, 36).
// 숫자는 원문 표기로 비교한다. Args 가 JSON 이 아니면 원문 바이트를 해시한다.
func (c ToolCall) ArgsDigest() string {
	sum := sha256.Sum256(canonicalJSON(c.Args))
	return hex.EncodeToString(sum[:])
}

func (r ToolResult) Validate() error {
	if r.CallID == "" {
		return invalid("toolResult: callId required")
	}
	if !r.Status.Valid() {
		return invalid("toolResult %q: status %q", r.CallID, r.Status)
	}
	for i, b := range r.Content {
		if b.Kind != BlockText && b.Kind != BlockImage {
			return invalid("toolResult %q content[%d]: kind %q not allowed", r.CallID, i, b.Kind)
		}
		if err := b.Validate(); err != nil {
			return invalidAt(err, "toolResult %q content[%d]", r.CallID, i)
		}
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(raw)
}

func canonicalJSON(raw json.RawMessage) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}
