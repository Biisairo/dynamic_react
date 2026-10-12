# examples/modes — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (D12, R24, R26, R27)
- 의존 명세: `L2_harness/SPEC.md` (3.6e 모드), `L2_harness/agent/SPEC.md` (3.14b), `L1_inject/sets/SPEC.md` (3.2a verify, 3.2b 보조 모델), `examples/verify/SPEC.md` (같은 대화형 형태)
- 사용하는 쪽: 개발자 (모드 메커니즘 사용법 예시)

---

## 1. 소개

### 1.1 목적
R27 모드 메커니즘으로 **goal 모드**(Hermes `/goal` 과 같은 판정 루프)와 **plan 모드**(쓰기 도구를 실제로 막는 계획 전용 턴)를 만드는 방법을 실행 가능한 코드로 보여 준다. 제품 진입점이 아니라 예시다 (R27 범위: "메커니즘만").

### 1.2 범위
- 포함 (모듈 `examples/modes`, 패키지 `main`): goal·plan 세트, 두 모드를 등록한 대화형 진입점
- 제외: 한 번 실행하는 CLI (모드는 여러 메시지에 걸치므로 대화형만), 판정 프롬프트의 품질 튜닝, WAIT(백그라운드 작업이 끝날 때까지 대기 후 자동 재개 — run 드라이버, R28 에서 하지 않기로 함)

---

## 2. 전체 설명

```text
modes-example [-check "<shell command>"] [-attempts N] [-goal-turns N] [-model provider/id] [-cwd dir] [-config file] [-no-color]
```

```text
 main ── run(ctx, args, stdout, stderr) int
          └─ agent.New(ctx, Options{Cwd, ConfigPath, Model, Approver, OnEvent,
               Sets(d)  = DefaultSets(d) + {goal, Off} + {plan, Off} (+ {verify, Off} if -check),
               Modes    = [ goal: Push [verify?, goal], plan: Push [plan] ] })
          └─ terminal (dr 과 같음): /modes, /mode goal <목표>, /mode plan <과제>, /mode off
```

모듈 의존: examples/modes → agent, harness, sets, inject, core, contract, terminal, 표준 라이브러리. go.work 에 포함하고 독립 go.mod(`v0.0.0` + replace, R8)를 갖는다.

### 2.2 제약
- MX-C1. 키 값을 출력하지 않는다. 환경변수를 읽지 않는다 (R21).
- MX-C2. 로직은 `run` 에 두고 `main` 은 `os.Exit(run(signalCtx, os.Args[1:], os.Stdout, os.Stderr))` 만 한다. 테스트는 `runWith(ctx, args, stdout, stderr, mod func(*agent.Options))` 와 dr 과 같은 `isTerminal`·`open` 바꿔 넣기로.
- MX-C3. 판정은 보조 모델 역할 `goal_judge` 다 (설정 `agent.aux.goal_judge`, 없으면 본 모델).

---

## 3. 상세 요구사항

```go
func GoalSet(d sets.Deps, maxTurns int) inject.FunctionSet  // Name "goal"
func PlanSet(d sets.Deps) inject.FunctionSet                // Name "plan"
```

| id | 요구사항 |
|---|---|
| MX-FR-01 | **goal 4 context.inject**: `Deps.Modes.Current(sid)` 의 Arg 가 비지 않으면 임시 메시지 하나 `[goal mode] Goal: <Arg>\nWork toward this goal. When it is complete, say so and show concrete evidence (command output, file contents, test results).` (user 역할, 이번 요청에만) |
| MX-FR-02 | **goal 20 turn.finish**: `ctx.Next()` 의 신호에 settle 이 없으면(에러 포함) 그대로 돌려준다. 있으면: Arg 가 비면 `Modes.Exit("no goal")` 후 그대로. 아니면 판정(MX-FR-03) → `done`: `Exit("goal achieved: <reason>")` 후 그대로 / `blocked`: `Exit("goal blocked: <reason>")` 후 그대로 / `continue`: 상태 `progress = {arg, turns}`(arg 가 지금 Arg 와 다르면 turns 0 부터)의 turns+1 을 저장하고, turns ≥ maxTurns 면 `Exit("goal turn budget <n>/<max>")` 후 그대로, 아니면 `[inject_input(continuation), continue_turn]` (settle 은 뺌, 다른 효과 신호는 유지) |
| MX-FR-03 | 판정 = `sets.Complete(ctx, d.Model, {Model: AuxModel(cfg, "goal_judge"), System: [판정 지시], Messages: [user: "Goal:\n<Arg>\n\nAgent's most recent response:\n<응답 텍스트 앞 4000자>"]})`. 판정 지시 = Hermes `JUDGE_SYSTEM_PROMPT` 의 done/blocked/continue 부분 요약 + `Reply ONLY with one JSON object: {"verdict": "done"|"blocked"|"continue", "reason": "<one sentence>"}`. 응답에서 첫 `{…}` 를 JSON 으로 읽고, 호출 에러·JSON 아님·모르는 verdict 는 `continue`(reason = 그 사유) — Hermes 와 같은 fail-open, 상한은 maxTurns |
| MX-FR-04 | continuation = user 메시지(ID `d.NewID()`, 시각 `d.Now()`) `[Continuing toward your standing goal]\nGoal: <Arg>\nJudge: <reason>\nTake the next concrete step. If the goal is complete, say so with evidence and stop.` |
| MX-FR-05 | **plan 4 context.inject**: `[plan mode] Plan only — do not implement. Investigate with read-only tools and reply with a step-by-step plan (goal, approach, tasks with exact file paths, verification commands, risks).` + Arg 가 있으면 `\nTask: <Arg>` |
| MX-FR-06 | **plan 7 request.finalize**: `ctx.Next(req)` 결과에서 tools 중 `write`·`edit`·`bash` 를 뺀다. **plan 16 tool.before**: 그 세 이름이면 ShortCircuit `{Status: error, text "blocked in plan mode"}`, 아니면 `ctx.Next(call)` |
| MX-FR-07 | **plan 20 turn.finish**: `ctx.Next()` 에 settle 이 있으면 `Modes.Exit("plan done")` 후 그대로 (계획은 한 run) |
| MX-FR-08 | 플래그: `-check`(있으면 verify 세트를 `Off` 로 등록하고 goal 모드 Push 를 `[verify, goal]` — 검증 실패가 판정보다 먼저, Hermes 품질 게이트), `-attempts`(verify MaxAttempts), `-goal-turns`(기본 20), `-model`·`-cwd`·`-config`·`-no-color` 는 examples/verify VX-FR-05 와 같다. 위치 인자 → `modes-example: takes no prompt arguments`, 2 |
| MX-FR-09 | 대화형 실행은 examples/verify VX-FR-05 와 같다 (비TTY → 2, 승인은 터미널, 새 세션, 종료 코드). `Modes` = `[{Name: "goal", Description: "keep working until a judge says the goal is met (arg = goal)", Push}, {Name: "plan", Description: "plan only; write/edit/bash are blocked (arg = task)", Push: ["plan"]}]` |
| MX-FR-10 | 상태 네임스페이스: goal = `goal`(키 `progress`), plan 은 상태 없음 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| MX-FR-01~04 | 가짜 ModelCaller(작업 응답과 판정 응답을 요청 내용으로 구별)로 harness 위 결정적 테스트: `/mode goal X` 상당(EnterMode) → 첫 요청에 `[goal mode] Goal: X` → 판정 continue → 다음 요청에 continuation → 판정 done → `mode` 이벤트 goal → 기본(set) → 다음 run 요청에 goal 주입 없음 / blocked → 이탈 / 판정 JSON 깨짐 → continue / maxTurns 2 → 예산 이탈 / `-check` 실패 → 판정 요청 없이 verify 보고로 계속 |
| MX-FR-05~07 | plan 모드: 요청 tools 에 write·edit·bash 없음·read 있음, 모델이 write 를 불러도 `blocked in plan mode`, settle 뒤 다음 run 은 기본(write 있음) |
| MX-FR-08~09 | 파이프 stdin·stdout + 가짜 `isTerminal`·`open`: `/modes` → 두 줄, `/mode goal …` → 예약 줄 → 메시지 → `── 모드 기본 → goal (user)` … `── 모드 goal → 기본 (set): goal achieved…`. 위치 인자 → 2, 비TTY → 2 |
| 실제 모델 | deepseek 로 수동 시연: 임시 Go 모듈에서 `/mode goal 테스트를 모두 통과시켜` + `-check "go test ./..."` → 여러 턴 후 이탈, `/mode plan …` → write 없이 계획 응답 → 다음 메시지는 기본 |

---

## 5. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.1 | 모드 예시 없음 | 이 모듈 | R27 (범위 "메커니즘만" — goal·plan 은 예시) |
