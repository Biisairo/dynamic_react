# 인계: impl-04 — 대화형 터미널 surface

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 아래 순서로 진행한다. 기준 문서는 `docs/DECISIONS.md`(R16~R18), `L2_harness/agent/SPEC.md` **v0.2**(3.4~3.9 surface 공통 계층), `L2_harness/SPEC.md` **v0.7**(3.6a surface 훅).

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋 `7510dc7` 이후 이 문서 커밋 하나).
2. **대화형 터미널 surface 스펙 인터뷰 (사용자 지시).** 사용자: "대화형 터미널 해보자." 공통 계층(승인·끼어들기·이벤트·세션·잠금)은 이미 L2 agent 에 있으므로, 터미널 surface 는 **입출력만** 한다 (R17, R18): `agent.Approver` 구현(터미널에서 묻기), `OnEvent` 렌더링, 입력을 `Start`·`Steer`·`FollowUp`·`Abort` 로 잇기, 세션 목록·이어가기 UI.
   - 인터뷰로 정할 것(의존 순서로, 한 번에 하나, 권장안 포함, **설명을 먼저 충분히 쓰고** AskUserQuestion): ① 형태 — 줄 단위 REPL(표준 라이브러리만) vs 풀스크린 TUI(외부 라이브러리 필요 → 외부 의존 정책 결정, 현재 harness 외 의존은 x/text 만 허용 R15) ② 모듈 위치(`L3_ui/cmd/<이름>` 또는 `L3_ui/surfaces/<이름>`) ③ 키 바인딩(Abort·Steer·FollowUp 구분 — 실행 중 입력을 Steer 로 볼지 FollowUp 으로 볼지) ④ 승인 프롬프트 UX(한 번/세션/거부+사유) ⑤ 렌더링(텍스트 스트리밍, thinking 표시 여부, 도구 시작/끝/실시간 출력, retry·compaction 표시) ⑥ 세션 선택(시작 시 목록, `/sessions` 같은 명령) ⑦ 슬래시 명령 범위 ⑧ 터미널 비정상(파이프·비TTY) 동작.
   - 확정되면 SDD: 스펙(IEEE 29148, 새 SPEC.md) → 사용자 확인 → 커밋 → 테스트(RED) → 구현(GREEN) → 검증.
3. 각 단계 뒤: `gofmt -l`, `go vet`, `go test -race -count=3`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test`, 커버리지, **뮤테이션 확인**, 사용자 보고. 커밋은 사용자 확인 후.

**하지 말 것**: 라이선스 고지(O2). 사용자: "라이센스 고지는 아직은 하지마. 마지막에 정리하면서 할거야."

## 어디까지 왔나 (커밋)

| 커밋 | 내용 |
|---|---|
| `8a600ea` | R16: 시스템 프롬프트 편집 필드(Preamble·Append) 제거, harness 스펙 v0.5 |
| `83090aa` | R17 레이어 원칙 + osenv·agent·dr-print 스펙 v0.1 |
| `6f64c28` | osenv·agent·dr-print 구현, adapters 환경 접근 제거 (AD-C6) |
| `5f32673` | R18 surface 공통 계층 스펙: harness v0.7, agent v0.2, inject O3 종결 |
| `7510dc7` | R18 구현: 이벤트·Gate·승인 규칙·Steer/FollowUp/Abort·세션 잠금·목록, `.gitignore`(.serena/) |

검증 상태: 전 모듈 9개(contract·core·adapters·inject·sets·harness·osenv·agent·dr-print) gofmt·vet·`-race -count=3`·`GOWORK=off` 단독 통과. 커버리지 harness 95.7%, agent 93.4%, osenv 90.4%, dr-print 88%. 뮤테이션: osenv 22/22, agent 45/45(+1 관측 불가: `NoRemoteCatalog` 전달 — 외부 네트워크), harness surface 31/31, dr-print 전부 KILLED.

## 현재 구조 (R17)

| 레이어 | 모듈 | 내용 |
|---|---|---|
| L0 | `contract`, `core` | 루프. 사용자 환경과 무관 |
| L1 | `inject`, `sets`, `adapters` | 주입 함수. 환경변수·파일을 읽지 않는다(adapters `Getenv` nil = 환경 안 읽음, `LoadConfig` 없음, AD-C6) |
| L2 | `L2_harness/harness` | Env·ModelCaller 위의 조립(실제 I/O 없음, HN-C1). 3.6a: `Options.OnEvent`·`Gate`, `Event` 11종, `tools.WithProgress`, `RunRequest.Observer`, `Agent.Emit`, `tools.Resolve` |
| L2 | `L2_harness/osenv` | `env.Env` OS 구현 (`$SHELL -c`, 프로세스 그룹 kill, 0644/0755) |
| L2 | `L2_harness/agent` | 완성된 에이전트 + surface 공통 계층. `agent.New(ctx, Options{Cwd, ConfigPath, Model, Observer, Getenv, NoRemoteCatalog, Tools, Permissions, Approver, OnEvent})` → `Start(ctx, sid, input) (*Run, error)`, `Run.{SessionID, Steer, FollowUp, Abort, Wait}`, `Sessions(ctx)`, `SessionKey(key)`, `Harness()`, `SessionDir()`. `ErrSessionBusy`, `ErrRunDone` |
| L3 | `L3_ui/cmd/dr-print` | 검증용 CLI (`-model -session -cwd -config -json`, 종료 코드 0/1/2/130) |
| L3 | `L3_ui/surfaces` | README 만 (구체 surface 미작성) — 이번 작업 |

설정 파일 `~/.dynamic_react/config.json`: `{sessionDir, agent(sets 설정), providers, models, permissions{allow,ask,deny}}`. 프로젝트 설정 `<cwd>/.dynamic_react/config.json` 은 `permissions` 만. 세션 디렉터리 우선순위 `DR_SESSION_DIR` > 설정 > `~/.dynamic_react/sessions`.

## 먼저 읽을 것

1. `docs/DECISIONS.md` — R16, R17, R18 (R14 정정 포함)
2. `L2_harness/agent/SPEC.md` v0.2 — 3.4 승인 규칙, 3.6 run 제어, 3.7 이벤트, 3.8 세션, 3.9 Options
3. `L2_harness/SPEC.md` v0.7 — 3.6a surface 훅 (Event 필드·생성 지점 표, HN-FR-32 순서 보장, HN-FR-34 동기·직렬화)
4. `L3_ui/surfaces/README.md`, `L3_ui/cmd/dr-print/SPEC.md`·`main.go` (L3 가 agent 를 쓰는 기존 예)
5. 참고 구현: pi TUI `../hermes_study/pi-mono/packages/coding-agent` (interactive mode), `hermes_study/analysis/06-build-your-own.md` 3.3

## 이 세션이 값을 치르고 배운 것

- **레이어 판단은 R17 의 한 줄로 한다**: "L0·L1 은 사용자 환경과 무관한 단독 라이브러리, L2 부터 사용자 환경 위의 실제 에이전트, L3 는 완성된 에이전트를 쓰는 입출력." 사용자는 이 선을 엄격히 본다 — 에이전트의 동작(정책·잠금·세션)이 L3 로 새지 않게 한다.
- **사용자는 기술 비교를 직접 묻는다** ("동기콜백과 비동기버퍼의 장단점"). 질문이 오면 표로 장단점을 정리하고, 결론 + 대안을 낸 뒤 다시 묻는다. 사용자는 권장안을 뒤집기도 한다(이벤트 = "동기만", 승인 = "규칙 단위", 프로젝트 설정 allow 포함, osenv "분리로").
- **inject 의 상태 네임스페이스는 step 함수를 가진 세트 이름 기준이다.** base 함수를 다른 세트로 겹쳐 감싸면 base 상태가 깨진다(초기 입력 소실 → run 이 모델 호출 없이 completed). 감싸기는 그 세트 안에서 제자리로 (`harness/surface.go` `wrap`).
- **core RunID 는 내부 생성이고 ObserverEvent 에 세션이 없다.** run 별로 무언가를 귀속시키려면 run ctx 값(`context.WithValue`)이나 run 별 Kernel(`RunRequest.Observer`)을 쓴다. dispatch ctx 는 run ctx 를 잇는다.
- **Gate 는 에러를 돌려줄 수 없다** (`*ToolResult` 만). ctx 취소는 aborted 결과로.
- **sets 는 빈 `tools` 를 "전부" 로 본다.** 빈 목록을 넘기면 모든 도구가 켜진다.
- **base 도구 계획은 호출마다 그룹 하나(순차)** — Steer 테스트가 결정적인 이유.
- **셸 heredoc 에 한국어 + 특수문자가 섞이면 python 이 "Non-UTF-8" 로 죽는다.** 긴 편집 스크립트는 Write 로 `/tmp/*.py` 에 쓰고 `# -*- coding: utf-8 -*-` + `open(..., encoding='utf-8')`.
- **뮤테이션 스크립트** `/tmp/mut.py` (이 세션의 임시 파일, 다음 세션엔 없다): 파일 백업 → 정확히 1회 매칭 치환 → `go test -count=1 ./...` → 복원, 출력은 `repr()` 한 줄. 줄바꿈이 든 치환을 그대로 print 하면 `grep -n` 줄 번호가 뮤턴트 번호와 어긋난다. import 를 쓰지 않게 만드는 뮤턴트는 **컴파일 에러로 KILLED** 되므로 동작 테스트로 따로 고정하라. 뮤턴트가 테스트 프로세스 cwd 에 파일을 남길 수 있다(`osenv/rel` 사례) — 끝나면 `git status` 를 본다.
- **`go build ./...` 는 main 패키지 바이너리를 모듈 디렉터리에 남긴다** — 빌드 확인은 `go vet` 로.
- **stub 으로 RED 를 볼 때 기다리는 테스트는 멈춘다** — `go test -timeout 60s`.
- **Serena 는 `initial_instructions` → `activate_project(path, session_id)` 후 `rename_symbol`.** `.serena/` 는 `.gitignore` 에 있다.
- 사용자는 AskUserQuestion 에 자유 입력으로 반문하기도 한다 — 그 질문에 먼저 답하고 다시 묻는다.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148 구조. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에. 스펙에 없는 Go 표현(새 export 포함)은 구현 전에 스펙에 먼저 적는다.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다. 구현 중 정한 세부는 보고 때 "검토 부탁" 으로 명시.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI (사용자 CLAUDE.md). 이름 변경은 Serena `rename_symbol`.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`.
- Go 1.23 호환 (`t.Context`, `b.Loop` 등 1.24+ API 금지). 외부 의존은 `golang.org/x/text`(harness) 만 — 새 외부 의존은 사용자 결정 필요.
- API 키는 환경변수로만, 구독 요금제 엔드포인트·OAuth 경로 금지.

## 사용자 결정 중 유효한 것

- D1~D16 (정정본), R1~R18 (R10 폐기, R14 는 R17 로 정정).
- R16: 시스템 프롬프트 편집 수단 없음 (개발자는 `sets.Deps.System` 직접 구성).
- R17: 레이어 원칙, osenv 분리 모듈, agent 모듈이 조립, 셸 `$SHELL -c`, 권한 보호 없음, 설정 중첩 형식, dr-print 종료 코드 0/1/2/130·stdin·`-json`.
- R18: surface 공통 계층 7항목 (도구 상한, 규칙 승인, Abort/Steer(남은 도구 skipped)/FollowUp, 동기 이벤트만, SessionKey·Sessions, 프로세스 내 잠금, 콜백 panic 격리).
- R18 구현 때 보정(보고됨): AG-FR-17 allow_session 은 deny 다음·ask 앞, AG-FR-21 빈 교집합은 에러, AG-FR-18 Approver ctx 취소 → aborted 결과.
- 라이선스 고지는 마지막 정리 때.

## 남은 미결

| 위치 | 내용 |
|---|---|
| L3 터미널 surface | 스펙 전체 (착수 블록 2) |
| L2 O2 | 라이선스 고지 — 마지막 정리 때 |
| adapters | vLLM `max_model_len` 필드 문서 확인 못 함, Xiaomi base URL 은 pi 값 |
| agent | `NoRemoteCatalog` 전달 뮤턴트는 관측 불가 (외부 네트워크) |
