# sets — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.7 (초안)
- 상위 근거: `docs/DECISIONS.md` (D6, D9, D11, D15, R2, R3, R7, R9, R13, R24, R26)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`
- 사용하는 쪽: `L2_harness` (도구·저장소·프롬프트·요약기 구현을 주입)

---

## 1. 소개

### 1.1 목적
22개 step 의 **기본 동작과 기본 정책을 함수 세트로** 제공한다. core·inject 는 기본값을 갖지 않으므로(D6), 기본값은 여기에 있다. 세션 파일·도구 구현·프롬프트 조립·요약 프롬프트 같은 **하네스 기능은 L2_harness** 가 가지고(R13), sets 는 그것들을 interface(2.3)로만 받는다.

### 1.2 범위
- 포함:
  - 부품 세트 (R26 ②): `base`·`require`(둘이 22 step 을 모두 채움), 정책 `cache`·`guard`·`truncate` — 3.0. 순서·기본 켜짐은 갖지 않는다 (R26 ③, 기본 구성은 L2 harness)
  - `resilient` 세트: 에러 재시도, overflow 복구, 임계치 압축 판정, follow-up
  - 의존 interface: `ModelCaller`, `ToolRegistry`, `Store`, `Summarizer` (2.3)
  - 메모리 Store, 도구 레지스트리(맵), 토큰 추정, 압축 절단점, 루프 설정 스키마
- 제외:
  - L2_harness 로 이동 (R13): 도구 4종 구현, JSONL 세션 저장소, 시스템 프롬프트 조립(기본 문안·AGENTS.md·스킬), 요약 프롬프트·요약기 구현, 설정 파일 경로 해석(→ `L2_harness/agent`), dr-print CLI(→ `L3_ui/cmd/dr-print`, R17), 실제 provider 종단 테스트(→ `L2_harness/agent`)
  - 다음 단계 세트 (`quality`·`assistant`): 병렬·선행 실행 스케줄러, 서브에이전트, soft tool requirement, 메모리, 백그라운드 리뷰, fallback provider, 권한 승인

### 1.3 참조 (수치의 출처)

| 값 | 출처 |
|---|---|
| 도구 결과 2000줄 / 50KB | pi `core/tools/` (02 문서 10장) |
| reserve = max(16384, 15%·W) | omp `effectiveReserveTokens` (04 문서 2.3) |
| keepRecent 20,000 토큰 | pi·omp 공통 기본값 (02 문서 8.1) |
| 요약 출력 = min(0.8·reserve, 모델 최대 출력, 16384) | pi `generateSummaryWithUsage`, omp `MAX_SUMMARY_TOKENS` |
| 추정: ASCII (len+3)/4, 비ASCII 바이트/4 + 한중일 1자=1토큰 | Hermes `estimate_tokens_rough` (04 문서 3.2) |
| 이미지 1200 토큰 | omp `IMAGE_TOKEN_ESTIMATE`, pi 4800 chars |
| overflow 복구 1회 | pi `_checkCompaction` compact-and-retry 1회 |
| 무효 압축 2회 연속 → 자동 압축 중단 | Hermes anti-thrash |
| 압축 이전 usage 무시 | pi `estimateProjectedContextTokens`, omp `assistantPredatesCompaction` |
| 재시도 3회, 2s·4s·8s, 상한 60s | pi `core/settings-manager.ts` `RetrySettings` 기본값 (maxRetries 3, baseDelayMs 2000, maxAgentDelayMs 60000) |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 L2_harness ── deps: Model(adapters.Client), Tools, Store, System, Summarizer
                        │
 sets.Base / Require / Cache / Guard / Truncate (deps)   parts of the former base (3.0) ── store ── estimate
 sets.Resilient(deps)   overrides: 3 context.maintain, 12 call.error, 13 response.classify, 21 run.beforeSettle
                        │
 inject.BindWith(inject.BindOptions{StateStore: sets.StateStoreOf(store)}, base, resilient)
```

sets 는 contract, core, inject 와 표준 라이브러리만 import 한다 (adapters 에 의존하지 않는다, R13).

### 2.2 루프 설정 (RunInput.Config)

```json
{
  "model":           { "provider": "anthropic", "id": "claude-opus-5-5" },
  "maxOutputTokens": 0,
  "thinking":        "",
  "tools":           [],
  "compaction":      { "enabled": true, "reserveTokens": 0, "keepRecentTokens": 20000 },
  "retry":           { "maxAttempts": 3, "baseDelayMs": 2000, "maxDelayMs": 60000 }
}
```

| id | 요구사항 |
|---|---|
| ST-FR-01 | 설정은 위 스키마의 JSON 이다. 빈 값·0 은 "기본값 사용" 이다 (`tools` 가 비면 레지스트리의 전부). 모르는 키는 에러. `model` 은 필수 |
| ST-FR-02 | 설정에 API 키 값을 넣는 필드는 없다 (adapters AD-C2) |

### 2.3 의존 (Deps)

| id | 요구사항 |
|---|---|
| ST-FR-32 | `ModelCaller`: `Call(ctx, req) iter.Seq2[contract.StreamEvent, error]`, `Lookup(ctx, provider, id) (contract.ModelInfo, bool)`. `adapters.Client` 가 그대로 만족한다 (필수) |
| ST-FR-33 | `ToolRegistry`: `Get(name) (Tool, bool)`, `Names() []string`. `Tool{Spec, Run(ctx, call) contract.ToolResult, Truncate(result) result}` (Truncate 가 nil 이면 18 의 기본 규칙). `sets.NewRegistry(tools...)` 가 맵 구현을 준다. nil 이면 도구 없음 |
| ST-FR-34 | `Store`: `Load(ctx, sessionID) (Session, error)`, `AppendMessages`, `AppendEdits`, `SaveState(ctx, sessionID, ns, inject.StateDelta)`. `Session{Messages, Edits, State}`. `sets.NewMemoryStore()` 가 메모리 구현을 준다. nil 이면 Base 가 메모리 Store 를 만든다. `sets.StateStoreOf(store)` 는 inject 의 StateStore 로 감싼다 |
| ST-FR-35 | `System []contract.Block`: 시스템 프롬프트. 하네스가 run 전에 조립해 넘긴다. run 동안 바꾸지 않는다 (ST-FR-19) |
| ST-FR-36 | `Summarizer`: `Summarize(ctx, SummaryRequest{Model, Messages, Previous, MaxTokens}) (string, error)`. nil 이면 압축을 하지 않는다 (resilient 의 압축 분기는 3·13 에서 next, 12 에서 fail) |
| ST-FR-37 | `Sleep(ctx, d) error`, `Now() time.Time`, `NewID() string`: nil 이면 타이머·`time.Now`·무작위 id. 테스트에서 주입한다 |

### 2.4 제약
- ST-C1. API 키는 sets 를 거치지 않는다 (ModelCaller 구현의 몫). 상태·Store 에 기록하지 않는다.
- ST-C2. base 세트에는 권한 승인이 없다 — 도구는 제한 없이 실행된다 (pi 와 같음). 경고는 하네스가 한다.

---

## 3. 상세 요구사항

### 3.0 세트 분해 (R26 ②)

3.1 의 표는 각 step 의 기본 **동작**이다. 그 동작들은 아래 세트로 나뉜다. 세트들은 inject 체인 바인딩(IN-FR-19, 60)으로 쌓이고, 위 층은 `ctx.Next` 로 아래 층을 부른다. 표의 "아래 층 필요" 는 그 함수가 `ctx.Next` 를 부른다는 뜻이다 — 같은 step 을 채운 세트가 아래에 없으면 그 dispatch 는 `ErrNoNext` 로 실패한다 (IN-FR-62).

| 세트 (`Name`) | 채우는 step 과 동작 (3.1 의 해당 부분) | 아래 층 필요 | 근거 |
|---|---|---|---|
| `base` | 1 History·이전 편집 복원, 편집 수 기록, 초기 입력 보관 / 2 입력·입력함, `inbox.consumed` 공개 / 5 편집 적용·Ephemeral·미기록 편집 저장 / 14 응답 저장 / 16 통과(`BeforeOut{Call}`) / 17 `Tool.Run` / 18 통과(결과 그대로) / 19 결과 메시지 저장 | 없음 | L2·L3 가 기대는 계약: agent History·Sessions·`/resume`(저장), Steer(`inbox.consumed`, AG-FR-24), Gate·이벤트가 감싸는 16·17·18 |
| `require` | 1 짝 복구(ST-FR-40, base 의 결과에 덧붙임) / 3·4·10·13·21·22 아무것도 안 함 / 6 요청 조립 / 7 짝 검증·복구(캐시 힌트 없음) / 8 모델 선택 / 9 모델 호출 / 11 응답 확정 / 12 재시도 없이 fail / 15 순차 계획 / 20 도구 호출이 있으면 continue_turn, 없으면 settle (R2) | 1 | 동작에 필수 (없으면 루프가 묶이지 않거나 provider 가 요청을 거부) |
| `cache` | 7 캐시 힌트(system 마지막 블록, 마지막 메시지) | 7 | 정책 |
| `guard` | 16 없는 도구·스키마 검사(ST-FR-38), 통과하면 아래 층 | 16 | 정책 |
| `truncate` | 18 `Tool.Truncate`, 없으면 2000줄 / 50KB 기본 절단 (아래 층 결과에) | 18 | 정책 |
| `resilient` | 3.2 그대로 (3·12·13·21) | 없음 | 정책 |
| `verify` | 3.2a (20) | 20 | 정책 |

| id | 요구사항 |
|---|---|
| ST-FR-51 | `sets.Base`·`Require`·`Cache`·`Guard`·`Truncate`(각 `(Deps) inject.FunctionSet`)는 위 표의 step 만 채우고 `Description` 에 한 줄 설명을 둔다 (`Resilient`·`Verify` 도 Description 을 둔다). `Bind(Base, Require, Cache, Guard, Truncate)` 의 동작은 v0.6 의 `Bind(Base)` 와 같고, 여기에 `Resilient` 를 더한 것은 v0.6 의 `Bind(Base, Resilient)` 와 같다 (모든 기존 base·resilient 테스트가 이 조합으로 통과) |
| ST-FR-52 | sets 는 세트의 순서·기본 켜짐·기본 목록을 갖지 않는다 (R26 ③). 테스트용 조합은 테스트 안에 둔다 |
| ST-FR-53 | 상태 네임스페이스: base 의 키(ST-FR-43)는 `base` 에 그대로. require·cache·guard·truncate 는 상태를 쓰지 않는다 |

### 3.1 step 별 기본 동작 (3.0 의 세트들이 나눠 가짐)

| # | step | 기본 동작 | 신호 |
|---|---|---|---|
| 1 | run.start | `Store.Load` 로 History(= Session.Messages) 와 이전 편집을 복원한다. 이전 편집은 `rewrite` 로 이번 run 의 편집 로그에 다시 올리고, 저장된 편집 수를 상태에 기록한다. **짝 없는 tool call 복구**: 결과 없는 호출마다 `Status: aborted` 결과 메시지를 History 에 붙이고 Store 에도 기록 (D9). 초기 입력(`RunInput.Input`)을 상태에 보관 | [rewrite…] next |
| 2 | turn.start | 첫 턴: 보관한 초기 입력. 이후 턴: 입력함에서 **아직 소비하지 않은** 메시지(소비 수는 공개 상태 `inbox.consumed`). 반환한 메시지를 Store 에 기록 | 첫 턴에 입력이 없으면 settle, 아니면 next |
| 3 | context.maintain | 아무것도 안 함 | next |
| 4 | context.inject | 빈 목록 | next |
| 5 | context.project | Transcript 에 편집 로그를 적용: `replace_range` → 구간을 요약 메시지로, `hide` → 제외, `truncate_after` → 이후 제외. 그 뒤 Ephemeral 을 끝에 붙임. **아직 Store 에 기록되지 않은 편집을 기록** | next |
| 6 | request.build | System = `Deps.System` · Tools = 설정 `tools`(비면 전부)의 spec · Messages = 투영 · MaxOutputTokens = 설정값 또는 마지막 ModelInfo 의 값 · Thinking = 설정 `thinking` | next |
| 7 | request.finalize | **짝 검증·복구**: 결과 없는 tool call 뒤에 aborted 결과를 끼움. 캐시 힌트: system 마지막 블록, 마지막 메시지 | next |
| 8 | model.select | 설정 model → `ModelCaller.Lookup`. 없으면 `ContextWindow 0`(모름), capability 없음으로 ModelInfo 구성 | next |
| 9 | model.call | `CallStream{Events: Deps.Model.Call(ctx.Context(), req), Live: nil}` (R9) | next |
| 10 | stream.event | 아무것도 안 함 | next |
| 11 | stream.end | Response = 입력 그대로, Discard = 선행 실행 전부 (base 는 선행 실행을 요청하지 않음) | next |
| 12 | call.error | 재시도 없음 | fail(CallError) |
| 13 | response.classify | 아무것도 안 함 | next |
| 14 | response.commit | 응답을 Store 에 기록 (usage 는 메시지에 있음, contract CT-FR-01a) | next |
| 15 | tools.plan | 응답의 호출마다 그룹 하나, `MaxParallel 1` (순차) | next |
| 16 | tool.before | 등록되지 않은 도구 → ShortCircuit(error "unknown tool: X"). 인자가 스키마에 맞지 않으면 ShortCircuit(error, 검증 메시지). 검사 범위는 ST-FR-38 | next |
| 17 | tool.execute | `Tool.Run(ctx.Context(), call)` | next |
| 18 | tool.after | `Tool.Truncate`, 없으면 텍스트를 앞에서 2000줄 / 50KB 로 자르고 잘렸음을 덧붙임 | next |
| 19 | tools.commit | 결과마다 tool_result 메시지(새 ID)를 만들어 Store 에 기록하고 출력으로 돌려줌 (core CORE-FR-24a) | next |
| 20 | turn.finish | 이번 턴에 도구 호출이 있었으면 continue_turn, 없으면 settle (R2) | continue_turn / settle |
| 21 | run.beforeSettle | 아무것도 안 함 | next |
| 22 | run.settled | 아무것도 안 함 (상태는 dispatch 마다 inject 가 저장) | — |

| id | 요구사항 |
|---|---|
| ST-FR-03 | 위 표의 동작은 3.0 의 세트들이 나눠 가지며, `Base`·`Require` 두 세트가 22 step 을 모두 채운다 |
| ST-FR-04 | (v0.7 삭제) step 별 생성자 `Base<Step>(Deps)` 는 없다 — 아래 층 동작을 감싸려면 자기 세트를 위에 쌓고 `ctx.Next` 를 부른다 (inject IN-FR-60). step 간에 공유할 상태는 publish 키로 읽는다 |
| ST-FR-40 | 1 의 짝 복구는 **마지막 assistant 메시지**(그 뒤에는 그 메시지의 결과만 있는 경우)의 짝 없는 호출만 History 끝에 aborted 결과로 붙이고 Store 에 기록한다 (Store 는 append-only). 그 밖의 짝 없는 호출은 7 이 요청 안에서 그 호출 바로 뒤에 끼운다 |
| ST-FR-38 | 16 의 스키마 검사 범위: 인자가 객체인지, `required` 키가 있는지, `properties` 에 선언된 최상위 키의 JSON 타입(`string`·`number`·`integer`·`boolean`·`array`·`object`·`null`)이 맞는지. 그 밖의 JSON Schema 키워드는 검사하지 않는다 (표준 라이브러리에 검증기가 없음) |

### 3.2 resilient 세트 — 덮어쓰는 step

`sets.Resilient(deps)` 는 `Name: "resilient"` 이고 아래 step 만 채운다. `Bind(Base, Resilient)` 로 겹쳐 쓴다.

**3 context.maintain — 임계치 압축**

```text
W        = ModelInfo.ContextWindow            (0 이면 아무것도 안 함)
reserve  = cfg.reserveTokens > 0 ? cfg.reserveTokens : max(16384, floor(0.15 * W))
used     = estimateContext(projection)        (3.6)
if used > W - reserve and not blocked and Summarizer != nil:
    edit = compact(projection, keepRecent)    (3.7)
    -> [rewrite(edit), next]
```

| id | 요구사항 |
|---|---|
| ST-FR-05 | 위 판정을 매 턴 한다. 도구 라운드마다 턴이 바뀌므로(20 → continue_turn → 2) 모델 호출 전마다 판정된다. 판정의 투영은 base 5 와 같은 규칙으로 Transcript 에 편집을 적용한 것이다 (3 은 5 보다 먼저라서) |
| ST-FR-06 | 압축 후 다음 응답의 usage 가 여전히 임계치 이상이면 "무효 압축" 1회로 센다. **2회 연속**이면 이 run 에서 자동 압축을 멈추고(blocked) 공개 상태 `compaction.blocked = true` 를 남긴다 (하네스가 알린다). 임계치 아래 응답이 오면 카운터를 0 으로 |

**12 call.error — 재시도와 overflow 복구**

| Class | 동작 |
|---|---|
| rate_limit, overloaded, transient_network | 같은 Class 의 이번 호출 재시도 횟수 < maxAttempts 이면 대기 후 `recall`. 대기 = RetryAfter(있으면, 상한 maxDelay) 또는 `min(baseDelay × 2^n, maxDelay)`. 대기는 ctx 취소를 따른다 (취소되면 fail). 넘으면 `fail` |
| overflow | 이번 턴에 overflow 복구를 안 했고 Summarizer 가 있으면 `[rewrite(compact), rebuild]` (R3). 그 외 `fail` |
| auth, invalid_request, payload_too_large, permanent, unknown | `fail` |

**13 response.classify — length 와 조용한 overflow**

| 조건 | 동작 |
|---|---|
| StopReason = length, 출력 < 요청한 MaxOutputTokens, 이번 턴 복구 미실시, Summarizer 있음 | `[rewrite(compact), rebuild]` (pi 의 recoverable length) |
| StopReason = stop 인데 `usage.Input + usage.CacheRead > W` (W > 0), 조건은 위와 같음 | overflow 로 보고 위와 같음 (pi 의 silent overflow) |
| 그 외 | next |

**21 run.beforeSettle — follow-up**

| 조건 | 동작 |
|---|---|
| 입력함에 소비하지 않은 메시지가 있음 (base 의 공개 상태 `inbox.consumed` 로 판단) | continue_turn |
| 없음 | next |

| id | 요구사항 |
|---|---|
| ST-FR-07 | resilient 는 위 네 step 만 채운다. 재시도·복구 횟수는 `ctx.State()` 에 사유별로 기록한다 (D11) |
| ST-FR-08 | 모든 대기·요약 호출은 ctx 취소를 따른다 |

### 3.2a verify 세트 — 자기검증 (R24)

`sets.Verify(deps, opts)` 는 `Name: "verify"` 이고 **20 turn.finish 만** 채운다. 기본 세트 위에 쌓아 쓴다: `Bind(Base, Require, …, Resilient, verify)` (harness `Sets` 주입). 아래 층의 turn.finish 는 `ctx.Next()` 로 부른다.

```go
type CheckResult struct { OK bool; Report string }
type VerifyOptions struct {
    Check       func(ctx context.Context) (CheckResult, error) // 필수
    MaxAttempts int                 // 0 이하 → 3
    Tools       []string            // nil → write, edit, bash (빈 슬라이스 = 변경 도구 없음 → 실패 뒤 재검증만)
}
var ErrVerifyFailed = errors.New("verify: checks still failing")
func Verify(d Deps, o VerifyOptions) (inject.FunctionSet, error) // Check nil → 에러 "sets: verify: Check is required"
```

| id | 요구사항 |
|---|---|
| ST-FR-46 | turn.finish 는 먼저 `ctx.Next()`(아래 층, 보통 require 의 R2 판정) 를 부른다. 에러면 그 에러, 신호 중 `settle` 이 없으면 그 신호를 그대로 돌려준다. `settle` 이 있으면 ST-FR-47 로 검증할지 정하고, 하지 않으면 그대로 돌려준다 |
| ST-FR-47 | 검증 조건: ① 마지막 검증 뒤(이 run 에서 아직 검증하지 않았으면 이 run 의 처음 = `len(Conv().History())` 부터 — Transcript 는 History 로 시작한다, core CORE-FR-31a) `Conv().Transcript()` 에 들어온 tool_result 메시지 중 `Status: ok` 이고 호출 이름(같은 Transcript 의 assistant `tool_call` 블록에서 CallID 로 찾음)이 `Tools` 에 있는 것이 있거나 ② 이 run 의 직전 검증이 실패였으면 검증한다. "마지막 검증 뒤" 의 기준은 검증 시점의 Transcript 길이다 |
| ST-FR-48 | 검증 = `Check(ctx.Context())`. error 면 `fail(fmt.Errorf("verify: check: %w", err))`. `OK` 면 `Next` 의 신호를 그대로 돌려준다. 아니면 이 run 의 실패 수를 1 늘리고, 실패 수 < MaxAttempts 면 `Next` 의 신호에서 `settle` 을 뺀 것 + `inject_input(보고 메시지)` + `continue_turn`, 실패 수 = MaxAttempts 면 `fail(fmt.Errorf("%w after %d attempts: %s", ErrVerifyFailed, n, 보고의 첫 비지 않은 줄(앞뒤 공백 제거)))` |
| ST-FR-49 | 보고 메시지: user 역할, ID = `d.NewID()`, Timestamp = `d.Now()`, text 블록 하나 = `Verification failed (attempt <k> of <N>). Fix the problems below, then finish.\n\n<보고>`. 보고는 `Report` 끝의 줄바꿈들을 뗀 뒤 **끝** 2000줄 / 50KB(온전한 줄, 마지막 한 줄이 50KB 를 넘으면 그 끝 50KB — bash 도구와 같은 방향)이고 잘리면 앞에 `[earlier output truncated]\n` 을 붙인다. 비면 `(no output)`. base 2 turn.start 가 입력함에서 꺼내 세션에 저장한다 |
| ST-FR-50 | 상태 (네임스페이스 `verify`, 키 `progress`): `{run, checked, failures, lastFailed}`. run 을 넘어 복원되므로 RunID 가 다르면 초기값(`checked` = History 길이, 나머지 0·false)으로 본다 (ST-FR-44 와 같은 방식). 검증할 때 `checked` = 그때의 Transcript 길이 |

### 3.3 도구 레지스트리

| id | 요구사항 |
|---|---|
| ST-FR-09 | (이동: L2_harness — 도구 4종과 경로 규칙) |
| ST-FR-10 | 도구는 `ToolRegistry` 로 받는다 (ST-FR-33). 사용자가 도구를 추가·교체할 수 있다 (`Registry.Add` 는 같은 이름을 덮는다) |
| ST-FR-11 | 도구 실행 에러는 `Status: error` 결과로 돌려준다. `Tool.Run` 의 panic 은 17 에서 recover 해 error 결과로 바꾼다 (루프를 멈추지 않는다) |

### 3.4 세션 저장소

| id | 요구사항 |
|---|---|
| ST-FR-12~14 | (이동: L2_harness — JSONL 파일 형식) |
| ST-FR-15 | `StateStoreOf(store)` 는 `Store.Load` 의 `Session.State` 로 복원하고 `SaveState` 로 저장한다 (inject IN-FR-26) |
| ST-FR-39 | 메모리 Store 는 세션별로 메시지·편집·상태를 순서대로 보관하고, 같은 프로세스의 다음 run 에서 `Load` 로 돌려준다. 동시 사용에 안전하다 |

### 3.5 시스템 프롬프트

| id | 요구사항 |
|---|---|
| ST-FR-16~18 | (이동: L2_harness — 기본 문안, 파일 대체, AGENTS.md·스킬 수집) |
| ST-FR-19 | 시스템 프롬프트는 run 동안 바뀌지 않는다 (`Deps.System` 을 그대로 쓴다. 프롬프트 캐시 보호, 06 문서 4.4) |

### 3.6 토큰 추정

| id | 요구사항 |
|---|---|
| ST-FR-20 | `EstimateText(s)`: ASCII 면 `(len+3)/4`, 아니면 한중일·한글·가나 글자 수 + 나머지의 `(UTF-8 바이트+3)/4` |
| ST-FR-21 | 메시지: text·thinking·도구 이름+인자 JSON·도구 결과 텍스트를 합산, 이미지 1개 1200, ProviderData·opaque payload 는 0 (provider usage 로만 가격이 매겨짐) |
| ST-FR-22 | `EstimateContext(projection, edits, system, tools)`: 투영 안에서 **마지막으로 성공한 응답의 usage** 를 찾고, 그 응답이 **가장 최근 편집보다 뒤**(메시지 Timestamp > 편집 Timestamp)이면 `usage.Total()` + 그 뒤 메시지 추정. 아니면(압축 이전 usage) 전부 추정 + 시스템 프롬프트·도구 스키마 추정 |

### 3.7 압축 (compact)

| id | 요구사항 |
|---|---|
| ST-FR-23 | 투영의 끝에서 거꾸로 추정 토큰을 쌓아 `keepRecentTokens` 이상이 되는 지점을 찾고, 그 앞쪽의 **유효 절단점**(user 메시지 또는 assistant 메시지. tool_result 는 절단점이 될 수 없음 — 호출과 결과를 떼지 않기 위함)에서 자른다. 절단점 앞 구간이 비면 압축하지 않는다 |
| ST-FR-24 | 절단점 앞 구간을 `Summarizer` 로 요약한다. 구간 첫 메시지가 이전 압축의 요약 메시지면 그 텍스트를 `Previous` 로 넘긴다. 요약 프롬프트는 Summarizer 구현(L2_harness)의 몫이다 |
| ST-FR-25 | 요약 출력 한도(`MaxTokens`) = `min(floor(0.8 × reserve), ModelInfo.MaxOutputTokens(>0 일 때), 16384)` |
| ST-FR-26 | 결과는 `ContextEdit{replace_range, from: 구간 첫 메시지 ID, to: 구간 끝 메시지 ID, messages: [요약을 담은 user 메시지]}` 하나다 |
| ST-FR-27 | 요약이 실패하면 압축을 포기하고 편집 없이 진행한다 (3 에서는 next, 12·13 에서는 fail). 무효 압축 카운트(ST-FR-06)에 포함하지 않는다 |

### 3.8 print CLI

| id | 요구사항 |
|---|---|
| ST-FR-28~31 | (이동: `L3_ui/cmd/dr-print` DP-FR-01~09, R17) |

### 3.9 비기능

| id | 요구사항 |
|---|---|
| ST-NFR-01 | 모든 step 함수는 가짜 View(`inject/injecttest`)로 단위 테스트한다 |
| ST-NFR-02 | 종단 테스트: 가짜 ModelCaller(스크립트된 스트림) + 가짜 도구 + 메모리 Store + 실제 base/resilient + core 로 "도구 2라운드 후 종료", "overflow → 압축 → 재호출", "429 → 대기 → 재시도", "세션 재개 시 짝 복구·편집 복원" 을 검증 |
| ST-NFR-03 | (이동: L2_harness — 실제 provider 종단 테스트) |
| ST-NFR-04 | sets 모듈은 contract·core·inject·표준 라이브러리 외 import 가 없다 |

### 3.10 Go 표현

스펙 요구사항을 코드로 옮길 때 정한 표현이다.

```go
type ModelCaller interface {
    Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error]
    Lookup(ctx context.Context, provider, id string) (contract.ModelInfo, bool)
}
type Tool struct {
    Spec     contract.ToolSpec
    Run      func(ctx context.Context, call contract.ToolCall) contract.ToolResult
    Truncate func(contract.ToolResult) contract.ToolResult
}
type ToolRegistry interface { Get(name string) (Tool, bool); Names() []string }
type Session struct { Messages []contract.Message; Edits []contract.ContextEdit; State map[string]inject.NamespaceState }
type Store interface {
    Load(ctx context.Context, sessionID string) (Session, error)
    AppendMessages(ctx context.Context, sessionID string, msgs []contract.Message) error
    AppendEdits(ctx context.Context, sessionID string, edits []contract.ContextEdit) error
    SaveState(ctx context.Context, sessionID, ns string, d inject.StateDelta) error
}
type SummaryRequest struct { Model contract.ModelRef; Messages []contract.Message; Previous string; MaxTokens int }
type Summarizer interface { Summarize(ctx context.Context, r SummaryRequest) (string, error) }
type Deps struct {
    Model ModelCaller; Tools ToolRegistry; Store Store; System []contract.Block; Summarizer Summarizer
    Sleep func(ctx context.Context, d time.Duration) error; Now func() time.Time; NewID func() string
}
```

| id | 요구사항 |
|---|---|
| ST-FR-41 | 공개 함수: `NewRegistry(tools...) *Registry`(등록 순서 유지, `Add` 는 같은 이름을 그 자리에서 덮음), `NewMemoryStore() *MemoryStore`(`Load` 는 복사본), `StateStoreOf(Store) inject.StateStore`(nil → nil), `ParseConfig(json.RawMessage) (Config, error)`, `Base`·`Require`·`Cache`·`Guard`·`Truncate`·`Resilient`(각 `(Deps) inject.FunctionSet`), `Resilient{ContextMaintain,CallError,ResponseClassify,RunBeforeSettle}(Deps)`, `EstimateText`, `EstimateMessage`, `EstimateContext`, `ApplyEdits(transcript, edits)`, `CheckArgs(schema, args)`, `Compact(ctx, Summarizer, ModelRef, proj, keepRecent, reserve, maxOut, now, editID, summaryID) (ContextEdit, ok, error)`, `ReserveTokens(cfg Config, window int) int`(3.5 의 reserve 식), `Verify`·`VerifyOptions`·`CheckResult`·`ErrVerifyFailed`(3.2a) |
| ST-FR-42 | 세트 생성자(`Base`·`Require`·`Cache`·`Guard`·`Truncate`·`Resilient`·`Verify`)는 Deps 의 nil 을 한 번만 기본값으로 채워 그 세트의 모든 step 이 같은 값을 쓴다 (nil Store → 메모리 Store 하나). 세트끼리는 각자 채우므로, 여러 세트가 같은 Store 를 써야 하면(base 의 저장과 require 의 짝 복구 기록) Deps 에 Store 를 채워 넘긴다. `Resilient<Step>` 생성자를 따로 부르면 각자 채운다 |
| ST-FR-43 | base 상태 키: `input`(초기 입력, 2 가 한 번 준 뒤 삭제), `inbox.consumed`(int, publish), `edits.saved`(Store 에 기록한 편집 수). 1 이 run 마다 다시 쓴다 (상태는 Store 로 run 을 넘어 복원되므로) |
| ST-FR-44 | resilient 상태는 run 을 넘어 복원되므로 RunID 를 함께 저장하고, RunID 가 다르면 초기 상태로 본다: `retry`{run, turn, class 별 횟수}(턴이 바뀌면 리셋), `recovered`{run, turn}(12·13 의 압축 복구), `compaction`{run, pending, invalid}, `compaction.blocked`(publish, `true`, 다른 run 에서는 삭제) |
| ST-FR-45 | 해석 (스펙 공백): ① `compaction.enabled=false` 는 3·12·13 의 모든 압축 분기를 끈다 (pi `_checkCompaction` 와 같음). ② 무효 압축 판정은 pending 일 때 다음 3 에서 `LastUsage().Prompt() >= W − reserve`. ③ 절단 구간이 이전 요약 메시지 하나뿐이면 압축하지 않는다. ④ 설정 `tools` 의 없는 이름은 무시한다. ⑤ 17 은 결과의 CallID 를 호출 ID 로 맞춘다. ⑥ 18 의 기본 절단은 text 블록마다 적용한다. ⑦ 요약 메시지 = user 역할, 고정 머리말 + 요약 텍스트. ⑧ 1·7 의 aborted 결과는 text 블록 하나("aborted")를 담는다 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| ST-FR-01, 02 | 설정 파싱: 기본값, 모르는 키 에러, model 누락, 키 필드 부재 |
| ST-FR-03, 04 | 22 step 모두 non-nil, exported 함수 목록 |
| 3.1 각 행 | step 별 단위 테스트 (정상 / 경계 / 실패) |
| ST-FR-05, 06 | 임계치 경계값(W − reserve ± 1), W=0, Summarizer 없음, 무효 압축 2회 연속 → blocked |
| 3.2 call.error 표 | Class 별 분기, RetryAfter 상한, 재시도 소진, overflow 1회 제한, 취소 중 대기 |
| 3.2 classify 표 | length 회복 조건, silent overflow |
| ST-FR-10, 11, 33, 38 | 레지스트리 추가·교체, 도구 panic → error 결과, 스키마 검사 범위 |
| ST-FR-15, 34, 39 | 메모리 Store 재생, 상태 복원 |
| ST-FR-19, 35 | run 중 System 불변 |
| ST-FR-20~22 | 추정: ASCII / 한글 / 이모지 / 이미지 / 압축 전후 usage 신뢰 |
| ST-FR-23~27 | 절단점이 tool_result 에 걸리지 않음, Previous 전달, 요약 한도, 실패 시 포기 |
| ST-NFR-02 | 종단 시나리오 4개 |
| ST-NFR-04 | `go list -deps` 검사 |
| ST-FR-46~50 | 가짜 ModelCaller·도구로 종단: 변경 도구 없이 끝남 → Check 안 부름, write 성공 뒤 끝남 → Check 1회·OK 면 completed, 실패 → 다음 요청에 보고 메시지(attempt 1 of N)·세션에 저장, 실패 뒤 변경 없이 끝남 → 재검증, MaxAttempts 번째 실패 → failed·`errors.Is(ErrVerifyFailed)`·첫 줄, error 결과의 변경 도구는 세지 않음, Tools 에 없는 도구는 세지 않음, 빈 Tools, 보고 끝 절단·빈 보고, Check error → failed, 아래 층을 바꿔 쌓으면(예: 항상 settle 하는 층) 그 판정을 따름·아래 층 에러 전달, 다른 run 은 상태 초기화, Check nil → 에러 |
| ST-FR-51~53 | 기존 base·resilient 테스트 전부를 `Base, Require, Cache, Guard, Truncate(, Resilient)` 조합으로 통과, 세트별 채운 step 목록·Description, 정책 세트를 뺀 조합: cache 없음 → 캐시 힌트 없음, guard 없음 → 없는 도구가 tool.execute 에서 `unknown tool` 결과(base 17), truncate 없음 → 절단 없음, require 없이 base 만 → Bind 에러(빈 step), 아래 층 없이 guard 만 16 에 → ErrNoNext |
| ST-FR-41~45 | 테스트가 공개 API 로만 동작을 검사, 복구 표시의 run·턴 범위(같은 run·턴 / 다른 턴 / 다른 run) |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| D6 | 3.1 전체 (기본값의 위치) |
| D9 | 3.1 의 1·7 행 |
| D11 | ST-FR-07, 15 |
| D15, R13 | 1.2 범위, 2.3 |
| R2 | 3.1 의 20 행 |
| R3 | 3.2 call.error overflow, classify |
| R7 | 3.1 의 5 행 (Transcript 사용) |
| 자격증명 보호 | ST-C1, ST-FR-02 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | (이동: L2_harness — 기본 시스템 프롬프트 문안) |
| O2 | ~~재시도 기본값 근거~~ → pi `RetrySettings` 기본값으로 확인 (1.3) |
| O3 | (이동: L2_harness — `~/.dynamic_react/sessions` 로 사용자 확정) |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.7 | `Base` 하나가 22 step 전부, step 별 생성자 `Base<Step>`, verify 의 `VerifyOptions.Next` | `Base`·`Require`·`Cache`·`Guard`·`Truncate` 로 분해(3.0), `Base<Step>` 삭제(ST-FR-04), verify 는 `ctx.Next()`. 전부 쌓으면 같은 동작(ST-FR-51) | R26 ② |
| 0.7 | `Base(Deps{Store: nil})` 의 메모리 Store 하나를 22 step 이 공유 | 세트마다 따로 채움 (ST-FR-42). nil Store 면 require 의 짝 복구 기록이 base 가 읽는 Store 에 남지 않음 | 세트 분해의 결과 (생성자가 서로를 모름). 구현 중 확인 (impl-08) |
| 0.6 | 세트 base·resilient | verify 세트 (3.2a) 추가. 기존 세트 동작 변화 없음 | R24 |
| 0.2 | 9 model.call = adapters 가 제공한 `ModelCallFn` 을 그대로 바인딩 | sets 가 `adapters.Call` 의 이벤트 시퀀스를 `core.CallStream` 으로 감쌈 | R9: adapters 가 contract 만 의존 |
| 0.2 | 19 = 결과를 세션에 기록 | 결과 메시지를 만들어 기록하고 출력 | core v0.2 CORE-FR-24a |
| 0.2 | ST-FR-04 제약 없음 | 감싼 함수의 상태 NS 는 감싼 세트 | inject v0.2 IN-FR-23 |
| 0.3 | sets 에 도구 4종·JSONL·프롬프트 조립·요약 프롬프트·CLI 포함, adapters 직접 사용 | 하네스 기능은 L2_harness, sets 는 `ModelCaller`·`ToolRegistry`·`Store`·`Summarizer` interface 로 받음 | R13: 하네스는 루프+주입보다 위의 레이어 (사용자) |
| 0.3 | 1 run.start: 세션 JSONL 에서 복원 | `Store.Load`, 이전 편집은 rewrite 로 다시 올림 | 편집 로그가 run 단위라 재개 후 투영에 이전 압축을 반영하려면 필요 |
| 0.3 | ST-FR-06 경고 이벤트 | 공개 상태 `compaction.blocked` | core 에 경고 통로가 없음 |
| 0.3 | 16 스키마 검증 범위 미정 | ST-FR-38 | 표준 라이브러리에 JSON Schema 검증기 없음 |
| 0.3 | 설정 `cwd`·`sessionDir`·`systemPrompt`·`catalog` | 제거 (하네스 설정), `thinking` 추가 | R13 |
| 0.3 | O2 근거 없음 | pi RetrySettings | 출처 확인 |
| 0.3 | 1 짝 복구: 결과 없는 호출마다 | 마지막 assistant 메시지만, 나머지는 7 (ST-FR-40) | Store 가 append-only 라 중간에 끼울 수 없음 |
| 0.3 | ST-FR-22 "편집보다 뒤" 기준 미정 | Timestamp 비교 | 투영만으로는 순서를 알 수 없음 |
| 0.3 | 재시도 횟수 범위 "이번 호출" | 턴 단위, Class 별 (ST-FR-07) | recall 은 같은 턴 안에서만 일어남 |
| 0.4 | Go 표현 미정 | 3.10 (ST-FR-41~45) | 구현 착수. 상태가 Store 로 run 을 넘어 복원되므로 run 범위 상태에 RunID 를 둠 |
| 0.5 | reserve 식은 비공개 | `ReserveTokens` 공개 (동작 같음) | harness 수동 압축(HN-FR-36)이 같은 식을 쓰도록 — 식을 두 곳에 두지 않기 위함 |
