# 인계: impl-06 — 완성된 에이전트까지 주입 통로 잇기 (R22)

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 진행한다. 사용자 지시: **"전부 진행해야해."** — 아래 R22 를 스펙 → 테스트(RED) → 구현(GREEN) → 검증까지 끝낸다.

배경 (impl-05 끝에서 사용자가 물음 "초기의 목적은 달성한건가? 기본 루프 안에서 원하는대로 외부 주입을 통해 컨트롤 하는 에이전트라는?"):
L0·L1 의 목적(D1~D6: 22 step 고정 루프, 모든 동작은 주입 함수, 기본값은 sets)은 달성됐지만, **사용자가 실제로 쓰는 완성된 에이전트(`agent.New` → dr / dr-print)에는 외부 함수를 끼울 통로가 없다.** `harness.Options` 에는 `Extra []inject.FunctionSet`·`Tools []sets.Tool` 이 있지만 ① `agent.Options` 가 받지도 넘기지도 않고 ② harness 의 Extra 를 쓰는 테스트가 하나도 없고 ③ 주입 예시가 README·코드 어디에도 없다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋은 이 문서 커밋).
2. 기준 문서를 읽는다: `docs/DECISIONS.md` **D1~D6, D12, R13, R17, R18, R21**, `docs/VERIFY-22step.md`(28개 기능 대응, △ 4개), `L2_harness/SPEC.md` **HN-FR-26(Bind 순서), HN-FR-30(Gate), 3.6a(surface 래핑)**, `L2_harness/agent/SPEC.md` **3.3 Options, AG-FR-09, 3.5(도구 상한 AG-FR-21)**, `L1_inject/inject/SPEC.md` 의 Bind·조합(3.7) 절, `L1_inject/sets/SPEC.md` 의 Deps.
3. **결정 해소부터 (구현 전, 사용자에게 한 번에 하나씩, 권장안과 함께).** 아래 "미결 결정" 표를 의존 순서대로 묻는다. 사용자는 AskUserQuestion 에 자유 입력으로 반문·수정하니 그 지시를 먼저 반영한다.
4. 스펙: `docs/DECISIONS.md` R22 추가, `L2_harness/agent/SPEC.md` v0.6 (주입 필드·전달·Gate 와의 관계), 필요하면 `L2_harness/SPEC.md` v1.0 (Extra 의 의미·Deps 노출), README 의 주입 절. 변경 기록에 이전/새/이유.
5. 테스트(RED 를 눈으로 본다) → 구현 → 각 단계 뒤 검증: `gofmt -l`, `go vet`, `go test -race -count=3`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test`, 커버리지, **뮤테이션**(살아남으면 동치면 단순화, 아니면 테스트 보강), 사용자 보고. 커밋은 사용자 확인 후 (impl-05 에서는 "구현까지" 라는 답으로 스펙·구현을 함께 커밋해도 된다고 했다 — 이번에도 물어서 정한다).
6. 마지막에 **deepseek 로 실제 종단 확인**: 주입한 세트(예: 매 턴 지시를 덧붙이는 `context.inject`, 특정 bash 를 막는 `tool.before`)가 실제 모델 run 에서 효과를 내는지 (아래 "실제 모델 시험 방법").

**하지 말 것**: 구독 계정 로그인(Claude Pro/Max·ChatGPT 플랜 OAuth) 구현 — impl-05 에서 사용자에게 설명했고 사용자가 "넘어가자" 로 정리했다 (다시 꺼내지 말 것). 환경변수 읽기 추가 (R21). L3 가 설정 파일을 직접 읽거나 쓰기 (R17).

## 미결 결정 (R22, 사용자와 해소할 것 — 권장안 포함)

| # | 질문 | 권장안 | 근거 |
|---|---|---|---|
| 1 | 주입 통로의 범위: `agent.Options` 에 `Extra []inject.FunctionSet` + `Tools []sets.Tool` 만 더해 그대로 harness 로 넘길까, 설정 파일로도 고를 수 있게(이름 → 세트 레지스트리) 할까 | **Go 필드만** (D12: 서드파티도 같은 프로세스의 Go FunctionSet). 설정 파일 선택은 비목표 | D12, R17 |
| 2 | 사용자 세트가 harness 내부 의존(`sets.Deps`: 세션 Store·ModelCaller·도구 레지스트리·요약기·시스템 프롬프트)을 볼 수 있게 할까. 지금은 harness 가 Deps 를 안에서 만들어 base·resilient 에만 준다 → 기본 함수를 **감싸서**(combine.Wrap) 쓰려면 Deps 가 필요 | **`Extra` 를 값 대신 생성 함수 `func(sets.Deps) inject.FunctionSet` 로도 받기** (예: `Options.ExtraFn`) — 또는 Extra 는 값 그대로 두고 감싸기는 base 를 다시 만들지 않는 `combine` 으로만. 사용자에게 두 안을 코드 미리보기로 보여 줄 것 | `L1_inject/sets/deps.go:216 type Deps`, `sets.Base(d)`, `combine` 패키지 `Wrap` |
| 3 | Extra 가 `tool.before` 를 덮을 때 agent 의 승인(Gate)·base 검사(없는 도구·스키마, ST-FR-38)는 어떻게 되나. 현재 `harness/surface.go:185` 는 **ToolBefore 를 가진 마지막 세트**에 Gate 를 씌운다 → 승인은 유지, base 검사는 사용자가 base 를 감싸지 않으면 빠진다 | **승인은 항상 유지(현재 동작), base 검사 생략은 사용자 책임으로 스펙에 명시** + 테스트로 고정 | HN-FR-30, 3.6a |
| 4 | `agent.Options.Tools` 의 추가 도구와 도구 상한(AG-FR-21, surface 프로필)·승인 규칙(`tool(pattern)`)의 관계 | 추가 도구도 registry 에 들어가 상한·규칙의 대상이 된다(이름으로). 규칙 패턴은 bash·파일 도구 외에는 `tool` 이름만 | AG-FR-12~21 |
| 5 | 주입 예시를 어디에 둘까 | README 에 짧은 코드 + 실행 가능한 `examples/` 모듈 하나(go.work 포함, 독립 go.mod) — 예: "매 턴 규칙을 덧붙이고 `rm -rf` 를 막는 에이전트" | README 에 예시 없음 (impl-05 확인) |

## 어디까지 왔나 (커밋)

| 커밋 | 내용 |
|---|---|
| `8c5d2a8` | R20 스펙 |
| `1eeea15` | R21 스펙: 환경변수 제거, 키는 auth.json (adapters v0.6, agent v0.5, harness v0.9, osenv v0.2, terminal v0.3, dr v0.3, dr-print v0.2) |
| `c0bfe66` | R20+R21 구현: adapters `Models`·`Providers`·`APIKey`·`RequiresKey`, agent `Model`·`Models`·`SetModel`·`SetKey`·`RemoveKey`·`Providers`·`shellPath`, terminal 모델·provider 선택·키 입력(`•`)·`/model`·`/login`·`/logout`, dr `-no-color` |
| `ab39c74` | fix: Responses 스트림 `error` 이벤트의 중첩 `error.{type,code,message}` 를 읽음 (OpenAI 크레딧 소진 문구가 비어 보이던 결함) |
| `1c3c743` | `THIRD_PARTY_NOTICES.md` (O2 종결), impl-05 인계 문서 정정 |
| `a0afc09` | README 현재화(구조·버전·실행·설정 파일 표·예시), adapters vLLM `max_model_len`·Xiaomi base URL 출처 확인 |

검증 상태(`a0afc09`): 모듈 11개 전부 gofmt·vet·`-race -count=3`·`GOWORK=off`·tidy 통과. 테스트 아닌 코드에 환경변수 읽기 없음(예외 `os.UserHomeDir`). 커버리지 harness 97.1%, session 97.9%, prompt 94.8%, summary 94.7%, tools 91.7%, osenv 90.3%, agent 93.6%, terminal 96.6%, adapters 91.5%.

실제 확인 (impl-05):
- pty 시험 16/16: raw mode 진입·복구, SIGWINCH 재그리기, SIGTERM(실행 중)·SIGHUP·SIGINT → 130·복구, 실행 중 Ctrl+C = Abort.
- deepseek(`deepseek/deepseek-flash`) 종단: AG-NFR-02 PASS, dr-print read→edit→bash 3라운드(파일 실제 변경, reasoning_content 왕복), 세션 이어가기, dr 승인 y/a/n+사유, 도구 실행 중 Steer, Alt+Enter FollowUp, 임계치 자동 압축(실제 요약 문서 확인).
- 미확인: Anthropic·OpenAI 방언 실제 응답(키·크레딧 없음), provider overflow 에러 복구(deepseek 창 1M), 뮤턴트 2개 관측 불가(agent `NoRemoteCatalog` 전달, terminal 종료 중 경합).

## 현재 구조 (주입 관련)

| 위치 | 내용 |
|---|---|
| `L2_harness/harness/harness.go:29` `Options` | `Env`, `Model`, `SessionDir`, `Tools []sets.Tool`(추가·교체), `Extra []inject.FunctionSet`(base·resilient 위에 겹침), `Observer`, `Core`, `OnEvent`, `Gate` |
| `harness.go:78·98·102` | `reg := sets.NewRegistry(tools.All(o.Env)...)` + Tools, `fsets := [Base(deps), Resilient(deps), Extra...]`, `inject.BindWith(...)` |
| `harness/surface.go:71~` | OnEvent·Gate 래핑. `last(ToolBefore != nil)` 세트의 ToolBefore 를 감싼다(185) |
| `L2_harness/agent/agent.go` `New` | `harness.New(ctx, harness.Options{Env, Model: a.client, SessionDir, OnEvent, Gate: a.gate})` — **Extra·Tools 를 넘기지 않음**. 도구 상한은 `limitTools(cfg, o.Tools []string, h.Tools().Names())` (이름 목록, `agent.Options.Tools` 는 이미 `[]string` 상한으로 쓰이고 있다 — 새 필드 이름이 겹치지 않게 할 것) |
| `L1_inject/sets/deps.go:216` | `type Deps struct` (base·resilient 가 받는 의존) |
| `L1_inject/inject/combine` | step 별 `Wrap`·`Concat` 등 조합 함수 (`zz_generated.go`) |

## 먼저 읽을 것

1. `docs/DECISIONS.md` D1~D6, D12, R13, R17, R18, R21
2. `L2_harness/harness/harness.go` (Options, New), `harness/surface.go` (래핑 규칙)
3. `L2_harness/agent/agent.go` `New`, `limitTools`
4. `L1_inject/sets/deps.go` (Deps, Tool, Registry), `L1_inject/inject/combine/zz_generated.go` 의 Wrap 서명
5. `docs/VERIFY-22step.md`

## 이 세션이 값을 치르고 배운 것

- **뮤테이션 스크립트** `/tmp/mut.py` 는 이번 세션에도 남아 있다(다음에도 있다고 가정하지 말 것). 형식: `python3 /tmp/mut.py <모듈 디렉터리> <파일> <뮤턴트 JSON 파일 경로>` — **세 번째 인자는 JSON 문자열이 아니라 파일 경로**다 (`[[old,new],...]`, 정확히 1회 매칭, `BADMATCH` 는 들여쓰기 불일치). JSON 은 heredoc 로 파일에 쓴다(쉘 `echo` 로 탭 이스케이프를 넣으면 깨짐). 돌리는 동안 그 모듈 파일을 편집하지 말 것.
- 뮤턴트가 살아남으면 대부분 **테스트 입력이 그 분기를 타지 않아서**였다 (예: rate 케이스가 type·code 둘 다 `rate_limit` 이라 한쪽만 바꾼 뮤턴트가 산다 → 한쪽으로만 분류되는 케이스 추가). 동치면 코드를 단순화한다 (agent `agentConfig` 의 "모델 완전하면 원래 에러" 조건을 지웠다).
- 여러 파일 문서 편집은 `assert s.count(a)==1` python 스크립트가 안전. 표의 위치 리터럴 struct 에 필드를 더하면 기존 행이 컴파일 에러 — 행 끝을 일괄 보정.
- `sed -i '' 's/\bfoo(/...'` 는 macOS sed 에서 `\b` 가 안 먹는다 → `perl -pi -e`.
- 테스트 헬퍼 이름 충돌 주의: agent 패키지 테스트에 이미 `newAgent`(history_test.go)·`setup`·`writeConfig`·`writeAuth`·`mkAgent`(model_test.go)가 있다.
- terminal 테스트: 가상 화면 `text()` 는 행 끝 공백을 자른다. ESC 다음 글자를 바로 보내면 Alt+글자로 해석된다(`\x1b/resume` ✗) — ESC 뒤 대기 확인 후 보낸다. 한글은 2칸 폭.
- pty 시험: cwd 를 HOME 과 같게 두면 사용자 설정이 프로젝트 설정(`<cwd>/.dynamic_react/config.json`, permissions 만 허용)으로도 읽혀 실패한다 — 분리할 것. dr 은 원격 카탈로그(models.dev)를 켠 채라 **첫 모델 요청 전 지연**이 있다 — 시그널 시험은 요청이 서버에 도착한 뒤 보낼 것. 파이썬 `pty.openpty` + `termios.tcgetattr(slave)` 로 raw mode·복구를 확인했다. DA 질의(`\x1b[c`)에 `\x1b[?62;c` 로 답해 줘야 협상이 끝난다.
- 실제 모델 시험에서 모델은 출력을 스스로 줄인다(`seq 1 1500 | tail -1`). 문맥을 키우려면 큰 파일을 read 로 통째로 읽게 한다. 큰 도구 결과 하나는 keepRecent 구간에 남아 압축되지 않는다(pi 와 같은 정상 동작).
- **실제 모델 시험 방법**: 임시 HOME(`/tmp/e2e/h-*`)에 `config.json` + `~/.dynamic_react/auth.json` 사본(0600), 임시 cwd. 키는 `python3 -c "import json,os;print(json.load(open(os.path.expanduser('~/.dynamic_react/auth.json')))['deepseek']['key'])"` 로 읽어 환경변수(`DR_E2E_KEY`)에만 넘기고 **출력하지 않는다**. 끝나면 키 사본이 든 임시 디렉터리를 즉시 지운다. 바이너리는 `go build -o /tmp/...` (저장소 안에 빌드하지 않음).
- OpenAI 실패의 원인은 계정 API 크레딧 소진(`credit_balance_exhausted`) — ChatGPT 플랜과 API 결제는 별개.
- 사용자는 레이어 책임을 엄격히 본다(목록·검증·저장은 L2). 결정은 한 번에 하나씩, 권장안과 설명을 먼저.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에. 스펙에 없는 Go 표현(새 export 포함)은 구현 전에 스펙에 먼저. 구현 중 정한 세부는 스펙에 반영하고 보고 때 "검토 부탁".
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`. 외부 의존: x/text(harness·terminal), x/term(terminal). 새 외부 의존은 사용자 결정.
- 키를 코드·대화·문서·이미지에 노출하지 않음. 키는 `<home>/.dynamic_react/auth.json`(0600)에만 (R21). 구독 요금제 엔드포인트·구독 OAuth 경로 금지 (AD-C1). 환경변수를 읽지 않음 (R21, 예외: 홈 디렉터리·자식 프로세스 상속·테스트 전용 `DR_E2E_*`).
- 라이선스: 서드파티 문안·데이터를 새로 가져오면 `THIRD_PARTY_NOTICES.md` 에 더한다.

## 사용자 결정 중 유효한 것

- D1~D16, R1~R21 (R10 폐기, R14 는 R17 로 정정).
- R20: 모델 없이도 실행, 앱 안에서 고르기, 저장은 설정 파일 `agent.model`, 목록 = 설정 models + 내장 스냅샷(원격 제외).
- R21: 환경변수 전부 제거, pi 방식(auth.json·`/login`), 키 없는 provider 의 모델은 목록에서 숨김, sessionDir = 설정만, 셸 = `shellPath`(기본 `/bin/bash`), 색 = `-no-color`.
- 이 저장소 자체의 LICENSE 는 미정 (사용자 결정 대기 — 묻지 않았으면 손대지 말 것).

## 남은 미결 (R22 밖)

| 위치 | 내용 |
|---|---|
| 저장소 | 자체 LICENSE 미정 |
| adapters | Anthropic·OpenAI 방언 실제 응답 검증 (API 키·크레딧 필요) |
| sets/harness | provider overflow 에러 복구의 실제 모델 확인 (가짜 서버로만 검증) |
| agent SPEC | 절 번호 중복: "3.13 API 키 (R21)" 와 "3.13 비기능" — R22 스펙 작업 때 바로잡을 것 |
