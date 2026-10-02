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

위 28개는 L1 세트(`inject.FunctionSet`)로 표현한다. impl-06 부터 이 세트를 **완성된 에이전트(`agent.New`)에 그대로 끼울 수 있다**: `agent.Options.Sets func(sets.Deps) []harness.SetEntry` 가 기본 세트(`harness.DefaultSets` = base·require·cache·guard·truncate·resilient, R26)를 받아 층으로 쌓기(`ctx.Next`)·덮기·교체를 하고, 함수가 쓰는 부품(도구·시스템 프롬프트·요약기·세션 저장소·모델 호출)도 `ToolSet`·`System`·`Summarizer`·`Store`·`ModelCaller` 로 바꾼다. 사용법은 [`INJECT.md`](INJECT.md).

| 표현 경로 | 실제 모델(deepseek)로 확인한 것 |
|---|---|
| #23 류 (4 context.inject 로 매 턴 지시 주입) | `examples/inject` 의 규칙이 모든 답에 반영, 세션에 저장되지 않음 |
| #28 류 (16 tool.before 로 호출 차단 + 기본 검사 유지) | `rm -rf` bash 차단, 그 밖의 bash 는 base 검사를 거쳐 실행 |

사용자 세트가 무엇이든 surface 계층의 승인(Gate)·이벤트는 step 마다 실제로 쓰이는 함수를 감싸 유지된다 (HN-FR-41). base 기본 검사(ST-FR-38)는 base `tool.before` 를 부르지 않으면 빠진다 — △ 와 같은 **함수 작성 규약**이다.
