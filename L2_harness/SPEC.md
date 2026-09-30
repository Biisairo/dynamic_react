# harness — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D14, D15, D16, R8, R13, R14)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`, `L1_inject/sets/SPEC.md`
- 사용하는 쪽: `L3_ui` (Env 구현·ModelCaller·사용자 입출력을 넣는다), `L3_ui/cmd/dr-print` (검증용)

---

## 1. 소개

### 1.1 목적
L1(inject + sets) 위에서 **코딩 에이전트를 함수 하나(`Agent`)로** 제공한다 (R13, R14). 도구가 무엇을 하는지(인자, 절단, edit 알고리즘), 세션을 어떤 형식으로 남기는지, 시스템 프롬프트·요약 프롬프트를 어떻게 만드는지는 여기서 정한다. **실제 입출력(파일, 프로세스, 네트워크, 터미널, 환경변수)은 하지 않고** `Env` interface 와 `sets.ModelCaller` 로 받는다 (pi `packages/agent/src/harness` 의 `ExecutionEnv` 구조).

### 1.2 범위
- 포함 (모듈 `L2_harness/harness`):
  - `Env` interface (FileSystem + Shell) — 3.1
  - 도구 4종 `read`·`write`·`edit`·`bash` 의 로직 (`sets.Tool`, Env 위에서 동작) — 3.2
  - JSONL 세션 형식과 `sets.Store` 구현 (Env.FileSystem 위에서 동작) — 3.3
  - 시스템 프롬프트 조립: 기본 문안, 대체·덧붙임, `AGENTS.md`·`CLAUDE.md` 탐색, 스킬 목록 (Env.FileSystem 으로 읽음) — 3.4
  - 요약기 (`sets.Summarizer`, pi 요약 프롬프트, ModelCaller 로 호출) — 3.5
  - 조립: `harness.New(Options)` → `Agent.Start` → `*core.Run` — 3.6
- 제외 — L3 (R14):
  - `Env` 의 OS 구현 (`os`·`os/exec`, 프로세스 그룹 kill, 임시 파일) → `L3_ui/osenv`
  - `ModelCaller` 생성 (adapters.Client, API 키 환경변수) → L3
  - 터미널 입출력, 스트리밍 표시, 사용자 이미지 첨부 읽기, 설정 파일·환경변수 해석, 시그널, 종료 코드
  - 검증용 CLI → `L3_ui/cmd/dr-print`
- 제외 — 다음 단계: 권한 승인, 서브에이전트, MCP, 병렬 도구 스케줄러 (sets 1.2 와 같음)

L3 가 쓰는 통로: 스트림·생명주기 = `core.Observer`, 사용자 입력(텍스트·이미지) = `contract.Message`, follow-up = `core.Run.Input`.

### 1.3 참조

| 값 | 출처 |
|---|---|
| Env = FileSystem + Shell, 도구·JSONL 이 Env 위에서 동작 | pi `packages/agent/src/harness/types.ts` `ExecutionEnv`, `tools/tool-context.ts`, `session/jsonl/io.ts` |
| 도구 4종 인자·동작, edit fuzzy 매칭 | pi `packages/agent/src/harness/tools/`, `packages/coding-agent/src/core/tools/` (02 문서 10장) |
| 도구 결과 2000줄 / 50KB | pi `utils/truncate.ts` (sets 1.3 과 같음) |
| 시스템 프롬프트 구조 (preamble·tools·rules·project_context·skills·cwd) | pi `coding-agent/src/core/system-prompt.ts` |
| 요약 시스템 프롬프트·요약 프롬프트·갱신 프롬프트, 직렬화 | pi `coding-agent/src/core/compaction/utils.ts`·`compaction.ts` |
| 기본 sessionDir `~/.dynamic_react/sessions`, `DR_SESSION_DIR` | impl-01 사용자 확정 (값의 해석은 L3, 3.3 HN-FR-15) |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 L3_ui ── osenv.New(cwd) : harness.Env        (os, os/exec)
       ── adapters.New(...) : sets.ModelCaller (HTTP, API key env)
       ── terminal / stream display (core.Observer) / image files -> contract.Message
        │ harness.Options{Env, Model, SessionDir, ...}
        ▼
 harness.New ── tools.All(env) ── session.New(env, dir) ── prompt.Build(...) ── summary.New(model)
        │        └────────────────── sets.Deps ─────────────────────┘
        ▼
 inject.BindWith(StateStore: sets.StateStoreOf(store), sets.Base, sets.Resilient, Extra...)
        ▼
 core.New(dispatcher, core.Options{Observer}) ── Agent.Start(ctx, RunRequest) -> *core.Run
```

모듈 의존: harness → contract, core, inject, sets, 표준 라이브러리. **adapters 에 의존하지 않는다** (ModelCaller 는 L3 가 넣음).

### 2.2 제약
- HN-C1. **harness 는 실제 입출력을 하지 않는다**: `os`, `os/exec`, `net`, `net/http`, `syscall`, `io/ioutil`, `path/filepath` 의 파일시스템 접근 함수(`Walk`, `Glob`, `Abs`, `EvalSymlinks`)를 쓰지 않는다. 파일·프로세스는 `Env`, 모델은 `ModelCaller` 로만 (R14). 시계·ID 는 `sets.Deps` 와 같이 주입(`Now`, `NewID`)한다
- HN-C2. API 키 값을 받는 필드가 없고, 세션 파일·에러 문구에 키 값을 쓰지 않는다 (키는 ModelCaller 구현의 몫).
- HN-C3. 권한 승인이 없다 — `bash`·`write`·`edit` 는 제한 없이 실행된다 (pi 와 같음, sets ST-C2). 경고 표시는 L3 의 몫이다.
- HN-C4. 시스템 프롬프트는 `New` 에서 한 번 조립하고 바꾸지 않는다 (sets ST-FR-19).

---

## 3. 상세 요구사항

### 3.1 Env (`harness` 패키지)

```go
type FileInfo struct { Name string; IsDir bool; Size int64 }

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
| HN-FR-04 | `harness/envtest` 패키지가 메모리 FileSystem 과 스크립트된 Shell 로 된 가짜 Env 를 제공한다 (테스트용, L3 구현 검증용 계약 테스트 `envtest.Conformance(t, env)` 포함) |

### 3.2 도구 (`harness/tools`)

| 도구 | 인자 | 동작 |
|---|---|---|
| `read` | `path`, `offset?`(1부터), `limit?` | 텍스트: 2000줄 또는 50KB 중 먼저 닿는 곳에서 자르고 "offset 으로 이어 읽어라" 안내. 확장자 png·jpg·jpeg·gif·webp 는 image 블록(MediaType 은 확장자로) |
| `write` | `path`, `content` | `WriteFile`. 결과: 쓴 바이트 수와 경로 |
| `edit` | `path`, `edits:[{oldText, newText}]` | pi 알고리즘: BOM 제거·LF 정규화 → 정확 매칭, 없으면 fuzzy(NFKC, 줄 끝 공백, 스마트 따옴표·유니코드 대시·특수 공백 정규화) → 없음·중복·겹침·무변화는 에러 → 모든 편집은 원본 기준 적용 → 원래 줄바꿈(CRLF)·BOM 복원. 결과에 unified diff |
| `bash` | `command`, `timeout?`(초) | `Exec{Command, Dir: Cwd, Timeout}`. 합친 출력의 **마지막** 2000줄 / 50KB 반환. 넘으면 전체를 `CreateTemp` 파일에 쓰고 경로 안내. 종료 코드 표시, 0 이 아니거나 TimedOut 이면 error 결과 |

| id | 요구사항 |
|---|---|
| HN-FR-05 | 경로 해석: 절대 경로는 그대로, `~`·`~/…` 는 `Home()` 기준(Home 이 ""면 error 결과), 그 외는 `Cwd()` 기준. `path.Clean` 으로 정리. 결과 문구에는 cwd 안이면 cwd 기준 상대 경로, 밖이면 절대 경로 |
| HN-FR-06 | 도구 실행 에러(파일 없음, 매칭 실패, 타임아웃, 0 이 아닌 종료 코드, Env 에러)는 `Status: error` 결과다. Go panic·error 로 루프를 멈추지 않는다 |
| HN-FR-07 | `read`·`bash` 는 자체 절단을 하므로 `Tool.Truncate` 를 항등 함수로 둔다 (sets 18 의 기본 절단이 이중으로 걸리지 않게). `write`·`edit` 는 nil |
| HN-FR-08 | `InputSchema` 는 sets ST-FR-38 범위(객체·required·최상위 타입)로 검사 가능한 JSON Schema 다 |
| HN-FR-09 | `tools.All(env)` 는 `read, bash, edit, write` 순서의 `[]sets.Tool`. 도구별 생성자 `tools.Read(env)` 등도 공개한다. 각 도구는 프롬프트용 한 줄 설명(`tools.Snippet(name)`)을 가진다 |
| HN-FR-10 | 같은 파일에 대한 `write`·`edit` 는 직렬화한다 (pi `file-mutation-queue`, 병렬 도구 대비) |

### 3.3 세션 (`harness/session`)

| id | 요구사항 |
|---|---|
| HN-FR-11 | `session.New(fs FileSystem, dir string) (*Store, error)` 는 `sets.Store` 구현이다. dir 는 절대 경로. 세션 파일 `<dir>/<sessionID>.jsonl`, append-only |
| HN-FR-12 | 줄 종류: `{"type":"message","message":Message}`, `{"type":"edit","edit":ContextEdit}`, `{"type":"state","ns":string,"delta":StateDelta}`. `Append*`·`SaveState` 호출 하나의 줄들은 `AppendFile` 한 번으로 쓴다. `Load` 는 `ReadFile` 후 순서대로 재생해 `sets.Session` 을 만든다 (state 는 delta 를 순서대로 적용). 파일이 없으면 빈 세션 |
| HN-FR-13 | 마지막 줄이 깨져 있으면(쓰다 중단) `Load` 는 그 줄만 버린다. 중간 줄이 깨져 있거나 모르는 type 이면 에러 |
| HN-FR-14 | sessionID 는 `[A-Za-z0-9._-]{1,128}` 이고 `.`·`..` 가 아니어야 한다. 어기면 에러 (경로 탈출 방지). `session.NewID(now, rand)` 는 `20060102-150405-<8 hex>` 형식 |
| HN-FR-15 | `session.DefaultDir(getenv func(string) string, home string) string` = `getenv("DR_SESSION_DIR")` 가 있으면 그것, 없으면 `<home>/.dynamic_react/sessions`. 순수 함수이고 값을 읽어 넘기는 것은 L3 다 |
| HN-FR-16 | 동시 사용에 안전하다 (같은 Store 값). 프로세스 간 동시 쓰기는 보장하지 않는다 |

### 3.4 시스템 프롬프트 (`harness/prompt`)

| id | 요구사항 |
|---|---|
| HN-FR-17 | `prompt.Build(Input) []contract.Block` 은 text 블록 하나. 순서: preamble(기본 문안 또는 `Input.Preamble`) → `<tools>` 도구 한 줄 설명 → `<rules>` → `<addendum>`(`Input.Append`, 있으면) → `<project_context>`(컨텍스트 파일, 있으면) → `<skills>`(있으면) → `<cwd>` |
| HN-FR-18 | 기본 문안은 코드 상수다 (미결 O1: 구현 시 초안, 사용자 확인) |
| HN-FR-19 | `prompt.ContextFiles(ctx, fs, cwd)`: cwd 부터 상위로 git 루트(`.git` 이 있는 디렉터리, 없으면 `/`)까지 `AGENTS.md`, `CLAUDE.md` 를 모아 **루트 쪽부터** `[]File{Path, Content}` 로. 한 디렉터리에 둘 다 있으면 `AGENTS.md` 먼저 |
| HN-FR-20 | `prompt.Skills(ctx, fs, cwd, home)`: `<home>/.agents/skills`, `<cwd>/.agents/skills`, `<home>/.claude/skills`, `<cwd>/.claude/skills` 순서로 `*/SKILL.md` frontmatter(`name`, `description`)를 모은다. 본문은 넣지 않는다 (모델이 `read` 로 읽음). 같은 이름은 먼저 찾은 것이 이긴다. frontmatter 가 없거나 깨진 파일, 없는 디렉터리는 건너뛴다. home 이 ""면 home 경로는 건너뛴다 |
| HN-FR-21 | 날짜·시각처럼 run 마다 바뀌는 값을 넣지 않는다 (프롬프트 캐시 보호) |

### 3.5 요약기 (`harness/summary`)

| id | 요구사항 |
|---|---|
| HN-FR-22 | `summary.New(model sets.ModelCaller)` 는 `sets.Summarizer` 다. 요청: System = 요약 시스템 프롬프트, Messages = user 메시지 하나(`<conversation>` 안에 직렬화한 구간 + `Previous` 가 있으면 `<previous-summary>` + 요약 또는 갱신 프롬프트), Model = `SummaryRequest.Model`, MaxOutputTokens = `SummaryRequest.MaxTokens`, Tools 없음 |
| HN-FR-23 | 직렬화: `[User]:`·`[Assistant]:`·`[Assistant thinking]:`·`[Assistant tool calls]: name(args)`·`[Tool result]:` 줄. 이미지는 `[image]`, opaque·ProviderData 는 생략 (pi `serializeConversation`) |
| HN-FR-24 | done 메시지의 text 블록을 이어 붙여 돌려준다. error 이벤트, iterator error, done 없이 끝남, StopReason `length`·`error`·`aborted`, 빈 텍스트는 에러 (pi `getSummarizationFailure`) |
| HN-FR-25 | ctx 취소를 따른다. 재시도하지 않는다 (sets ST-FR-27) |

### 3.6 조립 (`harness`)

```go
type Options struct {
    Env        Env                   // 필수
    Model      sets.ModelCaller      // 필수
    SessionDir string                // 필수, 절대 경로
    Preamble   string                // 기본 문안 대체
    Append     string
    Tools      []sets.Tool           // 추가·교체 (같은 이름은 덮음)
    Extra      []inject.FunctionSet  // base·resilient 위에 겹칠 세트
    Observer   core.Observer
    Core       core.Options          // Observer 는 위 필드가 이긴다
    Now        func() time.Time      // nil → time.Now
    NewID      func() string         // nil → 무작위
}
type RunRequest struct {
    SessionID string                 // 비면 session.NewID
    Input     []contract.Message
    Config    sets.Config
}
```

| id | 요구사항 |
|---|---|
| HN-FR-26 | `harness.New(ctx, Options) (*Agent, error)`: 레지스트리(`tools.All(env)` + `Options.Tools`), 세션 Store, 요약기, 시스템 프롬프트(`ContextFiles`·`Skills` 포함)를 한 번 만들고 `inject.BindWith(sets.StateStoreOf(store), sets.Base(deps), sets.Resilient(deps), Extra...)` 로 Dispatcher 를 만든다. Env·Model·SessionDir 누락, Bind 실패, 프롬프트 수집의 Env 에러(없는 파일 제외)는 에러 |
| HN-FR-27 | `Agent.Start(ctx, RunRequest) (*core.Run, string, error)`: sets.Config 를 JSON 으로 바꿔 `sets.ParseConfig` 로 검사하고(실패하면 run 없이 에러), sessionID 를 검사·생성해 run 을 시작한다. 두 번째 반환값은 쓰인 sessionID |
| HN-FR-28 | 여러 run 을 동시에 시작할 수 있다 (세션이 다르면 독립). 같은 세션의 동시 run 은 보장하지 않는다 |
| HN-FR-29 | `Agent.System() []contract.Block`, `Agent.Tools() sets.ToolRegistry` 로 조립 결과를 조회할 수 있다 (L3 표시용) |

### 3.7 비기능

| id | 요구사항 |
|---|---|
| HN-NFR-01 | 모든 테스트는 `envtest` 가짜 Env 로 한다. 실제 파일시스템·프로세스를 쓰지 않는다 (OS 구현의 테스트는 `L3_ui/osenv` 의 몫, `envtest.Conformance` 로) |
| HN-NFR-02 | 종단 테스트: 가짜 ModelCaller(스크립트된 스트림) + 가짜 Env + 실제 도구·세션·요약기 + `harness.New` 로 "read→edit→bash 라운드 후 종료", "세션 재개(새 Agent, 같은 Env·dir) 시 History·편집 복원", "overflow → 실제 요약기로 압축 → 재호출" |
| HN-NFR-03 | `go test -race` 통과, `GOWORK=off` 단독 테스트 통과 |
| HN-NFR-04 | import 는 contract·core·inject·sets·표준 라이브러리뿐이고, HN-C1 의 금지 패키지가 없다 (`go list -deps` + 금지 목록 검사) |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| HN-FR-01~04 | envtest 자체 테스트, `Conformance` 가 envtest 에서 통과 |
| HN-FR-05~10 | 도구별 단위: 정상 / 경계(offset 끝, 2000줄·50KB 경계, 빈 파일, 이미지, `~`, cwd 밖) / 실패(없는 파일, 매칭 0·2회, 겹침, 무변화, 타임아웃, 0 아닌 종료 코드, Env 에러, spill). edit fuzzy 정규화 표, CRLF·BOM 복원 |
| HN-FR-11~16 | 왕복(메시지·편집·상태 → Load), 깨진 마지막 줄 무시, 깨진 중간 줄·모르는 type 에러, 잘못된 sessionID, 호출당 AppendFile 1회, 동시 Append(-race), DefaultDir |
| HN-FR-17~21 | 골든: 기본 / 대체 / 덧붙임 / 컨텍스트 파일 순서(루트 쪽 먼저, git 루트에서 멈춤) / 스킬 우선순위·깨진 frontmatter·home 없음 |
| HN-FR-22~25 | 가짜 ModelCaller: 요청 모양(System·Previous·MaxTokens·Tools 없음), 직렬화 골든, length·error·빈 텍스트·done 없음 → 에러, 취소 |
| HN-FR-26~29 | New 에러 경로, Start 설정 검사·sessionID 검사, 동시 run 2개 |
| HN-NFR-02~04 | 종단 시나리오, `go list -deps` |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| R13 | 1.2 범위, 3.2~3.5 |
| R14 | HN-C1, 3.1, 3.6, HN-FR-15, HN-NFR-01, 04 |
| sets ST-FR-19, 35 | HN-C4, HN-FR-21 |
| sets ST-FR-24, 27, 36 | 3.5 |
| 자격증명 보호 | HN-C2 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | 기본 시스템 프롬프트 문안 (구현 시 pi preamble 을 참고해 초안 → 사용자 확인) |
| O2 | pi 에서 가져온 문안(요약 프롬프트, edit 정규화 규칙)의 라이선스 고지 — adapters 스냅샷과 함께 사용자가 나중에 처리 |
| O3 | `L3_ui/osenv`(Env OS 구현)·`L3_ui/cmd/dr-print` 스펙 — harness 구현 뒤 작성 |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.1 | 옛 sets v0.2 의 3.3·3.4·3.5·3.8 | 라이브러리 모듈 하나 + 검증용 CLI | 사용자 (R14 초안) |
| 0.1 | 세션 줄 `usage` | 제거 | usage 는 assistant 메시지에 있다 (contract CT-FR-01a) |
| 0.2 | 도구·세션·프롬프트 수집이 파일·프로세스를 직접 접근, 기본 ModelCaller = adapters | 실제 I/O 는 `Env`·`ModelCaller` 로만 받음 (HN-C1), adapters 의존 제거 | 사용자: "실제 입출력도 다 L3, 여기서는 에이전트 함수 하나" — pi 새 harness(`ExecutionEnv`) 구조 채택 (R14, 안 C) |
| 0.2 | dr-print = `L2_harness/cmd/dr-print` | `L3_ui/cmd/dr-print`, OS Env 는 `L3_ui/osenv` | 터미널·OS I/O 를 하므로 L3. L3 → L2 의존 방향 유지 (D16 정정) |
