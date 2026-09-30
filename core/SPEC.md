# core — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (D1~D13, R1~R6)
- 의존 명세: `contract/SPEC.md`
- 사용하는 쪽: `inject/SPEC.md`

---

## 1. 소개

### 1.1 목적
에이전트의 ReAct 루프 본체를 정의한다. core 는 **정해진 step 을 정해진 순서로 진행하고, 각 step 의 실제 동작을 Dispatcher 에게 맡기고, Dispatcher 가 돌려준 신호대로 다음 step 으로 옮기는 것**만 한다 (D1).

### 1.2 범위
- 포함: step 목록, 신호 목록, 전이 표, step 별 허용 신호, step 별 입출력 타입, 스트림 소비와 도구 실행의 동시성 구조, 선행 실행 핸들, 입력함(inbox), 편집 로그, 신호 기록, 취소, 전이 수 상한, 관찰자 이벤트.
- 제외 (전부 주입): 프롬프트, 압축, 재시도, 모델 선택, provider 호출, 저장, 권한, 토큰 추정, 도구 구현. step 별 ctx 와 조회 약속어는 inject 가 만든다.

### 1.3 용어

| 용어 | 뜻 |
|---|---|
| step | 루프의 한 단계. 22개로 고정 (3.1) |
| dispatch | core 가 step 하나의 실행을 Dispatcher 에게 요청하는 한 번의 호출 |
| 신호 (Signal) | Dispatcher 가 돌려주는 제어 약속어. 전이 신호와 효과 신호로 나뉜다 (R1) |
| 전이 신호 | 다음 step 을 정한다 |
| 효과 신호 | 상태에 작용하고 step 은 옮기지 않는다 |
| 단위 (unit) | step 이 반복되는 범위: 실행(run) / 턴 / 호출(call) / 이벤트 / 도구 |
| run | 입력 하나를 받아 `run.settled` 까지 가는 한 번의 실행 |
| 선행 실행 핸들 | 스트림이 끝나기 전에 시작된 도구 실행 (D8) |

### 1.4 참조
- `hermes_study/analysis/07-injectable-loop-design.md` 11장
- `hermes_study/analysis/02-pi-coding-agent.md` 5장 (이중 루프 구조)
- `hermes_study/analysis/03-oh-my-pi.md` 3·4장 (스트림 중 처리, 도구 스케줄링)

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 caller (surface / test)
     │ Start(input) / Input(msg) / cancel
     ▼
 core.Run  ── dispatch(step, input, view) ──►  Dispatcher (inject)
     │     ◄── (output, signals) ────────────
     │
     └── Observer (optional)                  lifecycle events
```

- core 는 contract 와 Go 표준 라이브러리만 import 한다.
- core 는 I/O 를 하지 않는다. 파일·네트워크·시간 지연은 모두 Dispatcher 안에서 일어난다.

### 2.2 기능 요약
1. step 을 고정 순서와 전이 표로 진행한다.
2. step 마다 Dispatcher 를 한 번 부른다 (반복 단위 step 은 단위마다 한 번).
3. 신호 배열을 검증하고 효과를 적용한 뒤 전이한다.
4. `model.call` 이 돌려준 스트림을 소비하며 이벤트마다 `stream.event` 를 dispatch 한다.
5. `tools.plan` 이 돌려준 계획대로 도구 step 들을 실행한다 (그룹 단위, 그룹 안은 병렬 가능).
6. 스트림 도중 요청된 도구 실행(선행 실행)을 관리한다.
7. 실행 상태를 읽기 전용 view 로 Dispatcher 에게 제공한다.
8. 취소·실패·상한 도달 시에도 `run.settled` 를 정확히 한 번 dispatch 한다.

### 2.3 제약
- CR-C1. 정책 금지 (D1): core 코드에 "몇 번 재시도", "몇 % 에서 압축", "어느 모델" 같은 판단을 넣지 않는다. 유일한 예외는 R6 의 전이 수 상한(안전장치)이다.
- CR-C2. 전역 상태 금지 (D10): 패키지 수준 가변 변수 없음. 한 프로세스에서 여러 Kernel·Run 이 동시에 독립적으로 돌 수 있다.
- CR-C3. 의존성: Go 1.23 이상 표준 라이브러리 + contract 만 (R5).
- CR-C4. step 목록, 신호 목록, 전이 표, 허용 표는 **코드 상수**다. 런타임에 바꿀 수 없다 (D13).

### 2.4 가정과 의존
- A1. Dispatcher 는 22개 step 모두를 처리할 수 있다 (바인딩 누락은 inject 가 생성 시점에 거부).
- A2. Dispatcher 는 동시 호출에 안전하다 (도구 그룹 병렬, 선행 실행 때문에).

---

## 3. 상세 요구사항

### 3.1 step 목록 (D13)

| # | step | 단위 | 입력 (core 가 넘김) | 출력 (Dispatcher 가 돌려줌) |
|---|---|---|---|---|
| 1 | `run.start` | run | `RunInput` | `RunInit{History []Message}` |
| 2 | `turn.start` | 턴 | — | `[]Message` 이번 턴에 대화에 추가할 메시지 (비어도 됨) |
| 3 | `context.maintain` | 턴 | — | 없음 (효과 신호로 작용) |
| 4 | `context.inject` | 턴 | — | `[]Message` 이번 턴에만 붙는 임시 메시지 |
| 5 | `context.project` | 턴 | — | `[]Message` 모델이 볼 투영 |
| 6 | `request.build` | 호출 | — | `contract.Request` |
| 7 | `request.finalize` | 호출 | `contract.Request` | `contract.Request` |
| 8 | `model.select` | 호출 | `contract.Request` | `contract.ModelInfo` |
| 9 | `model.call` | 호출 | `contract.Request` (Model 은 8 의 결과로 채움) | `CallStream{Events iter.Seq2[StreamEvent, error], Live LiveInput?}` |
| 10 | `stream.event` | 이벤트 | `contract.StreamEvent` | 없음 |
| 11 | `stream.end` | 호출 | `contract.Message` (done 이벤트의 확정 응답) | `StreamEndOut{Response Message, Discard []CallID}` |
| 12 | `call.error` | 호출 | `contract.CallError` | 없음 |
| 13 | `response.classify` | 턴 | — | 없음 |
| 14 | `response.commit` | 턴 | — | 없음 |
| 15 | `tools.plan` | 턴 | `[]ToolCall` (응답의 도구 호출) | `Plan{Groups []Group}`, `Group{Calls []ToolCall, MaxParallel int}` |
| 16 | `tool.before` | 도구 | `ToolCall` | `BeforeOut{Call ToolCall, ShortCircuit *ToolResult}` |
| 17 | `tool.execute` | 도구 | `ToolCall` | `ToolResult` |
| 18 | `tool.after` | 도구 | `ToolCall`, `ToolResult` | `ToolResult` |
| 19 | `tools.commit` | 턴 | `[]ToolCall`, `[]ToolResult` | 없음 |
| 20 | `turn.finish` | 턴 | — | 없음 |
| 21 | `run.beforeSettle` | run | — | 없음 |
| 22 | `run.settled` | run | `Settle{Reason, Err}` | 없음 (신호 무시) |

| id | 요구사항 |
|---|---|
| CORE-FR-01 | core 는 위 22개 step 을 `Step` 닫힌 열거형으로 정의해야 한다. 이름 문자열은 위 표와 같아야 한다 |
| CORE-FR-02 | core 는 각 step 의 dispatch 에 위 표의 입력을 넘기고, 출력의 **타입**이 표와 다르면 그 dispatch 를 실패(`fail`)로 처리해야 한다 |
| CORE-FR-03 | core 는 출력의 **내용**을 해석하지 않는다. 예외는 루프 진행에 필요한 구조뿐이다: 9 의 스트림, 11 의 Response 와 Discard, 15 의 Plan, 16 의 ShortCircuit, 17·18 의 ToolResult |

### 3.2 신호 목록 (R1)

**전이 신호**

| 신호 | 약어 | 다음 step | 뜻 |
|---|---|---|---|
| `next` | N | "다음 step" 표 (3.3) | 기본 진행 |
| `recall` | RC | 9 `model.call` | 같은 요청을 다시 호출 |
| `reselect` | RS | 8 `model.select` | 모델·provider 를 다시 고름 |
| `rebuild` | RB | 5 `context.project` | 투영부터 다시 (편집 반영) |
| `restart_turn` | RT | 2 `turn.start` | 이번 응답을 버리고 턴을 다시 |
| `continue_turn` | CT | 2 `turn.start` | 응답을 유지한 채 다음 턴 |
| `skip_tools` | ST | 19 `tools.commit` | 도구를 실행하지 않고 주어진 결과 사용 (payload: `[]ToolResult`) |
| `settle` | SE | 21 `run.beforeSettle` | 종료 절차로 |
| `stop` | SP | 22 `run.settled` | 즉시 정상 종료 |
| `fail` | F | 22 `run.settled` | 에러 종료 (payload: error) |

**효과 신호**

| 신호 | 약어 | 작용 |
|---|---|---|
| `rewrite` | W | `ContextEdit` 을 run 의 편집 로그에 추가 (payload: `contract.ContextEdit`) |
| `inject_input` | II | 메시지를 모델에 넣음 — 스트림 중이면 live, 아니면 입력함 (payload: `contract.Message`) |
| `start_tool` | SO | 도구 선행 실행 시작 (payload: `contract.ToolCall`) |

| id | 요구사항 |
|---|---|
| CORE-FR-04 | core 는 위 13개 신호를 `SignalKind` 닫힌 열거형으로 정의해야 한다 |
| CORE-FR-05 | 신호 배열은 **효과 신호 0개 이상 + 전이 신호 최대 1개, 전이는 마지막**이어야 한다. 어기면 그 dispatch 를 `fail` 로 처리한다 |
| CORE-FR-06 | 전이 신호가 없으면 `next` 로 본다. 단 `next` 가 허용되지 않는 step(3.4)에서 전이가 없으면 `fail` 로 처리한다 (R2) |
| CORE-FR-07 | core 는 효과 신호를 배열 순서대로 적용한 뒤 전이한다 |

### 3.3 "다음 step" 표 (`next` 의 목적지)

```text
 1 run.start          -> 2
 2 turn.start         -> 3
 3 context.maintain   -> 4
 4 context.inject     -> 5
 5 context.project    -> 6
 6 request.build      -> 7
 7 request.finalize   -> 8
 8 model.select       -> 9
 9 model.call         -> consume stream: 10 per event, then 11 on done, 12 on error
10 stream.event       -> (next event) or 11 on done / 12 on error
11 stream.end         -> 13
12 call.error         -> (no next)
13 response.classify  -> 14
14 response.commit    -> 15
15 tools.plan         -> for each planned call: 16 -> 17 -> 18, then 19 (empty plan: 19)
16 tool.before        -> 17 (or 18 if ShortCircuit)
17 tool.execute       -> 18
18 tool.after         -> (next call) or 19
19 tools.commit       -> 20
20 turn.finish        -> (no next)
21 run.beforeSettle   -> 22
22 run.settled        -> end of run
```

| id | 요구사항 |
|---|---|
| CORE-FR-08 | core 는 위 표를 코드 상수로 가져야 한다 |

### 3.4 step 별 허용 신호

| # | step | N | RC | RS | RB | RT | CT | ST | SE | SP | F | W | II | SO |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | run.start | ● | | | | | | | | ● | ● | ● | | |
| 2 | turn.start | ● | | | | | | | ● | ● | ● | | | |
| 3 | context.maintain | ● | | | | | | | | ● | ● | ● | | |
| 4 | context.inject | ● | | | | | | | | | ● | | | |
| 5 | context.project | ● | | | | | | | | | ● | | | |
| 6 | request.build | ● | | | | | | | | | ● | | | |
| 7 | request.finalize | ● | | | | | | | | | ● | | | |
| 8 | model.select | ● | | | ● | | | | | ● | ● | | | |
| 9 | model.call | ● | | | | | | | | | ● | | | |
| 10 | stream.event | ● | ● | ● | | ● | | | | ● | ● | | ● | ● |
| 11 | stream.end | ● | ● | | | ● | | | | | ● | | | |
| 12 | call.error | | ● | ● | ● | ● | | | | ● | ● | ● | | |
| 13 | response.classify | ● | ● | ● | ● | ● | | | | ● | ● | ● | | |
| 14 | response.commit | ● | | | | | | | | | ● | | | |
| 15 | tools.plan | ● | | | | | | ● | | | ● | | | |
| 16 | tool.before | ● | | | | | | | | | ● | | | |
| 17 | tool.execute | ● | | | | | | | | | ● | | | |
| 18 | tool.after | ● | | | | | | | | | ● | | | |
| 19 | tools.commit | ● | | | | | | | | | ● | | | |
| 20 | turn.finish | | | | | | ● | | ● | ● | ● | ● | ● | |
| 21 | run.beforeSettle | ● | | | | | ● | | | ● | ● | | ● | |
| 22 | run.settled | | | | | | | | | | | | | |

허용 표의 근거:
- 응답을 저장(14)한 뒤에는 `restart_turn` 불가 — 이미 대화에 들어간 응답을 버릴 수 없다.
- 13 에서 `continue_turn`·`settle` 불가 — 응답 저장(14)을 건너뛰게 된다. 멈춤 nudge 는 20 에서 `[inject_input(nudge), continue_turn]` 으로 한다.
- 도구 단위 step(16~18)은 `next`/`fail` 만 — 도구 하나가 턴 흐름을 바꾸지 못한다. 도구 차단은 16 의 `ShortCircuit` 출력으로 한다.
- 12·20 은 `next` 불가 (R2).

| id | 요구사항 |
|---|---|
| CORE-FR-09 | core 는 위 허용 표를 코드 상수로 가져야 하며, 허용되지 않은 신호가 오면 그 dispatch 를 `fail` 로 처리해야 한다 |
| CORE-FR-10 | 허용 표는 inject 가 step 별 ctx 타입을 생성하는 **단일 진실 공급원**이다. core 는 이 표를 공개 상수로 export 해야 한다 |

### 3.5 run 생명주기와 API

```go
k := core.New(dispatcher, core.Options{MaxDispatches: 10000, SettleTimeout: 5 * time.Second, Observer: obs})
run := k.Start(ctx, core.RunInput{SessionID: "s1", Input: msgs, Config: rawJSON})
run.Input(msg)            // thread-safe, appends to inbox
res := run.Wait()         // res.Reason: completed | stopped | failed | aborted | guard
```

| id | 요구사항 |
|---|---|
| CORE-FR-11 | `Kernel` 은 Dispatcher 와 옵션만 가진 값이고, `Start` 를 여러 번·동시에 호출해도 run 들이 서로 상태를 공유하지 않아야 한다 (CR-C2) |
| CORE-FR-12 | `Run.Input(msg)` 는 어느 goroutine 에서든 호출할 수 있고, 메시지를 run 의 입력함(inbox)에 추가만 한다. 입력함을 언제 어떻게 쓸지는 Dispatcher 가 정한다 (조회로 읽고 `turn.start` 출력이나 `inject_input` 으로 반영) |
| CORE-FR-13 | 모든 run 은 어떤 경로로 끝나든 `run.settled` 를 **정확히 한 번** dispatch 해야 한다. 이 dispatch 의 신호는 무시한다 |
| CORE-FR-14 | `Settle.Reason` 은 닫힌 종류 `completed`(21 에서 next) / `stopped`(stop) / `failed`(fail 또는 처리 불가 에러) / `aborted`(취소) / `guard`(상한 도달) 중 하나다 |

### 3.6 모델 호출과 스트림 (D8)

| id | 요구사항 |
|---|---|
| CORE-FR-15 | core 는 9 의 출력 `Events` 를 끝까지 순서대로 소비하며, 이벤트마다 10 을 dispatch 한다. **이전 이벤트의 10 dispatch 가 끝나야 다음 이벤트를 꺼낸다** (이벤트 순서 보장) |
| CORE-FR-16 | 10 이 `next` 외의 전이 신호를 내면 core 는 스트림 소비를 멈추고, 9 에 넘긴 context 를 취소하고, 전이를 적용한다 |
| CORE-FR-17 | 스트림이 `done` 으로 끝나면 core 는 done 의 Message 를 입력으로 11 을 dispatch 한다. `error` 이벤트로 끝나거나, 9 가 Go error 를 돌려주거나, 스트림 iterator 가 error 를 내면 core 는 `CallError` 를 입력으로 12 를 dispatch 한다 (Go error 는 `Class: unknown` 으로 감싼다) |
| CORE-FR-18 | `inject_input` 효과를 10 에서 받으면: 9 의 출력에 `Live` 가 있고 `Live.Send(msg)` 가 성공하면 live 전달, 아니면 입력함에 추가한다. 결과(`delivered_live` / `deferred`)를 신호 기록에 남긴다 |
| CORE-FR-19 | `inject_input` 효과를 20·21 에서 받으면 입력함에 추가한다 |

### 3.7 도구 실행

| id | 요구사항 |
|---|---|
| CORE-FR-20 | core 는 15 의 `Plan.Groups` 를 **순서대로** 실행한다. 그룹 안의 호출은 최대 `MaxParallel`(0 이하면 1) 개까지 동시에 실행한다 |
| CORE-FR-21 | 호출 하나는 16 → 17 → 18 순서로 dispatch 한다. 16 의 `ShortCircuit` 이 있으면 17 을 건너뛰고 그 결과로 18 을 dispatch 한다. 16 이 돌려준 `Call`(인자 수정 가능)을 17·18 에 넘긴다 |
| CORE-FR-22 | Plan 에 응답에 없는 CallID 가 있으면 15 를 `fail` 로 처리한다. **응답의 호출이 Plan 에서 빠진 것은 core 가 막지 않는다** — 짝 검증은 주입 함수의 몫이다 (D9) |
| CORE-FR-23 | 도구 step 중 하나가 `fail` 이면 core 는 실행 중인 나머지 도구의 context 를 취소하고 run 을 `failed` 로 끝낸다 |
| CORE-FR-24 | 결과는 Plan 의 순서대로 모아 19 에 넘긴다. 15 가 `skip_tools(results)` 를 내면 도구 step 없이 그 결과로 19 를 dispatch 한다 |

### 3.8 선행 실행 (D8)

| id | 요구사항 |
|---|---|
| CORE-FR-25 | 10 에서 `start_tool(call)` 효과를 받으면 core 는 그 호출의 16 → 17 을 별도 goroutine 에서 시작하고, `(CallID, ArgsDigest)` 를 키로 핸들을 보관한다 |
| CORE-FR-26 | 도구 실행 단계에서 호출 X 에 대해 **같은 CallID, 같은 ArgsDigest 의 핸들이 있고 11 의 `Discard` 에 X 가 없으면** core 는 16·17 을 다시 하지 않고 핸들의 결과를 기다려 18 을 dispatch 한다. 그 외에는 핸들을 취소하고 정상 순서로 실행한다 |
| CORE-FR-27 | `recall`, `reselect`, `rebuild`, `restart_turn`, `stop`, `fail` 전이나 취소가 일어나면 core 는 그 호출의 모든 선행 실행 핸들을 취소한다 |

인자가 달라진 선행 결과를 재사용하지 않는 것(CORE-FR-26)은 정책이 아니라 정합성 규칙이다. 재사용할지 말지의 판단은 11 의 `Discard` 로 주입 함수가 한다.

### 3.9 상태 view

| id | 요구사항 |
|---|---|
| CORE-FR-28 | core 는 dispatch 마다 읽기 전용 `View` 를 넘긴다. View 는 dispatch 시작 시점의 스냅샷이며, 같은 dispatch 안에서는 바뀌지 않는다 |
| CORE-FR-29 | View 가 제공하는 사실 (core 가 가진 것만, 가공 없이): RunID, SessionID, Config(raw), 현재 Step, dispatch 순번(Seq), 턴 번호, 호출 번호(턴 안), History(1 의 출력), 이번 턴 추가분(2), 임시 메시지(4), 투영(5), 요청(7), 선택된 모델(8), 스트림 초안(지금까지의 이벤트로 조립한 부분 응답), 현재 이벤트, 응답(11), CallError(12), Plan(15), 이번 턴의 도구 호출·결과(지금까지), 편집 로그, 입력함, 신호 기록, 선행 실행 핸들 상태, 경과 시간 |
| CORE-FR-30 | 신호 기록은 `{Seq, Step, Unit, Signals, Origin(설명 문자열), Result(효과 적용 결과)}` 의 목록이고, View 에서 **종류별·사유별 횟수**를 셀 수 있어야 한다 (조합 함수가 "같은 사유 재시도 N회" 를 구현하는 데 필요) |
| CORE-FR-31 | 편집 로그는 run 동안의 `rewrite` 효과를 순서대로 보관한다. 투영(5)에 편집을 반영하는 것은 주입 함수의 몫이다 |

### 3.10 취소와 에러

| id | 요구사항 |
|---|---|
| CORE-FR-32 | `Start` 에 넘긴 context 가 취소되면 core 는 새 dispatch 를 시작하지 않고, 진행 중인 dispatch·도구·선행 실행의 context 를 취소하고, `Reason: aborted` 로 22 를 dispatch 한다 |
| CORE-FR-33 | 22 의 dispatch 는 취소되지 않은 별도 context(`SettleTimeout` 기한)로 한다 |
| CORE-FR-34 | 9 외의 step 에서 Dispatcher 가 Go error 를 돌려주면 `fail(error)` 과 같게 처리한다 |
| CORE-FR-35 | Dispatcher 가 panic 하면 core 는 recover 해 `fail` 로 처리한다 (run 하나의 문제로 프로세스가 죽지 않게) |

### 3.11 안전장치 (R6)

| id | 요구사항 |
|---|---|
| CORE-FR-36 | core 는 run 당 dispatch 수(10·16·17·18 제외)가 `MaxDispatches`(기본 10,000)를 넘으면 `Reason: guard` 로 끝낸다 |

### 3.12 관찰자

| id | 요구사항 |
|---|---|
| CORE-FR-37 | `Options.Observer` 가 있으면 core 는 `RunStarted`, `StepStarted{Step, Seq, Unit}`, `StepEnded{Step, Seq, Signals, Err, Duration}`, `StreamEvent`, `RunSettled` 를 순서대로 알린다 |
| CORE-FR-38 | Observer 호출은 동기이며 흐름에 영향을 주지 않는다. Observer 가 오래 걸려도 core 는 기다린다 — 버퍼링은 Observer 구현(inject)의 책임이다 |

### 3.13 비기능

| id | 요구사항 |
|---|---|
| CORE-NFR-01 | core 패키지는 contract 와 표준 라이브러리 외 import 가 없어야 한다 (CI 검사) |
| CORE-NFR-02 | 같은 Dispatcher 동작(스크립트된 가짜)이면 dispatch 순서가 항상 같아야 한다. 단 그룹 안 병렬 도구의 **완료 순서**는 예외이며, 결과 순서는 CORE-FR-24 로 고정된다 |
| CORE-NFR-03 | core 는 `-race` 테스트를 통과해야 한다 |
| CORE-NFR-04 | 공개 API 는 3.5 의 `New`, `Start`, `Input`, `Wait` 와 상수·타입뿐이다. 내부 구현은 `internal/` 에 둔다 |

---

## 4. 검증

모든 검증은 **스크립트된 가짜 Dispatcher**(step·순번별로 정해진 출력·신호를 돌려주고 호출 기록을 남김)로 한다. 실제 모델·네트워크를 쓰지 않는다.

| 요구사항 | 방법 |
|---|---|
| CORE-FR-01, 04, 08, 09, 10 | 단위 테스트: 상수 표가 3.1~3.4 와 일치 (표 대조 테스트) |
| CORE-FR-02, 03 | 잘못된 출력 타입 → fail |
| CORE-FR-05~07 | 속성 테스트: 무작위 신호 배열 → 규칙 위반은 모두 fail, 적법한 배열은 효과 순서대로 적용 |
| CORE-FR-06 | 12·20 에서 빈 신호 → fail |
| CORE-FR-11, CR-C2 | 동시 run 100개, 서로 다른 스크립트 → 각자 기대 순서 |
| CORE-FR-12 | 다른 goroutine 에서 Input → 다음 dispatch 의 View 입력함에 보임 |
| CORE-FR-13, 14 | 모든 종료 경로(각 전이·취소·panic·상한)에서 22 가 정확히 1회 |
| CORE-FR-15~17 | 이벤트 N개 스트림 → 10 이 N회 순서대로, done → 11, error → 12, Go error → 12(unknown) |
| CORE-FR-16 | 3번째 이벤트에서 restart_turn → 4번째 이벤트를 꺼내지 않음, 9 의 context 취소됨 |
| CORE-FR-18, 19 | Live 있음/없음/Send 실패 각각의 전달 결과 기록 |
| CORE-FR-20~24 | 그룹 2개(병렬 3, 순차 1), ShortCircuit, 모르는 CallID, 도구 fail 시 나머지 취소, skip_tools |
| CORE-FR-25~27 | 선행 실행: 재사용 / Discard / 인자 변경 / 전이 시 취소 |
| CORE-FR-28~31 | View 스냅샷 불변(같은 dispatch 중 입력 추가해도 안 바뀜), 신호 기록 집계 |
| CORE-FR-32, 33 | 스트림·도구 도중 취소 → aborted, 22 는 취소 안 된 context |
| CORE-FR-34, 35 | Go error, panic → failed |
| CORE-FR-36 | 무한 continue_turn 스크립트 → guard |
| CORE-FR-37, 38 | Observer 이벤트 순서 |
| CORE-NFR-01 | `go list -deps` 검사 |
| CORE-NFR-03 | `go test -race` |

---

## 5. 추적성

| 결정 | 요구사항 |
|---|---|
| D1 | CR-C1, CORE-FR-03, 22, 31 |
| D2 | 3.1 전체, CORE-FR-02 |
| D3, R1 | CORE-FR-04~07, 09 |
| D4 | CORE-FR-10, 28 (ctx 생성은 inject) |
| D5 | CORE-FR-05, 07 |
| D8 | CORE-FR-15~19, 25~27 |
| D9 | CORE-FR-22 |
| D10 | CR-C2, CORE-FR-11 |
| D13 | CR-C4, CORE-FR-01, 08 |
| R2 | CORE-FR-06, 3.4 표 |
| R6 | CORE-FR-36 |
