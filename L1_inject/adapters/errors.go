package adapters

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// overflowPhrases 는 400 본문에서 컨텍스트 초과를 알아보는 문구다 (AD-FR-33).
func overflowPhrases() []string {
	return []string{"prompt is too long", "maximum context length", "context_length_exceeded", "context length exceeded",
		"prompt too long; exceeded", "input is too long", "too many tokens"}
}

func isOverflow(body string) bool {
	low := strings.ToLower(body)
	for _, p := range overflowPhrases() {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// mask 는 비밀 값을 지운다 (AD-FR-34).
func mask(s string, secrets []string) string {
	for _, k := range secrets {
		if k != "" {
			s = strings.ReplaceAll(s, k, "[redacted]")
		}
	}
	return s
}

const maxErrorBody = 8 * 1024

// classifyHTTP 는 HTTP 상태와 본문으로 CallError 를 만든다 (AD-FR-32).
func classifyHTTP(status int, h http.Header, body []byte, secrets []string) contract.CallError {
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody]
	}
	text := mask(string(body), secrets)
	ce := contract.CallError{Status: status, Raw: text, Message: http.StatusText(status)}
	if msg := errorMessage(body); msg != "" {
		ce.Message = mask(msg, secrets)
	}
	switch {
	case status == 429:
		ce.Class = contract.ClassRateLimit
		ce.RetryAfter = retryAfter(h)
	case status == 529 || status == 503:
		ce.Class = contract.ClassOverloaded
	case status == 401 || status == 403:
		ce.Class = contract.ClassAuth
	case status == 413:
		ce.Class = contract.ClassPayloadTooLarge
	case status == 400 && isOverflow(text):
		ce.Class = contract.ClassOverflow
	case status == 400 || status == 404 || status == 422:
		ce.Class = contract.ClassInvalidRequest
	case status >= 500:
		ce.Class = contract.ClassTransientNetwork
	default:
		ce.Class = contract.ClassUnknown
	}
	return ce
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// transportError 는 연결·타임아웃·스트림 중단이다.
func transportError(err error, secrets []string) contract.CallError {
	return contract.CallError{Class: contract.ClassTransientNetwork, Message: mask(err.Error(), secrets)}
}

// classifyStreamError 는 스트림 안의 에러 이벤트(종류 문자열 + 메시지)를 분류한다.
func classifyStreamError(kind, msg string, secrets []string) contract.CallError {
	msg = mask(msg, secrets)
	ce := contract.CallError{Class: contract.ClassUnknown, Message: msg}
	k := strings.ToLower(kind)
	switch {
	case isOverflow(k) || isOverflow(msg):
		ce.Class = contract.ClassOverflow
	case strings.Contains(k, "overloaded"):
		ce.Class = contract.ClassOverloaded
	case strings.Contains(k, "rate_limit"):
		ce.Class = contract.ClassRateLimit
	case strings.Contains(k, "authentication") || strings.Contains(k, "permission"):
		ce.Class = contract.ClassAuth
	case strings.Contains(k, "request_too_large"):
		ce.Class = contract.ClassPayloadTooLarge
	case strings.Contains(k, "invalid_request") || strings.Contains(k, "not_found"):
		ce.Class = contract.ClassInvalidRequest
	case strings.Contains(k, "api_error"):
		ce.Class = contract.ClassTransientNetwork
	}
	return ce
}

// errorMessage 는 흔한 에러 본문 모양에서 메시지를 꺼낸다: {"error":{"message"}}, {"error":"..."}, {"message"}.
func errorMessage(body []byte) string {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	var inner struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(v.Error, &inner) == nil && inner.Message != "" {
		return inner.Message
	}
	var s string
	if json.Unmarshal(v.Error, &s) == nil && s != "" {
		return s
	}
	return v.Message
}
