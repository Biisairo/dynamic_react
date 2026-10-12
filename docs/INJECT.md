# 주입 가이드 (R22, R26, R27)

완성된 에이전트(`L2_harness/agent`)를 core(L0, 22 step 루프)만 고정한 채 튜닝하는 방법이다. L1 을 포크하지 않고 `agent.New` 를 부를 때 Go 함수로 바꾼다. 근거는 `docs/DECISIONS.md` D12·R22, 요구사항은 `L2_harness/SPEC.md` 3.6c·3.6d(HN-FR-37~42, 51~54)와 `L2_harness/agent/SPEC.md` 3.14·3.14a(AG-FR-40~46), 층 바인딩은 `L1_inject/inject/SPEC.md` 3.5(IN-FR-19, 60~63)다. 모드(5장)는 `L2_harness/SPEC.md` 3.6e(HN-FR-55~72), `L2_harness/agent/SPEC.md` 3.14b(AG-FR-47~51), `L1_inject/sets/SPEC.md` 2.3·3.2b(ST-FR-54~59)를 따른다.

## 1. 무엇을 바꿀 수 있나

모든 필드는 **기본값을 받아 바꾼 값을 돌려주는 함수**다. nil 이면 기본값을 그대로 쓴다. 기본값을 받으므로 교체(받은 값을 버림)와 감싸기(받은 값을 안에서 부름)를 같은 방법으로 한다. 각 함수는 `New` 안에서 한 번만 불린다.

| `agent.Options` 필드 | 기본값 | 결과가 nil·빈 값이면 |
|---|---|---|
| `Sets func(d sets.Deps) []harness.SetEntry` | `harness.DefaultSets(d)` = base·require·cache·guard·truncate·resilient, 전부 `On: true` | 빈 step 이 생겨 `Bind` 에러 |
| `ToolSet func(def []sets.Tool) []sets.Tool` | `tools.All(env)` = read·bash·edit·write·grep·find·ls (+ 모드가 있으면 `mode`, 5장) | 도구 없는 에이전트 |
| `System func(def []contract.Block) []contract.Block` | 기본 시스템 프롬프트 (preamble·tools·rules·AGENTS.md·skills·cwd) | 시스템 프롬프트 없음 |
| `Summarizer func(def sets.Summarizer) sets.Summarizer` | pi 요약 프롬프트를 쓰는 요약기 | 자동 압축 끔, `Compact` 는 에러 |
| `Store func(def harness.Store) harness.Store` | `<sessionDir>/<id>.jsonl` JSONL 저장소 | `New` 에러 |
| `ModelCaller func(def sets.ModelCaller) sets.ModelCaller` | adapters Client | `New` 에러 |
| `Modes []harness.Mode` (함수가 아닌 값, R27) | 없음 | 모드 없음 (`mode` 도구도 없음) |

harness 를 직접 쓸 때는 `harness.Options` 의 같은 이름 필드(ModelCaller 제외, 모델은 `Options.Model` 값)를 쓴다.

### 만드는 순서 (HN-FR-38)

```text
① ToolSet(tools.All(env) [+ mode]) → registry (같은 이름은 뒤의 것이 앞 자리를 덮음)
② Store(session.New(env, dir))     → 저장소
③ System(기본 조립(①))              → 시스템 프롬프트
④ Summarizer(summary.New(model))   → 요약기 (OnEvent 가 있으면 compaction 이벤트로 감쌈)
⑤ Sets(Deps{Model, Tools: ①, Store: ②, System: ③, Summarizer: ④, Sleep, Now, NewID, Modes})  → 등록 목록
   → 모드 검사(Modes) → 배치(Layout / 설정 sets)
   → 기본 배치와 모드 배치마다: 켜진 세트를 배치 순서로 surface 래핑(승인·이벤트)
     → inject.BindWith(StateStore: ②, Runs: 공유 RunTable)
```

`Sets` 가 받는 `Deps` 에는 ①~④ 의 결과와 모드 통로(`Modes`, 5장)가 들어 있다. 그래서 사용자 세트도 같은 도구·저장소·요약기를 쓴다. `Deps.NewID` 는 nil 일 수 있다 (`harness.Options.NewID` 를 주지 않았을 때 — agent 는 주지 않는다). sets 의 세트는 내부에서 기본값을 채우지만 사용자 세트는 직접 확인해야 한다.

## 2. 세트 다루기

### 층 바인딩 (IN-FR-19, 60~63)

`Bind` 는 step 마다 그 step 을 채운 세트들을 **목록 순서대로 쌓는다 — 뒤의 세트가 위 층**이다. dispatch 는 가장 위 층 함수를 부른다. 위 층 함수는 `ctx.Next(...)`(인자는 그 step 함수의 ctx 뒤 인자들, 반환도 같음)로 바로 아래 층을 부를 수 있고, 부르지 않으면 아래 층을 덮는다. 가장 아래 층에서 `ctx.Next` 를 부르면 `inject.ErrNoNext` 를 감싼 에러다 (그 dispatch 실패). 신호 `next` 의 생성자는 `ctx.NextSignal()` 이다.

| 하려는 것 | 방법 |
|---|---|
| step 하나를 덮기 | 그 step 만 채운 세트를 위에 쌓고 `ctx.Next` 를 부르지 않는다 |
| 아래 층을 감싸기 | 위에 쌓고 `ctx.Next(...)` 의 결과·신호를 고치거나 그대로 돌려준다 (아래 층 신호를 그대로 돌려줘도 된다) |
| 기본 세트를 빼기·바꾸기 | `DefaultSets(d)` 의 원소를 빼거나 고친다. base·require 도 사용자가 넣는 부품이다 |
| 전부 바꾸기 | `DefaultSets` 를 쓰지 않고 22 step 을 모두 채운다 |

세트 이름은 상태 네임스페이스다 (IN-FR-23). 아래 층 함수는 **자기 세트의** 네임스페이스를 쓴다 (IN-FR-61). 같은 이름을 두 번 넣으면 에러이고, 기본 세트의 상태(`base` 의 `edits.saved`·`inbox.consumed` 등)에 기대는 함수를 다른 이름의 세트로 옮기면 상태가 이어지지 않는다.

### 기본 세트 (sets 3.0)

| 세트 | 채우는 것 | 아래 층 필요 |
|---|---|---|
| `base` | 세션 로드·저장, 입력·입력함, 편집 적용, 도구 실행, 16·18 통과 | 없음 |
| `require` | 요청 조립·모델 호출·응답 확정·실패·순차 계획·턴 루프(R2)·짝 복구, 빈 step | run.start |
| `cache` | 요청의 캐시 힌트 | request.finalize |
| `guard` | 없는 도구·인자 스키마 검사 | tool.before |
| `truncate` | 도구 결과 절단 | tool.after |
| `resilient` | 재시도·overflow 복구·임계치 압축·follow-up | 없음 |
| `verify` (주입용) | 변경 뒤 끝낼 때 검증 | turn.finish |

목록·순서·기본 켜짐은 L2 `harness.DefaultSets` 한 곳의 프리셋이다 (L0·L1 은 순서를 모른다). 아래 층이 필요한 세트를 같은 step 을 채운 세트보다 아래로 옮기면, 실행할 때 그 step 이 `ErrNoNext` 로 실패한다 (Bind 는 알 수 없음).

```go
a, err := agent.New(ctx, agent.Options{
    Sets: func(d sets.Deps) []harness.SetEntry {
        mine := inject.FunctionSet{Name: "mine", Description: "rules and rm -rf block",
            // 4 context.inject: 이번 턴의 요청에만 붙고 세션에 저장되지 않는다
            ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
                return []contract.Message{rule(d.Now())}, nil, nil
            },
            // 16 tool.before: 막을 호출은 ShortCircuit, 나머지는 아래 층(guard → base)으로
            ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
                if blocked(c) {
                    return core.BeforeOut{Call: c, ShortCircuit: &contract.ToolResult{CallID: c.ID, Status: contract.StatusError,
                        Content: []contract.Block{{Kind: contract.BlockText, Text: "blocked"}}}}, nil, nil
                }
                return ctx.Next(c)
            }}
        return append(harness.DefaultSets(d), harness.SetEntry{Set: mine, On: true})
    },
})
```

### 배치 바꾸기 (R26, HN-FR-51~54, AG-FR-44~46)

배치 = 등록된 세트 전부의 순서 + 각 켜짐. 실행 중에 켜기·끄기·순서를 바꿀 수 있고 **다음 run 부터** 적용된다 (실행 중인 run 은 그대로). 배치를 바꾸면 모드 배치(5장)도 새 기본 배치 위에서 다시 계산된다. 어느 모드 배치든 Bind 에 실패하면 배치 변경 전체를 거부한다.

| 어디서 | 방법 |
|---|---|
| dr | `/sets` 로 보기, `/set <name> on|off|to <n>` 로 바꾸기 (대기 상태에서만) |
| Go (agent) | `a.Sets()`, `a.ArrangeSets(ctx, layout)` — 성공하면 사용자 설정 `sets` 에 저장. 저장 실패면 배치를 되돌린다 |
| Go (harness) | `h.Sets()`, `h.Arrange(layout)` (저장 없음), 처음 배치는 `harness.Options.Layout` |
| 설정 파일 | `"sets": [{"name": "guard", "on": false}, …]` — 등록되지 않은 이름은 무시, 빠진 등록 세트는 등록 순서·기본 켜짐으로 뒤에 |

`Arrange` 의 layout 은 등록된 이름 전부를 한 번씩 담아야 한다 (아니면 `harness.ErrSetLayout`). 다시 묶어 빈 step 이 생기면(예: require 끔) 거부하고 지금 배치를 유지한다.

## 3. 부품 다루기

```go
agent.Options{
    ToolSet: func(def []sets.Tool) []sets.Tool {
        return append(def, myTool)                     // 추가. 같은 이름이면 그 자리를 교체
    },
    System: func(def []contract.Block) []contract.Block {
        return append(def, contract.Block{Kind: contract.BlockText, Text: "Team rule: …"})
    },
    Summarizer: func(def sets.Summarizer) sets.Summarizer { return logging{def} },
    Store: func(def harness.Store) harness.Store { return myDB },   // sets.Store + List
    ModelCaller: func(def sets.ModelCaller) sets.ModelCaller { return metered{def} },
}
```

- `harness.Store` = `sets.Store`(Load·AppendMessages·AppendEdits·SaveState) + `List(ctx) ([]string, error)`. run·수동 압축·세트 상태·agent 의 `Sessions`·`History` 가 모두 이 저장소 하나를 쓴다. `Load` 는 저장되지 않은 ID 에 빈 세션을 돌려줘야 한다. sessionID 검사(`session.CheckID`)는 harness·agent 가 저장소 앞에서 한다.
- `ModelCaller` 는 run 의 모델 호출과 압축 요약 호출에만 쓰인다. 모델 목록·provider 프로필·키 확인은 adapters 기준 그대로다.
- `System` 은 생성 시 한 번만 정해지고 run 동안 바뀌지 않는다 (HN-C4, 프롬프트 캐시 보호).

## 4. 지켜지는 것과 사용자 책임

| 항목 | 사용자 세트를 넣었을 때 |
|---|---|
| 승인 (permissions 규칙, Approver, deny) | **항상 유지.** surface 래핑이 step 마다 가장 위 층 `tool.before` 바깥을 감싼다 (HN-FR-41). 배치를 바꿔도 같다 |
| 이벤트 (OnEvent: message·tool·turn·run_end 등) | **항상 유지.** 같은 방식 |
| 기본 검사 (없는 도구, 인자 스키마, ST-FR-38) | `guard` 세트다. 끄거나, 그 위에 `ctx.Next` 를 부르지 않는 `tool.before` 를 쌓으면 **빠진다.** 이때 승인 Gate 는 검사 없이 그 호출에 불리고, 없는 도구는 base 의 실행 단계에서 `unknown tool:` 결과가 된다. 사용자 책임 |
| Steer 의 남은 도구 건너뛰기 | `base` 세트의 공개 상태 `inbox.consumed` 를 읽는다. `base` 를 빼고 그 상태를 갱신하지 않으면 Steer 한 번 뒤의 도구 호출이 계속 skipped 된다 (AG-FR-42) |
| 도구 상한·설정 `agent.tools`·이름 규칙 | `ToolSet` 적용 뒤 registry 기준. 추가한 도구도 대상이다 |
| 패턴 규칙 `tool(pattern)` | `bash`·`read`·`write`·`edit`·`grep`·`find`·`ls` 이름에만 쓴다 (추가 도구에 쓰면 `New` 에러). 비교 값은 인자 `command`/`path` 에서 꺼낸다 — 같은 이름으로 바꾼 도구에 그 인자가 없으면 패턴 규칙(deny 포함)이 **맞지 않는다.** 이름 규칙(`bash`)은 맞는다 |

## 5. 모드 — 턴 경계에서 세트 배치 바꾸기 (R27)

세션마다 **모드** 하나를 둘 수 있다. 모드는 이름이 붙은 배치 변형이고, 세션이 그 모드에 있는 동안 run 의 세트 묶음이 모드 배치로 바뀐다. Hermes 의 `/goal`(턴 뒤 판정 루프)·`/plan`(계획 전용 턴)처럼 "상황에 따라 다른 루프"를 core 를 고치지 않고 만드는 방법이다.

### 5.1 정의

```go
agent.Options{
    Sets: func(d sets.Deps) []harness.SetEntry {
        return append(harness.DefaultSets(d),
            harness.SetEntry{Set: goalSet(d)},        // On 생략 = false: 기본 배치에서는 꺼 두고 모드에서만 켠다
            harness.SetEntry{Set: planSet(d)})
    },
    Modes: []harness.Mode{
        {Name: "goal", Description: "keep working until a judge says done (arg = goal)", Push: []string{"goal"}},
        {Name: "plan", Description: "plan only", Push: []string{"plan"}},
    },
}
```

- `Off` 는 이 모드에서 끌 세트다 (예: 기본으로 켜 둔 사용자 규칙 세트를 plan 모드에서만 끄기).
- 모드 배치 = 지금 기본 배치에서 `Push` 의 세트를 자리에서 빼고, `Off` 의 세트를 끄고, 맨 위에 `Push` 를 순서대로 켜서 쌓은 것 (HN-FR-56). 그래서 `Push` 의 세트는 `ctx.Next` 로 기본 세트를 감쌀 수 있다.
- `New` 가 검사한다: 이름은 비지 않고 서로 다름, `Push`·`Off` 는 등록된 세트, `Push` 안 중복·`Push`/`Off` 겹침 없음, 세트 이름 `harness.mode` 는 예약어, 모든 모드 배치가 Bind 됨. 하나라도 어기면 `New` 에러.
- 모드 정의는 Go 코드다. 설정 파일로 모드를 만들 수 없다 (D12). `dr` 에는 등록된 모드가 없다.

### 5.2 진입과 이탈 — 누가 할 수 있나 (HN-FR-62)

| 요청한 쪽 | 통로 | 진입 | 이탈 |
|---|---|---|---|
| 사용자 | `/mode <name> [arg]`·`/mode off`, `Agent.EnterMode`·`ExitMode` | 항상 | 항상 (강제) |
| 모델 | `mode` 도구 (모드가 있으면 자동 등록) | 기본 모드이고 예약이 없을 때만 | 할 수 없음 |
| 세트 (판단기) | `sets.ModesOf(d).Exit(ctx, sid, reason)` | 할 수 없음 | 활성 모드가 있을 때만. 사용자 예약이 있으면 사용자가 이긴다 |

- 모델이 스스로 모드를 빠져나갈 수 없게 한 것이 핵심이다. goal 모드에서 작업 모델이 "다 했다"고 말하고 루프를 끝낼 수 없다. 이탈은 그 모드의 세트가 정한다.
- 요청은 바로 적용되지 않고 **예약**된다. 예약은 run 안에서는 다음 `turn.start` 직전에, run 밖에서는 다음 run 시작 직전에 적용된다 (HN-FR-64~66). 적용될 때 `mode` 이벤트가 나온다.
- `mode` 도구를 부른 턴은 도구 호출이 있으므로 기본 `require` 가 `continue_turn` 을 낸다. 그래서 바로 다음 턴부터 모드 배치로 돈다 (HN-FR-70).
- 모드 상태(활성 모드·인자·예약)는 세션 상태 `harness.mode` 에 저장된다. 이탈하기 전까지 run 과 `/resume`·재시작을 넘어 유지된다. 다른 세션에는 영향이 없다.

### 5.3 모드 세트 쓰기

모드 세트는 보통 두 가지를 한다: 이번 모드에 필요한 동작(지시 주입, 도구 제한)과 이탈 판단.

```go
func goalSet(d sets.Deps) inject.FunctionSet {
    modes := sets.ModesOf(d) // d.Modes 가 nil 이어도 안전한 "모드 없음" 구현
    return inject.FunctionSet{Name: "goal",
        TurnFinish: func(ctx *inject.TurnFinishCtx) ([]inject.TurnFinishSignal, error) {
            sigs, err := ctx.Next()                      // 아래 층(require, verify 등)의 판단
            if err != nil || !hasSettle(sigs) {
                return sigs, err                         // 이번 run 이 아직 계속되면 그대로
            }
            st, _ := modes.Current(ctx.Context(), ctx.Flow().SessionID()) // st.Arg = 목표
            switch judge(ctx, d, st.Arg) {               // 아래 7.4
            case "done":
                return sigs, modes.Exit(ctx.Context(), ctx.Flow().SessionID(), "goal achieved")
            default:                                     // 계속: settle 을 빼고 다음 턴으로
                return append(withoutSettle(sigs), ctx.InjectInput(continuation(d, st.Arg)), ctx.ContinueTurn()), nil
            }
        }}
}
```

- 모드 인자(`Arg`)는 진입할 때 넘긴 자유 텍스트다 (`/mode goal 테스트를 통과시켜`, `mode` 도구의 `arg`). `modes.Current` 로 읽는다.
- 결정적 검증을 판정보다 먼저 하려면 `sets.Verify` 를 `Push` 에 함께 넣고 판단기 세트를 그 위에 둔다 (`Push: ["verify", "goal"]`). 검증이 실패하면 verify 가 settle 을 빼고 보고를 넣으므로 판단기는 판정하지 않고 그대로 넘긴다. Hermes 의 품질 게이트와 같은 순서다.
- 도구를 막으려면 4 `context.inject` 의 지시에 막힌 도구 이름을 적고, 16 `tool.before` 에서 ShortCircuit 한다. 요청의 도구 목록에서는 빼지 않는다. 목록을 바꾸면 캐시 접두 전체가 깨진다 (5.5a). Hermes `/plan` 은 프롬프트 지시뿐이었다.
- 전체 예시: `examples/modes` 의 `GoalSet`(goal.go)·`PlanSet`(plan.go).

### 5.4 판단기의 보조 모델 (R27 ⑥)

판단기는 작업 모델과 다른 모델로 판정할 수 있다. 역할 이름으로 고른다.

```json
{"agent": {"model": {"provider": "deepseek", "id": "deepseek-flash"},
           "aux":   {"goal_judge": {"provider": "anthropic", "id": "claude-haiku-4-5-20251001"}}}}
```

```go
cfg, _ := sets.ParseConfig(ctx.Env().Config())
m := sets.AuxModel(cfg, "goal_judge")             // aux 에 없으면 본 모델
msg, err := sets.Complete(ctx.Context(), d.Model, contract.Request{
    Model:  contract.ModelRef{Provider: m.Provider, ID: m.ID},
    System: []contract.Block{{Kind: contract.BlockText, Text: judgeRules}},
    Messages: []contract.Message{question},
})
```

- `sets.Complete` 는 단발 호출이다. 도구를 실행하지 않고 재시도하지 않는다. 판정 실패를 어떻게 다룰지(Hermes 는 "계속"으로 봄)는 판단기가 정한다.
- run 밖에서 같은 호출이 필요하면 `Agent.Complete(ctx, role, system, msgs)` 를 쓴다.
- 판정은 작업 모델의 답과 하네스가 관찰한 것(예: `ctx.Conv().Transcript()` 의 도구 결과)을 근거로 쓸 수 있다. Hermes 의 judge 는 마지막 답 텍스트만 본다.

### 5.5 지켜지는 것과 사용자 책임

| 항목 | 내용 |
|---|---|
| 승인·이벤트 | 모드 배치에도 surface 래핑이 그대로 적용된다. `mode` 도구도 승인 규칙의 대상이다 (이름 규칙만, 맞는 규칙이 없으면 묻기) |
| 세트 상태 | 모든 배치가 run 상태 표(`inject.RunTable`)를 공유하므로 턴 경계에서 바꿔도 세트 상태가 이어진다 |
| 적용 시점 | `turn.start` 직전에만 바뀐다. turn.finish 의 흐름(`continue_turn`/`settle`)을 바꾸는 사용자 세트를 쓰면 적용 시점도 달라진다 (사용자 책임, HN-FR-70) |
| `Exit` 의 원자성 | `Exit` 는 dispatch 가 나중에 실패해도 바로 기록된다 (ST-FR-59). 실패할 수 있는 일을 먼저 하고 `Exit` 를 마지막에 부른다 |
| `Current` 의 시점 | dispatch 스냅샷 밖의 값이다. 같은 dispatch 안에서 두 번 읽으면 다를 수 있다 |
| 프롬프트 캐시 | 도구 목록과 system 을 바꾸지 않고 지시는 꼬리에 넣는다 (5.5a). 어기면 `prefix_break` 이벤트로 드러난다 |
| 등록이 사라진 모드 | 세션에 저장된 모드가 코드에서 사라지면 기본 모드로 고치고 `mode` 이벤트(Reason `unknown mode <name>`)로 알린다 |

### 5.5a 프롬프트 캐시 접두 (R29)

자동 접두 캐시 provider(OpenAI, DeepSeek, 로컬 서버 등)는 요청의 앞부분이 이전 요청과 같은 만큼 재사용한다. 앞쪽이 한 글자라도 바뀌면 그 위치부터 뒤는 모두 새로 계산한다. 그래서 요청을 아래 모양으로 유지한다.

```text
[tools: ALL tools][base system][stored messages ...][ephemeral tail]
 fixed for the run  fixed        append-only           this turn only
```

세트(모드 세트 포함)를 쓸 때 아래 규칙을 지킨다.

| 규칙 | 내용 |
|---|---|
| 도구 목록 | 바꾸지 않는다. 모드에서만 쓰는 도구도 처음부터 목록에 둔다. 막아야 하면 지시로 알리고 16 `tool.before` 로 막는다 |
| system | 바꾸지 않는다. 시각·턴 번호·남은 예산처럼 매번 바뀌는 값을 넣지 않는다 |
| 추가 지시 | 4 `context.inject` 로 넣는다. 이 메시지는 꼬리에 붙고, 이번 요청에만 들어가고, 저장되지 않는다. user 역할이므로 `[plan mode]` 처럼 하네스 표시를 붙인다 |
| 저장된 대화 | 덧붙이기만 한다. 중간을 바꾸는 것은 편집(압축·숨기기·되돌리기)뿐이다 |
| 투영 | 5 `context.project` 를 바꾸면 Ephemeral 을 끝에 둔다. 중간에 두면 그 위치부터 매 턴 놓친다 |

지점 방식 provider(Anthropic)는 기본 `cache` 세트가 캐시 지점을 꼬리 바로 앞 메시지에 둔다 (`sets.StableLen`).

**감지**: `OnEvent` 가 있으면 harness 가 요청마다 접두를 같은 세션·같은 모델의 직전 요청과 비교한다. 다르면 `prefix_break` 이벤트를 낸다. 요청은 막지 않는다.

| Part | 뜻 | 터미널 |
|---|---|---|
| `tools`, `system` | 도구 목록이나 system 이 바뀌었다. 위 규칙을 어긴 세트가 있다 | `⚠ 캐시 접두 깨짐: …` |
| `messages`, `Expected` false | 편집 없이 저장된 대화가 바뀌었다. 투영을 바꾼 세트를 확인한다 | `⚠ 캐시 접두 깨짐: 메시지 #N …` |
| `messages`, `Expected` true | 압축 같은 편집 때문에 바뀌었다. 정상이다 | 표시 안 함 |
| `model` | 이 세션에서 처음 쓰는 모델로 바뀌었다. 그 모델의 캐시는 비어 있다 | `· 캐시 새로 시작: …` |

감지하지 않는 것: `model.call` 을 감싸서 요청을 고치는 세트, 어댑터의 직렬화 차이, provider 측 캐시 만료. 실제 적중률은 `message_end` 의 `Usage.CacheRead` 로 본다.

### 5.6 하지 않는 것

- 모드 겹치기(동시에 둘 이상), 모드 → 다른 모드로 모델이 직접 전환.
- 전체 배치형 모드(`Push`/`Off` 대신 배치 전체를 지정) — 필요해지면 `Mode.Layout` 으로 넓힌다.
- 대기 제어: 백그라운드 작업이 끝날 때까지 턴을 쓰지 않고 기다렸다가 자동으로 다시 시작하기, 주기 실행(`/loop`·heartbeat) — R28 에서 하지 않기로 했다.

## 6. 실행 가능한 예시

`examples/inject` (스펙 `examples/inject/SPEC.md`): 기본 세트 위에 `example` 세트 하나를 겹친다.

- `context.inject`: 매 턴 `End every reply with the line: -- checked by example` 를 덧붙인다 (저장되지 않음)
- `tool.before`: `rm -rf` 가 든 bash 호출을 `blocked by example: rm -rf` 로 막고, 나머지는 `ctx.Next(call)` 로 아래 층에 넘긴다 (기본 검사 유지)

```sh
cd examples/inject && go run . "질문"          # 플래그: -model provider/id, -cwd, -config
```

설정·키는 dr 과 같은 `~/.dynamic_react/config.json`·`auth.json` 을 쓴다. 테스트는 httptest 가짜 서버로 두 효과와 base 검사 유지를 확인한다. deepseek(`deepseek/deepseek-flash`)로 실제 확인한 결과는 `docs/HANDOFF-impl-07.md` 에 있다.

| 예시 | 보여 주는 것 | 실행 |
|---|---|---|
| `examples/inject` | 매 턴 규칙 주입 + `rm -rf` 차단 (위 설명) | `cd examples/inject && go run . "질문"` |
| `examples/verify` | 변경 뒤 끝낼 때 셸 명령으로 검증 (`sets.Verify`) | `cd examples/verify && go run . -check "go test ./..." "질문"` (`-i` 대화형) |
| `examples/modes` | goal·plan 모드 (5장) | `cd examples/modes && go run . [-check "go test ./..."] [-goal-turns N]` 후 `/mode goal <목표>`, `/mode plan <과제>`, `/modes`, `/mode off` |

## 7. 범위 밖

- 새 세트를 설정 파일로 등록하기 — 세트는 Go 코드다 (D12). 설정 `sets` 는 이미 등록된 세트의 켜짐·순서만 정한다. `dr` 에 등록된 세트는 `DefaultSets` 뿐이고, 다른 세트는 자기 프로그램에서 `agent.New` 를 부른다.
- run 도중의 세트 교체는 모드(5장)로만 한다. 배치(`/set`, `Arrange`)는 여전히 다음 run 부터다.
- 실행 중 부품 교체, 프로젝트별 배치, Bind 시점의 `ctx.Next` 검사 — R26 비목표.
- run 이 끝난 뒤 다음 run 을 언제 시작할지 정하는 주입점(대기 제어) — R28 에서 하지 않기로 했다.
- 사용자 도구에 패턴 규칙의 비교 값을 꺼내는 함수 붙이기 — 필요해지면 추가.
