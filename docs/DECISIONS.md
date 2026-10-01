# 결정 기록 (Decision Log)

출처: `hermes_study/analysis/07-injectable-loop-design.md` 11장 (Hermes · pi · oh-my-pi 비교 분석 01~07 문서의 결론).
이 문서는 스펙(`*/SPEC.md`)의 상위 근거다. 스펙과 이 문서가 충돌하면 **작업을 멈추고 이 문서를 먼저 고친다.**

---

## 1. 구조 원칙

| id | 결정 |
|---|---|
| D1 | **루프는 루프로만 작동한다.** 루프 = 고정된 step 목록 + 고정된 전이 표(신호 → 다음 step). 정책은 하나도 없다 |
| D2 | **모든 step 은 주입된 함수가 수행한다.** `context.*`, `request.*`, `model.call` 포함. 주입 함수는 인터페이스로 정의하고, 이를 처리하는 dispatch layer(inject)를 둔다 |
| D3 | **흐름 제어는 주입 함수 안에서만, 약속어로.** 제어 약속어(신호)는 반환값으로만 낸다. 상태는 조회 약속어로 읽는다. 둘 다 닫힌 타입이고 버전으로만 추가된다 |
| D4 | **ctx 는 step 마다 만들어 주입한다.** step 별로 허용된 조회·신호만 들어 있고, 함수가 끝나면 폐기한다. 전역 API 는 없다 |
| D5 | **step 당 주입 함수는 하나.** 함수는 신호 배열을 반환할 수 있고 루프는 순서대로 실행한다. 여러 기능의 충돌 해결은 상위 레이어의 조합 함수가 맡는다. 루프에는 우선순위 표가 없다 |
| D6 | **전부 주입 가능, 기본값은 기본 함수 세트(sets)가 제공.** 기본값은 루프(core)와 주입 레이어(inject)에 두지 않는다 |

## 2. 세부 결정

| id | 항목 | 결정 |
|---|---|---|
| D7 | 언어 | Go |
| D8 | 스트림 중 개입 | 1차 범위. 루프는 창만 연다: 이벤트마다 `stream.event` dispatch, 스트림 도중 도구 실행이 시작될 수 있는 동시성 구조, 입력 주입 통로. 판단은 주입 함수 |
| D9 | 짝 없는 tool call | 복구 방식은 주입, 기본값은 aborted 결과 채우기. 요청 직전 짝 검증도 기본 함수(교체 가능). 검증을 빼면 provider 거부가 드러남 |
| D10 | 서브에이전트 실행기 | 주입, 기본값 같은 프로세스 goroutine, 별도 프로세스도 가능. → 루프는 전역 상태 없는 값 |
| D11 | 함수 세트의 상태 | 세션 저장소에 네임스페이스로, `ctx` 로만 접근, 재개·브랜치 시 함께 복원. 저장 방식은 주입 |
| D12 | 서드파티 | ~~별도 프로세스(gRPC) 로더~~ → **서드파티도 같은 프로세스의 Go FunctionSet** 이다. 사용자는 세트를 고치거나 새로 만들어 `Bind` 로 겹친다. 런타임 격리·RPC 는 목표가 아니다 (impl-01 에서 사용자 정정) |
| D13 | step 목록 | 고정하되 22개로 세분화 (L0_core/core/SPEC.md 3.1) |
| D14 | 레이어 묶음 | `L0_core/{contract, core}` · `L1_inject/{inject, sets, adapters}` · `L2_harness` · `L3_ui/{surfaces}`. 주입(프레임워크 포함)은 사용자가 수정하는 영역이라 L1. 하네스(세션 저장, 도구 구현, 프롬프트 조립, 요약기, 설정 파일)는 L2 (R13, impl-01 에서 정정). L2 = `harness`·`osenv`·`agent`, CLI 는 L3 (R17) |
| D15 | 1차 구현 범위 | **L0 + L1 = 주입으로 동작하는 루프 라이브러리.** sets 는 `base` + `resilient`(메모리 상태, 도구·저장소·프롬프트·요약기는 interface), adapters 는 Anthropic Messages + OpenAI Chat Completions + OpenAI Responses. 실행 가능한 에이전트는 L2_harness 에서 (R13) |
| D16 | 실행 진입점 | ~~검증용 print CLI 는 하네스의 일부 (`L2_harness/cmd/dr-print`)~~ → **검증용 print CLI 는 `L3_ui/cmd/dr-print`** (터미널 입출력을 하므로 L3, R14·R17). dr-print 는 `L2_harness/agent` 만 쓴다. 본격 UI 도 L3 |

## 3. 스펙 작성 중 확정한 세부 (R)

07 문서 이후, 스펙을 쓰며 정한 것. 모두 D1~D13 을 구체화한 것이며 새 방향 전환은 없다.

| id | 내용 | 근거 |
|---|---|---|
| R1 | 신호를 **전이 신호**(step 을 옮김)와 **효과 신호**(상태에 작용, step 은 안 옮김)로 나눈다. 신호 배열 = 효과 0개 이상 + 전이 최대 1개(마지막). 전이가 없으면 `next` | D5 의 "신호 배열 순차 실행"을 모호함 없이 정의 |
| R2 | `turn.finish` 와 `call.error` 에서는 `next` 를 허용하지 않는다. 다음 행동(`continue_turn` / `settle` 등)을 함수가 명시해야 한다 | "도구를 실행했으면 계속"이 루프 안의 정책이 되는 것을 막음 (D1) |
| R3 | overflow 복구 = `[rewrite(edit), rebuild]`. `recall` 은 "같은 요청 재전송"이라 압축이 반영되지 않는다 | 07 문서 초안의 예시 정정 |
| R4 | 모듈 경로 접두어 = `github.com/Biisairo/dynamic_react` (구현 시작 시 사용자 확정). 모듈 경로 = 접두어 + 폴더 경로 (예: `github.com/Biisairo/dynamic_react/L0_core/contract`) | 모듈별로 `go get` 가능해야 함 (R8) |
| R5 | Go 1.23 이상 (`iter.Seq2` 사용) | D8 스트림 표현 |
| R6 | core 가 루프 무한 반복을 막는 **전이 수 상한**을 생성 인자로 받는다(기본 10,000). 정책이 아니라 안전장치 | 함수 세트 버그로 인한 무한 루프 방지 |
| R7 | core 가 run 기록(Transcript)을 기계적으로 누적한다 (2·14·19 출력). 해석·편집 적용은 하지 않는다 | sets 스펙 작성 중 발견: 이전 턴 응답·결과를 View 로 볼 수 없어 투영을 만들 수 없었음. 누적은 판단이 아닌 기록이라 D1 과 충돌하지 않음 |
| R8 | **패키지 폴더마다 독립 Go 모듈**: `L0_core/contract`, `L0_core/core`, `L1_inject/inject`, `L1_inject/adapters`, `L1_inject/sets`, `L2_harness/harness`, `L2_harness/osenv`, `L2_harness/agent`, `L3_ui/cmd/dr-print`, `L3_ui/surfaces/terminal`, `L3_ui/cmd/dr` 가 각자 `go.mod` 를 가진다 (R14·R17·R19 에서 정정). 개발은 루트 `go.work` 로 묶는다. 버전 태그는 폴더 접두어(`L0_core/contract/v0.1.0`). 모듈 의존: contract ← core ← inject, contract ← adapters, sets → {contract, core, inject}, L2_harness → L1 이하. 모듈 간 require 는 공개 전까지 `v0.0.0` + `replace => ../상대경로` (모듈 단독 `go mod tidy` 가능, replace 는 소비자에게 적용되지 않음). 공개 시 태그 버전으로 교체 | 사용자가 pi·omp 처럼 필요한 모듈만 골라 쓰고 교체할 수 있게. 의존 방향을 `go.mod` 가 강제 (CORE-NFR-01 이 구조로 보장) |
| R9 | **adapters 는 contract 에만 의존**하는 단독 LLM 클라이언트다: `Call(ctx, req) iter.Seq2[contract.StreamEvent, error]`. 이것을 `ModelCallFn` 으로 감싸 `core.CallStream` 을 만드는 것은 sets 몫이다 | R8: 루프 없이 모델 호출만 쓰려는 사용자가 core·inject 를 받지 않게. 이전: adapters 가 inject 의 `ModelCallFn` 을 직접 제공 |
| R10 | ~~원격 경계는 결정적 JSON~~ → 폐기 (D12 정정으로 원격 경계가 없음) | — |
| R11 | 모델 수치는 필드 단위로 **사용자 설정 > provider 조회 API > 원격 카탈로그(models.dev) > 내장 스냅샷(oh-my-pi models.json 발췌)** 순으로 채운다. 모두 모르면 0(모름) | 사용자: 수동 고정을 피하고 조회 우선, 조회로 알 수 없는 provider 가 많아 원격·스냅샷을 뒤에 둠 |
| R12 | 내장 provider 프로필 = pi 의 provider 중 1차 방언 + API 키 방식인 것 + 로컬 서버. 구독 요금제 엔드포인트는 제외 | 사용자: pi 에 있는 것 기본 포함, claude·openai·z.ai·deepseek·xiaomi·minimax 필수 |
| R13 | **pi 식 레이어 분리**: L1 sets 는 22 step 기본 함수와 정책(재시도, overflow 복구, 임계치 압축 판정, follow-up)만 둔다. 세션 JSONL 저장, 도구 4종, 시스템 프롬프트 조립(AGENTS.md·스킬), 요약 프롬프트·요약기 구현, 설정 파일, dr-print 는 L2_harness. sets 는 adapters 에 의존하지 않고 `ModelCaller` interface 를 받는다 | 사용자: 하네스는 루프+주입보다 위의 레이어. pi 의 ai / agent-core / coding-agent 분리와 같음 |
| R14 | **L2_harness = 에이전트 함수 하나 (`L2_harness/harness` 모듈), 실제 입출력은 전부 L3**. 도구 로직(인자·절단·edit 알고리즘), JSONL 세션 형식, 프롬프트 조립, 요약 프롬프트, `Agent` 조립은 L2. 파일·프로세스는 `harness.Env`(FileSystem + Shell) interface, 모델은 `sets.ModelCaller` 로만 받고 L2 는 `os`·`os/exec`·`net` 을 쓰지 않는다. Env 의 OS 구현은 `L3_ui/osenv`, ModelCaller 생성(adapters, API 키 env)·터미널·스트리밍 표시·이미지 첨부 읽기·설정 파일·env 해석·시그널·종료 코드는 L3. 검증용 CLI 는 `L3_ui/cmd/dr-print`. L2 가 여는 통로: `core.Observer`(스트림), `contract.Message`(텍스트·이미지 입력), `core.Run.Input`(follow-up) → **R17 로 정정**: OS Env·ModelCaller 생성·설정 파일·환경변수 해석은 L2(`L2_harness/osenv`·`L2_harness/agent`), L3 는 터미널 입출력만. `harness` 모듈이 실제 I/O 를 하지 않는 것(HN-C1)은 유지 | 사용자: "1개의 라이브러리, 외부 입출력(L3 에서 사용할 입출력 전부: 스트리밍, 이미지 등)은 상위 레이어, CLI 는 테스트용 별도" → "실제 입출력도 다 L3, 여기서는 에이전트 함수 하나" (안 C 선택). 근거: pi 새 harness `packages/agent/src/harness` 가 도구·JSONL 을 `ExecutionEnv` 위에 두고 Node 구현을 분리함 |
| R15 | **harness 의 외부 의존은 `golang.org/x/text` 하나만 허용** (edit fuzzy 매칭의 NFKC, pi 와 같음). Go 1.23 호환 최신인 v0.28.0 으로 고정하고 `go.work` 는 `go 1.23.0`. 다른 외부 의존은 여전히 두지 않는다 | 사용자: O4 에서 "x/text 의존 허용" |
| R16 | **시스템 프롬프트 편집 수단을 두지 않는다.** `harness` 는 항상 기본 조립(부록 A 문안 + tools·rules·project_context·skills·cwd)을 쓰고 `Options.Preamble`·`Options.Append` 를 제거한다. 다른 프롬프트가 필요한 개발자는 `prompt` 함수들을 골라 쓰거나 `sets.Deps.System` 을 직접 구성한다. L3(dr-print 등)는 시스템 프롬프트용 플래그·설정 키를 두지 않는다 | 사용자: "코어부분을 사용하는 개발자면 알아서 수정할 거고, 사용자는 굳이 시스템 프롬프트를 수정할 필요가 없지" → "Preamble/Append 도 제거" |
| R17 | **레이어 원칙: L0·L1 은 사용자 환경과 무관한 단독 라이브러리, L2 부터가 사용자 환경 위의 실제 에이전트, L3 는 완성된 에이전트를 쓰는 입출력.** L0 core = react 루프. L1 = 루프에 주입되는 함수 — 환경변수·파일을 읽지 않는다 (adapters 의 `os.Getenv` 기본값·`LoadConfig` 제거, `Getenv` nil 이면 환경을 읽지 않음). HTTP 로 모델을 부르는 것은 함수 자체의 기능이라 L1 에 둔다. L2 = 루프를 쓰는 에이전트, 모듈 3개: `L2_harness/harness`(Env·ModelCaller 위의 조립, 실제 I/O 없음, 결정적 테스트), `L2_harness/osenv`(Env 의 OS 구현), `L2_harness/agent`(설정 파일·환경변수 해석 + osenv + adapters → `harness.New`, 완성된 에이전트). L3 = 터미널 입출력(프롬프트 입력, 스트리밍 표시, 시그널, 종료 코드); `L3_ui/cmd/dr-print` 는 agent 만 쓴다. 세부 확정: 셸 = `$SHELL -c`(없으면 `/bin/sh`), 파일 권한 별도 보호 없음(0644/0755, umask), 설정 파일 = 중첩 `{sessionDir, agent, providers, models}`, dr-print 종료 코드 0/1/2/130, stdin 입력과 `-json` | 사용자: "core: react 루프, l1: react 루프에 주입되는 함수들, l2: 완성된 react 루프를 이용하는 하네스(에이전트), l3: 완성된 하네스(에이전트)를 이용하는 입/출력" → "L1, core 는 사용자의 환경과 상관없는 단독 라이브러리가 될 거고 L2 부터는 사용자의 환경에 올라가는 실제 에이전트". osenv 는 분리 모듈, 조립은 agent 모듈 (impl-03 인터뷰) |
| R18 | **surface 공통 계층은 L2 에 둔다** (R17). surface(L3) 는 `Approver` 와 이벤트 콜백만 구현하고 입출력만 한다. ① 도구 세트 — surface 프로필이 상한(설정 tools 와 교집합) ② 승인 — 규칙 `tool` / `tool(pattern)`, bash 는 `*` 와일드카드·파일 도구는 glob, 평가 deny > ask > allow, 맞는 규칙이 없으면 ask, Approver 가 없으면 ask = allow, `*` 가 든 allow 규칙은 복합 명령(`;` `&&` `||` `|` 백틱 `$(` 줄바꿈)을 허용하지 않음, 답 = 한 번 허용 / 세션 동안 같은 대상 허용 / 거부(사유). 규칙 출처 = 사용자 설정 + 프로젝트 설정(allow 포함) + 프로필 ③ 끼어들기 — `Abort`, `Steer`(같은 턴의 아직 시작하지 않은 도구는 skipped, pi 풍), `FollowUp`(run 이 끝나면 같은 세션으로 새 run) ④ 이벤트 — **동기 콜백만**, 직렬화, 닫힌 종류: message_start/update/end(update = delta + 누적 부분 메시지), tool_start/update/end(update = bash 실시간 출력), approval, retry, compaction, turn_end, run_end. 느린 surface 는 콜백 안에서 자기 큐를 둔다 ⑤ 세션 — `SessionKey(key)` = `k-` + sha256 앞 16 hex, `Sessions()` 목록(내용 기반) ⑥ 동시성 — 프로세스 내 세션 잠금, 바쁜 세션 Start 는 `ErrSessionBusy`, 프로세스 간은 보장하지 않음 ⑦ panic — L2 가 surface 콜백(OnEvent·Approver·Observer) 호출부에서 recover 해 그 run 만 failed (inject O3 종결) | 사용자: 공통 계층 먼저, 6개 항목 전부, L2 agent 확장, 승인은 규칙 단위(권장 문법), 프로젝트 설정 allow 포함, Steer 는 남은 도구 건너뜀, 이벤트는 동기만 (impl-03 인터뷰) |
| R19 | **대화형 터미널 surface** (impl-04 인터뷰). ① 형태 = 줄 단위 REPL + raw mode (덧붙이기 출력 + 맨 아래 입력 줄, 풀스크린 아님). **`golang.org/x/term` v0.34.0(+`x/sys` v0.35.0) 의존을 `L3_ui/surfaces/terminal` 에 한해 허용** (R15 확장, Go 1.23 호환 최신) ② 모듈 = `L3_ui/surfaces/terminal`(라이브러리) + `L3_ui/cmd/dr`(진입점) ③ 키: Enter = Start/Steer, Alt+Enter = FollowUp, Shift+Enter(kitty → modifyOtherKeys 협상)·Ctrl+J = 줄바꿈, Esc·Ctrl+C(실행 중) = Abort, 대기 Ctrl+C = 지우기/두 번 종료, Ctrl+D = 빈 줄 종료 ④ 승인 = y 이번만 / a 세션 / n 거부+사유, Esc·Ctrl+C = Abort, 직렬 ⑤ 렌더링 = 텍스트 원문, thinking 흐림, 도구 한 줄, bash 출력 호출당 20줄, retry·compaction·run_end(토큰 합계) ⑥ 세션 = 기본 새 세션, `-session`·`-c`·`-r`, 번호 목록, 이어갈 때 **편집 적용 뒤 투영 전부를 다시 그림, 요약은 빼고 생략 수만** → agent `History` 추가 ⑦ 명령 = `/help` `/quit` `/exit` `/sessions` `/resume` `/new` `/compact`(즉시, 세션 잠금 → harness·agent `Compact`, sets `ReserveTokens` 공개), `//` 이스케이프 ⑧ 비TTY = 종료 코드 2, 시그널 = Abort·복구 후 130 | 사용자: "대화형 터미널 해보자" → 형태 B, 모듈 c, Ctrl+C 도 Abort·줄바꿈 Shift+Enter, 승인 권장안, 렌더링 권장안, 지난 대화 다시 그리기(편집 적용 뒤·전부·요약 걸러냄), `/compact` 즉시, 비TTY 권장안 |
| R20 | **모델이 설정되지 않아도 실행된다, 모델은 앱 안에서 고른다.** ① `agent.New` 는 모델 없음·모르는 provider·키 없음이어도 성공하고, 사유는 `Agent.Model()` 의 에러와 `Start` 에러로 알린다 ② 고를 수 있는 목록은 L2(agent)가 만든다: 사용자 설정 `models` + adapters 내장 카탈로그 스냅샷(원격 카탈로그 제외, 네트워크 없음), 항목마다 키 환경변수 상태. 목록 원천은 adapters `Client.Models()` (L1) ③ `Agent.SetModel` 은 검증(형식·provider·키) 후 사용 중인 사용자 설정 파일의 `agent.model` 을 고쳐 쓴다(없으면 생성, 다른 키는 원문 보존). `-model` 플래그는 저장하지 않는 이번 실행 덮어쓰기 ④ 터미널: 시작 시 모델 문제면 사유 한 줄 + 선택 화면, 선택 화면 = 필터 입력 + 다시 그리는 영역의 일치 항목 최대 10개 + ↑↓ 하이라이트 + Enter 선택 + Esc 취소, `/model [provider/id]` (대기 중) | 사용자: "provider, id 는 설정이 안되어있으면 들어가서 설정할 수 있게 해야해. 없다고 실행을 못하면 안될것같아", "등록한 모델중에 고를 수 있게하는건 당연한거야. 이건 하네스에서 해야하는거고" → 구조 a(앱 안에서), 저장 a(설정 파일), 범위 a(설정+카탈로그, 키 상태), UI a(필터+하이라이트) |
| R21 | **환경변수를 쓰지 않는다 — 모든 값은 설정에서 (pi 방식, 환경변수 경로만 뺌).** ① API 키 = `<home>/.dynamic_react/auth.json` `{"<provider>":{"type":"api_key","key":"…"}}` (pi `auth.json` 의 API 키 항목, 새로 만들 때 파일 0600·디렉터리 0700). 저장·삭제는 L2 agent(`SetKey`·`RemoveKey`·`Providers`), 터미널 `/login`·`/logout`. 키 출처는 auth.json 하나 (pi 의 `--api-key`·models.json `apiKey`·`$NAME`·`!command`·환경변수 폴백 없음). adapters 는 `Options.APIKey(provider)` 로 받고 프로필의 `APIKeyEnv` 는 `RequiresKey` 가 된다. AD-C2(키는 환경변수로만)를 뒤집는다 — 금지하는 것은 키를 코드·대화·문서·이미지에 노출하는 것이고, auth.json 은 저장소 밖 사용자 파일(0600)이며 프로젝트 설정에는 키 필드가 없다 ② 모델 목록 = 키가 필요 없거나 키가 있는 provider 의 모델만 (pi `/model`). R20 ② 의 키 상태 표시(`KeyEnv`·`KeyMissing`)를 대체 ③ sessionDir = 설정 `sessionDir` 만 (`DR_SESSION_DIR` 삭제) ④ 셸 = 설정 `shellPath`, 없으면 `/bin/bash` (pi settings `shellPath`. R17 의 `$SHELL`·`/bin/sh` 를 대체) ⑤ 색 = dr `-no-color` 플래그 (`NO_COLOR` 삭제. L3 는 설정 파일을 읽지 않으므로 플래그, R17) ⑥ 예외: 홈 디렉터리(`os.UserHomeDir`, 설정 파일 위치), bash 도구 자식 프로세스의 OS 환경 상속, 테스트 전용 환경변수(실제 provider 종단 테스트를 켜는 값) | 사용자: "아얘 env 안쓰고 설정으로만 하도록 수정하자. env 지우고" → 범위 "전부 제거하고 모든 값을 설정으로. 환경변수는 전혀 사용하지 않는것으로 한다", "pi 에서 하는 방식 동일하게 가자" → 매핑 제안대로 (impl-05) |
