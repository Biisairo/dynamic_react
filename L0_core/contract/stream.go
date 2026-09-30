package contract

// StreamEvent 는 모델 응답 스트림의 이벤트 하나다. Kind 에 해당하는 필드만 채운다 (CT-FR-14, 15, 17).
type StreamEvent struct {
	Kind     StreamEventKind `json:"kind"`
	Index    int             `json:"index"`
	Delta    string          `json:"delta,omitempty"`
	ToolCall *ToolCall       `json:"toolCall,omitempty"`
	Usage    *Usage          `json:"usage,omitempty"`
	Message  *Message        `json:"message,omitempty"`
	Error    *CallError      `json:"error,omitempty"`
}

func (e StreamEvent) Validate() error {
	if !e.Kind.Valid() {
		return invalid("streamEvent: kind %q", e.Kind)
	}
	if e.Index < 0 {
		return invalid("streamEvent %s: index < 0", e.Kind)
	}
	hasDelta := e.Delta != ""
	switch e.Kind {
	case EventStart:
		if hasDelta || e.ToolCall != nil || e.Usage != nil || e.Message != nil || e.Error != nil {
			return invalid("streamEvent start: foreign fields")
		}
	case EventTextDelta, EventThinkingDelta, EventToolCallDelta:
		if !hasDelta {
			return invalid("streamEvent %s: delta required", e.Kind)
		}
		if e.ToolCall != nil || e.Usage != nil || e.Message != nil || e.Error != nil {
			return invalid("streamEvent %s: foreign fields", e.Kind)
		}
	case EventToolCallStart, EventToolCallEnd:
		if e.ToolCall == nil {
			return invalid("streamEvent %s: toolCall required", e.Kind)
		}
		if hasDelta || e.Usage != nil || e.Message != nil || e.Error != nil {
			return invalid("streamEvent %s: foreign fields", e.Kind)
		}
		if e.Kind == EventToolCallEnd {
			return e.ToolCall.Validate()
		}
		if e.ToolCall.ID == "" || e.ToolCall.Name == "" {
			return invalid("streamEvent toolcall_start: id and name required")
		}
	case EventUsage:
		if e.Usage == nil {
			return invalid("streamEvent usage: usage required")
		}
		if hasDelta || e.ToolCall != nil || e.Message != nil || e.Error != nil {
			return invalid("streamEvent usage: foreign fields")
		}
		return e.Usage.Validate()
	case EventDone:
		if e.Message == nil {
			return invalid("streamEvent done: message required")
		}
		if hasDelta || e.ToolCall != nil || e.Usage != nil || e.Error != nil {
			return invalid("streamEvent done: foreign fields")
		}
		m := e.Message
		if m.Role != RoleAssistant || m.Usage == nil || m.StopReason == nil {
			return invalid("streamEvent done: assistant message with usage and stopReason required")
		}
		return m.Validate()
	case EventError:
		if e.Error == nil {
			return invalid("streamEvent error: error required")
		}
		if hasDelta || e.ToolCall != nil || e.Usage != nil || e.Message != nil {
			return invalid("streamEvent error: foreign fields")
		}
		return e.Error.Validate()
	}
	return nil
}

// ValidateStream 은 각 이벤트와, 스트림이 done 또는 error 하나로 끝나는지를 검사한다 (CT-FR-16, 35).
func ValidateStream(events []StreamEvent) error {
	if len(events) == 0 {
		return invalid("stream: empty")
	}
	last := len(events) - 1
	for i, e := range events {
		if err := e.Validate(); err != nil {
			return invalidAt(err, "stream[%d]", i)
		}
		terminal := e.Kind == EventDone || e.Kind == EventError
		if terminal != (i == last) {
			return invalid("stream[%d]: exactly one terminal event, last", i)
		}
	}
	return nil
}
