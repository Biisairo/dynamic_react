# dynamic_react

루프는 루프로만 작동하고, 모든 동작(컨텍스트 구성, 요청 조립, 모델 호출, 도구 실행, 흐름 제어)을 밖에서 주입하는 에이전트 코어. Go.

배경: Hermes · pi · oh-my-pi 비교 분석 (`../hermes_study/analysis/00~07`). 결정 기록: [`docs/DECISIONS.md`](docs/DECISIONS.md).

## 레이어 (D14)

```text
 L2_ui       surfaces: TUI / rpc / web / messenger              (scope only, later)
 ─────────────────────────────────────────────────────────────────
 L1_inject   user-modifiable layer
             inject    function interfaces, per-step ctx,       (spec v0.1)
                       binding, combinators, remote loader
             sets      default function sets: base, resilient   (spec v0.1)
             adapters  model dialects: Anthropic, OpenAI Chat   (spec v0.1)
             cmd       dr-print: minimal CLI for verification    (in sets spec 3.8)
 ─────────────────────────────────────────────────────────────────
 L0_core     the core
             core      the loop: 22 fixed steps, transitions,   (spec v0.1)
                       signals, stream/tool concurrency
             contract  shared data types                        (spec v0.1)
```

의존 방향은 위 → 아래만. 폴더마다 독립 Go 모듈이다 (R8, 경로 `github.com/Biisairo/dynamic_react/<폴더>`). 개발은 루트 `go.work` 로 묶는다.

```text
 contract ◄── core ◄── inject
    ▲                     ▲
    └── adapters          │
           ▲              │
           └──── sets ────┘  ◄── cmd/dr-print
```

`L0_core/core` 는 contract 와 표준 라이브러리만, `L1_inject/adapters` 는 contract 와 표준 라이브러리만 본다 (R9).

## 폴더

| 폴더 | 내용 | 상태 |
|---|---|---|
| [`L0_core/contract/`](L0_core/contract/SPEC.md) | 공유 데이터 계약 | 스펙 v0.1 |
| [`L0_core/core/`](L0_core/core/SPEC.md) | 루프 본체 | 스펙 v0.1 |
| [`L1_inject/inject/`](L1_inject/inject/SPEC.md) | 함수 주입 프레임워크 | 스펙 v0.1 |
| [`L1_inject/sets/`](L1_inject/sets/SPEC.md) | 기본 함수 세트 + 도구 + 세션 + print CLI | 스펙 v0.1 |
| [`L1_inject/adapters/`](L1_inject/adapters/SPEC.md) | 모델 방언 어댑터 + 카탈로그 | 스펙 v0.1 |
| [`L2_ui/surfaces/`](L2_ui/surfaces/README.md) | 입출력 | 범위만 |
| [`docs/`](docs/DECISIONS.md) | 결정 기록, 인계 문서 | — |

## 핵심 개념 (한 장 요약)

- **step 22개 고정** (core 3.1). 루프는 step 을 순서대로 진행하고 각 step 을 주입 함수에 맡긴다.
- **신호** = 주입 함수가 반환하는 제어 약속어. 전이 10종 + 효과 3종 (core 3.2). step 별 허용 표 (core 3.4).
- **조회** = ctx 의 읽기 메서드. 그룹 8종, step 별 사용 가능 표 (inject 3.4).
- **ctx 는 dispatch 마다 새로 만들고 반환 후 폐기.** 허용되지 않은 신호·조회는 컴파일 에러(봉인 interface), 재사용은 런타임 에러, 서드파티는 별도 프로세스 (inject 2.2).
- **step 당 함수 하나.** 여러 기능은 조합 함수로 합친다 (inject 3.7). 기본 동작은 sets 가 제공한다.

## 구현 순서 (D15: L0 + L1 전체)

1. 모듈 6개의 `go.mod` + 루트 `go.work` (R4, R8), Go 1.23+.
2. `L0_core/contract` — 타입 + Validate + JSON 왕복. 테스트 먼저.
3. `L0_core/core` — 스크립트된 가짜 Dispatcher 로 테스트 먼저 → 구현. `-race`.
4. `L1_inject/inject` — StepSpec + 생성기 → 컴파일 실패 테스트 → Dispatcher → 조합 → 원격 로더.
5. `L1_inject/adapters` — httptest SSE 재생 + 골든 테스트 → Anthropic → OpenAI Chat → 카탈로그.
6. `L1_inject/sets` — 도구 → 세션 저장 → 추정·압축 → base → resilient → 종단 테스트.
7. `L1_inject/cmd/dr-print` — 실행 확인.

각 단계는 스펙 4장(검증)의 테스트를 먼저 쓰고 실패를 확인한 뒤 구현한다. 스펙과 코드가 충돌하면 멈추고 스펙·DECISIONS 를 먼저 고친다.
