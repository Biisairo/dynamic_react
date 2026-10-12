# 인계: impl-10 — 프롬프트 캐시 (R29~R33)와 GOALS.md

## 착수 블록 (다음 세션의 지시)

`dynamic_react` 에서 진행한다. 남은 일은 모두 [`GOALS.md`](GOALS.md) 에 있다. 이 문서에는 GOALS 의 id 만 적는다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다. 마지막 커밋은 이 문서의 커밋이고, 그 앞은 `957c7a5` 다.
2. 먼저 읽을 것 (이 순서로):
   - `docs/GOALS.md` 전체 (짧다. 1장 목표, 3장 캐시 분석, 4~9장 남은 항목)
   - `docs/DECISIONS.md` 의 **R29~R33** (마지막 다섯 행)
   - `docs/INJECT.md` **5.5a** (캐시 접두 규칙과 `prefix_break` 해석)
3. 정해진 다음 작업은 아래 "다음 작업" 순서다. 착수 전에 사용자에게 순서를 한 번 확인한다.
4. 작업 순서는 지금까지와 같다: 인터뷰(질문은 한 번에 하나, 권장안 포함) → DECISIONS 행 → 모듈 SPEC → 테스트(RED 확인) → 구현(GREEN) → 검증 절차 → GOALS 갱신 → 커밋. 사용자는 방향을 함께 정하길 원한다. 권장안이 있어도 혼자 진행하지 말고 결정마다 묻는다.

### 다음 작업 (사용자가 고른 후보)

| 순서 | GOALS id | 내용 | 비고 |
|---|---|---|---|
| 1 | VF-7 | R30~R33 새 코드의 뮤테이션 검사 | 외부 호출 없음. 방법은 아래 "배운 것"의 뮤테이션 항목 |
| 2 | MD-1 | R27 모드 코드(harness `mode.go` 의 `decide`·`applyPending`·`runSwitch`)의 뮤테이션 검사 | 같은 방법 |
| 3 | DC-4 | 접두 기준표와 사용량 집계를 언제 지울지 (세션 종료 시 삭제, 개수 상한 등) | 결정이 필요하다. 인터뷰부터 |

VF-7 대상 (모두 커버리지는 100% 근처이고 뮤테이션은 안 했다):

| 모듈 | 파일·함수 |
|---|---|
| harness | `stats.go` 의 `record`·`add`·`Stats`, `surface.go` 의 `stream.end` 래핑 |
| agent | `status.go` 의 `ToolNames` |
| terminal | `status.go` 의 `statusLines`·`totalsLine`·`percent`, `render.go` 의 `runEndLine` 캐시 비율 |
| adapters | `chat.go` 의 `chatCacheHints`·`markText`, `profile.go` 의 `CacheControlKind.on`, `client.go` 의 `setCacheKey` |
| sets | `base.go` 의 `Cache` 가 채우는 `CacheKey` |

**하지 말 것**: 사용자가 고르지 않은 GOALS 항목에 손대기. run 드라이버(R28). core 루프(L0 core) 수정. L0 contract 는 사용자 승인으로만 바꾼다 (R33 이 첫 예외). 구독 계정 로그인 구현. 환경변수 읽기 추가(R21). L3 가 설정 파일을 직접 읽거나 쓰기(R17). 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃.

## impl-10 에서 한 일

### 배경

사용자는 "모드를 바꾸면 프롬프트 캐시가 깨진다"는 문제에서 시작했다. 분석 결과, 기준을 **자동 접두 캐시**(OpenAI·DeepSeek·로컬 서버)로 정했다. 요청 = [안정 접두][가변 꼬리]이고, 안정 접두(도구 목록 → system → 저장된 메시지)는 덧붙기만 한다. Anthropic 의 지점 방식은 이 모델을 흉내 내는 대상이다. 남은 개선 항목은 인계 문서마다 흩어져 있었으므로 `docs/GOALS.md` 로 모았다.

### 커밋

| 커밋 | 내용 |
|---|---|
| `d6f1a2f` | `GOALS.md` 신설: 전체 목표, 인계 문서 미결 통합, 캐시 실패 분석 C1~C4 |
| `f7f629f`, `f381614` | **R29** 캐시 접두: sets `StableLen`·`cache` 지점을 꼬리 앞으로, harness 접두 감지와 `prefix_break` 이벤트, terminal 경고 줄, plan 모드가 도구 목록을 바꾸지 않음 |
| `3db4c38` | DeepSeek 실측 (R29 전후 적중률) |
| `e5eed0e`, `9ecf2e3` | **R30** 캐시·상태 표시: harness `Stats`, agent `Stats`·`ToolNames`, terminal run_end 캐시 비율과 `/status` |
| `ba0104c`, `e74737e` | **R31** Chat 방언의 Anthropic 식 캐시 지점 (프로필 `CacheControl`, 내장 openrouter 는 `anthropic/` 모델) |
| `35552b0` | **R32** 로컬 서버의 보조 모델 캐시 간섭은 문서로 안내 (`INJECT.md` 5.4) |
| `93f8060`, `7ad7067` | **R33** OpenAI `prompt_cache_key`: `contract.Options.CacheKey`(ContractVersion 2), `cache` 세트가 세션 ID 로 채움, 프로필 `PromptCacheKey` |
| `bec751f` | R29 뮤테이션 29/29, 테스트 보강 |
| `957c7a5` | GOALS 정리 (CA-5 Anthropic 측정 제외, 새 항목 VF-6·VF-7·DC-4·DC-5), README 버전 |

### 스펙 버전 (지금)

contract v0.5, core v0.2, inject v0.5, sets v0.10, adapters v0.8, harness v1.6, osenv v0.3, agent v0.10, terminal v0.8, dr v0.3, dr-print v0.2, examples/inject v0.2, examples/verify v0.3, examples/modes v0.2.

### 인터뷰 기록 (사용자 답)

| 주제 | 사용자 답 |
|---|---|
| 기준 | "기본적으로 자동 접두방식을 사용한다고 생각하고 계획하자" |
| 구조 | "기본 시스템 프롬프트 + 툴리스트는 컨텍스트 앞단에 고정. 룰에는 추가 시스템프롬프트와 사용하면 안되는 도구, 사용할수있는 도구를 알려주고 이를 컨텍스트 맨 뒤에 붙이는거" → "좋아 그렇게 해보자" |
| R29 감지 | 방식 B(메시지까지 비교), 같음 = provider 에 보내는 필드, 정당한 변경도 항상 알림, 범위 = 세션·메모리, 모델별 기준, 터미널 종류별 표시, 범위 = 원칙+감지+plan+cache+지침 한 번에 |
| CA-5 측정 | "anthropic 말고 deepseek 로 하자" → 이후 "5는 아얘 제거해" (Anthropic 측정 하지 않음) |
| R30 | 위치 D(run_end + `/status`), 값 B(run_end = hit%, `/status` = hit% + 효율), 계산 A(harness), 항목 1~4 + "5 추가"(도구) |
| R31 | 조건 A(pi 자동 규칙 + 프로필), 위치 A(우리 힌트) |
| R32 | A(문서로 안내) |
| R33 | A(contract 필드 추가 허용) |

## 현재 코드 구조 (이번에 바뀐 곳)

| 위치 | 내용 |
|---|---|
| `L1_inject/sets/base.go` `StableLen`, `Cache` | 꼬리 = 끝에서 연속한 Ephemeral ID 메시지. `cache` 는 꼬리 앞 마지막 메시지에 지점, `CacheKey` = 세션 ID |
| `L2_harness/harness/prefix.go` | 지문(tools·system 해시, 메시지별 해시 — provider 에 보내는 필드만), (세션, 모델)별 기준표, `check`·`compare` |
| `L2_harness/harness/stats.go` | 세션별 사용량 집계(`Stats`, `UsageTotals`), 효율 상한 = 같은 모델의 직전 Prompt |
| `L2_harness/harness/surface.go` `wrap` | 맨 위 `request.finalize` 에서 지문, 맨 위 `model.call` 에서 비교·이벤트, 맨 위 `stream.end` 에서 집계. 모두 `OnEvent` 가 있을 때만 |
| `L2_harness/agent/status.go` | `Stats`, `ToolNames` |
| `L3_ui/surfaces/terminal/status.go`, `render.go` | `/status`, run_end 캐시 비율, `prefixLine` |
| `L1_inject/adapters/chat.go` | `chatMessages` 가 메시지별 마지막 Chat 메시지 위치를 함께 돌려줌, `chatCacheHints`·`markText` |
| `L1_inject/adapters/client.go` `setCacheKey`, `profile.go` `CacheControlKind` | `prompt_cache_key` (64 rune), 캐시 지점 켜는 조건 |
| `examples/modes/plan.go` | `request.finalize` 필터 삭제, 지시에 막힌 도구 이름 |

## 이 세션이 값을 치르고 배운 것

- **`TurnIndex` 는 0 에서 시작하지 않는다.** 테스트 세트가 턴을 구분할 때는 세트가 불린 횟수를 직접 센다 (`prefix_test.go` 의 `finalizeSet`). impl-08 의 경고와 같다.
- **`model.call` ctx 에는 `Conv()` 가 없다.** 꼬리·편집을 알려면 `request.finalize` 에서 계산해 run 상태에 두고 `model.call` 에서 쓴다. 이번 요청의 모델은 `request.finalize` 다음 step(`model.select`)에서 정해진다.
- **적중률(hit%)은 요청이 작으면 낮게 보인다.** 상한 = 직전 요청과 공통인 앞부분 ÷ 이번 요청. DeepSeek 은 cached 값을 128 토큰 단위로 내림한다(관찰). 실측 비교는 첫 메시지에 실행마다 고유값을 넣어야 한다. 그렇지 않으면 실행끼리 캐시를 공유한다.
- **실제 모델 측정 절차**: 임시 HOME(`/tmp/e2e/h`)에 `~/.dynamic_react/auth.json` 을 0600 으로 복사(값 출력 금지), 설정은 기존 `config.json` 복사. `examples/modes` 는 `package main` 이라 import 할 수 없다. 측정 프로그램은 `/tmp` 에 별도 모듈로 만들고 `go.mod` 의 replace 를 저장소 절대 경로로 둔다. 끝나면 `/tmp/e2e` 를 지운다. 외부 API 호출이므로 사용자 확인 후.
- **python 을 heredoc 으로 넘길 때 긴 한글 줄이 인코딩 오류를 냈다.** 큰 편집 스크립트는 파일로 쓰고 `# -*- coding: utf-8 -*-` 와 `encoding='utf-8'` 를 둔다.
- **zsh 는 변수를 단어로 나누지 않는다.** 모듈 반복 검증은 bash 스크립트 파일로 돌린다.
- **`git stash` 는 추적하지 않는 새 파일을 빼지 않는다.** 변경 전 상태 비교에 쓰면 빌드가 깨진다. 쓰지 말 것.
- **마크다운 표 칸 안에 줄바꿈을 넣으면 표가 깨진다.** 긴 형식은 표 밖 절로 뺀다 (terminal SPEC 3.8a).
- **뮤테이션 스크립트** (`/tmp/dr-mut/mut.py`, 다음 세션엔 없다 — 다시 만든다): JSON 목록 `[{"dir","file","label","old","new"}]` 를 받아 파일 백업 → 정확히 1회 매칭 치환 → `go test -count=1 -timeout 120s ./...` → 복원, `repr((label, KILLED|SURVIVED))` 출력. 매칭이 1회가 아니면 `BAD-MATCH`. **돌리는 동안 그 모듈을 편집하지 말 것.** 살아남으면 먼저 "테스트 입력이 그 분기를 실제로 타는가"를 의심한다 (이번 예: At 과 Kept 가 같은 값이라 구별 못 함).
- impl-08·09 의 "배운 것"은 그대로 유효하다.

## 검증 절차

모듈 14개 전부 (`go work edit -json` 의 `Use`): `gofmt -l`, `go vet ./...`, `go test -race -count=1 ./...`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test -count=1 ./...`. 새 코드 커버리지 확인. 뮤테이션 확인. 테스트 밖 코드에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 없음.

이번 세션의 마지막 상태: 14개 모두 통과. 커버리지 — harness `prefix.go`·`stats.go` 100%, `wrap` 98.2%, adapters `chatCacheHints` 100%·`markText` 90%, terminal `statusLines` 100%·`showStatus` 88.9%·`runEndLine` 90.9%. 뮤테이션 — R29 29/29.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 구현 중 정한 세부는 스펙에 반영하고 보고할 때 알린다. 스펙에 없는 Go 표현(새 export)은 구현 전에 스펙에 먼저 넣는다.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 새 개선 항목은 `GOALS.md` 에 먼저 넣는다. 항목을 끝내면 상태를 `해소` 로 바꾸고 근거를 적는다. 행은 지우지 않는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지. 스펙 커밋과 구현 커밋을 나눈다.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. `go 1.23.0`. 새 외부 의존은 사용자가 정한다.
- 키를 코드·대화·문서·이미지에 노출하지 않는다. 키는 `<home>/.dynamic_react/auth.json`(0600)에만. 구독 요금제 엔드포인트·구독 OAuth 금지. 환경변수를 읽지 않는다 (예외: 홈 디렉터리, 자식 프로세스 상속, 테스트 전용 `DR_E2E_*`).
- 레이어 책임: L0 core 는 기본값·순서를 모른다. 목록·검증·저장·집계는 L2. L3 는 입출력만.

## 사용자 결정 중 유효한 것

- D1~D16, R1~R33 (R10 폐기, R14 는 R17 로 정정, R22 비목표 두 개는 R26 이 뒤집음, R26 비목표 "run 도중 교체"는 R27 이 모드 경로에 한해 뒤집음, R28 은 "하지 않음", R32 는 문서 안내만).
- GOALS CA-5 의 Anthropic 측정은 하지 않는다.
- 저장소 LICENSE 는 미정이다 (묻지 않았으면 손대지 말 것).

## 남은 미결

[`GOALS.md`](GOALS.md) 를 본다. 이 문서는 목록을 복사하지 않는다.
