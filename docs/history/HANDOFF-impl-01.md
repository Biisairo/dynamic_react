# 인계: impl-01 — L0 + L1 구현 (실행 가능한 에이전트까지)

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 `README.md` 의 **"구현 순서" 1~7** 을 순서대로 진행한다. 기준 문서는 `docs/DECISIONS.md` 와 다섯 스펙(모두 v0.1)이다:
`L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`, `L1_inject/adapters/SPEC.md`, `L1_inject/sets/SPEC.md`.

1. `go.mod` 생성. **모듈 경로는 사용자에게 먼저 묻는다** (R4). Go 1.23+ (R5). ~~단일 모듈~~ → 폴더마다 독립 모듈로 변경됨 (R8, R9 — impl-01 세션에서 사용자 확정).
2. contract → 3. core → 4. inject → 5. adapters → 6. sets → 7. `L1_inject/cmd/dr-print`.

단계마다: 스펙 4장의 테스트를 먼저 쓰고 **실패를 확인**한 뒤 구현, 통과 확인 후 사용자에게 보고하고 다음 단계. 커밋은 사용자 확인 후에만.
스펙과 코드가 충돌하거나 스펙으로 결정할 수 없는 것이 나오면 **멈추고** 사용자에게 묻는다. 확정되면 스펙·DECISIONS 를 먼저 고친다.

## 어디까지 왔나

- git: `6f78c75 docs init`(사용자, 옛 평면 구조) → `e0dc207`(레이어 구조 이동, adapters·sets 스펙, R7) → 이 문서 수정 커밋. 인계 시점 작업 트리는 깨끗하다 — `git status` 로 확인하라.
- 코드는 한 줄도 없다. 문서만 있다.
- 설계 출처: `../hermes_study/analysis/00~07` (Hermes · pi · oh-my-pi 코드 분석). 07 문서 11장이 결정 원본이었고, 이제 기준은 이 폴더의 `docs/DECISIONS.md` 다.

## 먼저 읽을 것

1. `docs/DECISIONS.md` 전체 — D1~D16, R1~R7
2. `L0_core/core/SPEC.md` 3.1 step 표, 3.2 신호, 3.3 "다음 step" 표, 3.4 허용 표 — **이 네 표가 루프의 전부**. 3.6~3.9 (스트림, 도구, 선행 실행, View·Transcript)
3. `L1_inject/inject/SPEC.md` 2.2 강제의 세 층, 3.2~3.4 (시그니처, ctx, 조회 그룹 표), 3.5 바인딩, 3.7 조합, 3.8 원격
4. `L0_core/contract/SPEC.md` 3장
5. `L1_inject/adapters/SPEC.md` 2.2 제약(API 키만, 구독 OAuth 금지), 3.2~3.6
6. `L1_inject/sets/SPEC.md` 1.3 수치 출처, 3.1 base 표, 3.2 resilient, 3.7 압축
7. 필요할 때만: `../hermes_study/analysis/05-provider-wire-formats.md`(방언 JSON 예시 — adapters 골든 테스트 원본), `02-pi-coding-agent.md` 8장(압축)·10장(도구), `04-context-window-and-usage.md`

## 이 세션이 값을 치르고 배운 것

- **사용자의 설계 원칙: "루프는 루프로만, 전부 밖에서 주입, 대신 기본값 제공."** 선택지를 물으면 거의 항상 "주입 가능 + 기본값" 을 고른다. core·inject 안에 판단(우선순위 표, 기본 동작, "도구를 돌렸으면 계속")을 넣고 싶어지면 정책인지 의심하라. 이 세션에서 "dispatch 의 고정 우선순위" 를 권장했다가 원칙과 어긋나 "step 당 함수 하나 + 상위 조합 함수" 로 바꿨다 (D5).
- 사용자는 **실질적 강제**(C 의 define 처럼)를 중시한다. Go 를 고른 핵심 이유가 **봉인 interface(비공개 메서드)로 step 별 허용 신호를 컴파일 시점에 강제**하는 것이다. 런타임 검사로 대체하지 마라. 원격(gRPC) 경로만 런타임 검사가 1차 방어선 (IN-FR-35).
- 레이어는 **묶음 단위**로 본다 (D14): L0_core = 루프 + 계약, L1_inject = 사용자가 고치는 영역(주입 프레임워크 + 기본 세트 + 어댑터), L2_ui = 나중. 폴더를 레이어 경계를 넘어 섞지 마라.
- 신호: 전이(최대 1개, 마지막) + 효과(0개 이상) (R1). overflow 복구는 `[rewrite, rebuild]` (R3, `recall` 은 같은 요청 재전송이라 압축 미반영). `turn.finish`·`call.error` 는 `next` 금지 (R2).
- sets 스펙을 쓰다가 core 에 빈틈이 있었다: 이전 턴 기록을 볼 수 없었음 → **run 기록(Transcript)** 을 core 가 기계적으로 누적하도록 추가 (R7, CORE-FR-31a). 이런 빈틈이 구현 중 또 나오면 같은 방식으로: 멈추고, 판단이 아닌 기록인지 확인하고, 사용자에게 보고 후 스펙 수정.
- API 키는 환경변수로만, 코드·로그·세션 파일·트레이스에 노출 금지. 구독 계정 OAuth 경로 구현 금지 (adapters AD-C1, C2). 실제 provider 호출 테스트는 키가 있을 때만.

## 남은 미결 (구현 중 해당 단계에서 사용자에게 확인)

| 위치 | 내용 |
|---|---|
| R4 | 모듈 경로 |
| inject O1 | StepSpec 소스 형식 (Go / YAML) |
| inject O2 | 원격 plugin 관리 라이브러리 사용 여부 (도입 전 라이선스 확인) |
| inject O3 | dispatch 밖 panic 정책 — L2 에서 재검토 |
| adapters O1 | 공식 Go SDK 사용 여부 (1차는 net/http) |
| adapters O2 | 내장 카탈로그 초기 모델 목록 |
| sets O1 | 기본 시스템 프롬프트 문안 |
| sets O2 | 재시도 기본값(3회, 2s, 60s) 확정 |
| sets O3 | 기본 sessionDir |
| 07 문서 8장 | 기능 재현 검증을 22 step 기준으로 재수행 (inject 단계 시작 전 권장) |

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148 구조 유지. 동작 변경 시 이전 / 새 동작 / 이유 기록.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI (사용자 CLAUDE.md). Go 는 gopls.
- 미사여구 없이 본론만. 불명확하면 멈추고 한 번에 하나씩, 권장안과 함께 묻는다.
- 의존성 최소: core 는 표준 라이브러리 + contract 만 (CORE-NFR-01), adapters 는 net/http (AD-C3).

## 사용자 결정 중 유효한 것

- D1~D16, R1~R7 전부.
- 이번 세션(스펙 작성)은 여기서 끝나고, 구현은 이 인계를 받은 세션에서 시작한다.
