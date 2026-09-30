// Package contract 는 모든 레이어가 공유하는 데이터 타입과 그 불변 규칙을 정의한다.
// 동작(I/O)은 없고, 표준 라이브러리만 import 한다.
package contract

import (
	"errors"
	"fmt"
)

// ContractVersion 은 계약 버전이다. 증가 시 추가만 허용한다 (CT-FR-27).
const ContractVersion = 1

// ErrInvalid 는 모든 Validate 에러가 감싸는 값이다.
var ErrInvalid = errors.New("contract: invalid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// invalidAt 은 하위 값의 Validate 에러에 위치를 붙인다. ErrInvalid 는 그대로 감싸진다.
func invalidAt(err error, format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, err)...)
}
