package contract

import "time"

// Usage 는 정규화된 토큰 사용량이다 (CT-FR-18).
// Input 은 캐시 제외 입력, Output 은 추론 포함 출력, Reasoning 은 Output 의 일부다.
type Usage struct {
	Input         int `json:"input"`
	Output        int `json:"output"`
	CacheRead     int `json:"cacheRead"`
	CacheWrite    int `json:"cacheWrite"`
	Reasoning     int `json:"reasoning,omitempty"`
	ContextTokens int `json:"contextTokens,omitempty"`
}

// StopReason 은 정규화된 종료 사유와 provider 원문 값이다 (CT-FR-20).
type StopReason struct {
	Kind StopKind `json:"kind"`
	Raw  string   `json:"raw,omitempty"`
}

// CallError 는 모델 호출 에러다 (CT-FR-21).
type CallError struct {
	Class      ErrorClass    `json:"class"`
	Message    string        `json:"message"`
	RetryAfter time.Duration `json:"retryAfter,omitempty"`
	Status     int           `json:"status,omitempty"`
	Raw        string        `json:"raw,omitempty"`
}

// Prompt = Input + CacheRead + CacheWrite (CT-FR-19).
func (u Usage) Prompt() int { return u.Input + u.CacheRead + u.CacheWrite }

// Total = Prompt + Output (CT-FR-19).
func (u Usage) Total() int { return u.Prompt() + u.Output }

func (u Usage) Validate() error {
	if u.Input < 0 || u.Output < 0 || u.CacheRead < 0 || u.CacheWrite < 0 || u.Reasoning < 0 || u.ContextTokens < 0 {
		return invalid("usage: negative value")
	}
	if u.Reasoning > u.Output {
		return invalid("usage: reasoning %d > output %d", u.Reasoning, u.Output)
	}
	return nil
}

func (s StopReason) Validate() error {
	if !s.Kind.Valid() {
		return invalid("stopReason: kind %q", s.Kind)
	}
	return nil
}

func (e *CallError) Error() string { return string(e.Class) + ": " + e.Message }

func (e CallError) Validate() error {
	if !e.Class.Valid() {
		return invalid("callError: class %q", e.Class)
	}
	if e.RetryAfter < 0 {
		return invalid("callError: retryAfter < 0")
	}
	return nil
}
