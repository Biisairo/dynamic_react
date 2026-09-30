package contract

import "encoding/json"

// Request 는 방언과 무관한 모델 요청이다 (CT-FR-11).
type Request struct {
	Model    ModelRef   `json:"model"`
	System   []Block    `json:"system,omitempty"`
	Messages []Message  `json:"messages"`
	Tools    []ToolSpec `json:"tools,omitempty"`
	Options  Options    `json:"options"`
}

// Options 는 요청 옵션이다 (CT-FR-12).
type Options struct {
	MaxOutputTokens int                        `json:"maxOutputTokens,omitempty"`
	Thinking        string                     `json:"thinking,omitempty"`
	ToolChoice      *ToolChoice                `json:"toolChoice,omitempty"`
	CacheHints      []CacheHint                `json:"cacheHints,omitempty"`
	Extra           map[string]json.RawMessage `json:"extra,omitempty"`
}

// ToolChoice 는 도구 선택 지시다. nil = provider 기본 (CT-FR-33).
type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode"`
	Name string         `json:"name,omitempty"`
}

// CacheHint 는 캐시 지점이다. Index 는 Target 영역(System 또는 Messages)의 인덱스이고,
// 힌트는 그 위치의 마지막 블록에 붙는다 (CT-FR-12).
type CacheHint struct {
	Target CacheTarget `json:"target"`
	Index  int         `json:"index"`
}

// ModelRef 는 모델 식별자다 (CT-FR-13).
type ModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	API      string `json:"api"`
}

// Validate 는 Model 을 검사하지 않는다. Model 은 model.select 이후 채워진다.
func (r Request) Validate() error {
	if len(r.Messages) == 0 {
		return invalid("request: messages required")
	}
	for i, b := range r.System {
		if b.Kind != BlockText {
			return invalid("request system[%d]: kind %q not allowed", i, b.Kind)
		}
		if err := b.Validate(); err != nil {
			return invalidAt(err, "request system[%d]", i)
		}
	}
	for i, m := range r.Messages {
		if err := m.Validate(); err != nil {
			return invalidAt(err, "request messages[%d]", i)
		}
	}
	seen := make(map[string]bool, len(r.Tools))
	for i, t := range r.Tools {
		if err := t.Validate(); err != nil {
			return invalidAt(err, "request tools[%d]", i)
		}
		if seen[t.Name] {
			return invalid("request: duplicate tool %q", t.Name)
		}
		seen[t.Name] = true
	}
	if err := r.Options.Validate(); err != nil {
		return err
	}
	for i, h := range r.Options.CacheHints {
		n := len(r.Messages)
		if h.Target == CacheSystem {
			n = len(r.System)
		}
		if h.Index >= n {
			return invalid("request cacheHints[%d]: index %d out of range", i, h.Index)
		}
	}
	return nil
}

func (o Options) Validate() error {
	if o.MaxOutputTokens < 0 {
		return invalid("options: maxOutputTokens < 0")
	}
	if c := o.ToolChoice; c != nil {
		if !c.Mode.Valid() {
			return invalid("options toolChoice: mode %q", c.Mode)
		}
		if (c.Mode == ToolChoiceTool) != (c.Name != "") {
			return invalid("options toolChoice: name required only for mode tool")
		}
	}
	for i, h := range o.CacheHints {
		if !h.Target.Valid() || h.Index < 0 {
			return invalid("options cacheHints[%d]: target %q index %d", i, h.Target, h.Index)
		}
	}
	return nil
}

func (r ModelRef) Validate() error {
	if r.Provider == "" || r.ID == "" || r.API == "" {
		return invalid("modelRef: provider, id and api required")
	}
	return nil
}
