# 종료 기록: impl-12 — 반복 감지 세트, 프로젝트 종료

이 문서는 dynamic_react 의 마지막 세션 기록이다. 이 세션에서 프로젝트를 닫았다 (R42). 다음 세션은 없다. 다시 열 때는 아래 "다시 열 때" 절부터 읽는다.

## 결론

- 목적은 "루프는 고정하고, 모든 동작은 세트로 주입한다"는 구조의 증명이었다. 이 목적은 달성했다 (`GOALS.md` 1장 G1~G6).
- 사용자 판단: 이후의 일(기본 구성 확장, 임계값·문구 다듬기, 제품 편의)은 완성형 에이전트 쪽 일이다. 그래서 여기서 멈춘다.
- 열림·결정 대기였던 GOALS 항목은 모두 `보류 (R42)` 다. 내용은 지우지 않았다.
- 14개 모듈은 마지막 검증을 모두 통과했다 (아래 "마지막 상태").

## 이 세션에서 한 일

| 커밋 | 내용 |
|---|---|
| `2efaa04` | R41 스펙: sets v0.12 `repeat` 세트(ST-FR-64~69), harness v1.10, GOALS DC-6 |
| `3474742` | 구현하면서 정한 세부를 스펙에 반영: 지문은 contract `ArgsDigest`, 지시는 아래 층 신호 앞, 리셋 등가 분기 정리. GOALS ST-2 해소 |
| `f664023` | `sets.Repeat` 구현과 테스트. harness `DefaultSets` 에 repeat 추가 |
| (종료 커밋) | R42: repeat 를 `DefaultSets` 에서 뺌(harness v1.11, sets v0.13). 남은 항목 보류. 문서 최신화(README·GOALS·DECISIONS·INJECT·VERIFY·architecture.html), 이 문서 |

### R41 반복 감지 세트 (`sets.Repeat`, 주입용)

- 단위는 턴이다. 한 턴의 호출마다 `이름 + "\x00" + ArgsDigest()` 를 만들고, 정렬한 목록을 지문으로 쓴다.
- 지문이 직전 턴과 같으면 연속 수를 1 늘린다. 다르면 1 로 둔다. 호출이 없는 턴과 면제 도구만 부른 턴은 0 으로 되돌린다.
- 연속 수가 Warn(기본 5)이면 지시 메시지를 한 번 넣는다. Stop(기본 8)이면 `ErrRepeat` 로 run 을 끝낸다.
- 값은 생성자 옵션 `RepeatOptions{Warn, Stop, Exempt}` 로 받는다. 이 세트만 쓰는 값이므로 공용 설정 스키마에 넣지 않았다. 이 판단은 사용자가 지적한 것이다.
- 상태는 run 단위다. 네임스페이스는 `repeat`, 키는 `progress` 다.
- 뮤테이션: 36개 중 KILLED 35, 등가 1. 등가는 결과 블록 판정(`Kind == Text` 를 `!= Image` 로 바꾼 것)이다. contract 상 결과 블록이 text·image 뿐이라서(CT-FR-10) 결과가 같다. 다른 모듈의 규칙에 기대므로 기록만 했다.
- 기본 구성에는 없다 (R42). 쓰려면 자기 main 에서 `Sets` 로 주입한다.

```go
o.Sets = func(d sets.Deps) []harness.SetEntry {
    r, _ := sets.Repeat(d, sets.RepeatOptions{}) // 기본 옵션은 에러가 없다
    return append(harness.DefaultSets(d), harness.SetEntry{Set: r, On: true})
}
```

### R42 프로젝트 종료

- R41 ⑤ 를 뒤집었다. 기본 구성은 다시 base·require·cache·guard·truncate·resilient 다. 기본 동작은 harness v1.9 와 같다.
- ST-1 예산 가드는 구현하지 않았다. 인터뷰로 확정한 설계는 `DECISIONS.md` R42 ② 에 있다: 턴 수 기준, Max 200, 다 쓰면 요약 지시와 함께 한 턴 더, 그 뒤 `ErrBudget`, 요약 턴의 도구 호출은 tool.before 에서 막음.

## 이 세션에서 배운 것

- **20 turn.finish 의 신호 목록은 전이 신호로 끝나야 한다.** 아래 층 신호 뒤에 `inject_input` 을 붙이면 `transition continue_turn must be last` 로 run 이 실패한다. 효과 신호는 앞에 둔다.
- **20 에서 이번 턴의 호출과 결과는 `ctx.Tools().Calls()`·`Results()` 로 바로 읽는다.** Transcript 를 훑을 필요가 없다.
- **인자 정규화는 contract `ToolCall.ArgsDigest()` 에 이미 있다** (키 정렬, 공백 제거, 숫자 원문 유지). 정규화한 텍스트를 주는 공개 함수는 없다.
- **기본 구성을 바꾸면 여러 모듈의 테스트가 깨진다.** 배치 문자열을 고정한 테스트가 harness(`inject_test`·`layout_test`·`prefix_test`), agent(`sets_test`), terminal(`sets_test`·`status_test`)에 있다.
- **사용자는 목적을 기준으로 범위를 다시 묻는다.** "sets 방식의 loop 동작 구현이 목적인데 너무 완벽한 에이전트를 만드려고 하는 느낌 아니야?" 새 기능을 제안하기 전에, 그것이 구조 증명에 무엇을 더하는지 먼저 따진다.

## 마지막 상태

- 스펙 버전: contract v0.5, core v0.2, inject v0.5, sets v0.13, adapters v0.8, harness v1.11, osenv v0.3, agent v0.11, terminal v0.10, dr v0.3, dr-print v0.2, examples/inject v0.2, examples/verify v0.3, examples/modes v0.4.
- 검증: 모듈 14개 전부 `gofmt -l`, `go vet ./...`, `go test -race -count=1 ./...`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test -count=1 ./...` 를 통과했다. 테스트 밖 코드에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 이 없다.
- 결정: D1~D16, R1~R42.
- `docs/architecture.html` 의 카드 그림은 R27 시점 그대로다. 그 뒤에 더한 것은 글로만 적었다.

## 다시 열 때

1. `git status` 로 작업 트리가 깨끗한지 확인한다.
2. 읽는 순서: `README.md` → `docs/GOALS.md`(보류 항목) → `docs/DECISIONS.md` R39·R41·R42 → 다룰 모듈의 `SPEC.md`.
3. 보류 항목 중 하나를 사용자가 고르게 한다. 고르지 않은 항목에는 손대지 않는다.
4. 작업 순서는 그대로다: 인터뷰(질문은 한 번에 하나, 권장안 포함) → DECISIONS 행 → 모듈 SPEC → 테스트(RED) → 구현(GREEN) → 검증 → 뮤테이션 → GOALS 갱신 → 커밋(사용자 확인 후, AI 서명 없음).
5. 규약은 `HANDOFF-impl-11.md` 의 "변하지 않는 규약" 절과 같다. 특히 R39(세트는 자기 루프의 제어만 갖는다)를 모든 설계에 적용한다.
6. 실제 모델이 필요한 검증(VF-1~3, EX-2)은 외부 호출이다. 사용자 확인 뒤에만 한다.
