# 인계: impl-02 — L1 sets 구현 (L0 + L1 완료까지)

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 `README.md` 의 **"구현 순서" 6번(`L1_inject/sets`)** 을 끝낸다. 기준 문서는 `docs/DECISIONS.md` 와 `L1_inject/sets/SPEC.md` **v0.3** 이다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (sets 테스트 5개 파일과 SPEC·go.mod 는 이미 커밋돼 있다).
2. sets 테스트는 **이미 써 있고 RED(구현 없음, 빌드 실패)** 다. `cd L1_inject/sets && go test ./...` 로 RED 를 먼저 확인한다.
3. 아래 "구현 설계" 대로 구현 파일을 쓰고 GREEN 을 만든다. 테스트가 기대하는 API 가 곧 설계다 — 테스트와 스펙이 어긋나면 **멈추고 사용자에게 묻는다**.
4. GREEN 뒤: `go vet`, `gofmt`, `go test -race -count=3`, `GOWORK=off go mod tidy && GOWORK=off go test`, 커버리지, **뮤테이션 확인**(아래), 그리고 사용자에게 보고. 커밋은 사용자 확인 후.
5. sets 가 끝나면 L0 + L1 이 끝난다. 다음은 `L2_harness` 스펙 작성(범위: `L2_harness/README.md`) — 사용자와 착수 여부를 정한다.

## 어디까지 왔나 (커밋)

| 커밋 | 내용 |
|---|---|
| `77684d4` | 폴더별 독립 Go 모듈 6개 + go.work (R4, R8) |
| `16034ae` | contract 구현 (스펙 v0.2→현재 v0.4) |
| `b13e09a` | core 구현 (스펙 v0.2) |
| `95a4dbe` | 22 step 기준 기능 재현 검증 (`docs/VERIFY-22step.md`), inject O1·O2 |
| `e8fe7e4` | inject 4a: StepSpec·생성기·ctx·봉인 신호·Dispatcher·상태·트레이스·Fanout |
| `62e9bd3` | inject 4b: 조합 함수 combine |
| `5f7faa4` | 원격(gRPC) 로더 삭제 — D12 정정 |
| `327b08f`, `0dee9ca` | adapters 스펙 v0.3 + 구현 (방언 3종, 프로필 23종, 4단 카탈로그) |
| `3f214a0` | pi 식 레이어 분리: 하네스 → L2_harness (R13), sets 스펙 v0.3 |
| (이 인계 커밋) | sets 테스트(RED), sets 스펙 해석 보완(ST-FR-40, 22 기준, 재시도 범위), 이 문서 |

모든 모듈(contract, core, inject, adapters)은 `-race` 통과, 단독(`GOWORK=off`) 테스트 통과, 뮤테이션 확인 완료 상태다.

## 먼저 읽을 것

1. `docs/DECISIONS.md` 전체 — 특히 D12(정정), D14·D15·D16(정정), R8~R13
2. `L1_inject/sets/SPEC.md` v0.3 전체 — 2.2 설정, 2.3 Deps(ST-FR-32~37), 3.1 base 표, 3.2 resilient, 3.6 추정, 3.7 압축, 7장 변경 기록
3. `L1_inject/sets/*_test.go` 5개 — 기대 API 의 원본
4. `L1_inject/inject/SPEC.md` 3.11 (IN-FR-44~53) 과 `inject/injecttest` — 테스트 도구 (`Facts`, `Dispatch`, `Fill`)
5. `L0_core/core/SPEC.md` 3.14 (CORE-FR-39~49) — step 입출력 타입, 신호 규칙

## 구현 설계 (테스트가 요구하는 것)

패키지 `sets` (모듈 `github.com/Biisairo/dynamic_react/L1_inject/sets`, 의존: contract·core·inject 만, ST-NFR-04). go.mod 에 require·replace 이미 있음.

**파일 구성 제안**: `deps.go`(interface·Registry·MemoryStore·StateStoreOf·Config), `estimate.go`, `compact.go`(ApplyEdits·Compact·요약 메시지), `schema.go`(CheckArgs), `base.go`, `resilient.go`.

**공개 API** (테스트에서 쓰는 이름 그대로):

- `ModelCaller{Call(ctx, req) iter.Seq2[StreamEvent,error]; Lookup(ctx, provider, id) (ModelInfo, bool)}` — `adapters.Client` 가 만족
- `Tool{Spec contract.ToolSpec; Run func(ctx, contract.ToolCall) contract.ToolResult; Truncate func(contract.ToolResult) contract.ToolResult}`
- `ToolRegistry{Get(name) (Tool,bool); Names() []string}`, `NewRegistry(tools...) *Registry`, `(*Registry).Add` (같은 이름 덮어씀, 등록 순서 유지)
- `Session{Messages, Edits, State map[string]inject.NamespaceState}`, `Store{Load; AppendMessages; AppendEdits; SaveState(ctx, sid, ns, inject.StateDelta)}`, `NewMemoryStore()` (Load 는 복사본), `StateStoreOf(Store) inject.StateStore` (nil → nil)
- `SummaryRequest{Model, Messages, Previous, MaxTokens}`, `Summarizer{Summarize(ctx, SummaryRequest) (string, error)}`
- `Deps{Model, Tools, Store, System []contract.Block, Summarizer, Sleep func(ctx, time.Duration) error, Now func() time.Time, NewID func() string}` — nil 은 기본값 (Tools 빈 레지스트리, Store 메모리, Sleep 타이머+ctx, Now time.Now, NewID 무작위)
- `Config{Model{Provider, ID}, MaxOutputTokens, Thinking, Tools []string, Compaction{Enabled, ReserveTokens, KeepRecentTokens}, Retry{MaxAttempts, BaseDelayMs, MaxDelayMs}}`, `ParseConfig(json.RawMessage)` — 모르는 키 거부, model 필수, 기본값 enabled=true·keepRecent 20000·3/2000/60000
- `Base(Deps) inject.FunctionSet` (Name "base"), `Resilient(Deps) inject.FunctionSet` (Name "resilient", 3·12·13·21 만)
- 22 개 개별 생성자 `BaseRunStart(d) inject.RunStartFn` … `BaseRunSettled(d)`, `ResilientContextMaintain/CallError/ResponseClassify/RunBeforeSettle(d)` (테스트가 쓰는 것: BaseRunStart, BaseContextProject, BaseRequestBuild, BaseRequestFinalize, BaseModelSelect, BaseToolBefore, BaseToolExecute, BaseToolAfter, BaseToolsCommit, BaseTurnFinish, ResilientResponseClassify, ResilientContextMaintain)
- `EstimateText(string) int`, `EstimateMessage(contract.Message) int`, `EstimateContext(proj, edits, system, tools) int` (도구 추정 = 이름 + 설명 + 스키마)
- `ApplyEdits(transcript, edits) []contract.Message` (없는 ID 의 편집은 건너뜀)
- `CheckArgs(schema, args json.RawMessage) error` (ST-FR-38 범위; schema 가 object 타입이 아니면 통과)
- `Compact(ctx, Summarizer, model ModelRef, proj []Message, keepRecent, reserve, maxOut int, now time.Time, editID, summaryID string) (contract.ContextEdit, bool, error)` — ok=false 는 압축할 구간 없음, err 는 요약 실패

**상태 키** (base 네임스페이스): 초기 입력 보관(한 번 준 뒤 삭제), `inbox.consumed`(int, **publish** — resilient 21 이 `Read("base", …)` 로 읽음), 저장한 편집 수. resilient: 재시도 `{turn, class 별 횟수}`(턴이 바뀌면 리셋), overflow 복구한 턴, 압축 pending·무효 카운트·`compaction.blocked`(publish, `true`).

**step 세부 (테스트 근거)**:
- 1: `Store.Load` → History. 이전 편집은 `ctx.Rewrite(edit)` 로 다시 올리고(허용 표 1 에 W 있음) 저장된 편집 수를 상태에 둠. 마지막 assistant 메시지의 짝 없는 호출만 aborted 결과로 History 끝에 붙이고 Store 에 기록 (ST-FR-40). Config 파싱 실패·Store 에러는 error 반환 → run failed.
- 2: 초기 입력이 있으면 그것(한 번), 아니면 입력함[consumed:]. 첫 턴(TurnIndex==1)에 아무것도 없으면 `Settle()`. 반환 메시지는 Store 에 기록.
- 5: `ApplyEdits(Transcript, Edits)` + Ephemeral, 새 편집(Edits[saved:])을 Store 에 기록.
- 6: System=Deps.System, Tools=설정 tools 필터(비면 레지스트리 전부, `Names()` 순서), Messages=Projection, MaxOutputTokens=설정 또는 `Request().Model().MaxOutputTokens`, Thinking=설정.
- 7: 짝 없는 tool call 마다 그 assistant 메시지 **바로 뒤**에 aborted 결과(새 ID·Now) 삽입. CacheHints = `[{system, len(System)-1}, {message, len(Messages)-1}]` (system 이 비면 system 힌트 없음).
- 8: `Lookup` 실패 시 `ModelInfo{Ref{provider, id}}`.
- 9: `CallStream{Events: d.Model.Call(ctx.Context(), req)}` — **ctx.Context() 를 dispatch 안에서 꺼내 넘긴다** (반환 뒤 ctx 는 폐기되어 메서드가 panic).
- 12(base): `Fail(&ce)` (`*contract.CallError` 는 error).
- 16: 없는 도구 → ShortCircuit `unknown tool: X`, CheckArgs 실패 → ShortCircuit error.
- 17: `Tool.Run` panic → recover 해 error 결과.
- 18: Tool.Truncate 또는 앞 2000줄 / 50KB + "truncated" 안내 문구.
- 19: 결과마다 tool_result 메시지(NewID, Now) → Store → 출력.
- 20: `Tools().Calls()` 가 비어 있지 않으면 ContinueTurn, 아니면 Settle.
- resilient 3: W=`Request().Model().ContextWindow`, reserve=설정 또는 max(16384, 15%W), used=EstimateContext(ApplyEdits(Transcript, Edits), Edits, System, tools), `used > W-reserve` 이고 blocked 아님·Summarizer 있음 → `[Rewrite(edit), Next()]` (테스트가 `[rewrite next]` 를 기대 — Next 를 명시). pending 이면 먼저 `LastUsage().Prompt() >= 임계` 로 무효 카운트, 2 연속이면 blocked.
- resilient 12: 재시도 Class 는 대기(`RetryAfter` 있으면 min(그것, maxDelay), 없으면 min(base·2^n, maxDelay)) 후 `Recall()`; Sleep 에러 → Fail. overflow: 이번 턴 미복구·Summarizer 있음·압축 성공 → `[Rewrite, Rebuild]`, 아니면 Fail.
- resilient 13: 테스트 `TestClassify` 의 네 경우 (`[rewrite rebuild]` 또는 신호 없음). 압축할 구간이 없으면 next.
- resilient 21: 입력함 길이 > `Read("base","inbox.consumed")` → ContinueTurn.
- 요약 메시지: user 역할, 고정 머리말 + 요약 텍스트. 구간 첫 메시지가 그 머리말로 시작하면 `Previous` 로 넘기고 요약 대상에서 뺀다.

## 이 세션이 값을 치르고 배운 것

- **사용자 원칙 "루프는 루프로만, 전부 주입, 기본값 제공"** 에 더해, 이번에 확인된 것: **하네스(세션 파일·도구 구현·프롬프트·요약 프롬프트·CLI)는 L1 이 아니다** (R13). L1 에 넣고 싶어지는 I/O 기능은 interface 로 빼라.
- **서드파티·격리 RPC 는 목표가 아니다** (D12 정정). "나중을 위한" 계층을 스펙에 있다고 그대로 만들지 말고, 비용이 크면 목적부터 사용자에게 확인하라. 이번에 gRPC 로더를 설계하다 사용자가 "뭘 위한거야?" 로 멈췄다.
- **스펙에 없는 Go 표현은 구현 전에 스펙에 먼저 적는다** (각 스펙의 "Go 표현" 절, 변경 기록). 해석이 필요한 모호점은 해석을 적고 보고한다.
- **TDD 를 지켜라.** inject 4a 에서 런타임을 테스트보다 먼저 써 규약을 어겼고, 뮤테이션으로 보완했다. sets 는 테스트가 이미 있으니 그대로 따르면 된다.
- **뮤테이션 확인이 테스트 결함을 잡는다.** 매 단계 4~7건: 핵심 조건을 망가뜨려 테스트가 실패하는지 본다. 도구: `/tmp/mut.py <file> <old> <new> <label>` 은 이 세션의 임시 파일이라 없다 — 같은 방식(파일 백업 → 치환 → `go test` → 복원)으로 다시 만들어 쓴다. sed 는 `&&` 같은 패턴에서 조용히 실패한다(이번에 한 번 겪음) — python 치환을 써라.
- **테스트의 시각**: 편집·응답 Timestamp 가 모두 같으면 ST-FR-22 의 "편집보다 뒤" 판정이 항상 거짓이다. 무효 압축 테스트는 흐르는 시계를 쓴다(이미 반영).
- **inject ctx 는 dispatch 가 끝나면 폐기된다.** 9 에서 iterator 는 dispatch 뒤에 소비되므로 `ctx.Context()` 를 미리 꺼내 둘 것.
- **adapters 스냅샷**: `snapshot.json` 은 oh-my-pi `models.json` 에서 추린 데이터만 담는다. 라이선스 고지는 **사용자가 나중에 직접 처리**하기로 했다 — 건드리지 말 것.
- API 키는 환경변수로만, 구독 요금제 엔드포인트·OAuth 경로 금지.

## 남은 미결

| 위치 | 내용 |
|---|---|
| L2_harness | 스펙 미작성. 기본 시스템 프롬프트 문안(옛 sets O1), sessionDir=`~/.dynamic_react/sessions`(확정, `DR_SESSION_DIR`·설정으로 변경), 도구 4종, JSONL, 요약 프롬프트, dr-print |
| inject O3 | dispatch 밖 panic 정책 — L3_ui 에서 재검토 |
| adapters | vLLM `max_model_len` 필드 문서 확인 못 함, Xiaomi base URL 은 pi 값 |

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148 구조. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다 (AskUserQuestion).
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI (사용자 CLAUDE.md). Go 는 gopls.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈 의존은 go.mod 에 둘 다 적는다.
- Go 1.23 호환 유지 (`t.Context`, `b.Loop` 같은 1.24+ API 금지. 로컬 toolchain 은 1.25.4).

## 사용자 결정 중 유효한 것

- D1~D16 (D12·D14·D15·D16 은 impl-01 에서 정정된 내용), R1~R13 (R10 폐기).
- 각 스펙의 변경 기록에 "사용자 확정" 으로 적힌 항목 전부.
