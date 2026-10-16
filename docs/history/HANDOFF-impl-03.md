# 인계: impl-03 — 시스템 프롬프트 편집 가능성 → L3 osenv·dr-print

## 착수 블록 (이번 세션의 지시 전부)

`dynamic_react` 에서 아래 순서로 진행한다. 기준 문서는 `docs/DECISIONS.md`(R13~R15), `L2_harness/SPEC.md` **v0.4**.

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋 `a5ae660`).
2. **시스템 프롬프트 편집 가능성 (사용자 지시, 최우선).** 사용자: "시스템 프롬프트도 당연히 편집 가능해야 할 거고." 현재 편집 수단은 `harness.Options.Preamble`(preamble·tools·rules 세 절을 통째로 대체, pi `customPrompt`)과 `Options.Append`(`<addendum>` 절 추가)뿐이다. 빠진 것:
   - 전체 대체 (pi `forceSystemPrompt` — 조립 없이 문자열 그대로)
   - 절 단위 편집 (tools·rules·project_context·skills·cwd 중 일부만 바꾸거나 끄기, pi `sections` 처럼 사용자 절 추가)
   - 기본 문안(`prompt.DefaultPreamble`, 스펙 부록 A)은 코드 상수라 바꾸려면 Preamble 로 전부 대체해야 함
   → **어느 수준까지 편집 가능해야 하는지 사용자에게 먼저 묻는다** (AskUserQuestion, 한 번에 하나, 권장안 포함). 권장안 초안: `prompt.Input` 에 `Override string`(전체 대체) + `Sections map[string]*string`(nil=끔, 값=대체, 새 이름=추가, 순서 규칙 명시)을 두고 `harness.Options` 로 노출. 확정되면 SDD 순서: 스펙(HN-FR-17 개정·변경 기록) → 테스트(RED) → 구현 → 뮤테이션. 시스템 프롬프트는 run 동안 불변(HN-C4, sets ST-FR-19)은 유지 — 편집은 `New` 시점.
3. **L3_ui/osenv + L3_ui/cmd/dr-print 스펙 작성 (미결 O3)** → 사용자 확인 → 테스트 → 구현.
   - osenv: `env.Env` 의 OS 구현 (`os`, `os/exec`). `envtest.ConformanceFS(t, fs, t.TempDir())`·`envtest.ConformanceShell(t, sh, dir)` 를 통과해야 한다 (ConformanceShell 은 아직 한 번도 실제로 돌려 보지 않았다 — 실제 셸에서 처음 검증됨). 계약: 없는 파일 = `fs.ErrNotExist`, `AppendFile` 한 번 = write 한 번, `Exec` 는 `$SHELL -c`(없으면 `/bin/sh`), stdout+stderr 합쳐 out 에, 프로세스 그룹으로 실행해 ctx 취소·Timeout 시 자식 전체 kill, Timeout 은 `TimedOut=true`(에러 아님), `ListDir` 이름순, `CreateTemp` 는 `os.CreateTemp` 기반 절대 경로, 디렉터리 권한(세션 0700 등) 정할 것.
   - dr-print: 검증용 CLI (L2 SPEC 옛 3.6 → L3 로 이동). `harness.New` + `adapters.New` 로 run 하나. text delta stdout, 도구 호출·에러·sessionID stderr, 종료 코드 0/1, Ctrl-C 취소, `-model provider/id -session -cwd -config`. 설정 파일·env(`DR_SESSION_DIR`, API 키 env) 해석은 여기서. 모듈 `L3_ui/cmd/dr-print` 의 go.mod 는 이미 있다(내용은 module 선언뿐). osenv 도 새 모듈(`L3_ui/osenv`)로 — go.mod·go.work 추가.
   - 실제 provider 종단 테스트는 키 env 가 있을 때만 (HN-NFR-03 은 L3 로 옮겨 적을 것).
4. 각 단계 뒤: `go vet`, `gofmt`, `go test -race -count=3`, `GOWORK=off GOTOOLCHAIN=local go mod tidy && go test`, 커버리지, **뮤테이션 확인**, 사용자 보고. 커밋은 사용자 확인 후.

**하지 말 것**: 라이선스 고지(O2 — pi 문안·adapters 스냅샷·x/text). 사용자: "라이센스 고지는 아직은 하지마. 마지막에 정리하면서 할거야."

## 어디까지 왔나 (커밋)

| 커밋 | 내용 |
|---|---|
| `e71263d` | L1 sets 구현 (base·resilient), sets 스펙 v0.4 (ST-FR-41~45) → **L0 + L1 완료** |
| `7c815ce` | L2_harness 스펙 v0.2, R14(에이전트 함수 하나, 실제 I/O 는 L3), D16 정정(dr-print → `L3_ui/cmd/dr-print`), 모듈 배치 |
| `a5ae660` | L2_harness 구현 + 스펙 v0.4, R15(`golang.org/x/text` v0.28.0 허용), go.work `go 1.23.0` |

L2 검증 상태: `-race -count=3`, `GOWORK=off` 단독, 뮤테이션 21건 전부 KILLED, 커버리지 약 88%, 전 모듈(contract·core·inject·adapters·sets·harness) 테스트 통과.

## L2_harness 구조 (구현 완료)

모듈 `github.com/Biisairo/dynamic_react/L2_harness/harness`:

| 패키지 | 내용 |
|---|---|
| `env` | `FileSystem`(ReadFile, WriteFile, AppendFile, Stat, ListDir, CreateTemp) + `Shell`(Exec(ctx, ExecRequest{Command, Dir, Timeout}, out io.Writer) (ExecResult{ExitCode, TimedOut}, error)) + `Cwd()`·`Home()`. 경로는 절대 경로만 |
| `envtest` | 메모리 가짜 Env (`New(cwd, home)`, SetFile/SetDir/File/FailOn/AppendCount/SetScript/ExecRequests), `ConformanceFS`, `ConformanceShell` |
| `tools` | read·write·edit·bash (`All(env)` 은 큐 공유, `Snippet(name)`). edit = pi fuzzy(NFKC 포함) + LCS unified diff. bash = 끝 2000줄/50KB, 크면 CreateTemp spill |
| `session` | JSONL `sets.Store` (`New(fs, dir)`, `CheckID`, `NewID(now, entropy)`, `DefaultDir(getenv, home)`) |
| `prompt` | `Build(Input)`, `DefaultPreamble`(부록 A), `ContextFiles`, `Skills` |
| `summary` | `New(model, now)`, pi 요약·갱신 프롬프트, `Serialize` |
| `harness` | `New(ctx, Options{Env, Model, SessionDir, Preamble, Append, Tools, Extra, Observer, Core, Now, NewID})` → `Agent.Start(ctx, RunRequest{SessionID, Input, Config json.RawMessage})` → `(*core.Run, sessionID, error)`, `Agent.System()`, `Agent.Tools()` |

## 먼저 읽을 것

1. `docs/DECISIONS.md` — R13, R14, R15, D16(정정)
2. `L2_harness/SPEC.md` v0.4 전체 — 1.2 범위(L3 로 간 것), 2.2 HN-C1, 3.1 Env 계약(HN-FR-01~04), 3.4 프롬프트(HN-FR-17~21), 3.6 조립, 6 미결, 7 변경 기록, 부록 A
3. `L2_harness/harness/envtest/conformance.go` — osenv 가 통과해야 할 계약
4. `L2_harness/harness/prompt/prompt.go` — 편집 가능성 작업 대상
5. pi 참조: `../hermes_study/pi-mono/packages/coding-agent/src/core/system-prompt.ts`(customPrompt·forceSystemPrompt·sections), `packages/agent/src/harness/env/nodejs.ts`(Node Env 구현 — osenv 참고), `packages/agent/src/harness/types.ts`(ExecutionEnv)

## 이 세션이 값을 치르고 배운 것

- **사용자는 입출력 경계를 엄격하게 본다.** "외부 입출력은 상위 레이어" → 처음엔 터미널만 뺐다가 "실제 입출력도 다 L3" 로 정정됨. L2 는 `os`·`os/exec`·`net`·`path/filepath` import 금지(`TestImports` 가 검사). 새 기능이 I/O 를 요구하면 Env 로 빼라.
- **다른 제품 근거를 물으면 로컬 소스로 확인해 답한다.** `hermes_study/pi-mono`(pi), `hermes_study/oh-my-pi` 가 로컬에 있다. pi 새 harness(`packages/agent/src/harness`)가 우리 구조의 원형이다.
- **x/text 최신은 Go 1.26 을 요구한다.** Go 1.23 호환 최신은 v0.28.0 (v0.29 부터 1.24). `go get` 은 `GOTOOLCHAIN=local` 로 — 안 그러면 go 지시문을 올려 버린다. 모듈이 `go 1.23.0` 이면 go.work 도 `go 1.23.0` 이어야 한다.
- **Write 도구가 `"﻿"` 이스케이프를 실제 BOM 문자로 바꿔 쓴다** (Go 컴파일 에러 "illegal byte order mark"). BOM 은 쓴 뒤 python 으로 `\\ufeff` 이스케이프로 되돌려라.
- **"절이 없다" 를 부분 문자열로 검사하지 마라.** 문안에 `<project_context>` 라는 단어가 들어가자 오탐. 절 머리 `\n<name>\n` 로 검사.
- **뮤테이션 스크립트**: `/tmp/mut.py`, `/tmp/mut2.py` 는 이 세션의 임시 파일이라 없다. 같은 방식(파일 백업 → 정확히 1회 매칭 치환 → `go test ./...` → 복원)을 python 으로 다시 만든다. sed 는 쓰지 않는다.
- **sets.Config 값을 JSON 으로 넘기지 마라** — zero 값 `enabled=false` 가 압축을 끈다. 설정은 raw JSON 으로.
- **상태는 Store 로 run 을 넘어 복원된다** — run 범위 상태에는 RunID 를 둔다 (sets ST-FR-44).
- **inject ctx 는 dispatch 가 끝나면 폐기된다** — 9 에서 `ctx.Context()` 를 미리 꺼낸다.
- 사용자는 AskUserQuestion 팝업을 두 번 거절하고 직접 타이핑으로 답한 적이 있다. 이후 "자세히 설명하고 질문해줘" 에서는 팝업으로 답했다 → **설명을 먼저 충분히 쓰고 팝업으로 묻는다.**

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148 구조. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에. 스펙에 없는 Go 표현은 구현 전에 스펙에 먼저 적는다.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI (사용자 CLAUDE.md). Go 는 gopls.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다.
- Go 1.23 호환 유지 (`t.Context`, `b.Loop` 같은 1.24+ API 금지). 외부 의존은 `golang.org/x/text` 만 (R15, harness 한정).
- API 키는 환경변수로만, 구독 요금제 엔드포인트·OAuth 경로 금지.

## 사용자 결정 중 유효한 것

- D1~D16 (D12·D14·D15·D16 정정본), R1~R15 (R10 폐기).
- R14: L2 = 에이전트 함수 하나, 실제 I/O 전부 L3 (안 C, pi 새 harness 구조). OS Env 는 `L3_ui/osenv`, dr-print 는 `L3_ui/cmd/dr-print`(검증용).
- R15: `golang.org/x/text` v0.28.0 허용.
- 기본 문안(L2 SPEC 부록 A) 확정. 번역은 참고용으로 부록에.
- 시스템 프롬프트는 편집 가능해야 한다 (범위 미정 — 착수 블록 2).
- 라이선스 고지는 마지막 정리 때 (지금 하지 않음).
- 각 스펙 변경 기록의 "사용자 확정" 항목 전부.

## 남은 미결

| 위치 | 내용 |
|---|---|
| L2 편집 가능성 | 시스템 프롬프트 편집 범위 (착수 블록 2) |
| L2 O2 | 라이선스 고지 — 마지막 정리 때 |
| L2 O3 | `L3_ui/osenv`·`L3_ui/cmd/dr-print` 스펙 |
| inject O3 | dispatch 밖 panic 정책 — L3_ui 에서 재검토 |
| adapters | vLLM `max_model_len` 필드 문서 확인 못 함, Xiaomi base URL 은 pi 값 |
