# 인계: impl-05 — 모델 미설정 실행·앱 안 모델 선택 (R20) 테스트·구현

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 아래 순서로 진행한다. 스펙은 **이미 확정·커밋됐다** (`8c5d2a8`). 이번 세션은 **TDD: 테스트(RED) → 구현(GREEN) → 검증** 이다. 스펙을 바꿔야 할 일이 생기면 멈추고 사용자에게 묻는다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋 `8c5d2a8` 이후 이 문서 커밋 하나).
2. 기준 문서를 읽는다: `docs/DECISIONS.md` **R20**, `L1_inject/adapters/SPEC.md` v0.5 **AD-FR-52**, `L2_harness/agent/SPEC.md` v0.4 **AG-FR-04·08·10 (변경), 3.12 AG-FR-32~35 (신규), 4. 검증 표의 AG-FR-32~35 행**, `L3_ui/surfaces/terminal/SPEC.md` v0.2 **TM-FR-16 (변경), TM-FR-47a, 3.9 TM-FR-48~51, 4. 검증의 TM-FR-47a~51 행**, `L3_ui/cmd/dr/SPEC.md` v0.2 DR-FR-03.
3. 아래 계층부터 한 단계씩. **각 단계는 테스트를 먼저 쓰고 실패(RED)를 눈으로 본 뒤 구현한다** (직전 세션은 terminal `run_test.go` 를 구현 뒤에 써서 RED 를 못 봤다 — 반복하지 말 것).
   - ① adapters `Client.Models() []ModelRef` (`type ModelRef struct{ Provider, ID string }`) — 설정 `models` 키 + 내장 스냅샷(`c.snap`), 프로필에 없는 provider 제외, 중복 없이 Provider→ID 정렬, 네트워크 없음.
   - ② agent: `ErrNoModel`, `ModelChoice`, `Models()`, `Model() (string, error)`, `SetModel(ctx, ref) error` + `New` 가 모델 없음·모르는 provider·키 없음에서 **실패하지 않게**, `Start` 는 모델 문제면 run·잠금 없이 그 에러. `SetModel` 은 사용자 설정 파일(AG-FR-01 경로, 기본 경로에 없으면 생성)의 `agent.model` 만 바꿔 쓰고 다른 키는 원문 보존(들여쓰기 2칸 + 끝 줄바꿈, 0644, 상위 디렉터리 0755), 쓰기 실패면 아무것도 안 바꿈, `Options.Model` 보다 이김. 동시 호출 안전(-race).
   - ③ terminal: 모델 선택 상태(필터 입력 `모델: `, 낱말 AND·대소문자 무시, 최대 10개 + `  … +N개`/`  (일치하는 모델 없음)`, `▶ ` 하이라이트, ~~KeyMissing 흐리게 ` (키 없음: ENV)`~~ (R21: 키 없는 provider 의 모델은 목록에서 뺌), ↑↓ 창 이동, Enter → SetModel → `── 모델 <ref> (저장됨)`, Esc·Ctrl+C 취소), 시작 시 모델 문제면 `⚠ <문제>`(ErrNoModel 은 `⚠ 모델이 설정되지 않았습니다`) 후 선택 상태 (Pick 이 있으면 그 뒤), `/model [ref]` (대기 중만), Start 에러 시 모델 문제면 ` (/model)` 덧붙임, `/help` 에 `/model`.
   - ④ dr: 코드 변경은 거의 없다 (agent.New 가 더 이상 모델 문제로 실패하지 않음). 모델 없는 설정으로 dr 이 0 으로 끝나는(/quit) 테스트를 더한다.
   - 기존 테스트 중 "모델 없음·키 없음 → New 에러" 를 기대하는 것(agent `TestModelOverride`·`TestAPIKey` 등, dr-print 키 없음 테스트)은 새 동작(Model()/Start 에러)으로 바꾼다. dr-print 는 Start 에러로 여전히 종료 코드 1 이어야 한다 (DP-FR-04).
4. 각 단계 뒤: `gofmt -l`, `go vet`, `go test -race -count=3`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test`, 커버리지, **뮤테이션 확인**(살아남은 뮤턴트는 동치면 코드 단순화, 아니면 테스트 보강), 사용자 보고. 커밋은 사용자 확인 후.

**하지 말 것**: 라이선스 고지(O2) — 사용자: "라이센스 고지는 아직은 하지마. 마지막에 정리하면서 할거야." 원격 카탈로그를 목록에 넣지 말 것 (R20 ②). L3 가 설정 파일을 직접 읽거나 쓰지 말 것 (R17 — 저장은 agent.SetModel).

## 어디까지 왔나 (커밋)

| 커밋 | 내용 |
|---|---|
| `174d675` | R19 스펙: terminal v0.1, dr v0.1, agent v0.3(History·Compact), harness v0.8(Compact), sets v0.5(ReserveTokens) |
| `9cf1efa` | R19 구현: `L3_ui/surfaces/terminal`(라이브러리), `L3_ui/cmd/dr`(진입점), agent `History`·`Compact`, harness `Compact`, sets `ReserveTokens` 공개 |
| `8c5d2a8` | R20 스펙: adapters v0.5(Models), agent v0.4(Model·Models·SetModel), terminal v0.2(모델 선택·/model), dr v0.2 |

검증 상태(`9cf1efa`): 모듈 11개(contract·core·adapters·inject·sets·harness·osenv·agent·dr-print·terminal·dr) 전부 gofmt·vet·`-race -count=3`·`GOWORK=off` 통과. 커버리지 harness 97.1%, agent 94.0%, terminal 96.1%, dr 83.6%(main·run 은 시그널 배선만). 뮤테이션: sets 3/3, harness compact 전부(동치 제거), agent history 8/10(2개 동치), terminal 112/113(남은 1개: 종료 중 ErrRunDone 경합으로 대기 메시지를 시작하는 경로 — 관측 불가), dr 11/11.

수동 확인 미완: 실제 TTY 의 raw mode 진입, 시그널 배선(SIGTERM·SIGHUP·SIGWINCH). `cd L3_ui/cmd/dr && go run . -model <provider/id>`.

## 현재 구조

| 레이어 | 모듈 | 내용 |
|---|---|---|
| L0 | `contract`, `core` | 루프 |
| L1 | `inject`, `sets`, `adapters` | 주입 함수. 환경·파일을 읽지 않음 (AD-C6) |
| L2 | `harness` | Env·ModelCaller 위 조립. `Agent.Compact(ctx, sid, config)` (HN-FR-36) |
| L2 | `osenv` | OS Env |
| L2 | `agent` | 완성된 에이전트 + surface 공통 계층. `History`·`Compact` 추가 (`history.go`) |
| L3 | `cmd/dr-print` | 검증용 비대화형 CLI |
| L3 | `surfaces/terminal` | 대화형 터미널 라이브러리: `keys.go`(키 해석기), `editor.go`(입력 줄), `screen.go`(덧붙이기 출력 + 다시 그리는 영역), `render.go`(이벤트·History·승인 프롬프트 렌더), `terminal.go`(상태 기계·Run 루프·승인·세션·명령), `open.go`(x/term) |
| L3 | `cmd/dr` | 대화형 진입점 (`runWith` + `hooks` 로 테스트) |

terminal 상태: `stIdle`·`stRunning`·`stCompacting`·`stPick` + 승인 오버레이(`t.appr`). 모델 선택은 새 상태(`stModel` 등)로 넣는 것이 자연스럽다. 다시 그리는 영역 = 꼬리 + 상태 줄 + `view.lines`; 목록은 `view()` 에서 입력 줄 앞에 줄을 넣으면 된다 (`approvalView` 가 같은 방식).

agent 관련: `agent.go` `New`(AG-FR-09 순서) — `agentConfig(fc.Agent, o.Model)` 가 모델 없음에서 에러를 내고, `client.Profile(provider)`·키 확인이 에러를 낸다. 이 셋을 "모델 문제" 로 바꾸는 것이 ② 의 핵심. `a.config`(raw JSON) 를 Start·Compact 가 쓴다. adapters 내장 스냅샷은 `catalog.go` `loadSnapshot()` → `Client.snap map[provider]map[id]record`, 설정 모델은 `Client.models map["provider/id"]ModelPatch`, 프로필 목록은 `BuiltinProfiles()` + 설정 providers.

## 먼저 읽을 것

1. `docs/DECISIONS.md` R19, R20
2. 착수 블록 2 의 스펙 절들
3. `L2_harness/agent/agent.go` `New`·`agentConfig`·`loadConfig`, `L1_inject/adapters/client.go`·`catalog.go`
4. `L3_ui/surfaces/terminal/terminal.go` (`key`, `pickKey`, `view`, `begin`, `command`), `run_test.go` 의 하네스(`env.start`, `harnessT.typ/wait/waitFn/exit`, `config(url, agentExtra, extra)`, 가짜 `llm`)

## 이 세션이 값을 치르고 배운 것

- **terminal 테스트 하네스**: `syncScreen`(잠금 + 가상 화면 `vscreen`) 의 `text()` 는 **행 끝 공백을 잘라낸다** — 기대 문자열에 끝 공백(`"모델: "` 등)을 넣지 말 것. `harnessT.done` 은 닫히는 채널이고 결과는 `h.err` (예전에 결과 채널을 두 번 읽어 테스트마다 5초 낭비했다).
- **JSON 은 `<` `>` 를 `<` 로 이스케이프한다** — 요청 바디를 문자열로 비교할 때 그 문자를 피하라.
- **잠금 규칙(terminal)**: 화면에 영향을 주는 상태(editor, hint, st, appr, followUps…)는 `t.mu` 안에서 바꾼다 (`OnEvent` 고루틴이 `view()` 를 읽음). agent 호출(Start·Steer·Abort·Sessions·History·Compact·SetModel)은 **잠금 밖에서**. 메인 루프 고루틴만 쓰는 필드는 메인 루프에서 잠금 없이 읽어도 된다.
- **`editsSaved` 정합**: run 밖에서 편집을 저장해도 `BaseRunStart` 가 시작마다 Store 에서 다시 세므로 중복 저장되지 않는다.
- **뮤테이션 스크립트** `/tmp/mut.py` (다음 세션엔 없다 — 다시 만든다): `python3 mut.py <모듈 디렉터리> <파일> <json: [[old,new],...]>`, 파일 백업 → 정확히 1회 매칭 치환 → `go test -count=1 -timeout 120s ./...` → 복원, `repr()` 한 줄 출력. **뮤테이션이 도는 동안 그 모듈의 파일을 편집하지 말 것**, 다른 모듈 테스트도 돌리지 말 것(소스가 일시적으로 바뀌어 있음). 출력은 버퍼링되므로 끝날 때까지 안 보인다.
- **`grep -v KILLED` 로 살아남은 뮤턴트만 본다.** 테스트가 뮤턴트를 못 잡으면 먼저 "테스트 입력이 그 분기를 실제로 타는가" 를 의심하라 (K2: 붙여넣기 중 단독 ESC 는 버퍼가 정확히 1바이트일 때만 생김; R5: 폭에 **정확히** 맞는 줄이어야 `<=`/`<` 가 갈림).
- 셸 heredoc 에 한국어 + 특수문자가 섞인 python 은 Write 로 `/tmp/*.py` 에 쓰고 `# -*- coding: utf-8 -*-`. 여러 파일에 걸친 문서 편집은 `assert s.count(a)==1` 로 정확히 1회 매칭을 강제하는 python 스크립트가 안전했다.
- `go build ./...` 는 바이너리를 남긴다 — 빌드 확인은 `go vet`.
- Go 1.23 호환: `golang.org/x/term` v0.34.0(+x/sys v0.35.0)이 마지막 호환 버전 (v0.35+ 는 go 1.24).
- 사용자는 AskUserQuestion 에 자유 입력으로 반문·수정한다 (예: "1에서 ctrl+c abort, 줄바꿈은 shift+enter", "등록한 모델중에 고를 수 있게하는건 당연한거야. 이건 하네스에서 해야하는거고"). 그 지시를 먼저 반영하고 다시 묻는다. 사용자는 **레이어 책임(L2 vs L3)** 을 엄격히 본다 — 목록 구성·검증·저장은 L2.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에. 스펙에 없는 Go 표현(새 export 포함)은 구현 전에 스펙에 먼저 적는다. 구현 중 정한 세부는 스펙에 반영하고 보고 때 "검토 부탁" 으로 명시.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다 (설명을 먼저 충분히).
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI. 이름 변경은 Serena `rename_symbol` (`initial_instructions` → `activate_project(path, session_id)` 먼저).
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`. 외부 의존: x/text(harness·terminal), x/term(terminal 만, R19). 새 외부 의존은 사용자 결정.
- 키를 코드·대화·문서·이미지에 노출하지 않음, 구독 요금제 엔드포인트·OAuth 경로 금지. **(R21 로 정정)** 키는 환경변수가 아니라 `<home>/.dynamic_react/auth.json`(0600, agent `SetKey`·`RemoveKey`, 터미널 `/login`·`/logout`)에만 둔다. 설정 파일·프로젝트 설정에는 키 필드가 없다.

## 사용자 결정 중 유효한 것

- D1~D16 (정정본), R1~R20 (R10 폐기, R14 는 R17 로 정정).
- R19 (대화형 터미널): 형태 B(raw mode REPL), terminal + dr 모듈, 키(Enter=Start/Steer, Alt+Enter=FollowUp, Shift+Enter·Ctrl+J 줄바꿈, Esc·Ctrl+C(실행 중)=Abort, 대기 Ctrl+C 지우기/두 번 종료, Ctrl+D 빈 줄 종료), 승인 y/a/n+사유, 렌더링 권장안, 세션 이어가기 시 편집 적용 투영 전부 다시 그림(요약은 걸러내고 `이전 N개 압축됨`), `/compact` 즉시, 비TTY 종료 코드 2.
- R20 (이번 작업): 구조 a(앱 안에서 설정, agent.New 는 실패 안 함), 저장 a(설정 파일 `agent.model`), 목록 a(설정 models + 내장 카탈로그, 키 상태 표시, 원격 제외), UI a(필터 + 하이라이트 최대 10개).
- R20 스펙 때 내가 정해 "검토 부탁" 으로 보고했고 사용자가 "좋아" 로 진행시킨 세부: 원격 카탈로그 제외, `/model provider/id` 직접 입력은 목록에 없는 id 도 받음(provider·키만 검증), `-model` 은 저장 안 함·SetModel 이 이김, `-model` 형식 오류는 여전히 New 에러, 모델 없을 때 `/compact` 는 에러 줄.
- 라이선스 고지는 마지막 정리 때. **(impl-05 에서 완료: `THIRD_PARTY_NOTICES.md`)**

## 남은 미결

| 위치 | 내용 |
|---|---|
| R20 | 테스트·구현 (착수 블록) |
| L3 | 실제 TTY 수동 확인 (raw mode, 시그널) |
| L2 O2 | ~~라이선스 고지~~ → `THIRD_PARTY_NOTICES.md` (impl-05) |
| adapters | ~~vLLM `max_model_len` 문서 확인 못 함, Xiaomi base URL 은 pi 값~~ → impl-05 에서 확인: vLLM 소스 `ModelCard.max_model_len` (`vllm/entrypoints/serve/engine/protocol.py`, main `ac9126e`), Xiaomi 공식 문서 `https://api.xiaomimimo.com/v1` |
| agent | `NoRemoteCatalog` 전달 뮤턴트 관측 불가 (외부 네트워크) |
