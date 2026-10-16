# 린트 보고 (SRS-PF PF-FR-22)

- 측정일: 2026-10-16
- 도구: golangci-lint v2.14.0, 설정 `/.golangci.yml`
- 결과: 처음 215건이 0건이 되었다. 아래 표는 처리 방법별로 정리한 것이다.

## 1. 처리 요약

| 처리 | 건수 | 내용 |
|---|---|---|
| 설정에서 제외 | 176 | 출력 함수(`fmt.Fprint*`, `io.WriteString`)와 정리 함수(`Close`, `os.Remove`)의 반환값, 테스트 파일과 `envtest/` 의 errcheck, QF1001, pi 문안을 쓰는 도구 에러 문구(ST1005 6건) |
| 무시를 명시 (`_ =`) | 30 | 실패하면 0 값이나 기본값으로 진행하는 기존 동작을 그대로 두고, 무시한다는 의도만 코드에 드러냈다 |
| 동작 보존 수정 | 4 | `switch` 로 바꿈(QF1003), 테스트 도우미 반환 순서 변경(ST1008, 호출 11곳 포함), 테스트의 `strings.Index` 를 `strings.Cut` 으로 바꿈(offBy1 2건) |
| 오탐으로 표시 (`nolint` + 이유) | 4 | `append` 결과를 다른 변수에 담는 의도된 코드 3건, WebP 시그니처의 공백 1건 |
| 사용자 결정 뒤 수정 | 1 | 아래 2장 |

## 2. 사용자 결정 항목

### #1 `L2_harness/agent/agent.go:218` — `limitTools` 의 nil 맵 대입

- 린터: errcheck (`json.Unmarshal` 반환값 무시)
- 현상: `raw` 가 JSON `null` 이면 앞의 구조체 디코딩은 성공한다. 그런데 `m` 이 nil 맵으로 남아서 다음 줄 `m["tools"] = ...` 에서 panic 이 난다.
- 재현: `limitTools([]byte("null"), []string{"bash"}, []string{"bash"})` 를 부르면 `assignment to entry in nil map` panic 이 난다 (함수 단위로 확인).
- 도달 경로: 사용자 설정이 `"agent": null` 이고 `agent.Options.Tools` 를 지정하면 `agent.New` 에서 panic 이 난다 (회귀 테스트로 확인). `agentConfig` 는 길이 0 만 `{}` 로 바꾸고 `null` 은 바꾸지 않았다.
- 결정: 권장안대로 `null` 을 `{}` 로 취급한다 (2026-10-16).
- 이전 동작: `agent.New` 가 panic 한다.
- 새 동작: `"agent": null` 을 `"agent": {}` 와 같게 처리하고, `New` 가 성공한다.
- 이유: 길이 0 인 값과 `null` 은 둘 다 "설정 없음"이다. 같은 규칙을 적용해야 한다.
- 수정: `agentConfig` 에서 `null` 을 `{}` 로 바꾸고, `limitTools` 의 `nolint` 를 `_ =` 로 바꿨다. 회귀 테스트는 `TestToolsLimitNullAgent` 다.
- 상태: 해소

## 3. 의도를 유지한 항목 (참고)

| 위치 | 린터 | 유지 이유 |
|---|---|---|
| `L2_harness/harness/tools/edit.go:146·162·166`, `read.go:69`, `walk.go:29·32` | ST1005 | 모델에 보여주는 도구 에러 문구다. pi 도구 문안과 맞추려고 대문자와 마침표를 그대로 둔다. 문구를 바꾸면 모델이 받는 입력이 바뀐다. |
| `L1_inject/inject/combine/combine.go:14`, `L3_ui/surfaces/terminal/modes.go:64` | QF1001 | `!(a && b)` 형태가 조건의 뜻을 더 잘 드러낸다. |
