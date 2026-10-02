# harness — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 1.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D14, D15, D16, R8, R13, R14, R17, R18, R22, R23, R25)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`, `L1_inject/sets/SPEC.md`
- 사용하는 쪽: `L2_harness/agent` (OS Env·ModelCaller·설정을 넣는 완성된 에이전트, `agent/SPEC.md`). Env 의 OS 구현은 `L2_harness/osenv` (`osenv/SPEC.md`)

---

## 1. 소개

### 1.1 목적
L1(inject + sets) 위에서 **코딩 에이전트를 함수 하나(`Agent`)로** 제공한다 (R13, R14). 도구가 무엇을 하는지(인자, 절단, edit 알고리즘), 세션을 어떤 형식으로 남기는지, 시스템 프롬프트·요약 프롬프트를 어떻게 만드는지는 여기서 정한다. **실제 입출력(파일, 프로세스, 네트워크, 터미널, 환경변수)은 하지 않고** `Env` interface 와 `sets.ModelCaller` 로 받는다 (pi `packages/agent/src/harness` 의 `ExecutionEnv` 구조). 실제 입출력을 붙인 완성된 에이전트는 같은 L2 의 `osenv`·`agent` 모듈이다 (R17).

### 1.2 범위
- 포함 (모듈 `L2_harness/harness`):
  - `Env` interface (FileSystem + Shell) — 3.1
  - 도구 7종 `read`·`write`·`edit`·`bash`·`grep`·`find`·`ls` 의 로직 (`sets.Tool`, Env 위에서 동작) — 3.2
  - JSONL 세션 형식과 `sets.Store` 구현 (Env.FileSystem 위에서 동작) — 3.3
  - 시스템 프롬프트 조립: 기본 문안, 대체·덧붙임, `AGENTS.md`·`CLAUDE.md` 탐색, 스킬 목록 (Env.FileSystem 으로 읽음) — 3.4
  - 요약기 (`sets.Summarizer`, pi 요약 프롬프트, ModelCaller 로 호출) — 3.5
  - 조립: `harness.New(Options)` → `Agent.Start` → `*core.Run` — 3.6
  - 주입: 함수 세트 목록과 부품(도구·저장소·시스템 프롬프트·요약기)을 생성 시점에 바꾸는 함수 — 3.6c (R22)
- 제외 — 같은 L2 의 다른 모듈 (R17):
  - `Env` 의 OS 구현 (`os`·`os/exec`, 프로세스 그룹 kill, 임시 파일) → `L2_harness/osenv`
  - `ModelCaller` 생성 (adapters.Client, API 키 파일), 설정 파일·키 파일 해석 → `L2_harness/agent`
- 제외 — L3 (R17): 터미널 입출력, 스트리밍 표시, 사용자 이미지 첨부 읽기, 시그널, 종료 코드, 검증용 CLI `L3_ui/cmd/dr-print`
- 제외 — 다음 단계: 권한 승인, 서브에이전트, MCP, 병렬 도구 스케줄러 (sets 1.2 와 같음)
- 제외 — R22 비목표: 설정 파일로 세트 고르기, 실행 중(run 도중·run 사이) 세트·부품 교체

위 레이어가 쓰는 통로: 스트림·생명주기 = `core.Observer`, 사용자 입력(텍스트·이미지) = `contract.Message`, follow-up = `core.Run.Input`.

### 1.3 참조

| 값 | 출처 |
|---|---|
| Env = FileSystem + Shell, 도구·JSONL 이 Env 위에서 동작 | pi `packages/agent/src/harness/types.ts` `ExecutionEnv`, `tools/tool-context.ts`, `session/jsonl/io.ts` |
| 도구 4종 인자·동작, edit fuzzy 매칭 | pi `packages/agent/src/harness/tools/`, `packages/coding-agent/src/core/tools/` (02 문서 10장) |
| 도구 결과 2000줄 / 50KB | pi `utils/truncate.ts` (sets 1.3 과 같음) |
| 시스템 프롬프트 구조 (preamble·tools·rules·project_context·skills·cwd) | pi `coding-agent/src/core/system-prompt.ts` |
| 요약 시스템 프롬프트·요약 프롬프트·갱신 프롬프트, 직렬화 | pi `coding-agent/src/core/compaction/utils.ts`·`compaction.ts` |
| 기본 sessionDir `~/.dynamic_react/sessions` | impl-01 사용자 확정 (값의 해석은 `L2_harness/agent`, 3.3 HN-FR-15) |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 L2_harness/agent ── osenv.New(cwd) : harness.Env        (os, os/exec)
                  ── adapters.New(...) : sets.ModelCaller (HTTP, API key env)
                  ── config file / env
 L3_ui ── agent ── terminal / stream display (core.Observer) / image files -> contract.Message
        │ harness.Options{Env, Model, SessionDir, ...}
        ▼
 harness.New ── ToolSet(tools.All(env)) ── Store(session.New(env, dir)) ── System(prompt.Build(...)) ── Summarizer(summary.New(model))
        │        └──────────────────────────── sets.Deps ──────────────────────────────┘
        ▼
 inject.BindWith(StateStore: sets.StateStoreOf(store), Sets(deps)...)   // Sets nil → DefaultSets = sets.Base, sets.Resilient
        ▼
 core.New(dispatcher, core.Options{Observer}) ── Agent.Start(ctx, RunRequest) -> *core.Run
```

모듈 의존: harness → contract, core, inject, sets, 표준 라이브러리. **adapters·osenv 에 의존하지 않는다** (둘은 `L2_harness/agent` 가 넣음).

### 2.2 제약
- HN-C1. **harness 는 실제 입출력을 하지 않는다** (외부 모듈은 `golang.org/x/text` 만, HN-NFR-04): `os`, `os/exec`, `net`, `net/http`, `syscall`, `io/ioutil`, `path/filepath` 의 파일시스템 접근 함수(`Walk`, `Glob`, `Abs`, `EvalSymlinks`)를 쓰지 않는다. 파일·프로세스는 `Env`, 모델은 `ModelCaller` 로만 (R14). 시계·ID 는 `sets.Deps` 와 같이 주입(`Now`, `NewID`)한다
- HN-C2. API 키 값을 받는 필드가 없고, 세션 파일·에러 문구에 키 값을 쓰지 않는다 (키는 ModelCaller 구현의 몫).
- HN-C3. 권한 승인이 없다 — `bash`·`write`·`edit` 는 제한 없이 실행된다 (pi 와 같음, sets ST-C2). 경고 표시는 L3 의 몫이다.
- HN-C4. 시스템 프롬프트는 `New` 에서 한 번 조립하고 바꾸지 않는다 (sets ST-FR-19).

---

## 3. 상세 요구사항

### 3.1 Env (`harness/env` 패키지)

`harness` 가 `tools` 를 import 하므로 import 순환을 피하려고 별도 패키지에 둔다. `harness.Env` 는 `env.Env` 의 별칭이다.

```go
type FileInfo struct { Name string; IsDir bool; Size int64; Symlink bool }  // Symlink: ListDir 항목이 심볼릭 링크 (IsDir·Size 는 대상 기준)

type FileSystem interface {
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, data []byte) error   // 생성·덮어쓰기, 상위 디렉터리 생성
    AppendFile(ctx context.Context, path string, data []byte) error  // 없으면 생성, 상위 디렉터리 생성
    Stat(ctx context.Context, path string) (FileInfo, error)
    ListDir(ctx context.Context, path string) ([]FileInfo, error)    // 이름순
    CreateTemp(ctx context.Context, pattern string) (path string, w io.WriteCloser, err error)
}

type ExecRequest struct { Command string; Dir string; Timeout time.Duration }
type ExecResult  struct { ExitCode int; TimedOut bool }

type Shell interface {
    // Exec 는 Command 를 셸로 실행하고 stdout·stderr 를 합쳐 out 에 흘린다.
    Exec(ctx context.Context, req ExecRequest, out io.Writer) (ExecResult, error)
}

type Env interface {
    FileSystem
    Shell
    Cwd() string   // 절대 경로
    Home() string  // 절대 경로, 모르면 ""
}
```

| id | 요구사항 |
|---|---|
| HN-FR-01 | Env 의 경로 인자는 **절대 경로**다. 상대 경로·`~` 해석은 harness 가 한다 (HN-FR-05). 구분자는 `/` |
| HN-FR-02 | 없는 파일은 `errors.Is(err, fs.ErrNotExist)` 로 판별 가능한 에러여야 한다 (Env 구현 계약) |
| HN-FR-03 | `AppendFile` 호출 하나는 한 번의 write 로 쓴다 (세션 줄 원자성, HN-FR-12). `Exec` 는 ctx 취소·Timeout 시 **자식 프로세스 전체**를 끝내고 반환한다. Timeout 이면 `TimedOut=true`, 에러 아님. 실행 자체를 못 하면 error (Env 구현 계약) |
| HN-FR-04 | `harness/envtest` 패키지가 메모리 FileSystem 과 스크립트된 Shell 로 된 가짜 Env(`envtest.New(cwd, home)`)를 제공한다. `SetSymlink(p)` 는 이미 있는 파일·디렉터리 p 를 링크로 표시해 `ListDir` 이 `Symlink=true` 로 돌려주게 한다 (읽기·순회는 대상 그대로). 계약 테스트 `envtest.ConformanceFS(t, fs, root)`(HN-FR-02·03 의 FileSystem 부분, 메모리 FS 도 통과)와 `envtest.ConformanceShell(t, sh, dir)`(실제 셸용: 출력 합치기, 종료 코드, Timeout, 취소 시 자식 종료)를 둔다 |

### 3.2 도구 (`harness/tools`)

| 도구 | 인자 | 동작 |
|---|---|---|
| `read` | `path`, `offset?`(1부터), `limit?` | 텍스트: 2000줄 또는 50KB 중 먼저 닿는 곳에서 자르고 "offset 으로 이어 읽어라" 안내. 내용의 시그니처가 png·jpeg·gif·webp 이면 image 블록 (pi `detectSupportedImageMimeType`) |
| `write` | `path`, `content` | `WriteFile`. 결과: 쓴 바이트 수와 경로 |
| `edit` | `path`, `edits:[{oldText, newText}]` | pi 알고리즘: BOM 제거·LF 정규화 → 정확 매칭, 없으면 fuzzy(NFKC, 줄 끝 공백, 스마트 따옴표·유니코드 대시·특수 공백 정규화) → 없음·중복·겹침·무변화는 에러 → 모든 편집은 원본 기준 적용 → 원래 줄바꿈(CRLF)·BOM 복원. 결과에 unified diff |
| `bash` | `command`, `timeout?`(초) | `Exec{Command, Dir: Cwd, Timeout}`. 합친 출력의 **마지막** 2000줄 / 50KB 반환. 넘으면 전체를 `CreateTemp` 파일에 쓰고 경로 안내. 종료 코드 표시, 0 이 아니거나 TimedOut 이면 error 결과 |
| `grep` | `pattern`, `path?`, `glob?`, `ignoreCase?`, `literal?`, `context?`, `limit?`(기본 100), `respectIgnore?` | 3.2a HN-FR-43~47 |
| `find` | `pattern`(glob), `path?`, `limit?`(기본 1000), `respectIgnore?` | 3.2a HN-FR-43~45, 48 |
| `ls` | `path?`, `limit?`(기본 500) | 3.2a HN-FR-49 |

| id | 요구사항 |
|---|---|
| HN-FR-05 | 경로 해석(`tools.Resolve(env, path)`, agent 승인 규칙도 같은 함수를 쓴다): 절대 경로는 그대로, `~`·`~/…` 는 `Home()` 기준(Home 이 ""면 error 결과), 그 외는 `Cwd()` 기준. `path.Clean` 으로 정리. 결과 문구에는 cwd 안이면 cwd 기준 상대 경로, 밖이면 절대 경로 |
| HN-FR-06 | 도구 실행 에러(파일 없음, 매칭 실패, 타임아웃, 0 이 아닌 종료 코드, Env 에러)는 `Status: error` 결과다. Go panic·error 로 루프를 멈추지 않는다 |
| HN-FR-07 | `read`·`bash`·`grep`·`find`·`ls` 는 자체 절단을 하므로 `Tool.Truncate` 를 항등 함수로 둔다 (sets 18 의 기본 절단이 이중으로 걸리지 않게). `write`·`edit` 는 nil |
| HN-FR-08 | `InputSchema` 는 sets ST-FR-38 범위(객체·required·최상위 타입)로 검사 가능한 JSON Schema 다 |
| HN-FR-09 | `tools.All(env)` 는 `read, bash, edit, write, grep, find, ls` 순서의 `[]sets.Tool`. 도구별 생성자 `tools.Read(env)` 등도 공개한다. 각 도구는 프롬프트용 한 줄 설명(`tools.Snippet(name)`)을 가진다 |
| HN-FR-10 | 같은 파일에 대한 `write`·`edit` 는 직렬화한다 (pi `file-mutation-queue`, 병렬 도구 대비). `tools.All` 이 만든 도구들이 큐 하나를 공유하고, 따로 만든 `tools.Write`·`tools.Edit` 는 각자의 큐를 쓴다 |

### 3.2a 탐색 도구 `grep`·`find`·`ls` (R23)

출처: pi `coding-agent/src/core/tools/grep.ts`·`find.ts`·`ls.ts`. pi 는 rg·fd 를 실행하지만 여기서는 Env 위에서 순회한다 (HN-C1).

| id | 요구사항 |
|---|---|
| HN-FR-43 | **순회** (grep·find 공통): 시작 = `path`(없거나 "" 면 `.`)를 HN-FR-05 로 푼 경로. `Stat` 이 `fs.ErrNotExist` 면 `Path not found: <path>`, 다른 에러면 `Cannot access <path>: <에러>` error 결과 (`<path>` 는 인자 그대로, 없으면 `.`). 디렉터리면 `ListDir` 로 깊이 우선, 각 디렉터리 안은 `ListDir` 순서(이름순)로 항목을 내고 하위 디렉터리는 그 자리에서 들어간다. 이름이 `.git` 인 디렉터리는 항상 건너뛴다. 숨김 항목(`.` 으로 시작)은 포함한다. `Symlink=true` 인 디렉터리 항목에는 들어가지 않는다. 시작 디렉터리의 `ListDir` 에러는 error 결과, 하위 디렉터리의 `ListDir` 에러는 그 디렉터리만 건너뛴다. ctx 가 취소되면 순회를 멈추고 `ctx.Err()` 문구의 error 결과. 결과 경로는 시작 디렉터리 기준 상대 경로(`/` 구분) |
| HN-FR-44 | **`respectIgnore`** (grep·find, 기본 false): true 면 `.gitignore` 를 따른다. 읽는 파일 = 시작 디렉터리의 조상 중 `.git` 항목(파일·디렉터리)을 가진 가장 가까운 디렉터리부터 시작 디렉터리까지의 각 `.gitignore`(그런 조상이 없으면 시작 디렉터리의 것만), 그리고 순회 중 들어가는 각 디렉터리의 `.gitignore`. 읽을 수 없는 `.gitignore` 는 없는 것으로 본다. 문법 (git): 빈 줄·`#` 시작 줄 무시, 끝 공백 제거, 앞의 `\` 는 다음 문자(`#`·`!`) 이스케이프, `!` = 부정(다시 포함), 끝 `/` = 디렉터리에만 맞음, 맨 앞이나 중간에 `/` 가 있으면 그 `.gitignore` 의 디렉터리 기준 상대 경로에 맞고 없으면 어느 깊이의 이름에도 맞음, `*`·`?`·`[...]` 는 `/` 를 넘지 않고 `**` 는 0개 이상의 경로 단계. 한 항목에 맞는 규칙 중 **마지막** 것이 이긴다(얕은 파일의 규칙이 먼저, 깊은 파일이 나중). 무시된 디렉터리에는 들어가지 않는다. 비목표: `.git/info/exclude`, 전역 excludesFile, `.ignore`·`.rgignore` |
| HN-FR-45 | **glob 매칭** (find 의 `pattern`, grep 의 `glob`): `/` 가 없으면 항목 **이름**에 맞춘다. `/` 가 있으면 시작 디렉터리 기준 상대 경로에 맞춘다 — `/` 로 시작하면 앞 `/` 를 떼고 시작 디렉터리에 고정, 아니면 `**/` 를 앞에 붙인 것처럼 어느 깊이에서든 (pi find 의 `--full-path` 처리). 문법은 HN-FR-44 의 `*`·`?`·`[...]`·`**`. 잘못된 패턴(`[` 가 닫히지 않음 등)은 `Invalid glob pattern: <pattern>` error 결과 |
| HN-FR-46 | **grep 매칭**: `literal` 이면 `regexp.QuoteMeta(pattern)`, `ignoreCase` 면 앞에 `(?i)`. 컴파일 실패는 `Invalid regex: <에러>` error 결과. `path` 가 파일이면 그 파일만 검사하고(`respectIgnore`·`glob` 무시) 결과 경로는 파일 이름. 디렉터리면 순회한 파일 중 `Symlink=false`·`glob`(있으면) 에 맞는 것을 `ReadFile` 한다. 내용 앞 8000 바이트에 NUL 이 있으면 이진 파일로 보고 건너뛴다 (파일 경로로 준 경우도). 읽기 에러인 파일은 건너뛴다. 줄 = `\n` 으로 나누고 각 줄의 `\r` 제거, 줄 번호는 1부터. 맞는 줄마다 match 1건, 파일 순서·줄 순서대로 `limit`(1 미만이면 1)건까지 담고, 그 뒤에 match 가 하나라도 더 있으면 멈추고 limit 안내(HN-FR-47)를 붙인다 |
| HN-FR-47 | **grep 결과**: match 가 없으면 ok 결과 `No matches found`. 있으면 match 마다 `context`(기본 0, 음수는 0) 가 0 이면 `<경로>:<줄>: <텍스트>`, 크면 `[줄-context, 줄+context]`(파일 범위 안) 의 각 줄을 맞은 줄은 `<경로>:<n>: <텍스트>`, 나머지는 `<경로>-<n>- <텍스트>` 로 (pi 처럼 겹치는 구간을 합치지 않음). 각 줄 텍스트는 500 rune 을 넘으면 앞 500 rune + `... [truncated]`. 줄들을 `\n` 으로 잇고 50KB 를 넘으면 앞에서부터 온전한 줄만(줄 수 상한 없음). 안내를 `\n\n[` + `. ` 로 이은 것 + `]` 로 붙인다: limit 에 닿음 → `<limit> matches limit reached. Use limit=<limit*2> for more, or refine pattern`, 50KB → `50.0KB limit reached`, 줄 절단 → `Some lines truncated to 500 chars. Use read tool to see full lines` |
| HN-FR-48 | **find**: `pattern` 필수(비면 `pattern is required`). 시작 경로가 디렉터리가 아니면 `Not a directory: <path>`. 순회한 항목(파일·디렉터리·링크 모두) 중 HN-FR-45 로 맞는 것을 순회 순서대로, 디렉터리는 끝에 `/` 를 붙여 낸다. `limit`(1 미만이면 1)개까지 담고, 더 있으면 멈추고 limit 안내를 붙인다. 없으면 `No files found matching pattern`. 50KB 절단·안내는 HN-FR-47 방식: `<limit> results limit reached. Use limit=<limit*2> for more, or refine pattern`, `50.0KB limit reached` |
| HN-FR-49 | **ls**: 시작 경로 해석·없음 에러는 HN-FR-43, 디렉터리가 아니면 `Not a directory: <path>`, `ListDir` 에러는 `Cannot read directory: <에러>`. 항목을 소문자 비교로 정렬(같으면 원래 이름순), 디렉터리는 끝에 `/`. `.git` 포함 모든 항목을 낸다(들어가지 않으므로). `limit`(기본 500, 1 미만이면 1)개까지 담고, 더 있으면 limit 안내를 붙인다. 비면 `(empty directory)`. 50KB 절단·안내: `<limit> entries limit reached. Use limit=<limit*2> for more`, `50.0KB limit reached` |
| HN-FR-50 | 프롬프트 한 줄 설명(HN-FR-09): grep `Search file contents for patterns`, find `Find files by glob pattern`, ls `List directory contents`. 도구 Description 은 pi 문안에서 `.gitignore` 설명을 R23 ② 로 바꾼 것 (`Searches all files including hidden and ignored ones; set respectIgnore to skip files ignored by .gitignore.`) |

### 3.3 세션 (`harness/session`)

| id | 요구사항 |
|---|---|
| HN-FR-11 | `session.New(fs FileSystem, dir string) (*Store, error)` 는 `sets.Store` 구현이다. dir 는 절대 경로. 세션 파일 `<dir>/<sessionID>.jsonl`, append-only |
| HN-FR-12 | 줄 종류: `{"type":"message","message":Message}`, `{"type":"edit","edit":ContextEdit}`, `{"type":"state","ns":string,"delta":StateDelta}`. `Append*`·`SaveState` 호출 하나의 줄들은 `AppendFile` 한 번으로 쓴다. `Load` 는 `ReadFile` 후 순서대로 재생해 `sets.Session` 을 만든다 (state 는 delta 를 순서대로 적용). 파일이 없으면 빈 세션 |
| HN-FR-13 | 개행으로 끝나지 않은 마지막 조각(쓰다 중단)이 깨져 있으면 `Load` 는 그것을 버리고 파일을 그 앞까지로 `WriteFile` 해 복구한다 (이후 append 가 깨진 줄 뒤에 붙지 않게). 개행으로 끝나지 않았지만 온전한 줄이면 개행을 붙여 복구한다. 개행으로 끝난 깨진 줄이나 모르는 type 은 에러 |
| HN-FR-14 | sessionID 는 `[A-Za-z0-9._-]{1,128}` 이고 `.`·`..` 가 아니어야 한다. 어기면 에러 (경로 탈출 방지). `session.NewID(now, rand)` 는 `20060102-150405-<8 hex>` 형식 |
| HN-FR-15 | `session.DefaultDir(home string) string` = `<home>/.dynamic_react/sessions`. 순수 함수이고 환경변수를 보지 않는다 (R21) |
| HN-FR-16 | 동시 사용에 안전하다 (같은 Store 값). 프로세스 간 동시 쓰기는 보장하지 않는다 |
| HN-FR-37 | `(*Store).List(ctx) ([]string, error)` (R22, `harness.Store` 구현): dir 의 항목 중 디렉터리가 아니고 이름이 `<id>.jsonl` 이며 id 가 `CheckID` 를 통과하는 것의 id 를 `ListDir` 순서(파일 이름순, 3.1)대로 돌려준다. dir 이 없으면 빈 목록(nil, 에러 없음), 그 밖의 `ListDir` 에러는 에러 |

### 3.4 시스템 프롬프트 (`harness/prompt`)

| id | 요구사항 |
|---|---|
| HN-FR-17 | `prompt.Build(Input) []contract.Block` 은 text 블록 하나. 순서: preamble(기본 문안) → `<tools>` 도구 한 줄 설명 → `<rules>` → `<project_context>`(컨텍스트 파일, 있으면) → `<skills>`(있으면, `read` 도구가 있을 때) → `<cwd>`. 절은 빈 줄로 잇는다. 편집 수단을 두지 않는다 — 다른 프롬프트가 필요한 개발자는 `Options.System` 으로 바꾼다 (R16, HN-FR-38) |
| HN-FR-18 | 기본 문안은 코드 상수 `prompt.DefaultPreamble` 이고 내용은 부록 A 다 (사용자 확정). 이 환경의 동작(cwd·경로 해석, 출력 절단과 이어 읽기, edit 규칙, aborted 결과, 압축 요약, project_context)을 모델에 알린다 |
| HN-FR-19 | `prompt.ContextFiles(ctx, fs, cwd)`: cwd 부터 상위로 git 루트(`.git` 이 있는 디렉터리, 없으면 `/`)까지 `AGENTS.md`, `CLAUDE.md` 를 모아 **루트 쪽부터** `[]File{Path, Content}` 로. 한 디렉터리에 둘 다 있으면 `AGENTS.md` 먼저 |
| HN-FR-20 | `prompt.Skills(ctx, fs, cwd, home)`: `<home>/.agents/skills`, `<cwd>/.agents/skills`, `<home>/.claude/skills`, `<cwd>/.claude/skills` 순서로 `*/SKILL.md` frontmatter(`name`, `description`)를 모은다. 본문은 넣지 않는다 (모델이 `read` 로 읽음). 같은 이름은 먼저 찾은 것이 이긴다. frontmatter 가 없거나 깨진 파일, 없는 디렉터리는 건너뛴다. home 이 ""면 home 경로는 건너뛴다 |
| HN-FR-21 | 날짜·시각처럼 run 마다 바뀌는 값을 넣지 않는다 (프롬프트 캐시 보호) |

### 3.5 요약기 (`harness/summary`)

| id | 요구사항 |
|---|---|
| HN-FR-22 | `summary.New(model sets.ModelCaller, now func() time.Time)` 는 `sets.Summarizer` 다 (now 는 요청 메시지 Timestamp, nil 이면 time.Now). 요청: System = 요약 시스템 프롬프트, Messages = user 메시지 하나(`<conversation>` 안에 직렬화한 구간 + `Previous` 가 있으면 `<previous-summary>` + 요약 또는 갱신 프롬프트), Model = `SummaryRequest.Model`, MaxOutputTokens = `SummaryRequest.MaxTokens`, Tools 없음 |
| HN-FR-23 | 직렬화: `[User]:`·`[Assistant]:`·`[Assistant thinking]:`·`[Assistant tool calls]: name(args)`·`[Tool result]:` 줄. 이미지는 `[image]`, opaque·ProviderData 는 생략. 도구 결과는 2000자에서 자르고 표시 (pi `serializeConversation`, `TOOL_RESULT_MAX_CHARS`) |
| HN-FR-24 | done 메시지의 text 블록을 이어 붙여 돌려준다. error 이벤트, iterator error, done 없이 끝남, StopReason `length`·`error`·`aborted`, 빈 텍스트는 에러 (pi `getSummarizationFailure`) |
| HN-FR-25 | ctx 취소를 따른다. 재시도하지 않는다 (sets ST-FR-27) |

### 3.6 조립 (`harness`)

```go
type Options struct {
    Env        Env                   // 필수
    Model      sets.ModelCaller      // 필수
    SessionDir string                // 필수, 절대 경로
    ToolSet    func(def []sets.Tool) []sets.Tool             // HN-FR-38, nil → def
    Sets       func(d sets.Deps) []inject.FunctionSet        // HN-FR-38, nil → DefaultSets(d)
    System     func(def []contract.Block) []contract.Block   // HN-FR-38, nil → def
    Summarizer func(def sets.Summarizer) sets.Summarizer     // HN-FR-38, nil → def
    Store      func(def Store) Store                         // HN-FR-38, nil → def
    Observer   core.Observer
    Core       core.Options          // Observer 는 위 필드가 이긴다
    Now        func() time.Time      // nil → time.Now
    NewID      func() string         // nil → 무작위
}
type RunRequest struct {
    SessionID string                 // 비면 session.NewID
    Input     []contract.Message
    Observer  core.Observer          // 이 run 에만 쓰는 Observer. 있으면 Options.Observer 대신 쓴다 (HN-FR-27)
    Config    json.RawMessage        // sets 설정 JSON (agent 가 설정 파일에서 읽은 그대로). sets.Config 값을 쓰지 않는 이유: zero 값 enabled=false 가 압축을 끔
}
```

| id | 요구사항 |
|---|---|
| HN-FR-26 | `harness.New(ctx, Options) (*Agent, error)`: 레지스트리, 세션 Store, 시스템 프롬프트(`ContextFiles`·`Skills` 포함), 요약기를 HN-FR-38 의 순서로 한 번 만들고 `inject.BindWith(sets.StateStoreOf(store), Sets(deps)...)` 로 Dispatcher 를 만든다. Env·Model·SessionDir 누락, Bind 실패, 프롬프트 수집의 Env 에러(없는 파일 제외), HN-FR-38 의 nil 결과 에러는 에러 |
| HN-FR-27 | `Agent.Start(ctx, RunRequest) (*core.Run, string, error)`: Config 를 `sets.ParseConfig` 로 검사하고(실패하면 run 없이 에러), sessionID 를 검사·생성해 run 을 시작한다. 두 번째 반환값은 쓰인 sessionID. `RunRequest.Observer` 가 있으면 그 run 만 그 Observer 로 도는 Kernel(같은 Dispatcher)에서 시작한다 — core 의 RunID 는 내부에서 만들어지고 Observer 이벤트에 세션이 없으므로, run 별 Observer 가 panic 을 그 run 에 귀속시키는 유일한 방법이다 |
| HN-FR-28 | 여러 run 을 동시에 시작할 수 있다 (세션이 다르면 독립). 같은 세션의 동시 run 은 보장하지 않는다 |
| HN-FR-29 | `Agent.System() []contract.Block`, `Agent.Tools() sets.ToolRegistry`, `Agent.Store() Store` 로 조립 결과를 조회할 수 있다 (L3 표시용, Store 는 agent 의 목록·이력용, R22) |

### 3.6a surface 훅 (R18)

agent(`L2_harness/agent`)가 surface 공통 계층을 만들 때 쓰는 훅이다. `Bind` 는 step 마다 nil 이 아닌 마지막 함수 하나를 고르므로(IN-FR-19) 덧붙이려면 그 함수를 감싸야 한다. 감싸기는 **그 함수를 가진 세트 안에서 제자리로** 한다 — 별도 세트로 겹치면 inner 의 상태 네임스페이스(세트 이름 기준)가 바뀌어 base 의 상태가 깨진다. 재시도 대기는 `sets.Deps.Sleep`, 압축은 `sets.Deps.Summarizer` 를 감싼다. 루프(L0)·sets(L1)는 바꾸지 않는다.

```go
// harness.Options 추가
OnEvent func(Event)                                            // nil 이면 이벤트를 만들지 않는다
Gate    func(ctx context.Context, in GateInput) *contract.ToolResult // nil 이면 모두 통과

type GateInput struct {
    RunID, SessionID string
    TurnIndex        int
    InboxConsumed    int // base 공개 상태 inbox.consumed (ST-FR-43): 지금까지 소비한 입력함 메시지 수
    Call             contract.ToolCall
}

type EventKind string // 아래 표의 11개

type Event struct {
    Kind       EventKind
    RunID      string
    SessionID  string
    Message    *contract.Message     // message_*: 부분 또는 확정 메시지, turn_end: 이 턴의 assistant 메시지
    Delta      *contract.StreamEvent // message_update: 이번 조각
    Call       *contract.ToolCall    // tool_*, approval
    Result     *contract.ToolResult  // tool_end
    Results    []contract.ToolResult // turn_end
    Output     string                // tool_update: 새로 나온 출력 조각
    Approval   *Approval             // approval (agent 가 만든다)
    Error      *contract.CallError   // retry
    Attempt    int                   // retry: 몇 번째 재시도인지 (1부터)
    Delay      time.Duration         // retry
    Phase      string                // compaction: "start" | "end"
    Err        error                 // compaction end 실패
    Settle     *core.Settle          // run_end
}

type Approval struct {
    Rule     string // 맞은 규칙 (없으면 "")
    Target   string // bash command 또는 절대 경로 (이름 규칙만 되는 도구는 "")
    Decision string // "allow" | "allow_session" | "deny" (Approver 의 답) | "rule_deny" (deny 규칙)
    Reason   string
}
```

| id | 요구사항 |
|---|---|
| HN-FR-30 | `Gate`: 실제로 쓰이는 `tool.before`(nil 이 아닌 마지막 함수, 기본은 base)가 에러·ShortCircuit 없이 돌려준 호출마다 부른다 — 기본 세트면 base 검사(없는 도구·스키마 불일치, ST-FR-38)를 통과한 호출이다 (사용자 세트와의 관계는 HN-FR-41). nil 이 아닌 결과를 돌려주면 그것이 ShortCircuit 결과가 된다(CallID 는 harness 가 채움). ctx 는 dispatch 의 ctx 다 (취소를 따름). `GateInput.InboxConsumed` 는 이름이 `base` 인 세트 네임스페이스의 공개 상태 `inbox.consumed` 이고, 없으면 0 이다 |
| HN-FR-31 | 이벤트 종류와 생성 지점. `OnEvent` 가 nil 이면 감싸지 않는다 |

| Kind | 생성 지점 | 채우는 필드 |
|---|---|---|
| `message_start` | `stream.event` 에서 `start` 이벤트 | Message = 그 시점의 Draft |
| `message_update` | `stream.event` 에서 `text_delta`·`thinking_delta`·`toolcall_start`·`toolcall_delta`·`toolcall_end` | Delta = 그 이벤트, Message = 누적 부분 메시지(`Stream().Draft()`) |
| `message_end` | `stream.end` | Message = 확정 메시지 (usage 포함) |
| `tool_start` | `tool.before` 진입 (Gate 전) | Call |
| `tool_update` | bash 출력 조각 (HN-FR-33) | Call, Output |
| `tool_end` | `tool.after` 의 결과(절단 뒤) 또는 ShortCircuit 결과 | Call, Result |
| `approval` | agent 가 만든다 (harness 는 만들지 않음) | Call, Approval |
| `retry` | resilient `call.error` 가 재시도를 결정했을 때 | Error, Attempt, Delay |
| `compaction` | 요약기 호출 전·후 | Phase, Err |
| `input` | `turn.start` 가 메시지를 돌려줬을 때 (에러 없이), 메시지마다 순서대로 (R25) | Message = 그 메시지 (surface 가 보낸 입력도 포함 — surface 는 자기 메시지 ID 로 거른다) |
| `turn_end` | `turn.finish` 진입 | Message = 이 턴의 응답, Results |
| `run_end` | `run.settled` | Settle |

| id | 요구사항 |
|---|---|
| HN-FR-32 | 순서 보장: 한 호출(tool call)의 `tool_start` 는 정확히 한 번, `tool_end` 도 정확히 한 번이고 그 사이에만 `tool_update`·`approval` 이 온다. 한 메시지는 `message_start` → `message_update`* → `message_end`. `run_end` 는 그 run 의 마지막 이벤트다. 스트림 도중 에러로 `message_end` 없이 끝날 수 있다 |
| HN-FR-33 | `tools.WithProgress(ctx, fn func(callID, chunk string)) context.Context`: bash 는 ctx 에 fn 이 있으면 출력 조각을 받을 때마다 부른다. 조각은 UTF-8 문자 경계에서 자른다(끊긴 문자는 다음 조각으로). harness 는 `OnEvent` 가 있으면 `Start` 의 ctx 에 fn 을 실어 `tool_update` 로 바꾼다 |
| HN-FR-35 | `Agent.Emit(ev Event)`: `OnEvent` 를 HN-FR-34 와 같은 직렬화로 부른다 (agent 의 `approval` 이벤트용). `OnEvent` 가 nil 이면 아무것도 하지 않는다 |
| HN-FR-34 | `OnEvent` 호출은 **Agent 전체에서 직렬화**된다 (한 번에 하나, 병렬 도구 실행 중에도). 동기이며, 콜백이 오래 걸리면 run 이 그만큼 늦어진다 (R18: 동기만). dispatch 안에서 부르므로 콜백의 panic 은 그 dispatch 의 에러가 되어 run 이 failed 로 끝난다 (IN-FR-11) |

### 3.6b 수동 압축 (impl-04)

```go
func (a *Agent) Compact(ctx context.Context, sessionID string, config json.RawMessage) (bool, error)
```

| id | 요구사항 |
|---|---|
| HN-FR-36 | `Compact`: run 밖에서 세션 하나를 즉시 압축한다. 순서: config 를 `sets.ParseConfig`(실패 = 에러) → `session.CheckID`(실패 = 에러) → 요약기가 nil 이면(HN-FR-38 ④) 에러 `harness: compaction disabled: no summarizer` → Store(HN-FR-38 ②의 결과) `Load` → 투영 `sets.ApplyEdits(Messages, Edits)` → 모델 정보 `Model.Lookup(cfg.Model)`(없으면 Ref 만 있는 ModelInfo, ContextWindow 0) → `sets.Compact(ctx, 요약기, Ref, 투영, cfg.Compaction.KeepRecentTokens, sets.ReserveTokens(cfg, ContextWindow), MaxOutputTokens, Now(), NewID(), NewID())`. ok 면 Store `AppendEdits([edit])` 후 `(true, nil)`, 압축할 구간이 없으면 `(false, nil)`, 요약 실패·저장 실패는 에러. `cfg.Compaction.Enabled` 와 무관하다 (사용자가 명시적으로 요청). 요약기를 부를 때 `OnEvent` 로 `compaction` start/end 를 낸다 (RunID "", SessionID = 인자, HN-FR-34 직렬화). 그 콜백의 panic 은 recover 해 `core.ErrPanic` 을 감싼 에러로 돌려준다. 같은 세션의 run 과 겹치지 않게 하는 것은 호출자의 몫이다 (HN-FR-28, agent 는 세션 잠금 AG-FR-31). 다음 run 의 base 는 시작 때 저장된 편집 수를 Store 에서 다시 세므로(`BaseRunStart` 의 `edits.saved`) 편집이 중복 저장되지 않는다 |

### 3.6c 주입 (R22)

core(L0) 외의 모든 것을 생성 시점에 바꾼다. 부품은 **기본값을 받아 바꾼 값을 돌려주는 함수**이고 nil 이면 기본값 그대로다. 기본값을 받으므로 교체와 감싸기를 같은 방법으로 한다. 각 함수는 `New` 안에서 한 번만 부른다. 실행 중 교체는 하지 않는다 (비목표).

```go
// Store 는 세션 저장소다. 기본은 session.Store (3.3).
type Store interface {
    sets.Store
    List(ctx context.Context) ([]string, error) // 저장된 세션 ID, 중복 없음, 순서 무관
}

func DefaultSets(d sets.Deps) []inject.FunctionSet // [sets.Base(d), sets.Resilient(d)]
func (a *Agent) Store() Store
```

| id | 요구사항 |
|---|---|
| HN-FR-38 | `New` 는 아래 순서로 만든다. ① 도구: def = `tools.All(Env)`. `ToolSet(def)` 의 결과를 순서대로 `sets.NewRegistry` 에 넣는다 (같은 이름은 뒤의 것이 앞의 자리를 덮음, ST-FR-41). 빈 결과는 허용한다 (도구 없는 에이전트) ② 저장소: def = `session.New(Env, SessionDir)` (SessionDir 은 여전히 필수). `Store(def)` 가 nil 이면 에러 `harness: Store returned nil` ③ 시스템 프롬프트: def = 3.4 의 조립(①의 registry 기준). `System(def)` 의 결과를 그대로 쓴다 (nil·빈 결과 = 시스템 프롬프트 없음, HN-C4 는 그대로) ④ 요약기: def = `summary.New(Model, Now)`. `Summarizer(def)` 가 nil 이면 압축을 끈다 (ST-FR-36, 수동 압축은 HN-FR-36 의 에러). `OnEvent` 가 있으면 nil 이 아닐 때만 compaction 이벤트로 감싼다 (3.6a) ⑤ 세트: `sets.Deps{Model, Tools: ①, Store: ②, System: ③, Summarizer: ④, Sleep, Now, NewID}` 로 `Sets(deps)` 를 부르고(nil → `DefaultSets(deps)`), 그 결과에 3.6a 래핑(HN-FR-41)을 한 뒤 `inject.BindWith(StateStore: sets.StateStoreOf(②))`. 빈 step·같은 이름 세트는 Bind 에러 그대로 (IN-FR-19, 22) |
| HN-FR-39 | `DefaultSets(d)` = `[sets.Base(d), sets.Resilient(d)]`. 사용자는 이것에 덧붙이거나(겹치기), 원소를 고치거나(감싸기·교체), 쓰지 않는다(전부 교체) |
| HN-FR-40 | `Store` 의 `List` 는 그 저장소에 저장된 세션 ID 를 돌려준다. `Load` 는 저장되지 않은 ID 에 빈 세션을 돌려준다 (`sets.Store` 와 같음). run·수동 압축·state 저장(`StateStoreOf`)·agent 의 목록과 이력(AG-FR-29·30)이 모두 이 하나의 저장소를 쓴다 |
| HN-FR-41 | 사용자 세트와 surface 래핑: 3.6a 래핑은 `Sets` 결과에서 step 마다 실제로 쓰이는 함수를 그 세트 안에서 감싼다. 그래서 **승인(Gate)과 이벤트(OnEvent)는 사용자 세트가 무엇이든 유지된다.** base 의 기본 검사(ST-FR-38)는 base `tool.before` 의 일부다 — 사용자 세트가 `tool.before` 를 덮으면서 base 의 함수(`sets.BaseToolBefore(d)`)를 부르지 않으면 검사가 빠지고, Gate 는 검사 없이 그 호출에 불린다. 이것은 사용자의 책임이다. base 공개 상태 `inbox.consumed`(HN-FR-30)를 쓰는 기능(agent 의 Steer, AG-FR-24)은 이름이 `base` 인 세트가 그 상태를 갱신할 때만 맞게 동작한다 |
| HN-FR-42 | `Agent.Store()` 는 HN-FR-38 ②의 결과다 |

### 3.7 비기능

| id | 요구사항 |
|---|---|
| HN-NFR-01 | 모든 테스트는 `envtest` 가짜 Env 로 한다. 실제 파일시스템·프로세스를 쓰지 않는다 (OS 구현의 테스트는 `L2_harness/osenv` 의 몫, `envtest.Conformance` 로) |
| HN-NFR-02 | 종단 테스트: 가짜 ModelCaller(스크립트된 스트림) + 가짜 Env + 실제 도구·세션·요약기 + `harness.New` 로 "read→edit→bash 라운드 후 종료", "세션 재개(새 Agent, 같은 Env·dir) 시 History·편집 복원", "overflow → 실제 요약기로 압축 → 재호출" |
| HN-NFR-03 | `go test -race` 통과, `GOWORK=off` 단독 테스트 통과 |
| HN-NFR-04 | import 는 contract·core·inject·sets·표준 라이브러리와 `golang.org/x/text`(edit 의 NFKC, v0.28.0 = Go 1.23 호환 최신) 뿐이고, HN-C1 의 금지 패키지가 없다 (직접 import 검사) |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| HN-FR-01~04 | envtest 자체 테스트, `Conformance` 가 envtest 에서 통과 |
| HN-FR-43~50 | envtest 로 표 기반: 순회 순서·`.git` 제외·숨김 포함·링크 디렉터리 안 들어감·하위 ListDir 에러 건너뜀·취소, gitignore 문법 표(부정·디렉터리 전용·앵커·`**`·이스케이프·마지막 규칙 우선·조상 `.gitignore` 와 `.git` 경계·무시된 디렉터리 안의 부정은 효과 없음), glob 표(이름·경로·앞 `/`·잘못된 패턴), grep(literal·ignoreCase·context·파일 경로·glob·이진·limit·500 rune·50KB·잘못된 regex·없는 경로), find(디렉터리 `/`·limit·없음·파일 경로), ls(정렬·`/`·빈 디렉터리·limit·파일·없음) |
| HN-FR-05~10 | 도구별 단위: 정상 / 경계(offset 끝, 2000줄·50KB 경계, 빈 파일, 이미지, `~`, cwd 밖) / 실패(없는 파일, 매칭 0·2회, 겹침, 무변화, 타임아웃, 0 아닌 종료 코드, Env 에러, spill). edit fuzzy 정규화 표, CRLF·BOM 복원 |
| HN-FR-11~16 | 왕복(메시지·편집·상태 → Load), 깨진 마지막 줄 무시, 깨진 중간 줄·모르는 type 에러, 잘못된 sessionID, 호출당 AppendFile 1회, 동시 Append(-race), DefaultDir |
| HN-FR-17~21 | 골든: 기본 / 컨텍스트 파일 순서(루트 쪽 먼저, git 루트에서 멈춤) / 스킬 우선순위·깨진 frontmatter·home 없음 |
| HN-FR-22~25 | 가짜 ModelCaller: 요청 모양(System·Previous·MaxTokens·Tools 없음), 직렬화 골든, length·error·빈 텍스트·done 없음 → 에러, 취소 |
| HN-FR-30~34 | 가짜 ModelCaller·가짜 Env 로: Gate ShortCircuit 결과가 모델에 전달됨, Gate 는 base 검사 실패 호출에는 불리지 않음, 이벤트 순서(텍스트 턴·도구 턴·병렬 도구 2개·스트림 에러), `input` 이 첫 턴의 입력·이후 턴의 입력함 메시지(세트의 `inject_input` 포함)마다 그 턴의 `message_start` 전에 옴·입력 없는 턴에는 없음, message_update 의 누적본 = 앞 조각들의 합, tool_end 는 절단 뒤 결과, retry 의 Attempt·Delay, compaction start/end, run_end 가 마지막, bash progress 조각 합 = 전체 출력(UTF-8 경계), OnEvent 동시 호출 없음(-race), 콜백 panic → failed |
| HN-FR-36 | 가짜 ModelCaller(요약 응답)·가짜 Env: 긴 세션 → true·edit 저장·이어서 Start 한 run 의 요청에 요약 메시지·세션 파일의 edit 줄이 하나, 짧은 세션·빈 세션 → false, 요약 실패 → 에러·저장 없음, 잘못된 config·ID → 에러, Enabled false 여도 압축, compaction 이벤트 start/end(RunID ""), OnEvent panic → ErrPanic 에러 |
| HN-FR-26~29 | New 에러 경로, Start 설정 검사·sessionID 검사, 동시 run 2개 |
| HN-FR-37 | 여러 세션 파일 + 디렉터리·다른 확장자·잘못된 ID 이름 → 맞는 id 만 이름순, 없는 dir → 빈 목록, ListDir 에러 → 에러 |
| HN-FR-38~42 | 가짜 ModelCaller·가짜 Env 로: nil 함수 = 지금 동작(기존 테스트 그대로), `ToolSet` 추가 도구가 모델 요청의 tools 와 시스템 프롬프트에 들어감·기본 도구 제거·같은 이름 교체·빈 결과, `Store` 가 감싼 저장소에 run 의 메시지·state 가 기록됨·`Agent.Store()` 가 그것·nil 결과 → New 에러, `System` 결과가 요청의 System·nil 결과 → System 없음, `Summarizer` 감싸기가 압축에 쓰임·nil 결과 → 압축 없음·`Compact` 에러, `Sets` 로 덧붙인 세트의 `context.inject` 가 요청에 들어감·base 를 빼면 Bind 에러(빈 step)·같은 이름 → Bind 에러, `Sets` 결과의 `tool.before` 를 덮어도 Gate 결과가 모델에 전달되고 tool_start/end 이벤트가 옴, base 를 부르지 않는 `tool.before` 면 없는 도구 호출에도 Gate 가 불림(검사 빠짐), base 를 부르면 없는 도구 호출에 Gate 가 불리지 않음 |
| HN-NFR-02~04 | 종단 시나리오, `go list -deps` |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| R13 | 1.2 범위, 3.2~3.5 |
| R14 | HN-C1, 3.1, 3.6, HN-FR-15, HN-NFR-01, 04 |
| R17 | 1.2, 2.1, HN-FR-15, HN-NFR-01 |
| R22 | 1.2, 2.1, HN-FR-17, 26, 29, 30, 36, 3.6c (HN-FR-37~42) |
| R23 | 1.2, 3.1 FileInfo·HN-FR-04, 3.2, 3.2a (HN-FR-43~50), HN-FR-07, 09 |
| sets ST-FR-19, 35 | HN-C4, HN-FR-21 |
| sets ST-FR-24, 27, 36 | 3.5 |
| 자격증명 보호 | HN-C2 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | ~~기본 시스템 프롬프트 문안~~ → 부록 A 로 확정 (사용자) |
| O2 | ~~pi 에서 가져온 문안(요약 프롬프트, edit 정규화 규칙)의 라이선스 고지~~ → 루트 `THIRD_PARTY_NOTICES.md` (pi-mono·oh-my-pi MIT, x/text·x/term·x/sys BSD-3-Clause 전문, adapters 스냅샷 포함) |
| O3 | ~~`L3_ui/osenv`·`L3_ui/cmd/dr-print` 스펙~~ → `L2_harness/osenv/SPEC.md`, `L2_harness/agent/SPEC.md`, `L3_ui/cmd/dr-print/SPEC.md` (R17) |
| O4 | ~~NFKC 와 표준 라이브러리 제약 충돌~~ → `golang.org/x/text` 의존 허용 (사용자) |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 1.2 | 이벤트 11종. 세트가 `inject_input` 으로 넣은 메시지를 surface 가 알 방법 없음 | `input` 이벤트 (HN-FR-31) | R25 |
| 1.1 | 도구 4종, `tools.All` = read, bash, edit, write. 모델에 보내는 tools 4개, 시스템 프롬프트 rules 에 `Use bash for file operations like ls, rg, find` | 도구 7종 (3.2a), `tools.All` 끝에 grep, find, ls. 기본 프롬프트에서 그 rule 이 빠짐 (`prompt` 의 기존 pi 조건: bash 가 있고 grep·find·ls 가 모두 없을 때만 넣음) | R23 |
| 1.1 | `env.FileInfo{Name, IsDir, Size}` | `Symlink` 추가 (envtest `SetSymlink`) | R23 ③: 순회가 링크 디렉터리를 구별해야 함 |
| 1.0 | `Options.Tools []sets.Tool`(추가·교체), `Options.Extra`(base·resilient 위에 겹침). 저장소·시스템 프롬프트·요약기는 고정, base·resilient 는 뺄 수 없음 | `ToolSet`·`Sets`·`System`·`Summarizer`·`Store` 함수(기본값을 받아 바꾼 값을 돌려줌, nil = 기본), `DefaultSets`, `harness.Store`(`sets.Store` + `List`), `session.Store.List`, `Agent.Store()` (3.6c). `Tools`·`Extra` 삭제 (쓰는 곳 없음, 새 필드가 포함) | R22: core 외 전부 튜닝 |
| 1.0 | Gate 는 "base 검사를 통과한 호출"에 불림 | 실제로 쓰이는 `tool.before` 가 ShortCircuit 없이 돌려준 호출에 불림 (기본 세트면 같음) | R22 ④: 사용자 세트가 `tool.before` 를 덮을 때의 의미를 정함 |
| 1.0 | `Compact` 는 요약기가 항상 있음 | 요약기가 nil 이면 에러 | R22: `Summarizer` 가 nil 을 돌려주면 압축을 끈다 |
| 0.9 | O2 라이선스 고지 미결 | `THIRD_PARTY_NOTICES.md` 로 종결 | 사용자: 마지막 정리 때 처리 → impl-05 "전부 진행" |
| 0.9 | `session.DefaultDir(getenv, home)` 가 `DR_SESSION_DIR`(`session.DirEnv`)를 먼저 봄 | `session.DefaultDir(home)`, `DirEnv` 삭제 | R21 |
| 0.1 | 옛 sets v0.2 의 3.3·3.4·3.5·3.8 | 라이브러리 모듈 하나 + 검증용 CLI | 사용자 (R14 초안) |
| 0.1 | 세션 줄 `usage` | 제거 | usage 는 assistant 메시지에 있다 (contract CT-FR-01a) |
| 0.2 | 도구·세션·프롬프트 수집이 파일·프로세스를 직접 접근, 기본 ModelCaller = adapters | 실제 I/O 는 `Env`·`ModelCaller` 로만 받음 (HN-C1), adapters 의존 제거 | 사용자: "실제 입출력도 다 L3, 여기서는 에이전트 함수 하나" — pi 새 harness(`ExecutionEnv`) 구조 채택 (R14, 안 C) |
| 0.4 | 기본 문안 미정 (pi preamble 초안) | 부록 A: 환경 동작 안내를 더한 문안 | 사용자: "현재 우리 구조에 맞게 개선하여 확정, 영어로 쓰고 번역 별도" |
| 0.4 | 표준 라이브러리만, NFKC 없음 | `golang.org/x/text` v0.28.0 허용, NFKC 포함 (pi 와 같음) | 사용자 결정 (O4). 최신 v0.42.0 은 Go 1.26 을 요구해 Go 1.23 호환 최신인 v0.28.0 으로 고정. go.work 는 `go 1.23.0` |
| 0.5 | `Options.Preamble`·`Options.Append`(`prompt.Input` 같은 필드)로 preamble·tools·rules 대체, `<addendum>` 추가 | 편집 필드 없음. 항상 기본 조립. 다른 프롬프트는 `sets.Deps.System` 직접 구성 | 사용자: "코어를 쓰는 개발자는 알아서 수정, 사용자는 시스템 프롬프트를 수정할 필요 없음" → Preamble/Append 제거 (R16) |
| 0.6 | OS Env·ModelCaller 생성·설정/env 해석·dr-print 는 L3 | OS Env 는 `L2_harness/osenv`, ModelCaller·설정·env 는 `L2_harness/agent`, L3 는 터미널 입출력만. harness 모듈 자체(HN-C1)는 그대로 | R17: L2 부터가 사용자 환경 위의 실제 에이전트 |
| 0.8 | 수동 압축 없음 (압축은 루프 안 자동만) | `Agent.Compact` (3.6b) | impl-04 터미널 `/compact` 즉시 압축 |
| 0.7 | surface 훅 없음 | `Options.OnEvent`·`Options.Gate`, `Event` 11종, `tools.WithProgress` (3.6a) | R18: surface 공통 계층을 L2 에, base 감싸기는 Deps 가 있는 harness 에서 |
| 0.3 | Env 는 `harness` 패키지 | `harness/env` 패키지 + 별칭 | import 순환 (harness → tools → Env) |
| 0.3 | read 이미지 판별 = 확장자 | 내용 시그니처 | pi 와 같음 |
| 0.3 | Preamble 은 preamble 만 대체 | preamble·tools·rules 대체 | pi `customPrompt` 와 같음 |
| 0.3 | RunRequest.Config = sets.Config | json.RawMessage | zero 값이 압축을 끔 |
| 0.3 | 깨진 마지막 줄은 읽을 때만 버림 | 파일을 복구 | 복구하지 않으면 다음 append 가 깨진 줄 뒤에 붙어 이후 Load 가 항상 실패 |
| 0.3 | summary.New(model) | summary.New(model, now) | HN-C1 시계 주입 |
| 0.2 | dr-print = `L2_harness/cmd/dr-print` | `L3_ui/cmd/dr-print`, OS Env 는 `L3_ui/osenv` | 터미널·OS I/O 를 하므로 L3. L3 → L2 의존 방향 유지 (D16 정정) |

---

## 부록 A. 기본 문안 (HN-FR-18)

`prompt.DefaultPreamble` (모델에 보내는 원문, 영어):

```text
You are an expert coding assistant operating inside dynamic_react, a coding agent harness. You help users by reading files, executing commands, editing code, and writing new files with the tools listed below.

How this environment works:
- Tools run in the working directory shown in <cwd>. Relative paths resolve against it, and ~ resolves to the home directory.
- Large tool output is truncated. read tells you the offset to continue from; bash keeps the end of the output and reports the path of a file with the full output.
- edit replaces exact text: read the file first, copy oldText exactly, and make each oldText unique. All edits in one call apply to the original file.
- A tool result that says "aborted" was interrupted before it finished. Check the current state before running it again.
- In long sessions, earlier messages may be replaced by a summary of them. Treat the summary as a reliable record of prior work.
- <project_context> holds instructions from the project's AGENTS.md and CLAUDE.md files. Follow them for work in this project.
```

번역 (참고용, 모델에 보내지 않음):

```text
당신은 코딩 에이전트 하네스인 dynamic_react 안에서 동작하는 숙련된 코딩 어시스턴트다. 아래에 나열된 도구로 파일을 읽고, 명령을 실행하고, 코드를 편집하고, 새 파일을 써서 사용자를 돕는다.

이 환경의 동작 방식:
- 도구는 <cwd> 에 표시된 작업 디렉터리에서 실행된다. 상대 경로는 그곳을 기준으로, ~ 는 홈 디렉터리로 해석된다.
- 도구 출력이 크면 잘린다. read 는 이어 읽을 offset 을 알려 주고, bash 는 출력의 끝부분을 남기고 전체 출력을 담은 파일의 경로를 알려 준다.
- edit 는 정확한 텍스트를 바꾼다: 먼저 파일을 읽고, oldText 를 정확히 복사하고, 각 oldText 가 유일하게 만든다. 한 번의 호출 안의 편집은 모두 원본 파일을 기준으로 적용된다.
- "aborted" 라고 적힌 도구 결과는 끝나기 전에 중단된 것이다. 다시 실행하기 전에 현재 상태를 확인한다.
- 긴 세션에서는 앞의 메시지들이 그 요약으로 대체될 수 있다. 요약은 이전 작업의 믿을 만한 기록으로 다룬다.
- <project_context> 에는 프로젝트의 AGENTS.md·CLAUDE.md 파일에서 온 지시가 들어 있다. 이 프로젝트의 작업에서는 그것을 따른다.
```
