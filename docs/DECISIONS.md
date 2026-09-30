# 결정 기록 (Decision Log)

출처: `hermes_study/analysis/07-injectable-loop-design.md` 11장 (Hermes · pi · oh-my-pi 비교 분석 01~07 문서의 결론).
이 문서는 스펙(`*/SPEC.md`)의 상위 근거다. 스펙과 이 문서가 충돌하면 **작업을 멈추고 이 문서를 먼저 고친다.**

---

## 1. 구조 원칙

| id | 결정 |
|---|---|
| D1 | **루프는 루프로만 작동한다.** 루프 = 고정된 step 목록 + 고정된 전이 표(신호 → 다음 step). 정책은 하나도 없다 |
| D2 | **모든 step 은 주입된 함수가 수행한다.** `context.*`, `request.*`, `model.call` 포함. 주입 함수는 인터페이스로 정의하고, 이를 처리하는 dispatch layer(inject)를 둔다 |
| D3 | **흐름 제어는 주입 함수 안에서만, 약속어로.** 제어 약속어(신호)는 반환값으로만 낸다. 상태는 조회 약속어로 읽는다. 둘 다 닫힌 타입이고 버전으로만 추가된다 |
| D4 | **ctx 는 step 마다 만들어 주입한다.** step 별로 허용된 조회·신호만 들어 있고, 함수가 끝나면 폐기한다. 전역 API 는 없다 |
| D5 | **step 당 주입 함수는 하나.** 함수는 신호 배열을 반환할 수 있고 루프는 순서대로 실행한다. 여러 기능의 충돌 해결은 상위 레이어의 조합 함수가 맡는다. 루프에는 우선순위 표가 없다 |
| D6 | **전부 주입 가능, 기본값은 기본 함수 세트(sets)가 제공.** 기본값은 루프(core)와 주입 레이어(inject)에 두지 않는다 |

## 2. 세부 결정

| id | 항목 | 결정 |
|---|---|---|
| D7 | 언어 | Go |
| D8 | 스트림 중 개입 | 1차 범위. 루프는 창만 연다: 이벤트마다 `stream.event` dispatch, 스트림 도중 도구 실행이 시작될 수 있는 동시성 구조, 입력 주입 통로. 판단은 주입 함수 |
| D9 | 짝 없는 tool call | 복구 방식은 주입, 기본값은 aborted 결과 채우기. 요청 직전 짝 검증도 기본 함수(교체 가능). 검증을 빼면 provider 거부가 드러남 |
| D10 | 서브에이전트 실행기 | 주입, 기본값 같은 프로세스 goroutine, 별도 프로세스도 가능. → 루프는 전역 상태 없는 값 |
| D11 | 함수 세트의 상태 | 세션 저장소에 네임스페이스로, `ctx` 로만 접근, 재개·브랜치 시 함께 복원. 저장 방식은 주입 |
| D12 | 서드파티 | 1차부터 지원. 로더를 주입 가능하게 두고 둘 다 구현: 직접 만든 세트 = 같은 프로세스(Go 컴파일 시점 강제), 서드파티 = 별도 프로세스(gRPC). 조회·신호·ctx 는 직렬화 가능 |
| D13 | step 목록 | 고정하되 22개로 세분화 (L0_core/core/SPEC.md 3.1) |
| D14 | 레이어 묶음 | `L0_core/{contract, core}` · `L1_inject/{inject, sets, adapters}` · `L2_ui/{surfaces}`. 주입(프레임워크 포함)은 사용자가 수정하는 영역이라 L1 |
| D15 | 1차 구현 범위 | **L0 + L1 전체 = 실행 가능한 에이전트.** sets 는 `base` + `resilient`, adapters 는 Anthropic Messages + OpenAI Chat Completions |
| D16 | 실행 진입점 | L2 전이라 검증용 최소 print CLI 를 L1 예제로 둔다 (`L1_inject/cmd/dr-print`). 본격 UI 는 L2 |

## 3. 스펙 작성 중 확정한 세부 (R)

07 문서 이후, 스펙을 쓰며 정한 것. 모두 D1~D13 을 구체화한 것이며 새 방향 전환은 없다.

| id | 내용 | 근거 |
|---|---|---|
| R1 | 신호를 **전이 신호**(step 을 옮김)와 **효과 신호**(상태에 작용, step 은 안 옮김)로 나눈다. 신호 배열 = 효과 0개 이상 + 전이 최대 1개(마지막). 전이가 없으면 `next` | D5 의 "신호 배열 순차 실행"을 모호함 없이 정의 |
| R2 | `turn.finish` 와 `call.error` 에서는 `next` 를 허용하지 않는다. 다음 행동(`continue_turn` / `settle` 등)을 함수가 명시해야 한다 | "도구를 실행했으면 계속"이 루프 안의 정책이 되는 것을 막음 (D1) |
| R3 | overflow 복구 = `[rewrite(edit), rebuild]`. `recall` 은 "같은 요청 재전송"이라 압축이 반영되지 않는다 | 07 문서 초안의 예시 정정 |
| R4 | 모듈 경로 접두어 = `github.com/Biisairo/dynamic_react` (구현 시작 시 사용자 확정). 모듈 경로 = 접두어 + 폴더 경로 (예: `github.com/Biisairo/dynamic_react/L0_core/contract`) | 모듈별로 `go get` 가능해야 함 (R8) |
| R5 | Go 1.23 이상 (`iter.Seq2` 사용) | D8 스트림 표현 |
| R6 | core 가 루프 무한 반복을 막는 **전이 수 상한**을 생성 인자로 받는다(기본 10,000). 정책이 아니라 안전장치 | 함수 세트 버그로 인한 무한 루프 방지 |
| R7 | core 가 run 기록(Transcript)을 기계적으로 누적한다 (2·14·19 출력). 해석·편집 적용은 하지 않는다 | sets 스펙 작성 중 발견: 이전 턴 응답·결과를 View 로 볼 수 없어 투영을 만들 수 없었음. 누적은 판단이 아닌 기록이라 D1 과 충돌하지 않음 |
| R8 | **패키지 폴더마다 독립 Go 모듈**: `L0_core/contract`, `L0_core/core`, `L1_inject/inject`, `L1_inject/adapters`, `L1_inject/sets`, `L1_inject/cmd/dr-print` 가 각자 `go.mod` 를 가진다. 개발은 루트 `go.work` 로 묶는다. 버전 태그는 폴더 접두어(`L0_core/contract/v0.1.0`). 모듈 의존: contract ← core ← inject, contract ← adapters, sets → {contract, core, inject, adapters}, dr-print → sets 이하. 모듈 간 require 는 공개 전까지 `v0.0.0` + `replace => ../상대경로` (모듈 단독 `go mod tidy` 가능, replace 는 소비자에게 적용되지 않음). 공개 시 태그 버전으로 교체 | 사용자가 pi·omp 처럼 필요한 모듈만 골라 쓰고 교체할 수 있게. 의존 방향을 `go.mod` 가 강제 (CORE-NFR-01 이 구조로 보장) |
| R9 | **adapters 는 contract 에만 의존**하는 단독 LLM 클라이언트다: `Call(ctx, req) iter.Seq2[contract.StreamEvent, error]`. 이것을 `ModelCallFn` 으로 감싸 `core.CallStream` 을 만드는 것은 sets 몫이다 | R8: 루프 없이 모델 호출만 쓰려는 사용자가 core·inject 를 받지 않게. 이전: adapters 가 inject 의 `ModelCallFn` 을 직접 제공 |
