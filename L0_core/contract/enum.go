package contract

import (
	"fmt"
	"slices"
)

// Role 은 메시지 역할이다 (CT-FR-01).
type Role string

const (
	RoleSystem     Role = "system"
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "tool_result"
)

// BlockKind 는 콘텐츠 블록 종류다 (CT-FR-02).
type BlockKind string

const (
	BlockText     BlockKind = "text"
	BlockImage    BlockKind = "image"
	BlockThinking BlockKind = "thinking"
	BlockToolCall BlockKind = "tool_call"
	BlockOpaque   BlockKind = "opaque"
)

// ToolStatus 는 도구 결과 상태다 (CT-FR-10).
type ToolStatus string

const (
	StatusOK      ToolStatus = "ok"
	StatusError   ToolStatus = "error"
	StatusAborted ToolStatus = "aborted"
	StatusSkipped ToolStatus = "skipped"
)

// StreamEventKind 는 스트림 이벤트 종류다 (CT-FR-14).
type StreamEventKind string

const (
	EventStart         StreamEventKind = "start"
	EventTextDelta     StreamEventKind = "text_delta"
	EventThinkingDelta StreamEventKind = "thinking_delta"
	EventToolCallStart StreamEventKind = "toolcall_start"
	EventToolCallDelta StreamEventKind = "toolcall_delta"
	EventToolCallEnd   StreamEventKind = "toolcall_end"
	EventUsage         StreamEventKind = "usage"
	EventDone          StreamEventKind = "done"
	EventError         StreamEventKind = "error"
)

// StopKind 는 정규화된 stop reason 이다 (CT-FR-20).
type StopKind string

const (
	StopStop    StopKind = "stop"
	StopLength  StopKind = "length"
	StopToolUse StopKind = "tool_use"
	StopError   StopKind = "error"
	StopAborted StopKind = "aborted"
)

// ErrorClass 는 모델 호출 에러 분류다 (CT-FR-21).
type ErrorClass string

const (
	ClassOverflow         ErrorClass = "overflow"
	ClassRateLimit        ErrorClass = "rate_limit"
	ClassOverloaded       ErrorClass = "overloaded"
	ClassAuth             ErrorClass = "auth"
	ClassPayloadTooLarge  ErrorClass = "payload_too_large"
	ClassTransientNetwork ErrorClass = "transient_network"
	ClassInvalidRequest   ErrorClass = "invalid_request"
	ClassPermanent        ErrorClass = "permanent"
	ClassUnknown          ErrorClass = "unknown"
)

// EditKind 는 컨텍스트 편집 종류다 (CT-FR-24).
type EditKind string

const (
	EditReplaceRange  EditKind = "replace_range"
	EditHide          EditKind = "hide"
	EditTruncateAfter EditKind = "truncate_after"
)

// ToolChoiceMode 는 도구 선택 방식이다 (CT-FR-33).
type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceTool     ToolChoiceMode = "tool"
)

// CacheTarget 은 캐시 힌트가 가리키는 영역이다 (CT-FR-12).
type CacheTarget string

const (
	CacheSystem  CacheTarget = "system"
	CacheMessage CacheTarget = "message"
)

func isOneOf[T ~string](v T, set ...T) bool { return slices.Contains(set, v) }

func marshalEnum[T ~string](v T, ok bool) ([]byte, error) {
	if !ok {
		return nil, fmt.Errorf("contract: unknown %T value %q", v, string(v))
	}
	return []byte(v), nil
}

func unmarshalEnum[T ~string](text []byte, dst *T, valid func(T) bool) error {
	v := T(text)
	if !valid(v) {
		return fmt.Errorf("contract: unknown %T value %q", v, string(text))
	}
	*dst = v
	return nil
}

func (v Role) Valid() bool {
	return isOneOf(v, RoleSystem, RoleUser, RoleAssistant, RoleToolResult)
}
func (v Role) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *Role) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, Role.Valid) }

func (v BlockKind) Valid() bool {
	return isOneOf(v, BlockText, BlockImage, BlockThinking, BlockToolCall, BlockOpaque)
}
func (v BlockKind) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *BlockKind) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, BlockKind.Valid) }

func (v ToolStatus) Valid() bool {
	return isOneOf(v, StatusOK, StatusError, StatusAborted, StatusSkipped)
}
func (v ToolStatus) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *ToolStatus) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, ToolStatus.Valid) }

func (v StreamEventKind) Valid() bool {
	return isOneOf(v, EventStart, EventTextDelta, EventThinkingDelta, EventToolCallStart,
		EventToolCallDelta, EventToolCallEnd, EventUsage, EventDone, EventError)
}
func (v StreamEventKind) MarshalText() ([]byte, error) { return marshalEnum(v, v.Valid()) }
func (v *StreamEventKind) UnmarshalText(b []byte) error {
	return unmarshalEnum(b, v, StreamEventKind.Valid)
}

func (v StopKind) Valid() bool {
	return isOneOf(v, StopStop, StopLength, StopToolUse, StopError, StopAborted)
}
func (v StopKind) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *StopKind) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, StopKind.Valid) }

func (v ErrorClass) Valid() bool {
	return isOneOf(v, ClassOverflow, ClassRateLimit, ClassOverloaded, ClassAuth, ClassPayloadTooLarge,
		ClassTransientNetwork, ClassInvalidRequest, ClassPermanent, ClassUnknown)
}
func (v ErrorClass) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *ErrorClass) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, ErrorClass.Valid) }

func (v EditKind) Valid() bool {
	return isOneOf(v, EditReplaceRange, EditHide, EditTruncateAfter)
}
func (v EditKind) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *EditKind) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, EditKind.Valid) }

func (v ToolChoiceMode) Valid() bool {
	return isOneOf(v, ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired, ToolChoiceTool)
}
func (v ToolChoiceMode) MarshalText() ([]byte, error) { return marshalEnum(v, v.Valid()) }
func (v *ToolChoiceMode) UnmarshalText(b []byte) error {
	return unmarshalEnum(b, v, ToolChoiceMode.Valid)
}

func (v CacheTarget) Valid() bool {
	return isOneOf(v, CacheSystem, CacheMessage)
}
func (v CacheTarget) MarshalText() ([]byte, error)  { return marshalEnum(v, v.Valid()) }
func (v *CacheTarget) UnmarshalText(b []byte) error { return unmarshalEnum(b, v, CacheTarget.Valid) }
