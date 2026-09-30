package contract

import "time"

// Message 는 대화의 메시지 하나다 (CT-FR-01, 01a).
// Role == RoleToolResult 이면 Result 가 있고 Content 는 비어 있다.
// Usage·StopReason 은 assistant 메시지에만 쓴다.
type Message struct {
	ID         string      `json:"id"`
	Role       Role        `json:"role"`
	Content    []Block     `json:"content,omitempty"`
	Result     *ToolResult `json:"result,omitempty"`
	Usage      *Usage      `json:"usage,omitempty"`
	StopReason *StopReason `json:"stopReason,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// Block 은 콘텐츠 블록이다. Kind 에 해당하는 필드만 채운다 (CT-FR-02, 29).
type Block struct {
	Kind         BlockKind     `json:"kind"`
	Text         string        `json:"text,omitempty"`
	Image        *Image        `json:"image,omitempty"`
	ToolCall     *ToolCall     `json:"toolCall,omitempty"`
	Opaque       *Opaque       `json:"opaque,omitempty"`
	ProviderData *ProviderData `json:"providerData,omitempty"`
}

// Image 는 image 블록의 내용이다 (CT-FR-32).
type Image struct {
	MediaType string `json:"mediaType"`
	Data      []byte `json:"data"`
}

// Opaque 는 adapter 가 해석하지 않고 보관·재생하는 블록이다 (CT-FR-05).
type Opaque struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Payload  []byte `json:"payload,omitempty"`
}

// ProviderData 는 provider 전용 값(서명, 암호화 reasoning 등)이다 (CT-FR-04).
// 같은 Provider·Model 로 가는 요청에만 재전송해야 한다. 다른 곳으로 가는 요청에서는 버린다.
type ProviderData struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Payload  []byte `json:"payload,omitempty"`
}

func (m Message) Validate() error {
	if !m.Role.Valid() {
		return invalid("message %q: role %q", m.ID, m.Role)
	}
	if m.ID == "" {
		return invalid("message: id required")
	}
	if m.Timestamp.IsZero() {
		return invalid("message %q: timestamp required", m.ID)
	}
	if m.Role == RoleToolResult {
		if m.Result == nil {
			return invalid("message %q: tool_result requires result", m.ID)
		}
		if len(m.Content) > 0 {
			return invalid("message %q: tool_result must not have content", m.ID)
		}
		if err := m.Result.Validate(); err != nil {
			return err
		}
	} else if m.Result != nil {
		return invalid("message %q: result only for tool_result", m.ID)
	}
	if m.Role != RoleAssistant && (m.Usage != nil || m.StopReason != nil) {
		return invalid("message %q: usage and stopReason only for assistant", m.ID)
	}
	if m.Usage != nil {
		if err := m.Usage.Validate(); err != nil {
			return err
		}
	}
	if m.StopReason != nil {
		if err := m.StopReason.Validate(); err != nil {
			return err
		}
	}
	for i, b := range m.Content {
		if err := b.Validate(); err != nil {
			return invalidAt(err, "message %q content[%d]", m.ID, i)
		}
	}
	return nil
}

func (b Block) Validate() error {
	switch b.Kind {
	case BlockText, BlockThinking:
		if b.Image != nil || b.ToolCall != nil || b.Opaque != nil {
			return invalid("block %s: foreign fields", b.Kind)
		}
	case BlockImage:
		if b.Image == nil {
			return invalid("block image: image required")
		}
		if b.Text != "" || b.ToolCall != nil || b.Opaque != nil {
			return invalid("block image: foreign fields")
		}
		if b.Image.MediaType == "" || len(b.Image.Data) == 0 {
			return invalid("block image: mediaType and data required")
		}
	case BlockToolCall:
		if b.ToolCall == nil {
			return invalid("block tool_call: toolCall required")
		}
		if b.Text != "" || b.Image != nil || b.Opaque != nil {
			return invalid("block tool_call: foreign fields")
		}
		if err := b.ToolCall.Validate(); err != nil {
			return err
		}
	case BlockOpaque:
		if b.Opaque == nil {
			return invalid("block opaque: opaque required")
		}
		if b.Text != "" || b.Image != nil || b.ToolCall != nil {
			return invalid("block opaque: foreign fields")
		}
		if b.Opaque.Provider == "" || b.Opaque.Kind == "" {
			return invalid("block opaque: provider and kind required")
		}
	default:
		return invalid("block: kind %q", b.Kind)
	}
	if b.ProviderData != nil {
		return b.ProviderData.Validate()
	}
	return nil
}

func (p ProviderData) Validate() error {
	if p.Provider == "" || p.Model == "" {
		return invalid("providerData: provider and model required")
	}
	return nil
}
