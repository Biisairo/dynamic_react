# 인계: impl-11 — 뮤테이션 검사, 모드·goal 판정 개선, R39 레이어 원칙

## 착수 블록 (다음 세션의 지시)

`dynamic_react` 에서 진행한다. 남은 일은 모두 [`GOALS.md`](GOALS.md) 에 있다. 이 문서에는 GOALS 의 id 만 적는다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다. 마지막 커밋은 이 문서의 커밋이고, 그 앞은 `6d6ad81` 이다.
2. 먼저 읽을 것 (이 순서로):
   - `docs/GOALS.md` 전체. 열린 항목은 4~8장에 있다.
   - `docs/DECISIONS.md` 의 **R34~R40** (마지막 일곱 행). 특히 **R39** 는 앞으로 모든 작업에 걸리는 레이어 원칙이다.
   - `L1_inject/sets/SPEC.md` 1.1·2.3 (R39 가 바꾼 sets 의 책임과 `Deps`)
   - `L2_harness/SPEC.md` HN-FR-72·84~87 (보조 모델 호출과 sets 에서 옮겨 온 것)
3. 착수 전에 아래 "다음 작업" 순서를 사용자에게 한 번 확인한다.
4. 작업 순서는 지금까지와 같다: 인터뷰(질문은 한 번에 하나, 권장안 포함) → DECISIONS 행 → 모듈 SPEC → 테스트(RED 확인) → 구현(GREEN) → 검증 절차 → 뮤테이션 → GOALS 갱신 → 커밋. 사용자는 방향을 함께 정하길 원한다. 권장안이 있어도 혼자 진행하지 말고 결정마다 묻는다.

### 다음 작업 (제안 순서, 사용자 확인 필요)

| 순서 | GOALS id | 내용 | 비고 |
|---|---|---|---|
| 1 | ST-2 | 반복 감지 세트 | 외부 호출 없음. 설계 결정이 많다 — 인터뷰부터. 감지와 개입(지시 주입·중단)은 루프 제어이므로 세트다 (R39) |
| 2 | ST-1 | 예산 가드 | 측정 기준(누적 토큰 또는 현재 컨텍스트 크기)부터 정한다 |
| 3 | ST-4 | `dr` 에 모드 등록 | 결정 대기 |
| 4 | DC-1 | verify 세트의 "검증 보고가 사용자 지시보다 우선" 문구 | 결정 대기 |
| — | ST-3 | 모델 라우팅 | 사용자가 고르면 |
| — | VF-1~3, EX-2 | 실제 모델·API 키가 필요한 검증 | **외부 호출이므로 사용자 확인 후에만** |

**하지 말 것**: 사용자가 고르지 않은 GOALS 항목에 손대기. run 드라이버(R28). core 루프(L0 core) 수정. L0 contract 는 사용자 승인으로만 바꾼다. 구독 계정 로그인 구현. 환경변수 읽기 추가(R21). L3 가 설정 파일을 직접 읽거나 쓰기(R17). **sets 에 루프 밖 로직 넣기(R39)**. 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃. DC-2(LICENSE)는 묻지 않으면 손대지 않는다.

## impl-11 에서 한 일

### 커밋

| 커밋 | GOALS | 내용 |
|---|---|---|
| `b3db412` | VF-7 해소 | R30~R33 뮤테이션 77개 중 KILLED 76, 등가 1(agent `ToolNames` 의 `err != nil` — 다른 모듈의 구현 세부에 기대지 않으려고 둠). 테스트 보강. stats `add` 의 등가 분기(`hasCeiling`) 제거 |
| `2fe366e`, `004fcbf` | MD-1 해소 | R27 모드 코드(`decide`·`applyPending`·`runSwitch.Dispatch`·`pick`) 뮤테이션 34/34. harness v1.7: "`Modes` 가 빈 Agent 의 run 은 모드 상태를 읽지 않음" 명시 (HN-FR-65·66) |
| `779b418` | DC-4 해소 | R34: 세션별 메모리 표 정리 정책은 만들지 않음. 서버형 surface 를 만들 때 다시 검토 |
| `9782a11` | VF-6 해소 | terminal `TestRunCompact` 경합의 원인을 찾아 고침 (아래 "배운 것") |
| `0296ae1`, `4610a68` | DC-5 해소 | R35: `PrefixBreak.Kept` 제거 (항상 `At` 과 같았음). harness v1.8, terminal v0.9 |
| `a865e7c`, `697d5c5` | MD-3 해소 | R36: terminal 이 실행 중에도 `/mode off` 를 받는다. 성공 줄 `(다음 턴부터)`. terminal v0.10 |
| `58fca45`, `0ef66c9` | EX-1 하지 않음, EX-3 해소 | R37: 판단기는 마지막 답만 본다 (Hermes·oh-my-pi 와 같음). R38: 판정 턴에 사용자 입력이 대기 중이면 판정을 미룸, goal 진행 중 사용자 메시지를 판정에 추가 기준으로 넘김. examples/modes v0.3 |
| `c507e32`, `2ff2b6f` | MD-2 해소 | R39 레이어 원칙과 sets 전수조사, R40 aux 의 `maxOutputTokens`·`thinking`. sets v0.11, harness v1.9, agent v0.11, examples/modes v0.4 |
| `6d6ad81` | — | MD-3 에서 쓴 `TestRunModes` 의 경합 수정 |

### R39 요약 (가장 중요한 변경)

사용자 원칙: **"실제 해당 loop 를 제어하는 로직을 제외한 모든 로직은 하네스에 있어야 한다. sets 는 나 스스로의 루프만을 제어한다."**

- 세트는 루프 밖 기능을 `Deps` 의 인터페이스로만 받는다: `ModelCaller`·`ToolRegistry`·`Store`·`Summarizer`·`ModeSwitcher`·**`AuxCaller`(새로 추가)**.
- 하네스가 준 통로로 **자기 루프를** 진행하는 것은 루프 제어다. 예: Base 의 저장 시점, Require 의 모델 호출, resilient 의 압축, goal 의 판정·이탈.
- 루프 규칙 함수(`ApplyEdits`·`Compact`·`Estimate*`·`ReserveTokens`·`StableLen`·`CheckArgs`)는 하네스도 쓰지만 sets 에 둔다.
- harness 로 옮긴 것은 넷이다.
  - 보조 모델 규칙과 단발 호출: `Agent.Complete`, `completeOnce`
  - 설정 `aux` 읽기: `harness.ParseAux`. sets `ParseConfig` 는 `aux` 키를 건너뛴다.
  - 도구 레지스트리: 비공개 `registry`
  - 메모리 Store: 테스트 지원 패키지 `harness/storetest`. sets 테스트는 자기 가짜를 쓴다.
  - Store 를 inject StateStore 로 감싸는 연결: 비공개 `stateStoreOf`
- `Deps.Tools`·`Deps.Store` 가 nil 이면 sets 안의 비공개 "없음" 구현을 쓴다. 도구가 없고, 빈 세션을 읽고, 저장하지 않는다.
- 세트가 보조 모델을 부르는 방법: `sets.AuxOf(d).Complete(ctx, ctx.Env().Config(), role, system, msgs)`

### 스펙 버전 (지금)

contract v0.5, core v0.2, inject v0.5, sets v0.11, adapters v0.8, harness v1.9, osenv v0.3, agent v0.11, terminal v0.10, dr v0.3, dr-print v0.2, examples/inject v0.2, examples/verify v0.3, examples/modes v0.4.

### 인터뷰 기록 (사용자 답)

| 주제 | 사용자 답 |
|---|---|
| 작업 순서 | 인계 순서대로 (VF-7 → MD-1 → DC-4), 이어서 VF-6 → DC-5 → MD-3, 다시 EX-1 → EX-3 → MD-2 |
| VF-7 살아남은 변이 | "테스트 보강 + 등가 코드 단순화". A1 은 "두고 등가로 기록" |
| MD-1 S1 | "스펙 명시 + 테스트" |
| DC-4 | "지금은 하지 않음" |
| VF-6 | "테스트가 상태를 직접 봄" (제품 코드 그대로) |
| DC-5 | "필드 제거" |
| MD-3 | 범위 "/mode off 만", 문구 "(다음 턴부터)" |
| EX-1 | "판단기가 도구 기록을 보는게 의미가 있어? 세션기록을 봐야하는거아니야?" → "실제 다른 에이전트들의 동작은?" → 조사 뒤 "하지 않음, EX-3 에서 조건 변경 처리" |
| EX-3 | 미룸 "Hermes 와 같음", 조건 변경 "자동으로 추가 기준으로 넘김", 크기 "총 2000자, 메시지당 500자" |
| MD-2 | 형식 "aux 항목에 필드 추가", 기본값 "지금 동작 유지" → 적용 범위 질문에서 "보조모델 자체가 하네스이다. sets 은 하나의 모델(loop)를 컨트롤하는 함수다" → R39 원칙과 전수조사 지시 |
| R39 | V2 "sets 는 하네스 키를 건너뜀", V1 "이 모양으로"(Deps.Aux), V3 "'없음' 구현으로" |

## 이 세션이 값을 치르고 배운 것

- **다른 에이전트의 동작을 근거로 묻는 경우가 있다.** 사용자는 "실제 다른 에이전트들의 동작은?" 처럼 근거를 요구한다. 로컬 소스가 `../hermes_study/` 에 있다: `hermes-agent`(Hermes, 예: `hermes_cli/goals.py`, `hermes_cli/cli_loops_mixin.py`), `oh-my-pi`, `pi-mono`. 기억으로 답하지 말고 소스를 먼저 본다.
- **레이어 판단은 R39 로 한다.** 새 기능을 설계할 때 "이 로직이 세트의 루프 진행을 정하는가"를 먼저 묻는다. 아니면 harness 에 두고 `Deps` 통로를 만든다 (`Deps.Modes`·`Deps.Aux` 와 같은 형태, nil 이면 "없음" 구현).
- **terminal 테스트에서 "화면에 무엇이 없음"으로 상태를 판정하지 않는다.** `screen.draw` 는 지우기와 그리기를 여러 번의 Write 로 나눠 보낸다. 그래서 지운 직후의 화면에는 상태줄이 잠깐 없다. 대기 상태는 `h.idle()`(`t.st == stIdle` 을 잠금 아래서 봄)로 기다린다.
- **실행 중 동작을 테스트할 때는 요청이 실제로 나간 것을 기다린다.** "● 실행 중" 이 보여도 run 의 첫 `turn.start` 는 아직일 수 있다. 그 틈에 모드를 예약하면 첫 턴에 바로 적용된다. `waitUntil(t, func() bool { return l.count() == N })` 로 기다린다.
- **경합 재현 방법**: 다른 모듈의 `go test -race -count=3 ./...` 를 백그라운드로 돌려 부하를 걸고, 대상 테스트를 `-race -count=40` 으로 여러 묶음 돌린다. 이번에는 묶음 3~4개마다 한 번 실패했다.
- **goal 판정 턴에는 도구 결과가 없다.** settle 이 있는 턴은 모델이 도구 없이 최종 답만 낸 턴이다. 증거는 같은 run 의 앞선 턴들에 있다 (`Transcript()[len(History()):]`).
- **세트가 넣은 메시지와 사용자 입력은 신호 기록으로 구별한다.** 메시지에는 출처 필드가 없다. `ctx.Flow().Signals()` 의 `SigInjectInput` 메시지 ID 가 세트가 넣은 것이다.
- **base 의 입력함 소비 수**는 공개 상태 `base`/`inbox.consumed` 다 (ST-FR-43). 다른 모듈에서는 상수를 직접 두고 읽는다.
- **zsh 는 `--include=*.go` 를 glob 으로 해석해 에러를 낸다.** 이런 grep 은 bash 스크립트 파일로 돌린다.
- **`git stash` 를 쓰지 않는다** (impl-10 경고). 이번에 실수로 한 번 실행했다. stash 내용이 작업 트리와 같은 것을 확인하고 지워서 피해는 없었다.
- **뮤테이션 스크립트**: `/tmp/dr-mut/mut.py` 와 `/tmp/dr-mut/verify.sh` 가 남아 있을 수 있다. 없으면 다시 만든다.
  - mut.py 는 JSON 목록 `[{"dir","file","label","old","new"}]` 를 받는다. 파일을 백업하고, 정확히 1회 매칭되는 곳만 치환하고, `go test -count=1 -timeout 120s ./...` 를 돌린 뒤 복원한다.
  - 결과는 `KILLED`·`SURVIVED`·`BUILD-FAIL`(출력에 `build failed`/`setup failed`)·`BAD-MATCH` 중 하나로 찍는다.
  - **변이가 미사용 변수를 만들면 BUILD-FAIL 이다.** `x && false`, `_ = v` 같은 형태로 다시 쓴다.
  - 돌리는 동안 그 모듈을 편집하지 않는다.
  - 살아남은 변이의 처리는 사용자 방침을 따른다: 테스트 빈틈이면 테스트를 보강하고, 같은 함수 안의 등가 분기는 코드를 줄이고, 다른 모듈의 구현 세부에 기대게 되는 등가는 기록만 한다.
  - 보안 리뷰 훅이 변이 코드(예: 권한 검사 제거)를 경고할 수 있다. 변이는 복원되므로 `git status` 와 원래 줄을 확인하고 답한다.
- **verify.sh** 는 14개 모듈마다 다음을 돌린다: `gofmt -l`, `go vet`, `go test -race`, `GOWORK=off GOTOOLCHAIN=local go mod tidy`(go.mod·go.sum 변경 여부), `GOWORK=off go test`. 마지막에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 이 테스트 밖 코드에 없는지 본다.
- impl-08·09·10 의 "배운 것"은 그대로 유효하다.

## 검증 절차

모듈 14개 전부 (`go work edit -json` 의 `Use`): `gofmt -l`, `go vet ./...`, `go test -race -count=1 ./...`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test -count=1 ./...`. 새 코드 커버리지 확인. 뮤테이션 확인. 테스트 밖 코드에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 없음.

이번 세션의 마지막 상태: 14개 모두 통과.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 구현 중 정한 세부는 스펙에 반영하고 보고할 때 알린다. 스펙에 없는 Go 표현(새 export)은 구현 전에 스펙에 먼저 넣는다. 스펙을 바꾸면 버전·변경 기록·README 의 버전 표기를 함께 고친다.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 새 개선 항목은 `GOALS.md` 에 먼저 넣는다. 항목을 끝내면 상태를 `해소`(또는 `하지 않음`)로 바꾸고 근거를 적는다. 행은 지우지 않는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지. 스펙 커밋과 구현 커밋을 나눈다.
- 동작이 바뀌면 이전 동작 / 새 동작 / 이유를 보고한다.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. `go 1.23.0`. 새 외부 의존은 사용자가 정한다.
- 키를 코드·대화·문서·이미지에 노출하지 않는다. 키는 `<home>/.dynamic_react/auth.json`(0600)에만. 구독 요금제 엔드포인트·구독 OAuth 금지. 환경변수를 읽지 않는다 (예외: 홈 디렉터리, 자식 프로세스 상속, 테스트 전용 `DR_E2E_*`).
- 레이어 책임: L0 core 는 기본값·순서를 모른다. **sets 는 자기 루프의 제어만 갖는다 (R39).** 목록·검증·저장·집계·보조 모델은 L2. L3 는 입출력만.

## 사용자 결정 중 유효한 것

- D1~D16, R1~R40 (R10 폐기, R14 는 R17 로 정정, R22 비목표 두 개는 R26 이 뒤집음, R26 비목표 "run 도중 교체"는 R27 이 모드 경로에 한해 뒤집음, R28·R34·R37 은 "하지 않음", R32 는 문서 안내만, R35 는 `Kept` 제거).
- GOALS CA-5 의 Anthropic 측정은 하지 않는다.
- 저장소 LICENSE 는 미정이다 (묻지 않았으면 손대지 말 것).

## 남은 미결

[`GOALS.md`](GOALS.md) 를 본다. 이 문서는 목록을 복사하지 않는다.
