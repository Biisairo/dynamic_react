# 주입 가이드 (R22)

완성된 에이전트(`L2_harness/agent`)를 core(L0, 22 step 루프)만 고정한 채 튜닝하는 방법이다. L1 을 포크하지 않고 `agent.New` 를 부를 때 Go 함수로 바꾼다. 근거는 `docs/DECISIONS.md` D12·R22, 요구사항은 `L2_harness/SPEC.md` 3.6c(HN-FR-37~42)와 `L2_harness/agent/SPEC.md` 3.14(AG-FR-40~43)다.

## 1. 무엇을 바꿀 수 있나

모든 필드는 **기본값을 받아 바꾼 값을 돌려주는 함수**다. nil 이면 기본값을 그대로 쓴다. 기본값을 받으므로 교체(받은 값을 버림)와 감싸기(받은 값을 안에서 부름)를 같은 방법으로 한다. 각 함수는 `New` 안에서 한 번만 불린다.

| `agent.Options` 필드 | 기본값 | 결과가 nil·빈 값이면 |
|---|---|---|
| `Sets func(d sets.Deps) []inject.FunctionSet` | `harness.DefaultSets(d)` = `[sets.Base(d), sets.Resilient(d)]` | 빈 step 이 생겨 `Bind` 에러 |
| `ToolSet func(def []sets.Tool) []sets.Tool` | `tools.All(env)` = read·bash·edit·write | 도구 없는 에이전트 |
| `System func(def []contract.Block) []contract.Block` | 기본 시스템 프롬프트 (preamble·tools·rules·AGENTS.md·skills·cwd) | 시스템 프롬프트 없음 |
| `Summarizer func(def sets.Summarizer) sets.Summarizer` | pi 요약 프롬프트를 쓰는 요약기 | 자동 압축 끔, `Compact` 는 에러 |
| `Store func(def harness.Store) harness.Store` | `<sessionDir>/<id>.jsonl` JSONL 저장소 | `New` 에러 |
| `ModelCaller func(def sets.ModelCaller) sets.ModelCaller` | adapters Client | `New` 에러 |

harness 를 직접 쓸 때는 `harness.Options` 의 같은 이름 필드(ModelCaller 제외, 모델은 `Options.Model` 값)를 쓴다.

### 만드는 순서 (HN-FR-38)

```text
① ToolSet(tools.All(env))          → registry (같은 이름은 뒤의 것이 앞 자리를 덮음)
② Store(session.New(env, dir))     → 저장소
③ System(기본 조립(①))              → 시스템 프롬프트
④ Summarizer(summary.New(model))   → 요약기 (OnEvent 가 있으면 compaction 이벤트로 감쌈)
⑤ Sets(Deps{Model, Tools: ①, Store: ②, System: ③, Summarizer: ④, Sleep, Now, NewID})
   → surface 래핑(승인·이벤트) → inject.BindWith(StateStore: ②)
```

`Sets` 가 받는 `Deps` 에는 ①~④ 의 결과가 들어 있다. 그래서 사용자 세트도 같은 도구·저장소·요약기를 쓴다.

## 2. 세트 다루기

`Bind` 는 step 마다 **nil 이 아닌 마지막 함수**를 고른다 (IN-FR-19). 따라서:

| 하려는 것 | 방법 |
|---|---|
| step 하나를 덮기 | 그 step 만 채운 세트를 `DefaultSets(d)` 뒤에 붙인다 |
| 기본 함수를 감싸기 | 기본 생성자(`sets.Base<Step>(d)`, `sets.Resilient<Step>(d)`)를 받아 안에서 부른다. 조합 함수는 `inject/combine` 의 `Wrap`·`Concat` 등 |
| 기본 세트를 고치기 | `b := sets.Base(d); b.ToolBefore = …` 처럼 원소를 고쳐 목록에 넣는다 |
| 전부 바꾸기 | `DefaultSets` 를 쓰지 않고 22 step 을 모두 채운다 |

세트 이름은 상태 네임스페이스다 (IN-FR-23). 같은 이름을 두 번 넣으면 `Bind` 에러이고, 기본 세트의 상태(`base` 의 `edits.saved`·`inbox.consumed` 등)에 기대는 함수를 다른 이름의 세트로 옮기면 상태가 이어지지 않는다.

```go
a, err := agent.New(ctx, agent.Options{
    Sets: func(d sets.Deps) []inject.FunctionSet {
        base := sets.BaseToolBefore(d)
        mine := inject.FunctionSet{Name: "mine",
            // 4 context.inject: 이번 턴의 요청에만 붙고 세션에 저장되지 않는다
            ContextInject: func(ctx *inject.ContextInjectCtx) ([]contract.Message, []inject.ContextInjectSignal, error) {
                return []contract.Message{rule(d.Now())}, nil, nil
            },
            // 16 tool.before: 막을 호출은 ShortCircuit, 나머지는 base 검사로
            ToolBefore: func(ctx *inject.ToolBeforeCtx, c contract.ToolCall) (core.BeforeOut, []inject.ToolBeforeSignal, error) {
                if blocked(c) {
                    return core.BeforeOut{Call: c, ShortCircuit: &contract.ToolResult{CallID: c.ID, Status: contract.StatusError,
                        Content: []contract.Block{{Kind: contract.BlockText, Text: "blocked"}}}}, nil, nil
                }
                return base(ctx, c)
            }}
        return append(harness.DefaultSets(d), mine)
    },
})
```

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
| 승인 (permissions 규칙, Approver, deny) | **항상 유지.** surface 래핑이 step 마다 실제로 쓰이는 `tool.before` 바깥을 감싼다 (HN-FR-41) |
| 이벤트 (OnEvent: message·tool·turn·run_end 등) | **항상 유지.** 같은 방식 |
| base 기본 검사 (없는 도구, 인자 스키마, ST-FR-38) | `tool.before` 를 덮으면서 `sets.BaseToolBefore(d)` 를 부르지 않으면 **빠진다.** 이때 승인 Gate 는 검사 없이 그 호출에 불린다. 사용자 책임 |
| Steer 의 남은 도구 건너뛰기 | `base` 세트의 공개 상태 `inbox.consumed` 를 읽는다. `base` 를 빼고 그 상태를 갱신하지 않으면 Steer 한 번 뒤의 도구 호출이 계속 skipped 된다 (AG-FR-42) |
| 도구 상한·설정 `agent.tools`·이름 규칙 | `ToolSet` 적용 뒤 registry 기준. 추가한 도구도 대상이다 |
| 패턴 규칙 `tool(pattern)` | `bash`·`read`·`write`·`edit`·`grep`·`find`·`ls` 이름에만 쓴다 (추가 도구에 쓰면 `New` 에러). 비교 값은 인자 `command`/`path` 에서 꺼낸다 — 같은 이름으로 바꾼 도구에 그 인자가 없으면 패턴 규칙(deny 포함)이 **맞지 않는다.** 이름 규칙(`bash`)은 맞는다 |

## 5. 실행 가능한 예시

`examples/inject` (스펙 `examples/inject/SPEC.md`): 기본 세트 위에 `example` 세트 하나를 겹친다.

- `context.inject`: 매 턴 `End every reply with the line: -- checked by example` 를 덧붙인다 (저장되지 않음)
- `tool.before`: `rm -rf` 가 든 bash 호출을 `blocked by example: rm -rf` 로 막고, 나머지는 `sets.BaseToolBefore` 로 넘긴다 (기본 검사 유지)

```sh
cd examples/inject && go run . "질문"          # 플래그: -model provider/id, -cwd, -config
```

설정·키는 dr 과 같은 `~/.dynamic_react/config.json`·`auth.json` 을 쓴다. 테스트는 httptest 가짜 서버로 두 효과와 base 검사 유지를 확인한다. deepseek(`deepseek/deepseek-flash`)로 실제 확인한 결과는 `docs/HANDOFF-impl-07.md` 에 있다.

## 6. 범위 밖

- 설정 파일로 세트를 고르기 — 세트는 Go 코드다 (D12). 이미 빌드된 `dr`·`dr-print` 에는 끼울 수 없고, 자기 프로그램에서 `agent.New` 를 부른다.
- 실행 중(run 도중·run 사이)에 세트·부품 바꾸기 — 뒤로 미룸 (R22 비목표).
- 사용자 도구에 패턴 규칙의 비교 값을 꺼내는 함수 붙이기 — 필요해지면 추가.
