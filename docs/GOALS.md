# dynamic_react 전체 목표와 개선 목록

이 문서는 남은 개선 항목을 한곳에 모은 목록이다. 인계 문서(`HANDOFF-impl-*.md`)는 세션 하나의 기록이고, 이 문서는 세션이 바뀌어도 계속 갱신한다.

- 결정의 근거는 [`DECISIONS.md`](DECISIONS.md)에 있다. 이 문서는 결정을 다시 적지 않고 id 로 참조한다.
- 항목을 끝내면 상태를 `해소`로 바꾸고 커밋이나 결정 id 를 적는다. 행은 지우지 않는다.
- 새 개선 항목은 인계 문서가 아니라 이 문서에 먼저 넣는다. 인계 문서에는 이 문서의 id 만 적는다.

기준 시점: 2026-10-12, R29 구현 (프롬프트 캐시 접두).

## 1. 전체 목표

루프는 루프로만 작동한다. 모든 동작(컨텍스트 구성, 요청 조립, 모델 호출, 도구 실행, 흐름 제어)은 밖에서 주입한다 (D1~D6). 언어는 Go 다 (D7).

| id | 목표 | 현재 상태 |
|---|---|---|
| G1 | core(L0)는 고정된 22 step 과 전이 표만 갖는다. 정책과 기본값은 core 밖에 둔다 | 달성. core spec v0.2 |
| G2 | Hermes·pi·oh-my-pi 의 기능을 주입 함수만으로 재현할 수 있다 | 28개 기능을 세트로 표현했다 (`VERIFY-22step.md`). 실제 모델로는 일부만 확인했다 (5.2) |
| G3 | 완성된 에이전트에도 core 를 포크하지 않고 주입할 수 있다 | 달성. R22, R26 (세트 층·배치 변경) |
| G4 | 런타임에 세트 배치를 바꿀 수 있다 | run 사이 변경(R26)과 턴 경계 모드 전환(R27)은 달성했다. 실행 중 부품 교체는 비목표다 |
| G5 | 레이어 책임을 지킨다. L0·L1 은 사용자 환경과 무관하고, 목록·검증·저장은 L2, 입출력은 L3 가 맡는다 | 유지 중 (R17) |
| G6 | 키는 `auth.json`(0600)에만 두고, 구독 OAuth 를 쓰지 않고, 환경변수를 읽지 않는다 | 유지 중 (R12, R21) |

## 2. 상태 값

| 상태 | 뜻 |
|---|---|
| 열림 | 할 일이다. 착수할 때 사용자에게 확인한다 |
| 결정 대기 | 사용자의 결정이 먼저 필요하다 |
| 보류 | 결정에 따라 지금 범위 밖이다. 사용자가 다시 꺼낼 때만 다룬다 |
| 하지 않음 | 사용자가 범위에서 뺐다. 다시 꺼내지 않는다 |
| 해소 | 끝났다. 근거를 적는다 |

## 3. 프롬프트 캐시: 세트·모드 전환이 일으키는 실패

R29 에서 기준을 **자동 접두 캐시**로 정했다. 요청 = [안정 접두][가변 꼬리]이고, 안정 접두(도구 목록 → system → 저장된 메시지)는 덧붙기만 한다. 원칙과 감지는 `DECISIONS.md` R29, 사용법은 `INJECT.md` 5.5a 에 있다. 3.1·3.2 는 R29 이전의 분석이다.

### 3.1 R29 이전 구조

| 위치 | 동작 |
|---|---|
| `L1_inject/sets/base.go:81` `Cache` | `request.finalize`에서 캐시 힌트 두 개를 단다. system 의 마지막 블록과 요청의 마지막 메시지다 |
| `L1_inject/adapters/anthropic.go:44` | 힌트를 `cache_control: ephemeral`로 바꾼다. 요청 순서는 tools → system → messages 다 |
| `L1_inject/sets/base.go:286` `baseContextProject` | 투영 = 편집을 적용한 Transcript + Ephemeral. Ephemeral 은 항상 **맨 끝**에 붙는다 |
| `L0_core/core/loop.go:36`, `:131` | `context.inject`의 출력이 Ephemeral 이 된다. 턴이 끝나면 비우고, Store 에 저장하지 않는다 |
| `L2_harness/harness/mode.go:104` `modeLayout` | 모드 배치 = 기본 배치 위에 모드 세트를 얹은 것이다. 모드 세트가 `cache`보다 위층이다 |
| `examples/modes/plan.go` `PlanSet` | `context.inject`로 매 턴 계획 지시를 넣는다. `request.finalize`에서 `write`·`edit`·`bash`를 도구 목록에서 뺀다. `tool.before`에서도 같은 도구를 막는다 |
| `examples/modes/goal.go` `GoalSet` | `context.inject`로 매 턴 목표를 넣는다 |
| 시스템 프롬프트 | run 동안 바뀌지 않는다 (ST-FR-19, HN-FR-21). 모드 전환도 system 을 바꾸지 않는다 |

`docs/INJECT.md` 5.5 에는 "모드가 도구 목록·지시를 바꾸면 그 턴부터 캐시 접두가 달라진다"는 한 줄만 있다. 아래 3.2 의 C2 는 이 설명보다 범위가 넓다. 한 번만 실패하는 것이 아니라 모드가 켜진 동안 **매 턴** 실패한다.

### 3.2 실패 경우

provider 동작은 provider 문서를 기준으로 적었다. 실제 모델로는 아직 확인하지 않았다 (CA-5).

| id | 경우 | 원인 | 영향 | provider |
|---|---|---|---|---|
| C1 | 모드 진입·이탈 (plan) | `PlanSet`이 도구 목록을 바꾼다. 도구 목록은 접두의 맨 앞이다 | 진입 직후 첫 요청과 이탈 직후 첫 요청에서 system·messages 를 포함한 캐시 전체가 무효다 | Anthropic, OpenAI 계열 모두 (도구 정의가 접두에 들어감) |
| C2 | 모드가 켜진 동안 매 턴 (plan, goal) | `cache`가 마지막 메시지에 힌트를 단다. 모드가 켜져 있으면 마지막 메시지는 Ephemeral(모드 지시)이다. Ephemeral 은 저장되지 않으므로 다음 턴의 접두에는 없다. 그래서 "직전 history + 지시"로 쓴 캐시는 다시 맞지 않는다. 또 영속 history 의 끝에는 힌트가 없어서 그 지점에 캐시 항목이 생기지 않는다 | messages 구간의 cache read 가 매 턴 거의 0 이 되고, 매 턴 전체를 cache write 로 다시 쓴다. Anthropic 에서는 write 단가가 일반 입력보다 비싸므로 캐시가 없을 때보다 비용이 늘 수 있다 | Anthropic (명시적 지점 방식). OpenAI 자동 접두 캐시는 history 접두가 그대로 맞으므로 영향이 작다 |
| C3 | run 사이 세트 배치 변경 (R26 `Arrange`, `/set`) | 켜거나 끈 세트가 도구 목록·`context.inject`를 바꾸면 C1·C2 와 같다. `cache`를 끄면 힌트가 사라진다 | 다음 run 첫 요청에서 캐시 무효 (C1), 이후 매 턴 실패 (C2), 또는 캐시 없음 | 세트에 따라 다름 |
| C4 | 사용자 세트가 `context.inject`를 쓸 때 | C2 와 같은 원인이다. 모드와 무관하게 기본 배치에서도 생긴다 | C2 와 같음 | Anthropic |

영향이 없는 것:

- 모드 전환 자체. 같은 RunTable 을 쓰고 system 이 고정이므로 전환만으로는 접두가 바뀌지 않는다.
- goal 판단기의 보조 모델 호출(`Deps.Aux`, R39). 별도 요청이라 본 모델의 캐시와 섞이지 않는다.

### 3.3 개선 항목

| id | 내용 | 권장 (판단) | 상태 |
|---|---|---|---|
| CA-1 | `cache` 세트가 메시지 힌트를 Ephemeral 앞의 마지막 영속 메시지에 단다. `RequestFinalizeCtx.Conv().Ephemeral()`로 개수를 알 수 있다. 마지막 메시지에도 힌트를 둘지 정해야 한다 (Anthropic 은 지점을 최대 4개까지 허용) | C2·C4 를 한 곳에서 고친다. 1순위. sets spec ST-FR 갱신 필요 | 해소 (R29: 꼬리 앞 마지막 메시지에만, sets v0.9 ST-FR-60·61) |
| CA-2 | plan 모드는 도구 목록을 그대로 두고 `tool.before` 차단만 쓴다. 지시문에 막힌 도구 이름을 적는다 | C1 을 없앤다. 대가로 모델이 막힌 도구를 한 번 부를 수 있다. 차단은 이미 `tool.before`에 있으므로 안전성은 같다 | 해소 (R29, examples/modes v0.2) |
| CA-3 | 모드 세트 작성 지침: "도구 목록과 system 은 바꾸지 않는다. 매 턴 지시는 `context.inject`로 넣는다"를 `INJECT.md` 5.5 에 적는다. 3.2 의 표를 옮긴다 | CA-1·CA-2 결정 뒤에 적는다 | 해소 (R29, `INJECT.md` 5.5a) |
| CA-4 | 결정론적 테스트: 모드가 켜진 요청에서 메시지 힌트의 인덱스가 영속 구간의 끝인지 확인한다. 모드 진입 전후의 도구 목록이 같은지 확인한다 (CA-2 채택 시) | CA-1 과 함께 | 해소 (R29: sets `TestCacheHintBeforeTail`, examples `TestPlanMode`. 실행 중 감지 `prefix_break` 이벤트 — harness v1.5 3.6f, terminal TM-FR-60) |
| CA-5 | 실제 모델 확인: plan 모드를 여러 턴 돌려 턴마다 `Usage.CacheRead` 를 기록하고 R29 전후를 비교한다 | **DeepSeek(`deepseek-flash`, 자동 접두) 측정 완료 (2026-10-12)**. 시나리오 = 기본 1 run → plan 진입 → plan 1 run(파일 읽기 후 계획) → 이탈 → 기본 2 run. 예전 plan(도구 목록에서 write·edit·bash 를 뺌)과 R29 plan 을 같은 프로그램으로 돌림. 첫 메시지에 실행마다 고유값을 넣은 2회씩: 전체 적중 old 82.0%·83.6% → new 86.6%·88.8%. 진입 직후 요청 old 78.6%·70.2% → new 90.7%·92.2%. 이탈 직후 요청 old 60.4%·66.0% → new 64.5%·77.2%. plan 안의 턴끼리는 둘 다 80~93% (꼬리 지시가 매 턴 바뀌어도 자동 접두에서는 손해가 작음, C2 는 Anthropic 한정이라는 분석과 맞음). `prefix_break` 는 실제 모델에서도 old 에서만 진입·이탈 두 번 `tools` 로 나옴. 관찰: ① DeepSeek 은 도구 목록이 바뀌어도 앞부분 일부(1408토큰)가 맞았다 — 예전 실행이 "system + 줄어든 도구 목록" 접두를 캐시에 남겼기 때문으로 보인다 (첫 old 실행에서는 640토큰) ② 적중률이 Claude Code 처럼 99% 에 가깝지 않은 것은 접두 문제가 아니다. 상한 = 직전 요청과 공통인 앞부분 ÷ 이번 요청인데, 요청이 2~3.6k 토큰으로 작아 턴마다 새로 붙는 내용(답·도구 결과·새 입력, 100~1000 토큰)이 5~30% 를 차지한다. 또 cached 값은 모두 128 의 배수이고 "공통 앞부분을 128 단위로 내림한 값"과 대부분 맞는다(맞지 않는 칸 하나는 DeepSeek 의 최선 노력 캐시 영향일 수 있으나 확인 못 함). 이 기준으로 보면 new 는 상한에 거의 붙어 있다. 이탈 직후가 낮은 것도 계획 전문이 새 내용이기 때문이다(상한 약 69%, 실제 64.5%) ③ 같은 문장으로 반복 실행하면 실행끼리 캐시를 공유해 비교가 흐려진다. Anthropic(지점 방식, C2·CA-1 효과)은 측정하지 않음 | 해소 (DeepSeek). Anthropic 측정은 하지 않음 (사용자 결정, 2026-10-12) |
| CA-6 | 턴별 캐시 적중률을 이벤트나 `/status` 같은 곳에 보여 줄지 | 측정 수단이 없으면 회귀를 놓친다. 표시 위치는 L3 이고 값은 이미 `contract.Usage`에 있다. R29 의 `prefix_break` 는 접두 변경만 알리고 적중률은 재지 않는다. 표시한다면 hit% 와 함께 "직전 요청 대비 효율"(cached ÷ 직전과 공통인 앞부분)을 후보로 둔다 — 작은 세션에서는 hit% 만으로는 문제가 있는 것처럼 보인다 (CA-5 관찰 ②) | 해소 (R30: run_end 에 캐시 비율, `/status` 에 세션·모델·모드·세트·도구·컨텍스트·마지막 run·세션 누적과 효율. harness v1.6 3.6g, agent v0.10, terminal v0.8) |
| CA-7 | Chat 방언(`adapters/chat.go`)이 `CacheHints`를 버린다. OpenRouter 로 Claude 를 쓰면 캐시가 전혀 되지 않는다 (OpenRouter 는 메시지 안의 `cache_control` 을 요구) | 코드로 확인한 사실. 지점 방식 흉내(R29 ⓔ)를 Chat 방언에도 넓힐지 정해야 한다 | 해소 (R31: 프로필 `CacheControl`, 내장 openrouter 는 `anthropic/` 모델에서 켬, 위치는 `cache` 세트 힌트를 따름. adapters v0.7 AD-FR-54·55. 실제 OpenRouter 로는 확인 안 함) |
| CA-8 | 로컬 서버(llama.cpp·Ollama)에서 보조 모델 호출(goal 판단기)이 같은 서버·모델로 가면 슬롯의 KV 캐시를 덮어, 다음 본 요청이 대화 전체를 다시 계산할 수 있다 | 추론만 함. 실제 확인 필요 | 해소 (R32: 코드 변경 없이 `INJECT.md` 5.4 에 피하는 방법 안내. 서버 동작은 문서 기준, 실제 서버로는 확인 안 함) |
| CA-9 | OpenAI Responses 방언에 `prompt_cache_key`(세션 ID 기반)를 보내지 않는다. 보내면 같은 서버로 가서 적중률이 오른다 | 비용이 적다 | 해소 (R33: `contract.Options.CacheKey`(ContractVersion 2), `cache` 세트가 세션 ID 로 채움, 프로필 `PromptCacheKey` 가 켜진 provider 만 `prompt_cache_key` — 내장은 openai 만. 실제 OpenAI 로는 확인 안 함) |

## 4. 모드 (R27)

| id | 내용 | 상태 | 출처 |
|---|---|---|---|
| MD-1 | 뮤테이션 검사: harness `mode.go`의 `decide`·`applyPending`·`runSwitch` 핵심 분기 | 해소 (`decide`·`applyPending`·`runSwitch.Dispatch`·`pick` 34/34 KILLED. 처음 살아남은 8개 — 모드 안에서 세트 진입, 예약이 있을 때 지금 상태로 가는 요청, 같은 모드 다른 Arg, 세트 Exit 적용 이벤트의 Reason, 적용 저장 실패 때 고침 이벤트, 고침 저장 실패의 `Start` 에러, run 안 적용 실패, Modes 없는 Agent 의 run — 는 테스트를 보강해 잡음. 마지막 것은 스펙에 없어 harness v1.7 HN-FR-65·66 에 명시함 — 동작 변경 없음) | impl-09 |
| MD-2 | 보조 모델의 thinking·max tokens 설정 (지금은 모델만 정한다) | 해소 (R40: `aux.<역할>` 에 `maxOutputTokens`·`thinking`, 폴백 aux → 설정 → Lookup, thinking 은 물려받지 않음. 인터뷰 중 R39 원칙 "sets 는 자기 루프의 제어만" 을 정하고 sets 전수조사 — 보조 모델 규칙·aux 설정·레지스트리·메모리 Store·StateStore 연결을 harness 로 옮기고 세트는 `Deps.Aux` 통로로 부름. harness v1.9, sets v0.11, agent v0.11, examples/modes v0.4. 뮤테이션 32개 중 KILLED 31, 등가 1은 분기를 없앰) | impl-09 |
| MD-3 | 터미널에서 실행 중에 `/mode off` (지금은 대기 중에만 된다. agent API 는 실행 중에도 받는다) | 해소 (R36: terminal v0.10 이 실행 중 `/mode off` 를 받고 `(다음 턴부터)` 줄을 냄. 같은 run 의 다음 턴부터 기본 배치. 실행 중 `/mode <name>` 은 그대로 거부) | impl-09 |
| MD-4 | 전체 배치형 모드 (`Mode.Layout`) | 보류 (R27 비목표) | impl-09 |
| MD-5 | 모드 겹치기, 모델이 모드에서 다른 모드로 직접 전환 | 보류 (R27 비목표) | INJECT.md 5.6 |

## 5. 검증

| id | 내용 | 상태 | 출처 |
|---|---|---|---|
| VF-1 | `examples/modes` 실제 모델 시연: 임시 Go 모듈에서 `-check "go test ./..."` + `/mode goal …`, 그리고 `/mode plan …`. CA-5 와 묶는다 | 열림 (사용자 확인 후) | impl-09 |
| VF-2 | Anthropic·OpenAI 방언의 실제 응답 검증 | 열림 (API 키·크레딧 필요) | impl-06 |
| VF-3 | provider overflow 에러 복구를 실제 모델로 확인 (지금은 가짜 서버로만 검증) | 열림 | impl-06 |
| VF-5 | R29 새 코드의 뮤테이션 검사: harness `prefix.go` 의 `check`·`compare`, surface 래핑, sets `StableLen`·`Cache`, terminal `prefix_break` 줄 | 해소 (29/29 KILLED. 처음 살아남은 4개 — 도구 결과 해시, 편집 + tools 의 Expected, Kept 표시, 모델 줄 흐림 — 는 테스트를 보강해 잡음) | R29 |
| VF-6 | terminal `TestRunCompact` 가 부하 중 한 번 실패했다 (전체 `-race` 실행에서 다른 테스트가 10초 멈춘 때). 5회 반복·전체 2회에서는 재현 안 됨. 대기 조건 보강 여부 | 해소 (원인: `screen.draw` 는 지우기와 다시 그리기를 여러 번의 Write 로 나눠 보낸다. 그래서 "화면에 실행 중 상태줄이 없음"으로 대기를 판정하면 지운 직후의 화면에서 잘못 통과했다. 다른 모듈 부하 중 40회 반복으로 재현함. 고침: 테스트 헬퍼 `idle()` 이 `t.st == stIdle` 을 직접 보게 하고, 같은 판정 5곳을 바꿈. 제품 코드는 그대로. 수정 뒤 같은 조건 240회 실패 없음) | R30 작업 중 관찰 |
| VF-7 | R30~R33 새 코드의 뮤테이션 검사: harness `stats.go`, agent `ToolNames`, terminal `status.go`·run_end 비율, adapters `chatCacheHints`·`markText`·`setCacheKey`, sets 캐시 키 | 해소 (77개 중 KILLED 76, 등가 1. 처음 살아남은 11개 중 9개 — 요약기 없음·Window 0 의 Threshold, hit%·효율의 분자 혼동, developer 역할, system 없는 system 힌트, system 까지 거슬러 오름, 빈 문자열 content, `anthropic/` 의 `/` — 는 테스트를 보강해 잡음. 등가 2개(H4·H10, `add` 의 `hasCeiling` 분기)는 분기를 없애 코드를 줄임 — 동작 같음. 남은 등가 1개는 agent `ToolNames` 의 `err != nil` 검사: `sets.ParseConfig` 가 에러면 `Config{}` 를 돌려줘 결과가 같지만, 다른 모듈의 구현 세부에 기대지 않으려고 둠) | R30~R33 |
| VF-4 | 관측 불가 뮤턴트 2개: agent `NoRemoteCatalog` 전달, terminal 종료 중 경합 | 보류 (외부 네트워크·경합이라 관측 불가) | impl-05, impl-07 |

## 6. examples/modes

| id | 내용 | 상태 | 출처 |
|---|---|---|---|
| EX-1 | goal 판단기가 마지막 답 텍스트 대신 하네스가 관찰한 도구 결과(`Conv().Transcript()`)로 판정 | 하지 않음 (R37: Hermes 처럼 마지막 답 + verify gate 를 유지. 진행 중 조건 변경은 EX-3) | impl-09, VERIFY #31 |
| EX-2 | 판정 프롬프트 품질 튜닝 | 열림 (VF-1 뒤) | impl-09 |
| EX-3 | 진행 중 사용자 입력이 있으면 판정 미루기 | 해소 (R38, examples/modes v0.3: 판정 턴에 입력함에 사용자 메시지가 있으면 판정을 미루고 그 입력을 처리한 턴 뒤에 판정. goal 진행 중 사용자 메시지(신호 기록의 `inject_input` 이 아닌 입력)를 Arg 별로 모아 판정 요청에 추가 기준 블록으로 넘김 — 메시지당 500자, 총 2000자. 뮤테이션 17개 중 KILLED 14, 등가 1, 관측 불가 2) | VERIFY #34 |

## 7. 세트 후보

| id | 내용 | 상태 | 출처 |
|---|---|---|---|
| ST-1 | 예산 가드. 측정 기준(누적 토큰 또는 현재 컨텍스트 크기)부터 정해야 한다 | 결정 대기 | impl-08 |
| ST-2 | 반복 감지 | 열림 | impl-08 |
| ST-3 | 모델 라우팅 | 열림 | impl-08 |
| ST-4 | `dr`에 모드 등록. 지금 `dr`은 `DefaultSets` 프리셋만 등록하고 모드는 없다. 인자가 필요한 세트·모드는 자기 main 에서 등록한다 | 결정 대기 | impl-08, impl-09 |

## 8. 결정 대기 (기타)

| id | 내용 | 출처 |
|---|---|---|
| DC-1 | verify 세트: 검증 보고가 사용자 지시보다 우선한다는 문구를 바꿀지 | impl-08 |
| DC-2 | 저장소 LICENSE. 사용자가 묻지 않으면 손대지 않는다 | impl-06 |
| DC-3 | agent 사용자 도구의 패턴 규칙 비교 값 (`Target func(args) string`). 필요해지면 다룬다 | impl-07 |
| DC-4 | harness 의 접두 기준표(R29)와 사용량 집계(R30)는 세션마다 프로세스 메모리에 쌓이고 지우지 않는다. 대화형 터미널에서는 문제가 없지만, 오래 떠 있는 서버형 surface 에서는 세션 수만큼 늘어난다. 정리 정책(세션 종료 시 삭제, 개수 상한 등)을 정할지 → **해소** (R34: 만들지 않음. 서버형 surface 를 만들 때 다시 검토) | R29, R30 |
| DC-5 | `PrefixBreak.Kept` 는 Part messages 일 때 항상 `At` 과 같고, 그 밖에는 0 이다. 필드를 남길지 정리할지 → **해소** (R35: 필드 제거. harness v1.8, terminal v0.9 는 유지 수로 `At` 을 씀. 화면 출력 같음) | R29 |

## 9. 보류와 하지 않음

| id | 내용 | 상태 | 근거 |
|---|---|---|---|
| NG-1 | 실행 중 부품 교체 | 보류 | R26 비목표 |
| NG-2 | 프로젝트별 세트 배치 | 보류 | R26 비목표 |
| NG-3 | Bind 시점 Next 검사 | 보류 | R26 비목표 |
| NG-4 | 이미 빌드된 `dr`·`dr-print`에 주입하기. 주입은 자기 main 에서 한다 | 보류 (D12 의도) | impl-07 |
| NG-5 | run 드라이버(대기 제어): WAIT 자동 재개, `/loop`, heartbeat | 하지 않음 | R28 |
| NG-6 | core 루프(L0 core) 수정, 구독 계정 로그인, 환경변수 읽기 추가, L3 가 설정 파일 직접 읽기·쓰기. L0 contract 의 필드 추가는 사용자 승인으로만 (R33 `CacheKey`) | 하지 않음 | R21, R17, R33 |

## 10. 해소된 항목 (추적용)

| 출처 | 항목 | 해소 근거 |
|---|---|---|
| impl-01 | R4 모듈 경로 | R4 `github.com/Biisairo/dynamic_react` |
| impl-01 | inject O1 StepSpec 형식 | Go 코드로 확정 (inject SPEC 미결 O1) |
| impl-01 | inject O2 원격 plugin | 폐기 (inject v0.3, R10) |
| impl-01~04 | inject O3 dispatch 밖 panic | 유지로 종결 (R18 ⑦) |
| impl-01 | adapters O1 SDK, O2 모델 목록 | net/http 직접 구현, 동적 조회 (R11) |
| impl-01 | sets O1 시스템 프롬프트, O2 재시도 기본값, O3 sessionDir | harness 부록 A, pi `RetrySettings`, `~/.dynamic_react/sessions` |
| impl-01 | 07 문서 8장 재현 검증을 22 step 기준으로 다시 수행 | `VERIFY-22step.md` |
| impl-03 | L2 O2 라이선스 고지 | `THIRD_PARTY_NOTICES.md` (impl-05) |
| impl-03 | L2 O3 osenv·dr-print 스펙 | 각 모듈 SPEC (R17) |
| impl-02~04 | vLLM `max_model_len`, Xiaomi base URL | impl-05 에서 확인 |
| impl-05 | L3 실제 TTY 수동 확인 | impl-06 pty 시험 16/16 |
| impl-06 | R22 미결 결정 1~5 | R22 |
| impl-06 | agent SPEC 절 번호 중복 (3.13) | agent SPEC 에 3.13 은 하나만 남음 |
| impl-07 | R22 비목표 "실행 중 세트 교체" | R26 (run 사이), R27 (턴 경계 모드) |
| impl-08 | 세트 후보 plan→act | R27 plan 모드 (`examples/modes`) |
