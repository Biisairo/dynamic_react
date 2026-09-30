package contract

import "time"

// ContextEdit 은 원본을 바꾸지 않고 투영만 바꾸는 편집이다. 대상은 Message.ID 로 가리킨다 (CT-FR-24, 25).
//   - replace_range: From~To 구간을 Messages 로 대체 (압축)
//   - hide: Target 숨김
//   - truncate_after: Target 이후 제외
type ContextEdit struct {
	ID        string    `json:"id"`
	Kind      EditKind  `json:"kind"`
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	Messages  []Message `json:"messages,omitempty"`
	Target    string    `json:"target,omitempty"`
}

func (e ContextEdit) Validate() error {
	if e.ID == "" {
		return invalid("contextEdit: id required")
	}
	if e.Timestamp.IsZero() {
		return invalid("contextEdit %q: timestamp required", e.ID)
	}
	switch e.Kind {
	case EditReplaceRange:
		if e.From == "" || e.To == "" {
			return invalid("contextEdit %q: from and to required", e.ID)
		}
		if e.Target != "" {
			return invalid("contextEdit %q: target not allowed", e.ID)
		}
		for i, m := range e.Messages {
			if err := m.Validate(); err != nil {
				return invalidAt(err, "contextEdit %q messages[%d]", e.ID, i)
			}
		}
	case EditHide, EditTruncateAfter:
		if e.Target == "" {
			return invalid("contextEdit %q: target required", e.ID)
		}
		if e.From != "" || e.To != "" || len(e.Messages) > 0 {
			return invalid("contextEdit %q: from, to and messages not allowed", e.ID)
		}
	default:
		return invalid("contextEdit %q: kind %q", e.ID, e.Kind)
	}
	return nil
}
