# 인계: impl-09 — R27 세션 모드 구현과 R28 결정

## 착수 블록 (다음 세션의 지시)

`dynamic_react` 에서 진행한다. **정해진 다음 작업은 없다.** 먼저 사용자에게 아래 "다음 작업 후보" 중 무엇을 할지 묻는다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다. 직전 커밋은 이 문서를 포함한 문서 정리 커밋이고, 그 앞이 `9177f68`(R27 구현)·`7703fda`(R27 스펙)다.
2. 먼저 읽을 것 (이 순서로):
   - `docs/DECISIONS.md` 의 **R27**·**R28** (마지막 두 행)
   - `docs/INJECT.md` **5장** (모드 사용법)
   - `L2_harness/SPEC.md` **3.6e** (HN-FR-55~72)
   - `docs/VERIFY-22step.md` 끝의 "Hermes 의 상황별 루프 (R27)" 표
3. 작업을 고르면 R26·R27 과 같은 순서로 한다: 인터뷰(질문은 한 번에 하나, 권장안 포함) → DECISIONS 행 → 모듈 SPEC → 테스트(RED 확인) → 구현(GREEN) → 검증 절차 → 사용자 확인 후 커밋.

### 다음 작업 후보

| 후보 | 내용 | 비고 |
|---|---|---|
| 실제 모델 시연 | `examples/modes` 를 실제 모델로 돌린다. 임시 Go 모듈에서 `-check "go test ./..."` + `/mode goal 테스트를 통과시켜`, 그리고 `/mode plan …` | 외부 API 호출이므로 사용자 확인 후. 방법은 아래 "배운 것"의 실제 모델 항목 |
| 뮤테이션 검사 | R27 새 코드(harness `mode.go` 의 `decide`·`applyPending`·`runSwitch`)의 핵심 분기 | 이번 세션은 커버리지만 확인했다 |
| 판단기 개선 | goal 판단기가 마지막 답 텍스트 대신 하네스가 관찰한 도구 결과(`Conv().Transcript()`)로 판정 | examples 범위. VERIFY #31 결론 참고 |
| 모드 넓히기 | 전체 배치형 모드(`Mode.Layout`), 모드 겹치기 | R27 비목표. 사용자가 원할 때만 |
| impl-08 이전 미결 | 아래 "남은 미결" 표 | |

**하지 말 것**: run 드라이버(대기 제어 — WAIT 자동 재개·`/loop`·heartbeat)를 다시 꺼내지 않는다 (R28, 사용자가 범위에서 뺌). core(L0) 수정. 구독 계정 로그인 구현. 환경변수 읽기 추가(R21). L3 가 설정 파일을 직접 읽거나 쓰기(R17). 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃.

## impl-09 에서 한 일

### 배경

사용자는 `hermes_study` 에서 Hermes 에이전트를 분석했다. 이전 결론은 "Hermes 는 결국 ReAct 에 도구를 확장한 것"이었다. 다시 분석한 결과는 이렇다: 실행 엔진은 ReAct 턴 하나다. 하지만 턴 경계에 붙은 제어기(턴 안의 stop gate, 턴 뒤의 `/goal` judge 루프 등)가 상황마다 다른 루프를 만든다. 사용자는 이것을 dynamic_react 에서 "런타임에 세트를 바꾸고, 도구로 예약하면 다음 턴부터 바뀌게" 만들자고 했다. 그 결과가 R27 이다.

### 커밋

| 커밋 | 내용 |
|---|---|
| `7703fda` | **R27 스펙**: inject v0.5, sets v0.8, harness v1.4, agent v0.9, terminal v0.6, examples/modes v0.1, DECISIONS R27 |
| `9177f68` | **R27 구현**: 아래 표. 14개 모듈 `-race` 테스트 통과 |
| (이 문서의 커밋) | R28 결정, `INJECT.md` 5장(모드), `VERIFY-22step.md` R27 절, terminal `go.mod` tidy, 이 인계 문서 |

### R27 구현 내용

| 모듈 | 추가한 것 |
|---|---|
| inject v0.5 | `RunTable`, `NewRunTable`, `BindOptions.Runs`. 같은 표로 Bind 한 Dispatcher 들이 run 상태를 공유한다 |
| sets v0.8 | 설정 `aux.<role>`, `AuxModel`, `Complete`(단발 호출), `ModeState`, `ModeSwitcher{Current, Exit}`, `ModesOf`, `ErrNoModes`, `Deps.Modes` |
| harness v1.4 | `Mode{Push, Off}`, `Options.Modes`, 모드 검사, 모드 배치 계산, 공유 RunTable 로 기본·모드 배치를 함께 Bind, run 마다 만드는 스위처, 권한 표, 세션 상태 `harness.mode`, `mode` 도구, `mode` 이벤트, `Complete` |
| agent v0.9 | `Options.Modes`, `Modes`·`ModeStatus`·`EnterMode`·`ExitMode`·`Complete`, 설정 `agent.aux` |
| terminal v0.6 | `/modes`, `/mode <name> [arg]`, `/mode off`(대기 중만), `mode` 이벤트 줄 |
| examples/modes | goal 모드(`-check` 검증 → 보조 모델 판정 → 계속 또는 이탈), plan 모드(write·edit·bash 실제 차단, run 끝에서 이탈), 대화형 진입점 |

### 구현 중 고친 스펙

| 위치 | 이전 | 새 내용 | 이유 |
|---|---|---|---|
| sets ST-FR-57 | `Deps.Modes` 가 nil 이면 `filled()` 가 채움 | 세트는 `ModesOf(d)` 로 꺼냄 (`filled()` 도 같은 값) | `filled()` 는 sets 내부 생성자만 쓴다. 사용자 세트에는 적용되지 않아 nil 이 된다 |
| harness HN-FR-72 | `Lookup` 결과로 요청 모델을 채움 | Provider·ID 는 설정값, `Lookup` 에서는 API 만 | 테스트용 가짜 `Lookup` 이 인자와 무관한 Ref 를 돌려줘 aux 모델이 본 모델로 덮였다. 실제 adapters 에서도 설정값이 기준이어야 한다 |

### R27 인터뷰 기록 (사용자 답 그대로)

| # | 질문 | 사용자 답 |
|---|---|---|
| 0 | 착안 | "런타임에 유동적으로 set 을 바꿀 수 있게 만들고, 그 시점은 도구로 예약을 하면 다음턴부터 바뀌도록 한 뒤 셋을 바꿀 수 있는 도구를 만들면" |
| 1 | 범위 | 메커니즘만 (goal·plan 세트는 examples 로) |
| 2 | 전환 권한 | "진입은 모델, 혹은 사용자 명령어로, 이탈은 각자 판단기에 따라, 혹은 사용자 강제." |
| 3 | 모드 형태 | "일단 1번(기본 배치 위 덧씌우기)으로 하되 2번(전체 배치)도 고려" |
| 4 | 영속성 | 세션 상태, 이탈 전까지 유지 |
| 5 | 보조 모델 | 역할별 보조 모델 |
| 6 | 스펙 검토 | "좋아 진행" |
| 7 | 구현 커밋 | "좋아 커밋" |

### R28 인터뷰 기록

| # | 질문 | 사용자 답 |
|---|---|---|
| 1 | run 드라이버 구조 | a: run 연쇄 + 대기 |
| 2 | 대기 중 사용자 입력 | (질문을 거절) "대기를 컨트롤 하는건 헤르메스 뿐이잖아. 이건 안해도 될거같네" → R28 을 하지 않기로 기록 |

## 현재 코드 구조 (R27 이 바꾼 곳)

| 위치 | 내용 |
|---|---|
| `L1_inject/inject/dispatcher.go` 36 `RunTable` | `get`·`put`·`drop`. `runFor`(182)가 표를 쓴다. `run.start` 에서만 StateStore 로 복원하고, `run.settled` 뒤 지운다 |
| `L1_inject/sets/aux.go` | `AuxModel`(11), `Complete`(19), `noModes`, `ModesOf`(66) |
| `L1_inject/sets/config.go` | `Config.Aux`, `rawConfig.Aux`, `ParseConfig` 의 aux 검사 |
| `L2_harness/harness/mode.go` | 정의·검사 `checkModes`(67), 배치 `modeLayout`(104), 묶음 `bundle`·`bindAll`(143), 상태 `modeTable`·`modeLocked`(205, 처음 읽을 때 Store 로드 + 미등록 모드 고침)·`saveMode`(248), 요청 `RequestMode`(286)·`decide`(309, 권한 표), 적용 `applyPending`(338), 스위처 `runSwitch.Dispatch`(372, `turn.start` 직전에만 교체), `mode` 도구 `modeTool`(396), 세트용 `modeSwitcher`(438), `Complete`(456) |
| `L2_harness/harness/harness.go` 194 `Start` | run ctx 에 sessionID 를 싣고, 예약을 적용하고, run 마다 `runSwitch` 와 `core.New(sw, opts)` 를 만든다. `Agent` 의 `kernel`·`disp` 필드는 없어졌고 `bundle`·`runs`·`modes`·`mt` 가 있다 |
| `L2_harness/harness/layout.go` | `bind` 가 `Runs: a.runs` 로 Bind, `Arrange` 가 `bindAll` 로 모드 배치까지 다시 묶음, `checkEntries` 가 예약어 `harness.mode` 를 거부 |
| `L2_harness/agent/mode.go` | harness 모드 API 를 감싼 얇은 층 |
| `L3_ui/surfaces/terminal/modes.go` | `showModes`, `modeCommand`, `modeLine`. `terminal.go` `command` 와 `helpText`, `render.go` `event` 에 연결 |
| `examples/modes` | `goal.go`(`GoalSet`, `judge`, 판정 지시 `judgeSystem`), `plan.go`(`PlanSet`), `main.go`(`config.sets`·`config.modes`, `runWith`) |

## 이 세션이 값을 치르고 배운 것

- **inject 의 run 상태는 Dispatcher 마다 따로였다.** `run.start` 에서만 복원하므로 run 도중 다른 Bind 결과로 dispatch 하면 빈 상태가 새로 생겼다. 그래서 `RunTable` 이 필요했다. 스펙을 쓰기 전에 `dispatcher.go` `runFor` 를 읽어서 찾았다.
- **`core.Kernel` 은 Dispatcher 와 옵션만 가진 값이다.** run 마다 `core.New` 를 불러도 비용이 거의 없다. 그래서 run 마다 스위처를 새로 만들고 RunID 를 키로 하는 표를 두지 않았다.
- **사용자 세트가 받는 `Deps` 는 채워지지 않는다.** `filled()` 는 sets 내부 생성자만 쓴다. agent 는 `NewID` 를 넘기지 않으므로 `Deps.NewID` 가 nil 이다. 사용자 세트는 직접 기본값을 둔다 (examples/modes `newID`).
- **harness 테스트의 가짜 `Lookup` 은 인자와 무관하게 `p/m` 을 돌려준다.** 모델 Ref 를 `Lookup` 결과로 덮으면 테스트가 aux 모델을 확인하지 못한다.
- 테스트 헬퍼 이름이 이미 있을 수 있다. harness `textModel` 은 `inject_test.go` 에 있었다.
- 이름들: settle 신호 종류는 `core.SigSettle`. 에러 분류는 `contract.ClassRateLimit` 형태. `*contract.CallError` 는 `error` 를 구현한다.
- examples 테스트의 메시지 검색 헬퍼는 tool_result 의 `Result.Content` 도 봐야 한다. 처음에는 `Content` 만 봐서 "blocked in plan mode" 를 놓쳤다.
- terminal 테스트 환경 `env` 에 `mod func(*agent.Options)` 를 더했다. 테스트마다 agent 옵션(모드 등)을 넣을 때 쓴다.
- run 시작 전에 적용된 모드의 이벤트는 RunID 가 "" 이다. 터미널에서는 사용자 입력 줄 다음, 응답 앞에 그려진다.
- 모듈 단독 `GOWORK=off go mod tidy` 를 커밋 전에 돌리지 않아 terminal `go.mod` 가 어긋난 채 커밋됐다 (테스트가 inject·sets 를 직접 import). 문서 커밋에서 고쳤다. **커밋 전에 검증 절차 전체를 돌린다.**
- impl-08 의 "배운 것"은 그대로 유효하다 (Transcript 는 History 로 시작, `TurnIndex` 를 첫 턴 판별에 쓰지 말 것, dmctl 입력은 bracketed paste, macOS 에 `timeout` 없음, 실제 모델 확인 절차 등). 필요하면 `docs/HANDOFF-impl-08.md` 를 본다.

## 검증 절차

이번 세션에서 한 것:

- 모듈 14개 전부: `gofmt -l`, `go vet ./...`, `go test -race ./...`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test ./...`. 모듈 목록은 `go work edit -json` 의 `Use`.
- harness 모드 테스트 `-count=5` 반복.
- 테스트 밖 코드에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 없음.
- 새 코드 커버리지: harness `mode.go` 66.7~100%, sets `aux.go` 78.6~100%, examples `goal.go`·`plan.go` 66.7~100%, terminal `modes.go` 88.9~100%. 덜 덮인 곳은 오류 경로다 (`mode` 도구의 잘못된 인자·세션 없음, Store 로드 실패, done 메시지 없는 스트림, nil Agent).

이번 세션에서 하지 않은 것:

- 뮤테이션 검사.
- 실제 모델 확인 (가짜 모델·httptest 로만 검증).

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 구현 중 정한 세부는 스펙에 반영하고 보고할 때 알린다. 스펙에 없는 Go 표현(새 export)은 구현 전에 스펙에 먼저 넣는다.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`. 외부 의존: x/text(harness·terminal), x/term(terminal). 새 외부 의존은 사용자가 정한다.
- 키를 코드·대화·문서·이미지에 노출하지 않는다. 키는 `<home>/.dynamic_react/auth.json`(0600)에만 둔다. 구독 요금제 엔드포인트·구독 OAuth 금지. 환경변수를 읽지 않는다 (예외: 홈 디렉터리, 자식 프로세스 상속, 테스트 전용 `DR_E2E_*`).
- 사용자는 레이어 책임을 엄격히 본다. L0 core 는 기본값·순서를 모른다. 목록·검증·저장은 L2. L3 는 입출력만. 모드 목록·권한·적용 시점도 L2(harness)에 있고, L1 은 세트용 인터페이스만 갖는다.

## 사용자 결정 중 유효한 것

- D1~D16, R1~R28 (R10 폐기, R14 는 R17 로 정정, R22 의 비목표 두 개는 R26 이 뒤집음, R26 비목표 "run 도중 교체"는 R27 이 모드 경로에 한해 뒤집음, R28 은 "하지 않음" 결정).
- 이 저장소의 LICENSE 는 미정이다 (묻지 않았으면 손대지 말 것).

## 남은 미결

이 표는 impl-09 시점의 기록이다. 최신 개선 목록은 [`GOALS.md`](GOALS.md)를 본다.

| 위치 | 내용 |
|---|---|
| R27 | 실제 모델 시연, 뮤테이션 검사, 전체 배치형 모드(`Mode.Layout`), 보조 모델의 thinking·max tokens 설정(지금은 모델만), 터미널 실행 중 `/mode off`(지금은 대기 중만, agent API 는 실행 중에도 받음) |
| examples/modes | goal 판단기의 근거를 하네스가 관찰한 도구 결과로 넓히기, 판정 프롬프트 품질 튜닝, 진행 중 사용자 입력이 있을 때 판정 미루기(VERIFY #34) |
| verify | 검증 보고가 사용자 지시보다 우선되는 문구 — 바꿀지 사용자가 정하지 않았다 |
| dr | 등록 세트는 `DefaultSets` 프리셋뿐이고 모드는 없다 (인자가 필요한 세트·모드는 자기 main 에서) |
| 세트 후보 | 예산 가드(측정 기준 미정: 누적 토큰 vs 현재 컨텍스트 크기), 반복 감지, 모델 라우팅 |
| R26 비목표 | 실행 중 부품 교체, 프로젝트별 배치, Bind 시점 Next 검사 |
| 기타 (impl-07 이전부터) | 저장소 LICENSE, Anthropic·OpenAI 방언 실제 응답 검증, overflow 복구 실제 모델 확인, agent 사용자 도구의 패턴 비교 값(`Target func`) |
