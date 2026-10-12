# agent — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.10 (초안)
- 상위 근거: `docs/DECISIONS.md` (R8, R12, R14, R17, R18, R20, R21, R22, R23, R26, R27, R30)
- 의존 명세: `L2_harness/SPEC.md`, `L2_harness/osenv/SPEC.md`, `L1_inject/adapters/SPEC.md`, `L1_inject/sets/SPEC.md` (설정 JSON)
- 사용하는 쪽: `L3_ui/cmd/dr-print`, 이후 L3 surfaces

---

## 1. 소개

### 1.1 목적
사용자 환경 위에서 바로 도는 **완성된 에이전트**다 (R17). 설정 파일·키 파일을 해석하고 (환경변수는 읽지 않는다, R21), `osenv`(OS Env)와 `adapters`(ModelCaller)를 만들어 `harness.New` 에 넣는다. L3 는 이 모듈만 쓰고 입출력만 한다.

### 1.2 범위
- 포함 (모듈 `L2_harness/agent`, 패키지 `agent`): 설정 파일 읽기·검증, `-model` 덮어쓰기, sessionDir 해석, API 키 존재 확인, 조립, run 시작. **surface 공통 계층** (R18): 도구 세트 상한, 승인 규칙·Approver, Abort·Steer·FollowUp, 이벤트, 세션 키·목록, 세션 잠금, surface 콜백 panic 격리 — 3.4~3.9. 히스토리 조회·수동 압축 — 3.10~3.11 (impl-04). 모델 선택 — 3.12 (R20). API 키 파일 읽기·저장 — 3.13 (R21). 주입(세트·부품을 바꾸는 Go 함수) — 3.14 (R22)
- 제외: 터미널 입출력·시그널·종료 코드 (L3), 설정 파일로 시스템 프롬프트를 고치거나 세트를 고르기 (R16, R22), 실행 중 세트·부품 교체 (R22 비목표)

### 1.3 참조

| 값 | 출처 |
|---|---|
| 설정 파일 `<home>/.dynamic_react/config.json`, 중첩 형식 | 사용자 확정 (impl-03, R17) |
| 기본 sessionDir `~/.dynamic_react/sessions` | impl-01 사용자 확정, `session.DefaultDir` (HN-FR-15) |
| 키 파일 `<home>/.dynamic_react/auth.json`, 형식·권한 | pi `auth.json` (`coding-agent/src/core/auth-storage.ts`: 생성 시 0600, 디렉터리 0700), R21 |
| 셸 기본 `/bin/bash`, 설정 `shellPath` | pi settings `shellPath`, R21 |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 L3 ── agent.New(ctx, Options{Cwd, ConfigPath, Model, Observer})
            │ config.json ─┬─ "agent"                 → sets 설정 (Start 때 harness 에 raw JSON 으로)
            │              ├─ "providers", "models"   → adapters.ParseConfig → adapters.New(APIKey)
            │              ├─ "sessionDir"
            │              └─ "shellPath"
            │ auth.json ── 키 → APIKey
            ├─ osenv.New(cwd, shell)
            └─ harness.New(Options{Env, Model: ModelCaller(adapters), SessionDir, OnEvent, Gate,
                                   Sets, ToolSet, System, Summarizer, Store})   // 3.14
       ── Agent.Start(ctx, sessionID, input) → *core.Run
```

모듈 의존: agent → harness, osenv, adapters, sets, core, contract, 표준 라이브러리.

### 2.2 제약
- AG-C1. 키 값은 auth.json(3.13)에만 둔다. 설정 파일·프로젝트 설정에는 키 필드가 없고, 에러 문구·이벤트에 키 값을 쓰지 않는다 (AD-C1, C2, R21).
- AG-C2. 환경변수를 읽지 않는다 (R21. 예외: 홈 디렉터리 = `osenv.Env.Home()`). 파일은 이 모듈이 읽어 하위 모듈에 값으로 넘긴다. adapters 에는 auth.json 의 키를 돌려주는 `APIKey` 를 넘긴다 (AD-C6).

---

## 3. 상세 요구사항

### 3.1 설정 파일

```json
{
  "sessionDir": "~/work/sessions",
  "shellPath": "/bin/zsh",
  "agent": {"model": {"provider": "anthropic", "id": "claude-sonnet-5-5"}, "thinking": "medium"},
  "providers": {"mine": {"api": "openai-chat", "baseUrl": "http://localhost:8000/v1"}},
  "models": {"mine/qwen": {"contextWindow": 32768}}
}
```

| id | 요구사항 |
|---|---|
| AG-FR-01 | 경로: `Options.ConfigPath` 가 있으면 그것(상대 경로면 Cwd 기준), 없으면 `<home>/.dynamic_react/config.json`. **기본 경로에 파일이 없으면 빈 설정** `{}`, 명시한 경로에 없으면 에러 |
| AG-FR-02 | 최상위 키는 `sessionDir`·`shellPath`·`agent`·`providers`·`models`·`permissions`·`sets`(AG-FR-44) 뿐이다. 모르는 키, JSON 이 아닌 내용, 뒤따르는 데이터는 에러 |
| AG-FR-03 | `providers`·`models` 는 `{"providers":…, "models":…}` 로 묶어 `adapters.ParseConfig` 로 검사한다 (모르는 키 에러는 adapters 가 낸다) |
| AG-FR-04 | `agent` 는 sets 설정 JSON 이다 (없으면 `{}`). `Options.Model` 이 있으면 `agent.model` 을 `{"provider", "id"}` 로 덮는다. 합친 결과를 `New` 에서 검사한다: **모델이 없으면 에러가 아니라 모델 문제(`ErrNoModel`, AG-FR-32)** 로 기록하고, 그 밖의 `sets.ParseConfig` 에러는 `New` 에러다. **raw JSON 그대로** Start 때 넘긴다 (sets.Config 값을 쓰지 않음, HN-FR-27) |
| AG-FR-05 | `Options.Model` 형식은 `provider/id` 다. **첫** `/` 에서 나눈다 (id 에 `/` 허용, 예: `openrouter/anthropic/claude-x`). 어느 쪽이 비면 에러 |

### 3.2 환경

| id | 요구사항 |
|---|---|
| AG-FR-06 | `Options.Cwd` 비면 `os.Getwd()`, 상대 경로면 `os.Getwd()` 기준 절대 경로. home 은 `osenv.Env.Home()`. 셸 = 설정 `shellPath`(절대 경로가 아니면 `New` 에러 `agent: config: shellPath …`), 없으면 osenv 기본(`/bin/bash`). 설정은 `osenv.New(cwd, "")` 로 읽고, `shellPath` 가 있으면 `osenv.New(cwd, shellPath)` 로 다시 만든다 |
| AG-FR-07 | sessionDir 우선순위: 설정 `sessionDir` > `session.DefaultDir(home)` (`<home>/.dynamic_react/sessions`). 환경변수는 보지 않는다 (R21). `~` 또는 `~/` 로 시작하면 home 으로 바꾼다. 결과가 절대 경로가 아니면 에러 (home 이 "" 인데 필요할 때 포함). 디렉터리는 미리 만들지 않는다 (세션 첫 쓰기의 `AppendFile` 이 만듦, R17 보호 없음) |
| AG-FR-08 | 모델 provider 를 `adapters.Client.Profile` 로 찾는다. 없으면 모델 문제 `unknown provider "<name>"`. 프로필이 `RequiresKey` 인데 그 provider 의 키(3.13)가 없으면 모델 문제 — `ErrNoAPIKey` 를 감싼 `missing API key for "<name>"` (키 값은 출력하지 않음). 모델 문제는 `New` 를 실패시키지 않는다 (R20, AG-FR-32) |

### 3.3 조립·실행

```go
type Options struct {
    Cwd        string              // "" → os.Getwd
    ConfigPath string              // "" → <home>/.dynamic_react/config.json
    Model      string              // "provider/id", 설정의 agent.model 을 덮음
    Observer   core.Observer
    NoRemoteCatalog bool           // adapters.Options.NoRemoteCatalog 로 전달 (테스트·오프라인)
}

func New(ctx context.Context, o Options) (*Agent, error)
func (a *Agent) Harness() *harness.Agent
func (a *Agent) SessionDir() string
```

| id | 요구사항 |
|---|---|
| AG-FR-09 | `New` 순서: Cwd → `osenv.New` → 설정 파일(AG-FR-01~05) → 셸(AG-FR-06) → 키 파일(AG-FR-36) → sessionDir(AG-FR-07) → `adapters.New(Options{APIKey, Config, NoRemoteCatalog})` → 모델 문제 확인(AG-FR-08) → 승인 규칙(AG-FR-12) → `harness.New(ctx, harness.Options{Env, Model: AG-FR-41 의 caller, SessionDir, OnEvent, Gate, Sets, ToolSet, System, Summarizer, Store})` → 도구 상한(AG-FR-21). 어느 단계든 실패하면 에러 (앞 단계 이름을 접두어로: `agent: config: …`) |
| AG-FR-10 | 모델 문제가 있으면 `Start` 는 run 없이 그 에러를 돌려준다 (잠금도 잡지 않음). 아니면 `Start` = `harness.Agent.Start(ctx, RunRequest{SessionID, Input, Config: AG-FR-04 의 raw})` 를 3.6 의 `*Run` 으로 감싸 돌려준다. 쓰인 sessionID 는 `Run.SessionID()` |
| AG-FR-11 | `Harness()` 는 조립된 `*harness.Agent` (System·Tools·Store 조회용), `SessionDir()` 은 AG-FR-07 의 결과 |

### 3.4 승인 규칙 (R18 ②)

```json
"permissions": {"allow": ["read", "bash(go test *)", "edit(src/**)"], "ask": ["bash(git push *)"], "deny": ["write(/etc/**)"]}
```

| id | 요구사항 |
|---|---|
| AG-FR-12 | 규칙 출처는 셋이고 목록을 이어 붙인다: 사용자 설정(3.1)의 `permissions`, **프로젝트 설정** `<cwd>/.dynamic_react/config.json` 의 `permissions`(allow 포함, 파일이 없으면 무시, 최상위 키는 `permissions` 만 — 다른 키는 에러), `Options.Permissions`(surface 프로필). `permissions` 의 키는 `allow`·`ask`·`deny` 뿐이다 |
| AG-FR-13 | 규칙 문법: `name` 또는 `name(pattern)`. name 은 도구 이름, pattern 은 비어 있지 않은 문자열(괄호 안 전체). 패턴 규칙은 `bash`·`read`·`write`·`edit`·`grep`·`find`·`ls` 에만 쓸 수 있다. 문법이 틀리거나 다른 도구에 패턴을 쓰면 `New` 에러 |
| AG-FR-14 | 대상: `bash` = 인자 `command`. `read`·`write`·`edit` = 인자 `path` 를 도구와 같은 규칙(HN-FR-05: 상대 = cwd 기준, `~` = home)으로 절대 경로로 바꾼 것. `grep`·`find`·`ls` = 같은 방식, 단 `path` 가 없거나 "" 면 cwd (도구의 기본 시작 경로, HN-FR-43) (R23). 패턴도 같은 규칙으로 절대 경로로 바꾼다. 같은 이름을 `ToolSet` 으로 바꾼 도구(AG-FR-43)도 같은 인자에서 꺼낸다 — 인자가 없으면 대상은 ""(grep·find·ls 는 cwd) 이고, "" 이면 그 도구의 패턴 규칙(deny 포함)은 맞지 않는다. 이름 규칙은 맞는다 |
| AG-FR-15 | 매칭: `bash` 패턴은 전체 문자열 일치이고 `*` 는 임의의 문자열(빈 문자열 포함)이다. 경로 패턴은 glob — `*` 는 `/` 를 넘지 않는 임의 문자열, `**` 는 0개 이상의 경로 단계. 이름 규칙은 그 도구의 모든 호출에 맞는다 |
| AG-FR-16 | **복합 명령 방어**: `*` 가 든 `bash` allow 규칙은 command 에 `;` `&&` `\|\|` `\|` 백틱 `$(` 줄바꿈 중 하나라도 있으면 맞지 않는다. `*` 없는 allow 규칙(정확 일치)과 deny·ask 규칙에는 적용하지 않는다 (사용자가 그 문자열 전체를 적었으므로) |
| AG-FR-17 | 평가: deny 에 맞으면 거부 → 이 세션에서 `allow_session` 으로 허용한 대상(AG-FR-19)이면 허용 → ask 에 맞으면 묻기 → allow 에 맞으면 허용 → 어디에도 안 맞으면 묻기. (allow_session 이 ask 보다 먼저인 이유: ask 규칙에 걸린 대상을 "세션 동안 허용" 해도 다시 묻는다면 그 답이 의미가 없다) |
| AG-FR-18 | 묻기: `Options.Approver` 가 nil 이면 허용한다 (지금 동작 유지, HN-C3). 있으면 `Approve(ctx, ApprovalRequest{SessionID, RunID, Call, Rule, Target})` 를 부른다. 답 `allow` = 이번만, `allow_session` = AG-FR-19, `deny` = 거부. Approver 가 error 를 돌려주면: ctx 가 취소됐으면 `Status: aborted` 결과(run 은 ctx 취소로 aborted 로 끝남), 그 외는 거부(사유 = 에러 문구). approval 이벤트는 `harness.Agent.Emit` 으로 낸다 (HN-FR-35) |
| AG-FR-19 | `allow_session`: 그 세션에 `name(대상 그대로)`(대상이 없는 도구는 `name`) 정확 일치 allow 규칙을 **메모리에** 더한다. 프로세스가 끝나면 사라진다 |
| AG-FR-20 | 거부된 호출의 결과는 `Status: error`, 내용 `denied: <사유>`(deny 규칙이면 `denied by rule <규칙>`)이고 모델에 전달된다. Approver 를 부른 경우와 deny 규칙으로 거부한 경우 `approval` 이벤트를 낸다 (Decision = 답 또는 `rule_deny`). allow 규칙·Approver 없음으로 허용한 경우는 이벤트가 없다 |

### 3.5 도구 세트 (R18 ①)

| id | 요구사항 |
|---|---|
| AG-FR-21 | `Options.Tools` 가 nil 이 아니면 **상한**이다: 실제 도구 = `Options.Tools` ∩ (설정 `agent.tools`, 비면 레지스트리 전부). 레지스트리는 `ToolSet` 적용 뒤의 것이다 (AG-FR-43). 합친 목록을 `agent.tools` 에 써서 sets 에 넘긴다. `Options.Tools` 에 레지스트리에 없는 이름이 있거나 교집합이 비면 `New` 에러 (sets 는 빈 tools 를 "전부" 로 해석하므로 빈 결과를 넘기면 오히려 모든 도구가 켜진다) |

### 3.6 run 제어 (R18 ③⑥⑦)

```go
func (a *Agent) Start(ctx context.Context, sessionID string, input []contract.Message) (*Run, error)

type Run struct{ /* ... */ }
func (r *Run) SessionID() string
func (r *Run) Steer(msg contract.Message) error    // 끝난 run 이면 ErrRunDone
func (r *Run) FollowUp(msg contract.Message) error // 끝난 run 이면 ErrRunDone
func (r *Run) Abort()
func (r *Run) Wait() core.Settle

var ErrSessionBusy, ErrRunDone error
```

| id | 요구사항 |
|---|---|
| AG-FR-22 | `Start`: sessionID 가 비어 있지 않으면 그 세션의 **프로세스 내 잠금**을 잡는다. 이미 잡혀 있으면 run 없이 `ErrSessionBusy`. 비면 harness 가 새 ID 를 만든다. 잠금은 Run 이 완전히 끝날 때(FollowUp 연쇄 포함) 푼다. 프로세스 간 잠금은 보장하지 않는다 |
| AG-FR-23 | `Abort`: run 의 ctx 를 취소한다 → aborted (짝 없는 tool call 은 aborted 결과, D9). 대기 중인 FollowUp 은 버린다. 여러 번 불러도 된다 |
| AG-FR-24 | `Steer`: `core.Run.Input(msg)` 로 입력함에 넣고 steer 수를 1 늘린다. Gate(HN-FR-30)는 `steer 수 > GateInput.InboxConsumed` 이면(= 아직 반영되지 않은 steer 가 있으면) 호출을 실행하지 않고 `Status: error`, 내용 `skipped: the user sent a new message` 로 돌려준다. 승인보다 먼저 판단한다. 메시지는 다음 턴 시작에 반영된다 (ST 2 turn.start) |
| AG-FR-25 | `FollowUp`: 대기열에 넣는다. run 이 `completed`·`stopped` 로 끝나면 대기열 전부를 입력으로 같은 세션에 새 run 을 시작하고(잠금 유지), 대기열이 빌 때까지 반복한다. `failed`·`guard`·`aborted` 면 대기열을 버린다. `Wait` 는 연쇄 전체가 끝난 뒤 마지막 Settle 을 돌려준다 |
| AG-FR-26 | surface 콜백 격리: `OnEvent`·`Approver` 는 dispatch 안에서 불리므로 panic 이 그 run 의 failed 가 된다 (HN-FR-34, IN-FR-11). `Observer` 는 core 가 dispatch 밖에서 부르므로 agent 가 run 마다 감싼 Observer 를 `RunRequest.Observer` 로 넘겨(HN-FR-27) recover 하고, panic 이면 그 run 을 취소한 뒤 `Wait` 는 `Settle{failed, core.ErrPanic 을 감싼 에러}` 를 돌려준다. 다른 run·세션에는 영향이 없다 |

### 3.7 이벤트 (R18 ④)

| id | 요구사항 |
|---|---|
| AG-FR-27 | `Options.OnEvent func(harness.Event)` 를 harness 에 넘긴다 (HN-FR-31~34, 동기·직렬화). agent 는 `approval` 이벤트를 더한다 (AG-FR-20). FollowUp 연쇄의 run 마다 `run_end` 가 온다 |

### 3.8 세션 (R18 ⑤)

```go
func SessionKey(key string) string
func (a *Agent) Sessions(ctx context.Context) ([]SessionInfo, error)

type SessionInfo struct {
    ID       string
    Title    string    // 첫 user 메시지의 텍스트 앞 80자 (rune 기준)
    Updated  time.Time // 마지막 메시지의 Timestamp
    Messages int       // message 줄 수
}
```

| id | 요구사항 |
|---|---|
| AG-FR-28 | `SessionKey(key)` = `"k-"` + sha256(key) 의 앞 16 hex. 같은 키는 항상 같은 ID 이고 `session.CheckID` 를 통과한다. 키 구성(채널·스레드·사용자)은 surface 가 정한다 |
| AG-FR-29 | `Sessions`: harness 의 저장소(`Harness().Store()`, HN-FR-40)의 `List` 로 ID 를 얻고 각각 `Load` 해 `SessionInfo` 를 만든다. Updated 내림차순. `List` 에러는 에러 (기본 저장소는 sessionDir 이 없으면 빈 목록, HN-FR-37). `Load` 에 실패한 세션(기본 저장소: 읽을 수 없거나 깨진 파일)은 건너뛴다. 편집(압축)과 무관하게 저장된 원본 메시지 기준이다 |

### 3.9 Options 추가

```go
Tools       []string            // 도구 상한 (AG-FR-21)
Permissions Permissions         // {Allow, Ask, Deny []string} (AG-FR-12)
Approver    Approver            // nil = 묻기 없이 허용 (AG-FR-18)
OnEvent     func(harness.Event) // AG-FR-27

type Approver interface {
    Approve(ctx context.Context, req ApprovalRequest) (Decision, error)
}
type ApprovalRequest struct {
    SessionID, RunID string
    Call             contract.ToolCall
    Rule             string // 맞은 ask 규칙, 없으면 "" (기본 묻기)
    Target           string // AG-FR-14 의 대상
}
type Decision struct {
    Kind   string // "allow" | "allow_session" | "deny"
    Reason string
}
```

### 3.10 히스토리 (impl-04 ⑥)

```go
func (a *Agent) History(ctx context.Context, sessionID string) (History, error)

type History struct {
    Messages []contract.Message // 투영 중 원본 메시지만, 순서대로
    Omitted  int                // 투영에서 빠진 원본 메시지 수 (압축 등)
}
```

| id | 요구사항 |
|---|---|
| AG-FR-30 | `History`: sessionID 가 `session.CheckID` 를 통과하지 않으면 에러. harness 의 저장소(`Harness().Store()`, HN-FR-40)의 `Load` 결과에 `sets.ApplyEdits(Messages, Edits)` 를 적용한 투영에서 **원본 Messages 에 있는 ID 의 메시지만** 남긴다 (편집으로 들어온 메시지 — 압축 요약 — 는 뺀다). `Omitted` = 원본 메시지 수 − 남은 메시지 수. 저장되지 않은 세션은 빈 결과, `Load` 에러는 에러 (기본 저장소: 깨진 파일). 세션 잠금을 잡지 않는다 (읽기 전용, 실행 중인 세션도 읽음) |

### 3.11 수동 압축 (impl-04 ⑦)

```go
func (a *Agent) Compact(ctx context.Context, sessionID string) (bool, error)
```

| id | 요구사항 |
|---|---|
| AG-FR-31 | `Compact`: sessionID 가 비면 에러. 모델 문제(AG-FR-32)가 있으면 잠금 없이 그 에러. AG-FR-22 와 같은 세션 잠금을 잡는다 (잡혀 있으면 `ErrSessionBusy`) — 잠금 중이라 그 세션의 run 과 겹치지 않는다. `harness.Agent.Compact(ctx, sessionID, AG-FR-04 의 raw 설정)` 의 결과를 그대로 돌려주고 잠금을 푼다. compaction 이벤트는 harness 가 낸다 (HN-FR-36) |

### 3.12 모델 선택 (R20)

```go
var ErrNoModel error  // "no model configured"
var ErrNoAPIKey error // "missing API key" (AG-FR-08 이 감쌈)

type ModelChoice struct{ Provider, ID string }

func (a *Agent) Models() []ModelChoice
func (a *Agent) Model() (string, error)
func (a *Agent) SetModel(ctx context.Context, ref string) error
```

| id | 요구사항 |
|---|---|
| AG-FR-32 | 모델 문제: 모델 없음 = `ErrNoModel`, 모르는 provider·키 없음 = AG-FR-08 의 에러. `Model()` 은 현재 모델 `provider/id`(없으면 "")와 모델 문제(없으면 nil)를 돌려준다. 키 확인은 `Model()`·`Start` 를 부를 때마다 현재 키(3.13)로 다시 한다 (SetModel·SetKey·RemoveKey 뒤 상태를 반영) |
| AG-FR-33 | `Models()`: `adapters.Client.Models()` 중 provider 가 `RequiresKey` 가 아니거나 키가 있는 항목만 (R21 ②, pi `/model`). 순서는 adapters 와 같다 |
| AG-FR-34 | `SetModel(ctx, ref)`: ref 형식은 AG-FR-05(틀리면 에러). provider 가 프로필에 없으면 에러 `unknown provider`, 키가 없으면 AG-FR-08 의 `ErrNoAPIKey` 에러 — 에러면 아무것도 바꾸지 않는다. 검증을 통과하면 **사용자 설정 파일**(AG-FR-01 의 경로, 기본 경로에 파일이 없으면 새로 만듦, 상위 디렉터리 0755)을 읽어 `agent.model` 을 `{"provider","id"}` 로 바꿔 쓴다: 최상위의 다른 키와 `agent` 안의 다른 키는 값을 그대로 두고(키 순서는 사전순, 들여쓰기 2칸으로 다시 씀) 끝 줄바꿈, 파일 0644. 다시 읽은 파일이 AG-FR-02 검사에 걸리면 에러이고 쓰지 않는다. 쓰기에 실패하면 에러이고 바꾸지 않는다. 성공하면 이후 `Start`·`Compact` 의 설정(AG-FR-04 의 raw)에서 모델을 바꾼다 (이미 시작한 run 은 그대로). `Options.Model` 덮어쓰기보다 이긴다 |
| AG-FR-35 | `Model`·`Models`·`SetModel`·`Start`·`Compact`·`Providers`·`SetKey`·`RemoveKey` 는 동시에 불러도 안전하다 |

### 3.13 API 키 (R21)

```go
type ProviderChoice struct {
    Name        string
    RequiresKey bool // 프로필의 RequiresKey
    HasKey      bool // auth.json 에 키가 있음
}

func (a *Agent) Providers() []ProviderChoice
func (a *Agent) SetKey(ctx context.Context, provider, key string) error
func (a *Agent) RemoveKey(ctx context.Context, provider string) error
```

| id | 요구사항 |
|---|---|
| AG-FR-36 | 키 파일 = `<home>/.dynamic_react/auth.json` (home 이 "" 면 키 파일 없음: 읽기는 빈 것, 쓰기는 에러). 형식 `{"<provider>": {"type": "api_key", "key": "<값>"}}` (pi auth.json 의 API 키 항목). `New` 에서 읽는다: 없으면 비어 있음. JSON 이 아님·뒤따르는 데이터·항목의 모르는 키·`type` 이 `api_key` 가 아님(OAuth 항목 포함, AD-C1)·`key` 가 문자열이 아님 → `New` 에러 `agent: auth: <path>: …` (키 값은 문구에 넣지 않는다). 프로필에 없는 provider 의 항목은 쓰지 않고 파일에는 보존한다. `key` 가 "" 이면 키 없음. adapters 의 `APIKey(provider)` 는 메모리의 현재 키를 돌려준다 |
| AG-FR-37 | `SetKey(ctx, provider, key)`: provider 가 프로필에 없으면 에러 `unknown provider "<name>"`. key 의 앞뒤 공백을 자르고 비면 에러 `empty API key`. 키 파일을 다시 읽어(없으면 `{}`, AG-FR-36 검사에 걸리면 에러) 그 provider 항목을 `{"type":"api_key","key":<key>}` 로 바꾸고 다른 항목은 값을 그대로 두고(키 순서는 사전순, 들여쓰기 2칸으로 다시 씀) 끝 줄바꿈으로 쓴다. 쓰기 = 같은 디렉터리의 임시 파일(0600)에 쓰고 rename (중간 실패 시 원래 파일 그대로). 상위 디렉터리가 없으면 0700 으로 만든다. 에러면 파일과 메모리의 키 모두 그대로. 성공하면 메모리의 키를 바꾼다 — 이후 모델 호출·`Model()`·`Models()`·`Providers()` 가 새 키를 쓴다 |
| AG-FR-38 | `RemoveKey(ctx, provider)`: 키 파일에 그 provider 항목이 없으면 에러 `no stored API key for "<name>"`. 있으면 그 항목만 지우고 AG-FR-37 과 같은 방식으로 쓰고, 성공하면 메모리의 키를 지운다 |
| AG-FR-39 | `Providers()`: adapters `Client.Providers()` 순서대로 Name·RequiresKey·HasKey |

### 3.14 주입 (R22)

```go
// Options 추가. 모두 nil = 지금 동작. 기본값을 받아 바꾼 값을 돌려준다.
Sets        func(d sets.Deps) []harness.SetEntry          // → harness.Options.Sets (R26)
ToolSet     func(def []sets.Tool) []sets.Tool             // → harness.Options.ToolSet
System      func(def []contract.Block) []contract.Block   // → harness.Options.System
Summarizer  func(def sets.Summarizer) sets.Summarizer     // → harness.Options.Summarizer
Store       func(def harness.Store) harness.Store         // → harness.Options.Store
ModelCaller func(def sets.ModelCaller) sets.ModelCaller   // AG-FR-41
```

| id | 요구사항 |
|---|---|
| AG-FR-40 | `Sets`·`ToolSet`·`System`·`Summarizer`·`Store` 는 harness 의 같은 이름 필드로 그대로 넘긴다. 의미·순서·nil 결과는 HN-FR-38~42 를 따른다. harness 의 에러는 `agent: ` 접두어로 돌려준다 |
| AG-FR-41 | `ModelCaller`: def = adapters Client. 결과를 `harness.Options.Model` 로 넘긴다 — run 의 모델 호출과 자동·수동 압축의 요약 호출이 이것을 거친다. nil 결과는 `New` 에러 `agent: ModelCaller returned nil`. 모델 목록·provider 프로필·키 확인(AG-FR-08, 32~34, 39)은 adapters Client 그대로 쓴다 |
| AG-FR-42 | 승인과의 관계 (HN-FR-41): 승인 규칙·Approver·Steer 의 skip(AG-FR-17~20, 24)은 Gate 이므로 사용자 세트와 무관하게 유지된다. base 기본 검사를 빼는 것은 사용자 책임이다. Steer 의 skip 판단은 base 공개 상태 `inbox.consumed` 를 읽으므로, 이름이 `base` 인 세트가 그 상태를 갱신하지 않으면 Steer 뒤의 도구 호출이 계속 skipped 된다 |
| AG-FR-43 | 도구: 도구 상한(AG-FR-21)·설정 `agent.tools`·이름 규칙(AG-FR-13·15)은 `ToolSet` 적용 뒤의 registry 를 기준으로 한다 — 추가한 도구도 상한·규칙의 대상이다. 패턴 규칙은 계속 AG-FR-13 의 이름(`bash`·`read`·`write`·`edit`·`grep`·`find`·`ls`)에만 쓴다 (AG-FR-13, 추가 도구에 패턴 → `New` 에러). 패턴 비교 값은 AG-FR-14 |

### 3.14a 세트 배치 (R26)

```json
"sets": [{"name": "base", "on": true}, {"name": "require", "on": true}, {"name": "guard", "on": false}]
```

```go
func (a *Agent) Sets() []harness.SetState
func (a *Agent) ArrangeSets(ctx context.Context, layout []harness.SetState) error
```

| id | 요구사항 |
|---|---|
| AG-FR-44 | `sets` 는 `{"name": 비지 않은 문자열, "on": bool}` 객체의 배열이다 (다른 키·형식 → `New` 에러 `agent: config: sets: …`). `New` 는 이것을 `harness.Options.Layout` 으로 넘긴다 (HN-FR-52: 등록되지 않은 이름은 무시, 빠진 등록 세트는 뒤에). 그 배치로 harness 가 실패하면 `New` 에러다 |
| AG-FR-45 | `Sets()` = `Harness().Sets()`. `ArrangeSets(ctx, layout)` = `harness.Arrange(layout)` 가 성공한 뒤 **사용자 설정 파일**의 `sets` 를 새 배치 전체(`[{name, on}]`, 배치 순서)로 바꿔 쓴다 — 파일 다루기(경로·생성·다른 키 보존·키 순서·0644·검사 실패 시 쓰지 않음)는 AG-FR-34 와 같다. harness 가 거부하면 파일을 건드리지 않는다. 파일 쓰기에 실패하면 에러를 돌려주고 harness 배치를 이전으로 되돌린다 |
| AG-FR-46 | `Sets`·`ArrangeSets` 는 AG-FR-35 의 함수들과 동시에 불러도 안전하다 |

### 3.14b 모드 (R27)

```go
// Options 추가
Modes []harness.Mode // harness.Options.Modes 로 그대로

func (a *Agent) Modes() []harness.Mode
func (a *Agent) ModeStatus(ctx context.Context, sessionID string) (harness.ModeStatus, error)
func (a *Agent) EnterMode(ctx context.Context, sessionID, name, arg string) error
func (a *Agent) ExitMode(ctx context.Context, sessionID string) error
func (a *Agent) Complete(ctx context.Context, role string, system []contract.Block, msgs []contract.Message) (contract.Message, error)
```

| id | 요구사항 |
|---|---|
| AG-FR-47 | 설정 `agent.aux`(sets ST-FR-54)는 `agent` 의 다른 키와 같이 sets 설정으로 그대로 넘어간다 (AG-FR-04). 형식 에러는 `New` 에러다 |
| AG-FR-48 | `Options.Modes` 를 `harness.Options.Modes` 로 넘긴다. harness 의 모드 검사 에러(HN-FR-55)는 `agent: ` 접두어로 `New` 에러 |
| AG-FR-49 | `Modes()` = harness `Modes()`. `ModeStatus` = harness `ModeStatus`. `EnterMode(sid, name, arg)` = `RequestMode(sid, {To: name, Arg: arg, Origin: user})`, `ExitMode(sid)` = `RequestMode(sid, {To: "", Origin: user})`. 세션 잠금(AG-FR-22)을 잡지 않으므로 실행 중인 세션에도 부를 수 있다 (적용은 다음 turn.start, HN-FR-66) |
| AG-FR-50 | `Complete(role, system, msgs)`: 모델 문제(AG-FR-32)가 있으면 그 에러. 아니면 `harness.Complete(ctx, AG-FR-04 의 raw 설정, role, system, msgs)` |
| AG-FR-51 | `mode` 도구(HN-FR-68)의 승인은 이름 규칙만 쓴다 (패턴 규칙 대상이 아님, AG-FR-13). 맞는 규칙이 없으면 묻는다 (R18 ②). 도구 상한(AG-FR-21)·설정 `agent.tools` 의 대상이다 |

### 3.14c 상태 조회 (R30)

| id | 요구사항 |
|---|---|
| AG-FR-52 | `Stats(sessionID) (harness.Stats, bool)` = harness `Stats` 그대로 |
| AG-FR-53 | `ToolNames() []string` = 모델에 보내는 도구 이름: 지금 설정(AG-FR-04, 도구 상한 AG-FR-21 적용 뒤)의 `tools` 가 비지 않으면 그중 레지스트리에 있는 것, 비면 레지스트리 전부. 순서는 레지스트리 순서. 설정을 읽지 못하면 레지스트리 전부 |

### 3.15 비기능

| id | 요구사항 |
|---|---|
| AG-NFR-01 | 종단 테스트는 실제 osenv(`t.TempDir()`) + httptest 로 띄운 Chat Completions 호환 서버(설정 `providers` 로 등록, 키 불필요) + `HOME` 을 임시 디렉터리로 한다. 외부 네트워크를 쓰지 않는다 (`Options.NoRemoteCatalog`) |
| AG-NFR-02 | 실제 provider 종단 테스트는 테스트 전용 환경변수 `DR_E2E_MODEL`(`provider/id`)과 `DR_E2E_KEY`(그 provider 의 키)가 있을 때만 돈다 (옛 HN-NFR-03, R21 ⑥ 예외 — 테스트 코드만 읽고 임시 HOME 의 auth.json 에 쓴다). 없으면 skip |
| AG-NFR-03 | `go test -race` 통과, `GOWORK=off` 단독 테스트 통과 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| AG-FR-01 | 기본 경로 없음 → 성공(모델은 `Options.Model`), 명시 경로 없음 → 에러, 상대 ConfigPath → Cwd 기준 |
| AG-FR-02~03 | 모르는 최상위 키, 깨진 JSON, 뒤따르는 데이터, `providers` 안의 모르는 키 → 에러 |
| AG-FR-04~05 | 모델 덮어쓰기 우선, `openrouter/a/b` → provider `openrouter`·id `a/b`, `x/`·`/y`·`xy` → New 에러, 모델 없음 → New 성공·ErrNoModel |
| AG-FR-06~07 | 설정 > 기본, 프로세스 환경에 `DR_SESSION_DIR` 를 두어도 무시, `~/s` 확장, 상대 경로 에러, `shellPath` 상대 경로 → 에러, `shellPath` 로 bash 도구 실행 |
| AG-FR-08 | 없는 provider, 키 없음 → New 성공·모델 문제 문구에 provider 이름 (`errors.Is(err, ErrNoAPIKey)`), 키 값은 어느 문구에도 없음 |
| AG-FR-09~11 | 종단: httptest 서버가 bash 도구 호출 → 텍스트 응답, 실제 osenv 로 명령 실행, 세션 파일이 sessionDir 에 생김, 같은 sessionID 로 재개 시 History 이어짐 |
| AG-NFR-02 | `DR_E2E_MODEL`·`DR_E2E_KEY` 있을 때만 "ok 라고만 답하라" run → completed |
| AG-FR-12~13 | 세 출처 합침, 프로젝트 파일의 다른 키 에러, 없는 프로젝트 파일 무시, 잘못된 규칙·패턴 규칙을 다른 도구에 → New 에러 |
| AG-FR-14~17 | 표 기반: bash 와일드카드·정확 일치, 경로 glob(`*` 한 단계, `**` 여러 단계, 상대·`~` 패턴), grep·find·ls 경로 규칙(`path` 있음·없음 → cwd), 복합 명령 6종이 `*` allow 에 안 맞음·정확 일치 allow 에는 맞음·deny 에는 맞음, 우선순위 deny > ask > allow > 기본 ask |
| AG-FR-18~20 | 가짜 Approver: nil → 허용, allow·deny(사유 전달)·allow_session(같은 세션 다음 호출은 묻지 않음, 다른 세션은 물음, deny 규칙이 여전히 이김), Approver error → 거부, ctx 취소 → aborted, approval 이벤트 유무 |
| AG-FR-21 | 상한 교집합, 설정 tools 와 교집합, 없는 이름 → 에러, 모델 요청의 tools 목록 |
| AG-FR-22 | 같은 세션 동시 Start → ErrSessionBusy, 끝난 뒤 다시 Start 성공, 다른 세션 동시 실행 |
| AG-FR-23~25 | Abort → aborted·FollowUp 버림, 도구 2개 턴 도중 Steer → 남은 도구 skipped·다음 턴 요청에 steer 메시지·그다음 턴 도구는 정상, FollowUp → 연쇄 run·Wait 는 마지막 Settle·failed 면 버림, 끝난 run 에 Steer·FollowUp → ErrRunDone |
| AG-FR-26 | OnEvent·Approver·Observer panic → 그 run 만 failed(ErrPanic), 동시에 돌던 다른 세션 run 은 completed |
| AG-FR-27 | approval·run_end 가 이벤트 순서에 맞게 옴 (연쇄 run 마다 run_end) |
| AG-FR-28~29 | SessionKey 결정성·CheckID 통과, Sessions 의 Title(80 rune)·Updated·Messages·정렬·깨진 파일 건너뜀·없는 dir |
| AG-FR-30 | 압축된 세션 → 요약 메시지 없음·Omitted = 대체된 원본 수·나머지 순서, hide 편집, 편집 없는 세션 = 원본 전부, 없는 파일 → 빈 결과, 깨진 파일·잘못된 ID → 에러, 실행 중인 세션도 읽음 |
| AG-FR-32~35 | 모델 없음 → New 성공·Model() ErrNoModel·Start ErrNoModel(잠금 없음), 모르는 provider·키 없음 → New 성공·같은 문제, Models 는 키 없는 provider 의 모델을 뺌·키 불필요/키 있는 것·설정 모델 포함, SetModel: 형식 틀림·모르는 provider·키 없음 → 에러·파일 그대로, 성공 → 파일의 agent.model 갱신·다른 키 보존·파일 없으면 생성·다음 Start 요청의 모델·Options.Model 보다 이김, 쓰기 실패 → 에러·모델 그대로, -race 동시 호출 |
| AG-FR-36~39 | 키 파일 없음 → 빈 키, 깨진 JSON·뒤따르는 데이터·모르는 키·type oauth·key 숫자 → New 에러(문구에 키 값 없음), 모르는 provider 항목 보존. SetKey: 모르는 provider·빈 키(공백만) → 에러·파일 그대로, 성공 → 파일 0600·디렉터리 0700(새로 만들 때)·다른 항목 원문 보존·앞뒤 공백 잘림·다음 Start 요청의 인증 헤더·Model() 문제 해소. RemoveKey: 없는 항목 → 에러, 성공 → 항목만 삭제·Model() ErrNoAPIKey. 쓰기 실패(디렉터리 쓰기 불가) → 에러·키 그대로. Providers 순서·HasKey. -race 동시 호출 |
| AG-FR-40~43 | httptest 서버 종단: `Sets` 로 덧붙인 `context.inject` 메시지가 서버가 받은 요청에 있음, `tool.before` 를 덮어도 deny 규칙·Approver 결과가 모델에 전달됨, `ToolSet` 추가 도구가 요청 tools 에 있고 상한·`agent.tools`·이름 deny 규칙이 적용됨·추가 도구 패턴 규칙 → New 에러, `bash` 를 `command` 없는 도구로 바꾸면 `bash(rm *)` deny 가 맞지 않고 `bash` 이름 deny 는 맞음, `System` 결과가 요청 system, `Summarizer` 감싸기가 `Compact` 에 쓰임, `Store` 로 바꾼 저장소가 `Sessions`·`History` 에 쓰임(파일이 없어도 목록에 나옴), `ModelCaller` 감싸기가 run 요청을 거침·nil → New 에러, 모든 nil = 기존 테스트 그대로 |
| AG-FR-52~53 | `Stats` 가 run 뒤 harness 값과 같음·기록 없음 false, `ToolNames`: 상한 없음 → 레지스트리 전부, `Options.Tools` 상한 → 교집합(레지스트리 순서), 설정 `agent.tools` → 그 목록 |
| AG-FR-47~51 | httptest 종단: `agent.aux` 형식 에러 → New 에러, `Complete` 가 aux 모델로 요청(없으면 본 모델), 모델 문제 → 에러, `Options.Modes` 검사 에러 → New 에러, `EnterMode` → 다음 Start 첫 요청에 모드 세트의 주입이 있음, 실행 중 `ExitMode` → 다음 turn.start 에서 기본 배치, `mode` 도구 호출에 Approver 가 불림·`mode` deny 규칙 → error 결과, `ModeStatus` 가 재시작(New 다시) 뒤에도 같음 |
| AG-FR-44~46 | 설정 `sets` 형식 에러 → New 에러, 저장된 배치가 harness 에 반영(모르는 이름 무시), `ArrangeSets` 성공 → 파일의 `sets` 가 새 배치·다른 키 보존·다음 `New` 가 그 배치, harness 거부 → 파일 그대로, 파일 쓰기 실패(읽기 전용 디렉터리) → 에러·배치 되돌림, 동시 호출(-race) |
| AG-FR-31 | 긴 세션(작은 keepRecentTokens) → true·세션 파일에 edit 줄 하나·다음 run 요청에 요약 메시지·edit 줄 중복 없음, 짧은 세션 → false, 실행 중인 세션 → ErrSessionBusy, 빈 ID → 에러, compaction 이벤트 start/end |

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| R17 | 1.1, AG-C2, AG-FR-06~09 |
| R21 | AG-C1, C2, AG-FR-06~08, 33, 36~39 |
| AD-C1, C2 | AG-C1, AG-FR-08 |
| HN-FR-15 | AG-FR-07 |
| HN-FR-27 | AG-FR-04, 10 |
| R22, HN-FR-38~42 | 1.2, 2.1, AG-FR-09, 11, 14, 21, 29, 30, 3.14 (AG-FR-40~43) |
| R27, HN-FR-55~72 | 3.14b (AG-FR-47~51) |
| R30, HN-FR-79~83 | 3.14c (AG-FR-52~53) |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.10 | 상태 조회 없음 | `Stats`, `ToolNames` (3.14c) | R30 |
| 0.9 | 모드 없음, run 밖 모델 호출 없음 | `Options.Modes`, `Modes`·`ModeStatus`·`EnterMode`·`ExitMode`, `Complete`(역할별 보조 모델, 설정 `agent.aux`) (3.14b). 모드를 등록하지 않으면 이전과 같음 | R27 |
| 0.8 | `Sets` 는 `[]inject.FunctionSet`, 세트 구성 고정, 설정 최상위 키 6개 | `Sets` 는 `[]harness.SetEntry`, 설정 `sets`(배치), `Sets()`·`ArrangeSets`(저장) | R26 |
| 0.7 | 패턴 규칙은 bash·read·write·edit 만 (grep·find·ls 에 패턴 → `New` 에러) | grep·find·ls 도 경로 패턴 규칙, `path` 없으면 대상 cwd | R23 ④ |
| 0.6 | 외부 함수를 끼울 통로 없음 (`harness.Options` 의 Extra·Tools 를 받지도 넘기지도 않음) | `Sets`·`ToolSet`·`System`·`Summarizer`·`Store`·`ModelCaller` (3.14) | R22: core 외 전부 튜닝 |
| 0.6 | `History`·`Sessions` 가 `session.New` 로 sessionDir 의 파일을 직접 읽음 | harness 의 저장소(`Harness().Store()`)의 `Load`·`List` 를 거침 (기본 저장소면 결과 같음) | R22 ③: 저장소를 바꿨을 때 목록·이력이 어긋나지 않게 |
| 0.6 | 절 번호 중복 (3.13 API 키, 3.13 비기능) | 3.14 주입, 3.15 비기능 | 번호 정정 |
| 0.1 | 설정·env 해석, ModelCaller 생성은 L3 (R14) | `L2_harness/agent` | R17 |
| 0.5 | 키 = 환경변수(`Options.Getenv`, 프로필 `APIKeyEnv`), sessionDir 에 `DR_SESSION_DIR`, 셸 `$SHELL`(없으면 `/bin/sh`), `Models` 에 KeyEnv·KeyMissing | 키 = auth.json(`SetKey`·`RemoveKey`·`Providers`, 3.13), sessionDir = 설정만, 셸 = 설정 `shellPath`(없으면 `/bin/bash`), `Models` 는 쓸 수 있는 모델만, `Options.Getenv` 삭제, `ErrNoAPIKey` | R21 |
| 0.4 | 모델 없음·모르는 provider·키 없음 → `New` 에러 | `New` 성공, 모델 문제는 `Model()`·`Start` 에러. `Models`·`SetModel`(설정 파일 저장) | R20: 설정이 없어도 실행되고 앱 안에서 고른다 |
| 0.3 | 히스토리·수동 압축 없음 | `History`(투영 중 원본 메시지 + Omitted), `Compact`(세션 잠금 + harness 수동 압축) | impl-04 터미널 surface: 이어갈 때 지난 대화 다시 그리기(편집 적용 뒤, 요약은 걸러냄), `/compact` 즉시 압축 |
| 0.2 | `Start` → `(*core.Run, string, error)`, surface 기능 없음 | `Start` → `(*Run, error)` (Steer·FollowUp·Abort·Wait·SessionID), 승인 규칙·Approver, 도구 상한, 이벤트, 세션 키·목록, 세션 잠금, 콜백 panic 격리 (3.4~3.9) | R18 |
