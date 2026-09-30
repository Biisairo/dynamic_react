package sets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

type objectSchema struct {
	Type       json.RawMessage `json:"type"`
	Required   []string        `json:"required"`
	Properties map[string]struct {
		Type json.RawMessage `json:"type"`
	} `json:"properties"`
}

// CheckArgs 는 인자를 ST-FR-38 범위에서 검사한다: 객체 여부, required, 선언된 최상위 키의 JSON 타입.
// schema 의 type 이 "object" 가 아니면 검사하지 않는다.
func CheckArgs(schema, args json.RawMessage) error {
	var s objectSchema
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}
	if types := typeNames(s.Type); len(types) != 1 || types[0] != "object" {
		return nil
	}
	var obj map[string]json.RawMessage
	if !bytes.HasPrefix(bytes.TrimSpace(args), []byte("{")) || json.Unmarshal(args, &obj) != nil {
		return fmt.Errorf("arguments must be a JSON object")
	}
	for _, k := range s.Required {
		if _, ok := obj[k]; !ok {
			return fmt.Errorf("missing required argument %q", k)
		}
	}
	for k, p := range s.Properties {
		v, ok := obj[k]
		types := typeNames(p.Type)
		if !ok || len(types) == 0 {
			continue
		}
		if !matchesAny(v, types) {
			return fmt.Errorf("argument %q must be %v", k, types)
		}
	}
	return nil
}

// typeNames 는 "type" 값(문자열 또는 문자열 배열)을 목록으로 돌려준다.
func typeNames(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	json.Unmarshal(raw, &many)
	return many
}

func matchesAny(v json.RawMessage, types []string) bool {
	for _, t := range types {
		if matches(v, t) {
			return true
		}
	}
	return false
}

func matches(v json.RawMessage, t string) bool {
	v = bytes.TrimSpace(v)
	if len(v) == 0 {
		return false
	}
	switch t {
	case "string":
		return v[0] == '"'
	case "number":
		return isNumber(v)
	case "integer":
		if !isNumber(v) {
			return false
		}
		f, err := strconv.ParseFloat(string(v), 64)
		return err == nil && f == math.Trunc(f)
	case "boolean":
		return string(v) == "true" || string(v) == "false"
	case "array":
		return v[0] == '['
	case "object":
		return v[0] == '{'
	case "null":
		return string(v) == "null"
	}
	return true
}

func isNumber(v json.RawMessage) bool {
	return v[0] == '-' || (v[0] >= '0' && v[0] <= '9')
}
