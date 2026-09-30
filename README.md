# dynamic_react

루프는 루프로만 작동하고, 모든 동작(컨텍스트 구성, 요청 조립, 모델 호출, 도구 실행, 흐름 제어)을 밖에서 주입하는 에이전트 코어. Go.

배경: Hermes · pi · oh-my-pi 비교 분석 (`../hermes_study/analysis/00~07`). 결정 기록: [`docs/DECISIONS.md`](docs/DECISIONS.md).

## 레이어 (D14)

```text
 L3_ui       all real I/O: osenv (files, processes),            (scope only, later)
             ModelCaller setup, terminal, stream display,
             config/env, dr-print test CLI; surfaces
 ─────────────────────────────────────────────────────────────────
 L2_harness  harness: the agent as one function — tool logic,   (spec v0.2)
             JSONL session format, prompt assembly, summarizer
             over Env + ModelCaller interfaces (no real I/O)
 ─────────────────────────────────────────────────────────────────
 L1_inject   user-modifiable layer
             inject    function interfaces, per-step ctx,       (spec v0.3)
                       binding, combinators
             sets      default function sets: base, resilient   (spec v0.3)
             adapters  dialects: Anthropic, Chat, Responses;    (spec v0.3)
                       provider profiles, model catalog
 ─────────────────────────────────────────────────────────────────
 L0_core     the core
             core      the loop: 22 fixed steps, transitions,   (spec v0.2)
                       signals, stream/tool concurrency
             contract  shared data types                        (spec v0.4)
```

의존 방향은 위 → 아래만. 폴더마다 독립 Go 모듈이다 (R8, 경로 `github.com/Biisairo/dynamic_react/<폴더>`). 개발은 루트 `go.work` 로 묶는다.

```text
 contract ◄── core ◄── inject
    ▲                     ▲
    └── adapters          │
           ▲              │
           └── (L3) ──► harness ──► sets ──► inject, core, contract
```

`L0_core/core` 는 contract 와 표준 라이브러리만, `L1_inject/adapters` 는 contract 와 표준 라이브러리만 본다 (R9).

## 폴더

| 폴더 | 내용 | 상태 |
|---|---|---|
| [`L0_core/contract/`](L0_core/contract/SPEC.md) | 공유 데이터 계약 | 스펙 v0.4, 구현 |
| [`L0_core/core/`](L0_core/core/SPEC.md) | 루프 본체 | 스펙 v0.2, 구현 |
| [`L1_inject/inject/`](L1_inject/inject/SPEC.md) | 함수 주입 프레임워크 + 조합 함수 | 스펙 v0.3, 구현 |
| [`L1_inject/sets/`](L1_inject/sets/SPEC.md) | 기본 함수 세트 (base, resilient) | 스펙 v0.4, 구현 |
| [`L1_inject/adapters/`](L1_inject/adapters/SPEC.md) | 모델 방언 어댑터 + provider 프로필 + 카탈로그 | 스펙 v0.3, 구현 |
| [`L2_harness/`](L2_harness/SPEC.md) | 하네스: 에이전트 함수 (도구 로직, 세션 형식, 프롬프트, 요약기), 실제 I/O 는 Env interface | 스펙 v0.2 |
| [`L3_ui/`](L3_ui/surfaces/README.md) | 실제 입출력: osenv, dr-print(검증용), surfaces | 범위만 |
| [`docs/`](docs/DECISIONS.md) | 결정 기록, 인계 문서 | — |

## 핵심 개념 (한 장 요약)

- **step 22개 고정** (core 3.1). 루프는 step 을 순서대로 진행하고 각 step 을 주입 함수에 맡긴다.
- **신호** = 주입 함수가 반환하는 제어 약속어. 전이 10종 + 효과 3종 (core 3.2). step 별 허용 표 (core 3.4).
- **조회** = ctx 의 읽기 메서드. 그룹 8종, step 별 사용 가능 표 (inject 3.4).
- **ctx 는 dispatch 마다 새로 만들고 반환 후 폐기.** 허용되지 않은 신호·조회는 컴파일 에러(봉인 interface), 재사용은 런타임 에러 (inject 2.2). 서드파티도 Go FunctionSet 으로 `Bind` 에 겹친다 (D12).
- **step 당 함수 하나.** 여러 기능은 조합 함수로 합친다 (inject 3.7). 기본 동작은 sets 가 제공한다.

## 구현 순서 (D15: L0 + L1 전체)

1. 모듈 6개의 `go.mod` + 루트 `go.work` (R4, R8), Go 1.23+.
2. `L0_core/contract` — 타입 + Validate + JSON 왕복. 테스트 먼저.
3. `L0_core/core` — 스크립트된 가짜 Dispatcher 로 테스트 먼저 → 구현. `-race`.
4. `L1_inject/inject` — StepSpec + 생성기 → 컴파일 실패 테스트 → Dispatcher → 조합.
5. `L1_inject/adapters` — httptest SSE 재생 + 골든 테스트 → Anthropic → OpenAI Chat → 카탈로그.
6. `L1_inject/sets` — 의존 interface·메모리 Store → 추정·압축 절단 → base → resilient → 종단 테스트 (가짜 ModelCaller·도구).
7. (다음 단계) `L2_harness/harness` — Env·ModelCaller 위의 에이전트 함수 (R13, R14). 이어서 `L3_ui/osenv` + `L3_ui/cmd/dr-print` 로 실행 가능한 에이전트.

각 단계는 스펙 4장(검증)의 테스트를 먼저 쓰고 실패를 확인한 뒤 구현한다. 스펙과 코드가 충돌하면 멈추고 스펙·DECISIONS 를 먼저 고친다.
