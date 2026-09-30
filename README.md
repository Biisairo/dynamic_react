# dynamic_react

루프는 루프로만 작동하고, 모든 동작(컨텍스트 구성, 요청 조립, 모델 호출, 도구 실행, 흐름 제어)을 밖에서 주입하는 에이전트 코어. Go.

배경: Hermes · pi · oh-my-pi 비교 분석 (`../hermes_study/analysis/00~07`). 결정 기록: [`docs/DECISIONS.md`](docs/DECISIONS.md).

## 레이어

```text
 surfaces   TUI / print / rpc / web / messenger          (spec: later)
 sets       default function sets: core, resilient, ...  (spec: later)
 adapters   model dialect adapters + catalog              (spec: later)
 ─────────────────────────────────────────────────────────
 inject     layer 1: function interfaces, per-step ctx,   (spec: v0.1)
            binding, combinators, remote loader (gRPC)
 core       the loop: 22 fixed steps, transition table,   (spec: v0.1)
            signals, stream/tool concurrency
 contract   shared data types (messages, events, usage)   (spec: v0.1)
```

의존 방향: 위 → 아래만. core 는 contract 와 표준 라이브러리만, inject 는 core·contract 만 본다.

## 폴더

| 폴더 | 내용 | 상태 |
|---|---|---|
| [`contract/`](contract/SPEC.md) | 공유 데이터 계약 (C1~C6) | 스펙 v0.1 |
| [`core/`](core/SPEC.md) | 루프 본체 | 스펙 v0.1 |
| [`inject/`](inject/SPEC.md) | 1차 레이어: 함수 주입 | 스펙 v0.1 |
| [`sets/`](sets/README.md) | 기본 함수 세트 | 범위만 |
| [`adapters/`](adapters/README.md) | 모델 방언 어댑터 | 범위만 |
| [`surfaces/`](surfaces/README.md) | 입출력 | 범위만 |
| [`docs/`](docs/DECISIONS.md) | 결정 기록 | — |

## 핵심 개념 (한 장 요약)

- **step 22개 고정** (core 3.1). 루프는 step 을 순서대로 진행하고 각 step 을 주입 함수에 맡긴다.
- **신호** = 주입 함수가 반환하는 제어 약속어. 전이 10종 + 효과 3종 (core 3.2). step 별 허용 표 (core 3.4).
- **조회** = ctx 의 읽기 메서드. 그룹 8종, step 별 사용 가능 표 (inject 3.4).
- **ctx 는 dispatch 마다 새로 만들고 반환 후 폐기.** 허용되지 않은 신호·조회는 컴파일 에러(봉인 interface), 재사용은 런타임 에러, 서드파티는 별도 프로세스 (inject 2.2).
- **step 당 함수 하나.** 여러 기능은 조합 함수로 합친다 (inject 3.7). 기본 동작은 sets 레이어가 제공한다.

## 구현 순서 (다음 세션)

1. `go.mod` (모듈 경로 확정, R4), Go 1.23+.
2. contract: 타입 + Validate + JSON 왕복 테스트 (contract 4장).
3. core: 테스트 먼저 — 스크립트된 가짜 Dispatcher (core 4장) → 구현.
4. inject: StepSpec + 생성기 → 컴파일 실패 테스트 → Dispatcher → 조합 → 원격 로더 (inject 4장).
5. 스펙과 코드가 충돌하면 멈추고 스펙·DECISIONS 를 먼저 고친다.
