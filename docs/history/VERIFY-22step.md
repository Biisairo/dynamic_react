# 기능 재현 검증 — 22 step 기준

07 문서 8장(세 에이전트 기능 재현 표)을 현재 스펙(core v0.2 의 22 step·신호·허용 표, inject v0.1 의 조회 그룹)으로 다시 대응시킨 결과. 07 문서의 directive / middleware / strategy 구분은 없어졌고, 전부 "step 함수 + 신호"로 표현한다.

판정: ● 표현 가능 · △ 표현 가능하나 제약 있음 · ✕ 표현 불가

| # | 기능 (출처) | step · 신호 | 판정 | 비고 |
|---|---|---|---|---|
| 1 | 임계치 압축 (pi, Hermes, omp) | 3 context.maintain → `[rewrite, next]` | ● | sets resilient ST-FR-05 |
| 2 | 턴 중 압축 (omp) | 3 (도구 라운드마다 20 → CT → 2 → 3) | ● | 모델 호출 전마다 판정됨 |
| 3 | 선행 요약 (omp) | 3 에서 밴드 진입 시 함수가 백그라운드 요약 시작, 임계치에서 준비된 결과로 `rewrite` | △ | 백그라운드 작업은 core 밖. `ctx.State()` 쓰기는 dispatch 안에서만 반영되므로 결과는 세트 자체 메모리에 **RunID 로 구분해** 둬야 한다 (Bind 결과는 run 간 공유) |
| 4 | overflow 복구 + 1회 (pi) | 12 `[rewrite, rebuild]`, 13(silent overflow·length) 같음. 횟수는 State 또는 `SignalCount` | ● | R3 |
| 5 | context promotion (omp) | 12·13 → `reselect`, 8 이 State 플래그로 큰 모델 선택 | ● | |
| 6 | 에러 분류 + 백오프 (Hermes) | 12 → 대기 후 `recall` | ● | 대기는 함수 안, ctx 취소 준수 (ST-FR-08) |
| 7 | fallback provider 체인 (Hermes) | 12 → `reselect`, 8 이 State 의 순번으로 다음 provider | ● | |
| 8 | 멈춤 nudge (Hermes) | 20 `[inject_input(nudge), continue_turn]` | ● | 13 에서는 불가 (응답 저장 전) — 허용 표 근거 |
| 9 | Harmony 누출 재샘플링 (omp) | 10 에서 감지 → `recall`, 상한은 `SignalCount(recall, reason)` | ● | |
| 10 | TTSR (omp) | 10 에서 위반 감지 → `[inject_input(rule), restart_turn]` | ● | LiveInput 없으면 입력함 → 2 가 반영 |
| 11 | live steering (omp) | 10 이 입력함의 새 메시지를 `inject_input` | △ | `inject_input` 이 deferred 되면 입력함에 **중복**으로 들어간다. 함수가 `Capabilities.LiveInput` 을 먼저 보고 없으면 보내지 않아야 한다 |
| 12 | steering / follow-up (pi) | afterTools = 2(다음 턴에 입력함 반영), beforeSettle = 21 → `continue_turn` | ● | sets resilient 21 |
| 13 | 스트림 중 도구 선행 실행 (omp) | 10 `start_tool`, 11 `Discard`, 인자 변경 시 폐기 | ● | CORE-FR-25~27 |
| 14 | shared/exclusive 스케줄링 (omp) | 15 Plan 그룹·MaxParallel | ● | |
| 15 | aborted 결과 채우기 (omp) | 1 run.start, 7 request.finalize (sets 기본) | ● | D9. 07 의 커널 불변식 I1 → 주입 기본값으로 이동 |
| 16 | 시작 시 짝 없는 tool call **실행** (omp) | 1 run.start 함수가 도구를 **직접** 실행하고 결과를 History 에 넣음 | △ | 07 에서 ✕ 였던 항목. 16~18 step(권한·자르기)을 거치지 않는다. 기본값(aborted 채우기)은 ● |
| 17 | deadline (omp) | 3·8·10·13·20 에서 `Elapsed` 로 `stop`/`settle`, 또는 호출자 ctx 기한 → aborted | ● | 16~19 는 stop 불가 → ctx 로 |
| 18 | soft tool requirement (omp) | 15 `skip_tools(error "call X first")` + 6 이 State 로 tool_choice 강제 | ● | |
| 19 | 권한 승인 (Hermes, omp) | 16 `ShortCircuit` | ● | 승인 대기는 함수 안 |
| 20 | 결과 자르기 (pi) | 18 tool.after | ● | |
| 21 | 모델 라우팅 / 가상 모델 (pi) | 8 model.select | ● | |
| 22 | 프롬프트 조립·캐시 마커 (셋 다) | 6 request.build, 7 request.finalize | △ | 07 의 불변식 I3(과거 바이트 불변)은 core 가 강제하지 않는다 (D1). sets 기본값이 보장 (ST-FR-19) |
| 23 | 메모리 주입 (Hermes) | 4 context.inject (또는 첫 턴 2) | ● | 동결은 sets 몫 |
| 24 | 백그라운드 리뷰 (Hermes) | 22 run.settled → 자식 Kernel 을 별도 goroutine 으로 | ● | 22 의 ctx 는 SettleTimeout 기한이므로 분리된 ctx 사용 |
| 25 | 서브에이전트 task (omp, Hermes) | 17 tool.execute 안에서 `core.New(...).Start(...).Wait()` | ● | CR-C2(전역 상태 없음)로 재진입 가능 — core 동시 run 100개 테스트로 확인 |
| 26 | 스킬 `/skill:name` 확장 (pi) | 2 turn.start 가 입력 메시지 변환 | ● | |
| 27 | 턴 수 예산 (Hermes) | 20 → `TurnIndex` 로 `settle`/`stop` | ● | |
| 28 | surfaceProfile (06 문서) | Bind 할 세트 선택 + 6 도구 필터 | ● | |

## 결론

- ✕ 없음. 07 문서의 유일한 ✕(#16)는 △ 로 바뀌었다 (run.start 함수 안에서 직접 실행, 도구 step 우회).
- △ 4건은 모두 **함수 작성 규약**으로 해결된다. core·inject 스펙 변경은 필요 없다:
  - #3: 세트 내부 메모리는 RunID 로 구분 (inject 문서화 대상)
  - #11: deferred 시 입력함 중복 — LiveInput capability 확인 후 보냄 (inject 문서화 대상)
  - #16: 도구 step 을 거치는 실행이 필요해지면 그때 step 추가를 검토
  - #22: 캐시 보호는 기본 세트의 책임

## 완성된 에이전트에서의 표현 (R22)

위 28개는 L1 세트(`inject.FunctionSet`)로 표현한다. impl-06 부터 이 세트를 **완성된 에이전트(`agent.New`)에 그대로 끼울 수 있다**: `agent.Options.Sets func(sets.Deps) []harness.SetEntry` 가 기본 세트(`harness.DefaultSets` = base·require·cache·guard·truncate·resilient, R26)를 받아 층으로 쌓기(`ctx.Next`)·덮기·교체를 하고, 함수가 쓰는 부품(도구·시스템 프롬프트·요약기·세션 저장소·모델 호출)도 `ToolSet`·`System`·`Summarizer`·`Store`·`ModelCaller` 로 바꾼다. 사용법은 [`INJECT.md`](../INJECT.md).

| 표현 경로 | 실제 모델(deepseek)로 확인한 것 |
|---|---|
| #23 류 (4 context.inject 로 매 턴 지시 주입) | `examples/inject` 의 규칙이 모든 답에 반영, 세션에 저장되지 않음 |
| #28 류 (16 tool.before 로 호출 차단 + 기본 검사 유지) | `rm -rf` bash 차단, 그 밖의 bash 는 base 검사를 거쳐 실행 |

사용자 세트가 무엇이든 surface 계층의 승인(Gate)·이벤트는 step 마다 실제로 쓰이는 함수를 감싸 유지된다 (HN-FR-41). base 기본 검사(ST-FR-38)는 base `tool.before` 를 부르지 않으면 빠진다 — △ 와 같은 **함수 작성 규약**이다.

## Hermes 의 상황별 루프 (R27)

Hermes 는 하나의 ReAct 턴 엔진(`agent/conversation_loop.py`) 위에, 턴 안의 stop gate 와 턴 밖의 드라이버(`/goal` 판정, `/loop`, heartbeat, cron)를 붙여 "상황마다 다른 루프"를 만든다. 드라이버는 턴이 끝난 뒤 다음 user 메시지를 입력 큐에 넣는 방식으로 엔진을 다시 부른다. R27 은 이것을 **모드**(세션별 배치 변형, 턴 경계에서 교체)로 표현한다. 근거 분석은 `hermes_study` 의 Hermes 소스(`hermes_cli/goals.py`, `agent/turn_stop_gates.py`, `agent/plan_prompt.py`, `hermes_cli/loops.py`)다.

판정: ● 표현 가능 · △ 표현 가능하나 제약 있음 · ✕ 표현 불가 (이 저장소 범위 밖). "확인" 열은 이 저장소의 테스트로 동작을 확인했는지다.

| # | 기능 (Hermes 위치) | dynamic_react 표현 | 판정 | 확인 |
|---|---|---|---|---|
| 29 | verify-on-stop: 코드 편집 뒤 증거 없이 끝내면 계속 (`turn_stop_gates.py`) | 20 turn.finish `[inject_input(보고), continue_turn]` — `sets.Verify` | ● | sets 테스트 (R24) |
| 30 | kanban 종료 도구 가드: 완료 도구 없이 끝내면 nudge | 20 turn.finish, Transcript 에서 완료 도구 호출 여부 확인 | ● | 구현 없음 (#29 와 같은 형태) |
| 31 | `/goal` 판정 루프: 턴 뒤 judge → done / blocked / continue, 턴 예산 (`goals.py` `evaluate_after_turn`) | 모드 `goal`: Push 세트의 20 turn.finish 가 `ctx.Next()` 의 settle 을 보고 보조 모델(`Deps.Aux` 통로, 역할 `goal_judge`)로 판정 → continue 면 settle 을 빼고 `[inject_input(continuation), continue_turn]`, done·blocked·예산이면 `Modes.Exit` | ● | `examples/modes` `TestGoalLoop`·`TestGoalStops` |
| 32 | `/goal` 품질 게이트: 셸 명령이 실패하면 judge 를 부르지 않음 | 모드 `goal` 의 Push 를 `[verify, goal]` 로 — verify 가 실패하면 settle 을 빼므로 판단기가 판정하지 않음 | ● | `TestGoalCheckFirst` |
| 33 | `/goal` contract·subgoal: 완료 조건을 judge 프롬프트에 넣음 | #31 의 판정 지시에 조건을 넣음. 모드 인자(`Arg`)나 세트 상태로 보관 | ● | 구현 없음 |
| 34 | `/goal` 진행 중 사용자 입력 우선 | 실행 중 입력은 Steer → 입력함 → 다음 턴의 2 turn.start 가 가져감. 판정을 미루려면 20 에서 `Conv().Inbox()` 를 확인 | ● | 미룸 규칙은 구현 없음 (입력은 다음 턴에 합쳐짐) |
| 35 | `/goal` Ctrl+C → 목표 일시 정지 | Abort 로 run 이 aborted. 모드는 예약이 없으면 그대로 남는다. 일시 정지가 필요하면 22 run.settled 에서 `Modes.Exit` | △ | 구현 없음. Hermes 의 pause/resume(턴 예산 초기화)은 모드에 상태가 없어 세트 상태로 따로 둔다 |
| 36 | `/goal` WAIT: 백그라운드 작업이 끝날 때까지 턴을 쓰지 않고 대기 후 자동 재개 | run 밖에서 다음 run 을 시작하는 주체가 필요 | ✕ | R28 에서 하지 않기로 함 |
| 37 | `/plan`: 계획 전용 턴 (`plan_prompt.py`, 프롬프트 지시뿐) | 모드 `plan`: 4 context.inject 지시 + 7 request.finalize 에서 write·edit·bash 제거 + 16 tool.before ShortCircuit, 20 에서 settle 이면 `Modes.Exit` | ● | `TestPlanMode`, 대화형 `TestInteractive`. Hermes 와 달리 실제로 막는다 |
| 38 | 모델이 모드에 들어감 | `mode` 도구 → 예약 → 다음 turn.start 에서 교체 | ● | harness `TestModeToolSwitchesNextTurn` |
| 39 | 칸반 작업 카드 goal 루프: 카드 상태를 먼저 보고 판정, WAIT 는 continue 로 | #31 과 같은 형태. 판단기가 외부 상태(카드)를 먼저 조회 | ● | 구현 없음 |
| 40 | `/loop`·heartbeat: 주기적으로 새 입력 주입 | run 밖 타이머 필요 | ✕ | R28 에서 하지 않기로 함 |
| 41 | cron: 프로세스를 넘는 예약 실행 | 에이전트 밖 스케줄러가 `Agent.Start` 를 부름 | ✕ | 범위 밖 (L3 이상의 일) |
| 42 | MoA: reference 모델들의 답을 aggregator 에 붙임 | 9 model.call 을 감싸는 세트 (`ctx.Next(req)` 전후로 reference 호출) | ● | 구현 없음 |
| 43 | codex app-server: 턴 전체를 외부 에이전트 프로세스에 넘김 | 외부 에이전트를 17 tool.execute 나 9 model.call 안에서 부를 수 있다. 외부 에이전트 안의 도구 호출·승인은 이 하네스의 step 을 거치지 않는다 | △ | 구현 없음 |

### 결론 (R27)

- Hermes 의 루프 차이는 **턴 경계의 제어**에서 나온다. dynamic_react 에서는 20 turn.finish(계속할지)와 모드 교체(다음 턴의 세트 묶음)로 표현되고, core 는 바뀌지 않는다.
- ✕ 는 모두 **run 밖에서 다음 run 을 시작하는 주체**가 필요한 기능이다 (#36, #40, #41). R28 에서 대기 제어를 하지 않기로 했으므로 범위 밖으로 둔다.
- 판정의 근거: Hermes 의 judge 는 작업 모델의 마지막 답 텍스트(최대 4000자)만 본다. dynamic_react 의 판단기는 20 에서 `Conv().Transcript()`·`Tools()` 로 하네스가 관찰한 도구 결과도 볼 수 있다. 다만 examples/modes 의 goal 세트는 Hermes 와 같이 마지막 답만 쓴다.

## 반복 감지와 턴 예산 (R41·R42)

| # | 기능 (출처) | 표현 | 상태 |
|---|---|---|---|
| 44 | 같은 tool call 반복 감지 (oh-my-pi `ToolCallLoopGuard`) | 20 turn.finish 에서 `Tools().Calls()` 로 턴 지문을 만들고, 상태에 연속 수를 둔다. 5턴이면 `inject_input`(지시), 8턴이면 `fail` | ● 구현 (`sets.Repeat`, 주입용). 가짜 모델 종단 테스트와 뮤테이션 검사(36개 중 KILLED 35, 등가 1)로 확인 |
| 27 | 턴 수 예산 (Hermes `IterationBudget`) | 20 에서 사용 턴 수를 세고, 다 쓰면 요약 지시 + 한 턴 더 → `fail`. 요약 턴의 호출은 16 tool.before 에서 막는다 | ● 표현만. 설계는 R42 ② 에 있고 구현하지 않았다 (프로젝트 종료) |

- 두 기능 모두 core·inject 를 바꾸지 않고 세트 하나로 표현된다. 루프 밖 기능이 필요 없으므로 R39 에 맞다.
- 기능을 하나 구현해 보면서 core 규칙을 하나 확인했다. 20 에서 낸 신호 목록의 **마지막**은 전이 신호여야 한다. 그래서 지시(`inject_input`)를 아래 층 신호 앞에 둔다.
