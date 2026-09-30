package contract

// Capabilities 는 모델·방언의 기능 플래그다 (CT-FR-22).
type Capabilities struct {
	LiveInput          bool `json:"liveInput"`
	MidConvoSystem     bool `json:"midConvoSystem"`
	ToolChanges        bool `json:"toolChanges"`
	ImagesInToolResult bool `json:"imagesInToolResult"`
	NativeCompaction   bool `json:"nativeCompaction"`
	ParallelToolCalls  bool `json:"parallelToolCalls"`
	Thinking           bool `json:"thinking"`
	Vision             bool `json:"vision"`
}

// ModelInfo 는 선택된 모델의 정보다. ContextWindow 0 = 모름 (CT-FR-23).
type ModelInfo struct {
	Ref             ModelRef     `json:"ref"`
	ContextWindow   int          `json:"contextWindow"`
	MaxOutputTokens int          `json:"maxOutputTokens"`
	Capabilities    Capabilities `json:"capabilities"`
}

func (m ModelInfo) Validate() error {
	if err := m.Ref.Validate(); err != nil {
		return err
	}
	if m.ContextWindow < 0 || m.MaxOutputTokens < 0 {
		return invalid("modelInfo: negative token limit")
	}
	return nil
}
