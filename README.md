# dynamic_react

루프는 루프로만 작동하고, 모든 동작(컨텍스트 구성, 요청 조립, 모델 호출, 도구 실행, 흐름 제어)을 밖에서 주입하는 에이전트 코어. Go.

배경: Hermes · pi · oh-my-pi 비교 분석 (`../hermes_study/analysis/00~07`). 결정 기록: [`docs/DECISIONS.md`](docs/DECISIONS.md).

## 레이어 (D14)

```text
 L3_ui       input/output over the finished agent: terminal,
             stream display, signals, exit codes
             surfaces/terminal  interactive terminal library    (spec v0.5)
             cmd/dr        interactive entry point              (spec v0.3)
             cmd/dr-print  non-interactive test CLI             (spec v0.2)
 ─────────────────────────────────────────────────────────────────
 L2_harness  the real agent on the user's environment (R17)
             harness   agent assembly over Env + ModelCaller:   (spec v1.3)
                       tool logic, JSONL sessions, prompt,
                       summarizer (no real I/O itself)
             osenv     Env on the OS: files, shell, groups      (spec v0.3)
             agent     config + auth.json + osenv + adapters    (spec v0.8)
                       -> harness.New (the finished agent),
                       surface layer: approval, steer, models
 ─────────────────────────────────────────────────────────────────
 L1_inject   user-modifiable layer
             inject    function interfaces, per-step ctx,       (spec v0.4)
                       layered binding (ctx.Next), combinators
             sets      function sets: base, require, cache,     (spec v0.7)
                       guard, truncate, resilient, verify
             adapters  dialects: Anthropic, Chat, Responses;    (spec v0.6)
                       provider profiles, model catalog
 ─────────────────────────────────────────────────────────────────
 L0_core     the core
             core      the loop: 22 fixed steps, transitions,   (spec v0.2)
                       signals, stream/tool concurrency
             contract  shared data types                        (spec v0.4)
```

L0·L1 은 사용자 환경(파일)과 무관한 단독 라이브러리이고, L2 부터가 사용자 환경 위의 실제 에이전트다 (R17). 환경변수는 어느 레이어도 읽지 않는다 — 모든 값은 설정 파일에서 온다 (R21). 의존 방향은 위 → 아래만. 폴더마다 독립 Go 모듈이다 (R8, 경로 `github.com/Biisairo/dynamic_react/<폴더>`). 개발은 루트 `go.work` 로 묶는다.

```text
 contract ◄── core ◄── inject
    ▲                     ▲
    └── adapters          │
           ▲              │
           └── agent ──► harness ──► sets ──► inject, core, contract
                 ▲  └──► osenv ──► harness/env
                 ├── dr-print (L3)
                 ├── examples/inject, examples/verify
                 └── terminal (L3) ◄── dr (L3)
```

`L0_core/core` 는 contract 와 표준 라이브러리만, `L1_inject/adapters` 는 contract 와 표준 라이브러리만 본다 (R9).

## 폴더

| 폴더 | 내용 | 상태 |
|---|---|---|
| [`L0_core/contract/`](L0_core/contract/SPEC.md) | 공유 데이터 계약 | 스펙 v0.4, 구현 |
| [`L0_core/core/`](L0_core/core/SPEC.md) | 루프 본체 | 스펙 v0.2, 구현 |
| [`L1_inject/inject/`](L1_inject/inject/SPEC.md) | 함수 주입 프레임워크 + 층 바인딩(`ctx.Next`) + 조합 함수 | 스펙 v0.4, 구현 |
| [`L1_inject/sets/`](L1_inject/sets/SPEC.md) | 함수 세트 부품 (base·require·cache·guard·truncate·resilient, 주입용 verify) | 스펙 v0.7, 구현 |
| [`L1_inject/adapters/`](L1_inject/adapters/SPEC.md) | 모델 방언 어댑터 + provider 프로필 + 카탈로그 | 스펙 v0.6, 구현 |
| [`L2_harness/harness/`](L2_harness/SPEC.md) | 에이전트 조립 (도구 7종 로직, 세션 형식, 프롬프트, 요약기), 실제 I/O 는 Env interface, 세트·부품 주입, 세트 배치(켜기·끄기·순서) | 스펙 v1.3, 구현 |
| [`L2_harness/osenv/`](L2_harness/osenv/SPEC.md) | Env 의 OS 구현 | 스펙 v0.3, 구현 |
| [`L2_harness/agent/`](L2_harness/agent/SPEC.md) | 완성된 에이전트: 설정·키 파일 + osenv + adapters → harness, surface 공통 계층(승인·Steer·FollowUp·세션·모델 선택·키·세트 배치 저장), 주입 통로 | 스펙 v0.8, 구현 |
| [`L3_ui/surfaces/terminal/`](L3_ui/surfaces/terminal/SPEC.md) | 대화형 터미널 surface 라이브러리 | 스펙 v0.5, 구현 |
| [`L3_ui/cmd/dr/`](L3_ui/cmd/dr/SPEC.md) | 대화형 진입점 | 스펙 v0.3, 구현 |
| [`L3_ui/cmd/dr-print/`](L3_ui/cmd/dr-print/SPEC.md) | 비대화형 검증용 CLI (입출력만) | 스펙 v0.2, 구현 |
| [`examples/inject/`](examples/inject/SPEC.md) | 주입 예시: 매 턴 규칙 덧붙이기 + `rm -rf` 차단 | 스펙 v0.2, 구현 |
| [`examples/verify/`](examples/verify/SPEC.md) | 자기검증 세트 예시: 변경 뒤 끝낼 때 셸 명령으로 검증, 실패하면 출력을 모델에 돌려줌, `-i` 대화형 | 스펙 v0.3, 구현 |
| [`docs/`](docs/DECISIONS.md) | 결정 기록, 주입 가이드([`INJECT.md`](docs/INJECT.md)), 22 step 표현 검증, 인계 문서 | — |

## 실행

```sh
cd L3_ui/cmd/dr && go run .                    # 대화형 (TTY 필요, 아니면 종료 코드 2)
cd L3_ui/cmd/dr-print && go run . "질문"        # 비대화형 (stdin 도 받음, -json 은 이벤트 JSON 줄)
```

- 처음 실행하면 모델이 없으므로 모델 선택 화면이 뜬다. 키가 필요한 provider 는 `/login <provider>` 로 키를 넣어야 목록에 나온다. 고른 모델은 설정 파일에 저장된다.
- dr 플래그: `-model provider/id`(이번 실행만, 저장 안 함) · `-cwd` · `-config` · `-session <id>` / `-c`(최근 세션) / `-r`(세션 목록) · `-no-color`.
- 명령: `/model [provider/id]` · `/login [provider]` · `/logout [provider]` · `/sets` · `/set <name> on|off|to <n>` · `/sessions` · `/resume [번호|id]` · `/new` · `/compact` · `/help` · `/quit`. 키: Enter 전송(실행 중 steer), Alt+Enter follow-up, Shift+Enter·Ctrl+J 줄바꿈, Esc·Ctrl+C 실행 중 중단.

| 파일 | 내용 |
|---|---|
| `~/.dynamic_react/config.json` | 사용자 설정: `sessionDir`·`shellPath`(기본 `/bin/bash`)·`agent`(모델·thinking·tools·compaction·retry)·`providers`·`models`·`permissions`·`sets`(세트 배치, `/set` 이 쓴다). 키 필드는 없다 |
| `~/.dynamic_react/auth.json` | API 키 (`{"<provider>": {"type": "api_key", "key": "…"}}`, 0600). `/login` 이 쓴다. 저장소에 넣지 않는다 |
| `~/.dynamic_react/sessions/` | 세션 JSONL (기본 위치) |
| `<cwd>/.dynamic_react/config.json` | 프로젝트 설정: `permissions` 만 |

```json
{
  "agent": {"model": {"provider": "deepseek", "id": "deepseek-flash"}, "thinking": "medium"},
  "providers": {"mine": {"api": "openai-chat", "baseUrl": "http://localhost:8000/v1"}},
  "models": {"mine/qwen": {"contextWindow": 32768}},
  "permissions": {"allow": ["read", "bash(go test *)"], "ask": ["bash(git push *)"]}
}
```

인증은 API 키만 지원한다. 구독 계정(Claude Pro/Max, ChatGPT 플랜)의 로그인·OAuth 경로는 넣지 않는다 (AD-C1).

## 주입 (R22)

core(22 step 루프) 외의 모든 것은 `agent.New` 를 부를 때 Go 함수로 바꾼다. 각 함수는 기본값을 받아 바꾼 값을 돌려준다. nil 이면 기본값 그대로다.

| 필드 | 바꾸는 것 |
|---|---|
| `Sets func(sets.Deps) []harness.SetEntry` | 등록할 함수 세트 목록과 기본 켜짐. 기본 = `harness.DefaultSets(d)` (base → require → cache → guard → truncate → resilient, 전부 켬). 뒤의 세트가 위 층이고 `ctx.Next` 로 아래 층을 부른다 |
| `ToolSet func([]sets.Tool) []sets.Tool` | 도구 목록 (추가·제거·같은 이름 교체) |
| `System func([]contract.Block) []contract.Block` | 시스템 프롬프트 |
| `Summarizer func(sets.Summarizer) sets.Summarizer` | 요약기 (nil 을 돌려주면 압축 끔) |
| `Store func(harness.Store) harness.Store` | 세션 저장소 (`sets.Store` + `List`). 세션 목록·이력도 이것을 쓴다 |
| `ModelCaller func(sets.ModelCaller) sets.ModelCaller` | 모델 호출 (목록·키 확인은 adapters 그대로) |

```go
a, err := agent.New(ctx, agent.Options{
    Sets: func(d sets.Deps) []harness.SetEntry {
        mine := inject.FunctionSet{Name: "mine",
            ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
                return []contract.Message{rule()}, nil, nil // 이번 턴의 요청에만 들어가고 저장되지 않는다
            },
            ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
                if blocked(c) {
                    return core.BeforeOut{Call: c, ShortCircuit: &contract.ToolResult{CallID: c.ID, Status: contract.StatusError}}, nil, nil
                }
                return ctx.Next(c) // 아래 층(guard → base)을 부르면 기본 검사(없는 도구·스키마)가 남는다
            }}
        return append(harness.DefaultSets(d), harness.SetEntry{Set: mine, On: true}) // 뒤의 세트가 위 층
    },
})
```

- 승인 규칙·Approver·이벤트는 사용자 세트·배치가 무엇이든 유지된다. 기본 검사는 `guard` 세트다 — 끄거나 `ctx.Next` 를 부르지 않고 덮으면 빠진다 (사용자 책임).
- 실행 중 배치: dr 의 `/sets` 로 보고 `/set guard off`·`/set guard to 1` 로 켜기·끄기·순서를 바꾼다. 다음 run 부터 적용되고 설정 `sets` 에 저장된다. 빈 step 이 생기는 배치(예: `/set require off`)는 거부된다. Go 에서는 `Agent.Sets()`·`ArrangeSets`.
- 추가 도구도 도구 상한·설정 `agent.tools`·이름 규칙의 대상이다. 패턴 규칙은 `bash`·`read`·`write`·`edit`·`grep`·`find`·`ls` 에만 쓴다.
- 실행 가능한 전체 예시: [`examples/inject`](examples/inject/main.go) (`cd examples/inject && go run . "질문"`). 자기검증: [`examples/verify`](examples/verify/main.go) (`go run . -check "go test ./..." "질문"`).
- 자세한 규칙(만드는 순서, 세트 이름과 상태, 저장소 계약, 사용자 책임): [`docs/INJECT.md`](docs/INJECT.md).

## 핵심 개념 (한 장 요약)

- **step 22개 고정** (core 3.1). 루프는 step 을 순서대로 진행하고 각 step 을 주입 함수에 맡긴다.
- **신호** = 주입 함수가 반환하는 제어 약속어. 전이 10종 + 효과 3종 (core 3.2). step 별 허용 표 (core 3.4).
- **조회** = ctx 의 읽기 메서드. 그룹 8종, step 별 사용 가능 표 (inject 3.4).
- **ctx 는 dispatch 마다 새로 만들고 반환 후 폐기.** 허용되지 않은 신호·조회는 컴파일 에러(봉인 interface), 재사용은 런타임 에러 (inject 2.2). 서드파티도 Go FunctionSet 으로 `Bind` 에 겹친다 (D12).
- **step 당 함수 하나.** 여러 기능은 조합 함수로 합친다 (inject 3.7). 기본 동작은 sets 가 제공한다.

## 구현 순서 (D15: L0 + L1 전체)

1. 폴더마다 `go.mod` (지금 11개) + 루트 `go.work` (R4, R8), Go 1.23+.
2. `L0_core/contract` — 타입 + Validate + JSON 왕복. 테스트 먼저.
3. `L0_core/core` — 스크립트된 가짜 Dispatcher 로 테스트 먼저 → 구현. `-race`.
4. `L1_inject/inject` — StepSpec + 생성기 → 컴파일 실패 테스트 → Dispatcher → 조합.
5. `L1_inject/adapters` — httptest SSE 재생 + 골든 테스트 → Anthropic → OpenAI Chat → 카탈로그.
6. `L1_inject/sets` — 의존 interface·메모리 Store → 추정·압축 절단 → base → resilient → 종단 테스트 (가짜 ModelCaller·도구).
7. `L2_harness/harness` — Env·ModelCaller 위의 에이전트 조립 (R13, R14).
8. `L2_harness/osenv` → `L2_harness/agent` → `L3_ui/cmd/dr-print` 로 실행 가능한 에이전트 (R17).
9. surface 공통 계층(R18) → `L3_ui/surfaces/terminal` → `L3_ui/cmd/dr` (R19), 모델 미설정 실행·앱 안 모델 선택 (R20), 환경변수 제거·키 파일 (R21).

각 단계는 스펙 4장(검증)의 테스트를 먼저 쓰고 실패를 확인한 뒤 구현한다. 스펙과 코드가 충돌하면 멈추고 스펙·DECISIONS 를 먼저 고친다.

## 서드파티 고지

pi-mono·oh-my-pi 에서 가져온 문안·데이터와 외부 Go 의존의 라이선스는 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) 에 있다.
