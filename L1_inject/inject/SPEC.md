# inject — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D2~D6, D11, D12)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`

---

## 1. 소개

### 1.1 목적
core 위의 1차 레이어다. **주입 함수를 인터페이스로 정의하고, 그것을 받아 core 의 Dispatcher 로 동작하게 만든다.** 주입 함수가 흐름을 제어하는 유일한 수단인 step 별 ctx(조회 약속어 + 신호 생성자)를 만들고, 수명과 범위를 강제한다 (D2~D5). 서드파티 함수 세트를 별도 프로세스로 받는 경계도 이 레이어가 가진다 (D12).

### 1.2 범위
- 포함: StepSpec(단일 진실 공급원)과 코드 생성, step 별 함수 타입·ctx 타입·봉인된 신호 타입, 조회 약속어, FunctionSet 과 바인딩, Dispatcher 구현, 함수 세트 상태(네임스페이스), 조합 함수, 로더(같은 프로세스 / 별도 프로세스 gRPC), 트레이스, 관찰자 분배.
- 제외: 기본 함수 구현 (sets), 방언 변환 (adapters), UI (surfaces). **inject 는 어떤 step 의 기본 동작도 갖지 않는다** (D6).

### 1.3 용어

| 용어 | 뜻 |
|---|---|
| 주입 함수 | step 하나를 수행하는 함수. step 마다 시그니처가 다르다 |
| 함수 세트 (FunctionSet) | 이름을 가진 주입 함수 묶음. 22개 step 중 일부 또는 전부를 채운다 |
| 바인딩 | 여러 함수 세트를 겹쳐 22개 step 을 하나씩 확정하는 것 |
| ctx | dispatch 한 번을 위해 만들어지는 객체. 그 step 에 허용된 조회와 신호 생성자만 가진다 |
| 조회 약속어 | ctx 의 읽기 메서드. 닫힌 목록 |
| 신호 생성자 | ctx 의 신호 생성 메서드. 그 step 에 허용된 신호만 |
| invocation 토큰 | dispatch 한 번을 식별하는 값. 신호와 원격 요청에 묶인다 |
| 원격 세트 | 별도 프로세스에서 gRPC 로 동작하는 함수 세트 |

### 1.4 참조
- `hermes_study/analysis/07-injectable-loop-design.md` 11장
- 대화 결정: "ctx 는 step 마다 만들어 주입", "함수 안에서만 사용 가능", "Go 는 sealed interface 로 컴파일 시점 강제"

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 function sets (sets layer / user / third-party)
   in-process: Go package  ────────┐
   out-of-process: plugin binary ──┤ loader
                                   ▼
 inject
   Bind(sets...) -> Dispatcher
     per dispatch:
       make ctx(step, view, token)  ->  call bound fn  ->  revoke ctx
       validate signals  ->  commit state delta  ->  trace
                                   │
                                   ▼ implements core.Dispatcher
 core
```

### 2.2 강제의 세 층

| 층 | 수단 | 막는 것 |
|---|---|---|
| 컴파일 시점 | step 별 ctx 타입, step 별 **봉인된(sealed) 신호 interface**(비공개 메서드), `internal/` 패키지 | 허용되지 않은 신호·조회를 쓰는 코드는 빌드 실패. 신호 값을 직접 만드는 코드는 빌드 실패 |
| 실행 시점 | invocation 토큰 검사, 함수 반환 후 ctx 폐기 | 다른 dispatch 의 신호 재사용, ctx 를 저장해 두고 나중에 쓰기 |
| 프로세스 | 원격 세트는 별도 프로세스, 조회는 요청·응답으로만 | 서드파티가 루프 내부에 접근 |

### 2.3 제약
- IN-C1. inject 는 contract, core, Go 표준 라이브러리, gRPC·protobuf 런타임만 import 한다.
- IN-C2. step 별 타입(ctx, 신호, 함수, FunctionSet 필드)은 **StepSpec 에서 생성**한다. 손으로 쓴 사본을 두지 않는다.
- IN-C3. inject 에 step 의 기본 동작을 두지 않는다 (D6).
- IN-C4. 조회·신호·ctx 가 다루는 모든 값은 직렬화 가능해야 한다 (D12, contract CT-C3).

### 2.4 가정
- A1. 원격 세트는 inject 가 제공하는 plugin SDK(Go)로 만들거나, 공개된 proto 를 구현한다.

---

## 3. 상세 요구사항

### 3.1 StepSpec — 단일 진실 공급원

| id | 요구사항 |
|---|---|
| IN-FR-01 | inject 는 StepSpec 표를 하나의 소스(Go 또는 YAML)로 가진다. 행 = step, 열 = 입력 타입, 출력 타입, 허용 신호, 허용 조회 그룹 |
| IN-FR-02 | StepSpec 의 step 목록·허용 신호는 core 가 export 한 상수(core CORE-FR-10)와 같아야 한다. 다르면 코드 생성이 실패해야 한다 |
| IN-FR-03 | `go generate` 가 StepSpec 에서 다음을 생성한다: step 별 ctx 타입, step 별 봉인된 신호 interface 와 구현, step 별 함수 타입, `FunctionSet` 구조체, Dispatcher 의 step 분기 코드, 원격용 protobuf 메시지 |
| IN-FR-04 | CI 는 생성 코드가 StepSpec 과 동기화돼 있는지 검사한다 (생성 후 diff 가 없어야 통과) |

### 3.2 주입 함수 시그니처

step 성격에 따라 세 모양이다. 이름은 StepSpec 에서 생성된다 (예시).

```go
// 1) 판정 step: 출력 없음, 신호만
type TurnFinishFn func(ctx *TurnFinishCtx) ([]TurnFinishSignal, error)

// 2) 데이터 step: 출력 + 신호
type RequestBuildFn func(ctx *RequestBuildCtx) (contract.Request, []RequestBuildSignal, error)

// 3) 입력이 있는 step: core 가 넘기는 입력 + 출력 + 신호
type ToolAfterFn func(ctx *ToolAfterCtx, call contract.ToolCall, res contract.ToolResult) (contract.ToolResult, []ToolAfterSignal, error)
```

| id | 요구사항 |
|---|---|
| IN-FR-05 | 22개 step 모두에 대해 L0_core/core/SPEC.md 3.1 의 입력·출력을 그대로 따르는 함수 타입을 생성한다 |
| IN-FR-06 | 신호 반환값은 **그 step 전용 봉인 interface 의 슬라이스**다. 다른 step 의 신호 타입은 대입할 수 없다 (컴파일 에러) |
| IN-FR-07 | `run.settled` 함수는 신호를 반환하지 않는다: `func(ctx *RunSettledCtx, s core.Settle) error` |

### 3.3 ctx

```go
func turnFinish(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
    u := ctx.Output().LastUsage()                  // 조회
    w := ctx.Request().Model().ContextWindow       // 조회
    if u.Prompt() > w-max(16384, w*15/100) {
        return []inject.TurnFinishSignal{ctx.Rewrite(edit), ctx.ContinueTurn()}, nil   // 신호
    }
    return []inject.TurnFinishSignal{ctx.Settle()}, nil
}
```

| id | 요구사항 |
|---|---|
| IN-FR-08 | 각 ctx 타입은 그 step 에 허용된 **조회 그룹 접근자**(3.4)와 **신호 생성자**만 메서드로 가진다 |
| IN-FR-09 | 신호 생성자는 봉인 interface 를 구현하는 비공개 타입의 값을 돌려준다. 그 값에는 invocation 토큰이 들어 있다 |
| IN-FR-10 | ctx 는 dispatch 마다 새로 만들고, 함수가 반환되면(에러·panic 포함) 폐기한다 |
| IN-FR-11 | 폐기된 ctx 의 메서드를 부르면 `ErrExpiredCtx` 로 panic 한다. dispatch 안의 panic 은 inject 가 recover 해 그 dispatch 의 error 로 돌린다. dispatch 밖(누수된 goroutine)에서의 panic 은 잡지 않는다 — 조용히 넘어가는 것보다 즉시 드러나는 것이 낫다는 판단 |
| IN-FR-12 | inject 는 반환된 신호의 토큰이 현재 invocation 과 같은지 검사하고, 다르면 그 dispatch 를 `fail` 로 바꾼다 |
| IN-FR-13 | ctx 는 `context.Context` 를 제공한다 (`ctx.Context()`). core 가 넘긴 취소를 그대로 전달한다 |

### 3.4 조회 약속어

**그룹과 메서드** (모두 읽기 전용, core View 의 사실을 그대로 전달 — 가공·추정 없음)

| 그룹 | 접근자 | 메서드 |
|---|---|---|
| Flow | `ctx.Flow()` | `Step`, `Seq`, `RunID`, `SessionID`, `TurnIndex`, `CallIndex`, `Elapsed`, `Signals()`(신호 기록), `Count(kind, reason)` |
| Conversation | `ctx.Conv()` | `History`, `Transcript`(run 기록, CORE-FR-31a), `TurnAppended`, `Ephemeral`, `Projection`, `Edits`, `Inbox` |
| Request | `ctx.Request()` | `Current()`(확정 요청, 이번 호출 이전이면 zero), `Model()`(마지막으로 선택된 ModelInfo, 없으면 zero) |
| Stream | `ctx.Stream()` | `Draft()`(부분 응답), `Event()`(현재 이벤트) |
| Output | `ctx.Output()` | `Response()`, `CallError()`, `LastUsage()`(이 run 에서 마지막으로 완료된 응답의 usage, 없으면 zero) |
| Tools | `ctx.Tools()` | `Plan`, `Calls`, `Results`(지금까지), `Speculative()`(핸들 상태) |
| Env | `ctx.Env()` | `Config()`(RunInput.Config raw), `ContractVersion`, `Binding()`(step 별 바인딩된 세트 이름) |
| State | `ctx.State()` | 3.6 |

**step 별 사용 가능 그룹** (● = 있음. 없는 그룹은 ctx 타입에 접근자 자체가 없다)

| # | step | Flow | Conv | Request | Stream | Output | Tools | Env | State |
|---|---|---|---|---|---|---|---|---|---|
| 1 | run.start | ● | | | | | | ● | ● |
| 2 | turn.start | ● | ● | | | ● | | ● | ● |
| 3 | context.maintain | ● | ● | ● | | ● | | ● | ● |
| 4 | context.inject | ● | ● | ● | | ● | | ● | ● |
| 5 | context.project | ● | ● | ● | | ● | | ● | ● |
| 6 | request.build | ● | ● | ● | | ● | | ● | ● |
| 7 | request.finalize | ● | ● | ● | | ● | | ● | ● |
| 8 | model.select | ● | ● | ● | | ● | | ● | ● |
| 9 | model.call | ● | | ● | | | | ● | ● |
| 10 | stream.event | ● | | ● | ● | | ● | ● | ● |
| 11 | stream.end | ● | | ● | ● | | ● | ● | ● |
| 12 | call.error | ● | ● | ● | ● | ● | | ● | ● |
| 13 | response.classify | ● | ● | ● | | ● | | ● | ● |
| 14 | response.commit | ● | ● | ● | | ● | | ● | ● |
| 15 | tools.plan | ● | ● | ● | | ● | ● | ● | ● |
| 16 | tool.before | ● | | | | | ● | ● | ● |
| 17 | tool.execute | ● | | | | | ● | ● | ● |
| 18 | tool.after | ● | | | | | ● | ● | ● |
| 19 | tools.commit | ● | ● | | | ● | ● | ● | ● |
| 20 | turn.finish | ● | ● | ● | | ● | ● | ● | ● |
| 21 | run.beforeSettle | ● | ● | ● | | ● | ● | ● | ● |
| 22 | run.settled | ● | ● | ● | | ● | ● | ● | ● |

| id | 요구사항 |
|---|---|
| IN-FR-14 | 조회는 위 그룹·메서드로 닫혀 있다. 추가는 버전을 올려 추가만 한다 (D3) |
| IN-FR-15 | 한 dispatch 안의 모든 조회는 같은 스냅샷(core CORE-FR-28)을 본다 |
| IN-FR-16 | 조회는 core 가 dispatch 시작 시 만든 스냅샷을 읽으므로 inject 는 별도 캐시를 두지 않는다 (투영은 core 에 이미 계산돼 있고, 신호 집계는 기록을 한 번 훑는다) |
| IN-FR-17 | 조회 결과로 받은 슬라이스·맵을 함수가 수정해도 스냅샷에 영향이 없어야 한다 (복사 또는 불변 뷰) |

### 3.5 FunctionSet 과 바인딩

```go
set := inject.FunctionSet{
    Name:       "my-compaction",
    TurnFinish: turnFinish,
    ContextMaintain: maintain,
}
d, err := inject.Bind(defaults.Core(), defaults.Resilient(), set)   // 뒤의 세트가 앞을 덮음
k := core.New(d, core.Options{})
```

| id | 요구사항 |
|---|---|
| IN-FR-18 | `FunctionSet` 은 `Name`(필수, 세트 간 유일)과 22개 step 의 함수 필드(각각 nil 가능)를 가진다 |
| IN-FR-19 | `Bind(sets ...)` 는 step 마다 **nil 이 아닌 마지막 함수**를 고른다. 22개 중 하나라도 비면 비어 있는 step 목록을 담은 에러를 돌려준다 |
| IN-FR-20 | `Bind` 는 결과로 `core.Dispatcher` 를 구현하는 값을 돌려준다. 이 값은 불변이며 여러 Kernel 이 공유해도 안전하다 |
| IN-FR-21 | 바인딩 결과(step → 세트 이름)는 `ctx.Env().Binding()` 과 트레이스로 확인할 수 있다 |
| IN-FR-22 | 같은 이름의 세트를 두 번 넣으면 `Bind` 는 에러를 돌려준다 (상태 네임스페이스 충돌 방지) |

### 3.6 함수 세트 상태 (D11)

| id | 요구사항 |
|---|---|
| IN-FR-23 | `ctx.State()` 는 **그 함수가 바인딩된 세트(`Bind` 에서 그 step 을 채운 FunctionSet)의 `Name`** 네임스페이스만 읽고 쓴다. 다른 세트의 함수를 조합해 자기 세트에 넣으면 그 함수도 자기 세트의 네임스페이스를 쓴다. 키는 문자열, 값은 JSON 직렬화 가능한 값이다 |
| IN-FR-24 | 쓰기는 dispatch 안에서 버퍼링되고, 함수가 에러 없이 반환되고 신호 검증을 통과했을 때만 run 상태에 반영된다 (dispatch 단위 원자성) |
| IN-FR-25 | 세트는 키를 공개(publish)로 표시할 수 있다. 다른 세트는 `ctx.State().Read(ns, key)` 로 공개 키만 읽을 수 있고 쓸 수 없다 |
| IN-FR-26 | 상태의 저장·복원은 `Bind` 옵션으로 주입하는 `StateStore` interface(`Load(sessionID)`, `Save(sessionID, ns, delta)`)가 한다. 없으면 run 메모리에만 둔다. 반영된 변경은 그 dispatch 직후 `Save` 로 넘긴다 |
| IN-FR-27 | 브랜치·재개 시 복원할 상태 시점의 선택은 StateStore 구현(sets)의 몫이다 |

### 3.7 조합 함수 (D5)

step 하나에 함수 하나라는 규칙은 유지하고, 여러 기능을 하나로 합치는 도구를 제공한다. 패키지 `inject/combine`. 모두 **하위 함수들에게 같은 ctx 를 넘긴다.**

| 조합 | 적용 step | 동작 |
|---|---|---|
| `FirstDecisive(fns...)` | 판정 step (3, 10, 12, 13, 20, 21) | 순서대로 부르고, 처음으로 "`next` 가 아닌 전이 또는 효과"를 낸 함수의 신호를 채택. 모두 `next` 면 `next` |
| `ByKind(order, fns...)` | 판정 step | 모두 부르고, `order` 목록에서 가장 앞선 전이 종류를 낸 함수의 신호 배열을 채택 |
| `Pipe(fns...)` | 입력과 출력 타입이 같은 step (7, 18) | 앞 함수의 출력을 다음 함수의 입력으로. 신호는 처음으로 `next` 가 아닌 것을 낸 함수에서 멈춤 |
| `Concat(fns...)` | 메시지 목록을 내는 step (2, 4) | 출력 목록을 순서대로 이어 붙임. 신호는 `FirstDecisive` 규칙 |
| `Wrap(inner, before, after)` | 모든 step | 앞뒤로 관찰·가공 (트레이스, 계측) |

| id | 요구사항 |
|---|---|
| IN-FR-28 | 위 5개 조합을 제공하고, 적용할 수 없는 step 에는 **타입으로** 쓸 수 없게 한다 (생성된 제네릭 제약 또는 step 별 생성 함수) |
| IN-FR-29 | 조합 함수는 채택하지 않은 하위 함수의 신호를 트레이스에 `dropped` 로 남긴다 |
| IN-FR-30 | 조합 함수는 inject 의 일반 사용자 코드와 같은 공개 API 만 사용한다 (특권 없음) — 사용자가 같은 방식으로 자기 조합을 만들 수 있어야 한다 |
| IN-FR-54 | 조합은 step 별 네임스페이스 값의 메서드로 생성한다: `combine.TurnFinish.FirstDecisive(a, b)`. 적용할 수 없는 조합은 메서드가 없다 (IN-FR-28 의 타입 강제). "결정적" = 신호가 비어 있지 않고 `[next]` 하나만이 아님. 하위 함수가 error 를 내면 즉시 그 error 로 끝낸다 |
| IN-FR-55 | `FirstDecisive`: 결정적 신호를 낸 첫 함수를 채택하고 뒤 함수는 부르지 않는다. 앞선 함수의 `[next]` 는 dropped. 모두 결정적이 아니면 신호 없음(= next, 12·20 에서는 core 가 거부) |
| IN-FR-56 | `ByKind(order, fns...)`: 모두 부른다. 각 배열의 전이 종류(없으면 next)를 `order` 의 위치로 순위를 매기고 가장 앞선 것을 채택한다. `order` 에 없는 종류는 모든 나열된 종류 뒤. 동순위는 앞 함수. 나머지 배열은 dropped |
| IN-FR-57 | `Pipe`: 앞 함수의 출력을 다음 함수의 입력으로 넘긴다 (18 은 결과만 바뀌고 호출은 그대로). 결정적 신호를 낸 함수에서 멈추고 그 출력과 신호를 돌려준다 |
| IN-FR-58 | `Concat`: 모두 부르고 출력을 순서대로 이어 붙인다. 신호는 결정적 신호를 낸 첫 함수의 것, 나머지 결정적 신호는 dropped |
| IN-FR-59 | `Wrap(inner, before, after)`: `before(ctx, 입력...)` 는 관찰용, `after(ctx, 입력..., 결과...)` 는 inner 의 결과를 받아 바꾼 결과를 돌려준다. 둘 다 nil 가능 |

### 3.8 로더 (D12)

| id | 요구사항 |
|---|---|
| IN-FR-31 | 로더는 `Loader` interface(`Load(spec) (FunctionSet, error)`)이며 `Bind` 전에 쓰는 주입 대상이다. inject 는 두 구현을 제공한다: `InProcess`(Go 값 그대로), `Remote`(하위 프로세스 + gRPC) |
| IN-FR-32 | `Remote` 는 plugin 실행 파일을 하위 프로세스로 띄우고, 핸드셰이크로 **plugin 이름, 구현한 step 목록, contract 버전, StepSpec 버전**을 받는다. 버전이 호환되지 않으면 로드 실패 |
| IN-FR-33 | `Remote` 가 돌려주는 FunctionSet 의 각 함수는 호출될 때 **dispatch 하나당 양방향 스트림 하나**를 연다: host → `Invoke{token, step, seq, input}`, plugin → `Query{token, group, method, args}` / host → `QueryResult`, plugin → `Result{token, output, signals}` |
| IN-FR-34 | host 는 `Result` 를 받으면 토큰을 폐기한다. 폐기된 토큰의 `Query`·`Result` 는 거부한다 |
| IN-FR-35 | 원격 신호는 `{kind, payload}` 데이터로 오므로 봉인 타입의 보호가 없다. host 는 **허용 표(core 3.4)와 신호 배열 규칙(core CORE-FR-05)을 직접 검사**하고 위반 시 `fail` 로 바꾼다 |
| IN-FR-36 | 원격 조회는 해당 step 에서 허용된 그룹·메서드만 받는다. 그 외는 에러로 응답한다 |
| IN-FR-37 | 원격 호출에는 dispatch 별 타임아웃(`Remote` 옵션)이 있고, 넘으면 그 dispatch 는 error 가 된다. plugin 프로세스가 죽으면 진행 중 dispatch 는 error, 이후 호출은 로더 옵션에 따라 재시작하거나 계속 error 를 돌려준다 |
| IN-FR-38 | `stream.event` 의 원격 전달은 두 모드를 가진다. `sync`: 이벤트마다 왕복(정확, 느림). `async`: 이벤트를 묶음으로 보내고 host 는 즉시 `next` 를 돌려준다. plugin 이 낸 `next` 외 신호는 **그다음 이벤트의 dispatch 에서** 적용된다 (지연 적용). 모드는 세트 로드 시 정한다 |
| IN-FR-39 | plugin SDK(Go)는 원격 세트를 in-process 세트와 **같은 함수 시그니처**로 작성하게 해 준다. 같은 코드를 `InProcess` 와 `Remote` 양쪽으로 로드할 수 있어야 한다 |

### 3.9 Dispatcher 동작

한 번의 dispatch:

```text
[1] look up bound fn for step
[2] new token, new ctx(step, view, token, set namespace)
[3] call fn  (recover panic -> error)
[4] revoke ctx
[5] verify signals: token, allowed table, array rule          violation -> fail
[6] commit state delta (only if [3] ok and [5] ok), StateStore.Save
[7] trace record
[8] convert to core.Result {Output, Signals}
```

| id | 요구사항 |
|---|---|
| IN-FR-40 | Dispatcher 는 위 순서로 동작해야 한다 |
| IN-FR-41 | Dispatcher 는 동시 호출에 안전해야 한다 (core A2) |

### 3.10 트레이스와 관찰자

| id | 요구사항 |
|---|---|
| IN-FR-42 | dispatch 마다 `{Seq, Step, Set, Duration, Signals, Dropped, Err, StateKeysChanged}` 를 `Tracer` interface(Bind 옵션)로 넘긴다. 없으면 버린다 |
| IN-FR-43 | inject 는 core.Observer 를 구현하는 분배기를 제공한다. 등록된 여러 sink 에 sink 별 제한된 버퍼로 비동기 전달하고, 버퍼가 차면 그 sink 의 이벤트를 버리고 버린 수를 센다 (루프를 막지 않기 위함). 버퍼 크기는 옵션 |

### 3.11 Go 표현

스펙 요구사항을 코드로 옮길 때 정한 표현이다. 이름은 step 이름의 CamelCase(`run.start` → `RunStart`)에서 온다.

| id | 요구사항 |
|---|---|
| IN-FR-44 | 생성 대상: `<Step>Fn`(함수 타입), `<Step>Ctx`(ctx), `<Step>Signal`(봉인 interface), `FunctionSet.<Step>`(필드). 함수 시그니처는 core CORE-FR-39 의 입출력을 따른다. 18 은 `(ctx, call, result)`, 19 는 `(ctx, calls, results)` 처럼 입력 구조체를 풀어서 받는다 |
| IN-FR-45 | `<Step>Signal` 은 `Kind() core.SignalKind`, `WithReason(string) <Step>Signal`, 비공개 표지 메서드를 가진다. 생성자는 그 step 에 허용된 것만 ctx 메서드로 있다: `Next()`, `Recall()`, `Reselect()`, `Rebuild()`, `RestartTurn()`, `ContinueTurn()`, `SkipTools(results)`, `Settle()`, `Stop()`, `Fail(err)`, `Rewrite(edit)`, `InjectInput(msg)`, `StartTool(call)` |
| IN-FR-46 | 조회 그룹 타입: `Flow`, `Conv`, `Request`, `Stream`, `Output`, `Tools`, `Env`, `State` (3.4 표의 메서드). `Output.LastUsage()` = 현재 Response 의 Usage, 없으면 Transcript 중 이 run 에서 추가된(History 이후) 마지막 assistant 메시지의 Usage. `State` 메서드: `Get(key)`, `Set(key, v) error`(JSON 직렬화), `Delete(key)`, `Publish(key)`, `Read(ns, key)`(공개 키만) |
| IN-FR-47 | 조회는 core View 를 직접 받지 않고 `ViewSource` interface(core.View 의 조회 메서드 집합)로 받는다. `core.View` 는 이를 그대로 만족한다. 테스트용 가짜 View 는 `inject/injecttest` 가 제공하고(sets ST-NFR-01), 원격 plugin 쪽은 조회 요청으로 구현한다 |
| IN-FR-48 | 조회 결과의 contract 값은 깊은 복사로 돌려준다 (IN-FR-17) |
| IN-FR-49 | `ctx.Dropped(sigs...)` 는 채택하지 않은 신호를 트레이스의 `Dropped` 에 남긴다 (IN-FR-29 의 공개 수단) |
| IN-FR-50 | `Bind(sets...) (*Dispatcher, error)`, `BindWith(BindOptions{StateStore, Tracer}, sets...)`. `*Dispatcher` 는 core.Dispatcher 이고 `DispatchSource(ctx, step, input, ViewSource)` 도 공개한다 |
| IN-FR-51 | `StateStore`: `Load(ctx, sessionID) (map[ns]NamespaceState, error)`, `Save(ctx, sessionID, ns, StateDelta) error`. `NamespaceState{Values, Published}`, `StateDelta{Set, Delete, Publish}`. Load 는 run 의 1(run.start) dispatch 에서 함수 호출 전에 한 번, run 상태는 22 dispatch 뒤 버린다. Load·Save 에러는 그 dispatch 의 error |
| IN-FR-52 | inject 에러 sentinel: `ErrExpiredCtx`(panic 값), `ErrForeignSignal`(토큰 불일치, nil 신호), `ErrSignalOrder`(배열 규칙), `ErrInputType`, `ErrPanic`, `ErrBind` |
| IN-FR-53 | 원격용 protobuf 메시지 생성(IN-FR-03 의 마지막 항목)은 원격 로더 단계에서 한다 |

### 3.12 비기능

| id | 요구사항 |
|---|---|
| IN-NFR-01 | in-process dispatch 의 inject 자체 오버헤드(함수 실행 시간 제외)를 벤치마크로 측정하고 기록한다. 목표 20µs 이하 |
| IN-NFR-02 | `go test -race` 통과 |
| IN-NFR-03 | 봉인·범위 강제를 검증하는 **컴파일 실패 테스트**(허용되지 않은 신호·조회를 쓰는 코드 조각이 빌드되지 않음)를 둔다 |
| IN-NFR-04 | 원격 경로는 in-process 경로와 **같은 테스트 스위트**를 통과해야 한다 (같은 가짜 세트를 두 로더로 로드) |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| IN-FR-01~04 | 생성기 테스트: core 상수와 불일치 시 생성 실패, 생성 결과 diff 검사 |
| IN-FR-05~07 | 생성된 타입 목록이 core 3.1 과 일치 |
| IN-FR-06, 08, IN-NFR-03 | 컴파일 실패 테스트 (`go build` 가 실패해야 하는 코드 조각 모음) |
| IN-FR-09, 12 | 다른 dispatch 의 신호 반환 → fail |
| IN-FR-10, 11 | 함수가 ctx 를 저장 후 반환 뒤 사용 → ErrExpiredCtx panic |
| IN-FR-13 | core 취소 → ctx.Context() 취소 |
| IN-FR-14~17 | step 별 조회 가능 그룹 표 대조, 스냅샷 불변, 반환값 수정 격리 |
| IN-FR-18~22 | 겹침 규칙, 누락 에러, 이름 중복 에러, Binding 조회 |
| IN-FR-23~27 | 네임스페이스 격리, 실패 dispatch 의 쓰기 미반영, publish 읽기, StateStore 호출 |
| IN-FR-28~30 | 조합별 동작·dropped 기록, 적용 불가 step 에서 컴파일 실패 |
| IN-FR-31~39 | 원격: 핸드셰이크 버전 불일치, 토큰 폐기 후 요청 거부, 허용 외 신호·조회 거부, 타임아웃, 프로세스 종료, sync/async 모드 차이 |
| IN-FR-40, 41 | 순서 검사(가짜 Tracer·StateStore 호출 순서), 병렬 dispatch |
| IN-FR-42, 43 | Tracer 기록, 느린 sink 에서 루프 비차단·드롭 카운트 |
| IN-NFR-01 | 벤치마크 |
| IN-NFR-04 | 같은 스위트를 두 로더로 실행 |

---

## 5. 추적성

| 결정 | 요구사항 |
|---|---|
| D2 | IN-FR-05~07, 18~20 |
| D3 | IN-FR-06, 08, 14 |
| D4 | IN-FR-08~13, 15~17 |
| D5 | IN-FR-19, 28~30 |
| D6 | IN-C3, IN-FR-19 (inject 는 기본값을 채우지 않음) |
| D11 | IN-FR-23~27 |
| D12 | IN-C4, IN-FR-31~39 |
| 대화: "함수 안에서만" | 2.2, IN-FR-09~12, IN-NFR-03 |

---

## 6. 미결 사항

| id | 내용 | 결정 시점 |
|---|---|---|
| O1 | ~~StepSpec 소스 형식~~ → **Go 코드** 로 확정 (사용자, impl-01). 허용 신호는 core 의 `AllowedSignals` 를 직접 읽는다 | 확정 |
| O2 | ~~원격 plugin 관리 라이브러리~~ → **직접 구현** 으로 확정 (사용자, impl-01): os/exec + unix socket 위 gRPC. 외부 의존성은 grpc(Apache-2.0)·protobuf(BSD-3) 만 | 확정 |
| O3 | IN-FR-11 의 "dispatch 밖 panic" 정책이 서버형 surface(메신저 게이트웨이)에서 허용 가능한지 | surfaces 스펙 작성 시 재검토 |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | 상태 NS = "함수가 속한 세트" | 바인딩된 세트의 Name (IN-FR-23) | Go 함수 값에는 소속이 없음. 사용자 확정 (함수에 NS 부착안 기각) |
| 0.2 | ctx 가 core View 를 직접 사용 | `ViewSource` interface (IN-FR-47) | core.View 는 외부에서 만들 수 없음: 가짜 View(sets 테스트)·원격 plugin 에 필요 |
| 0.2 | O1·O2 미결 | Go 코드 / 직접 구현 | 사용자 확정 |
| 0.2 | Go 표현 미정 | 3.11 추가 (IN-FR-44~53) | 구현 착수 |
| 0.2 | 비싼 조회는 dispatch 안에서 캐시 (IN-FR-16) | 캐시 없음 | 스냅샷이 이미 계산돼 있음, dispatch 372ns. 사용자 확정 |
| 0.2 | 조합 함수 세부 규칙 미정 | IN-FR-54~59 | 구현 착수 (3.7 문장의 해석) |
