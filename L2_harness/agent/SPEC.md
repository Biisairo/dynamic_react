# agent — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (R8, R12, R14, R17)
- 의존 명세: `L2_harness/SPEC.md`, `L2_harness/osenv/SPEC.md`, `L1_inject/adapters/SPEC.md`, `L1_inject/sets/SPEC.md` (설정 JSON)
- 사용하는 쪽: `L3_ui/cmd/dr-print`, 이후 L3 surfaces

---

## 1. 소개

### 1.1 목적
사용자 환경 위에서 바로 도는 **완성된 에이전트**다 (R17). 설정 파일·환경변수를 해석하고, `osenv`(OS Env)와 `adapters`(ModelCaller)를 만들어 `harness.New` 에 넣는다. L3 는 이 모듈만 쓰고 입출력만 한다.

### 1.2 범위
- 포함 (모듈 `L2_harness/agent`, 패키지 `agent`): 설정 파일 읽기·검증, `-model` 덮어쓰기, sessionDir 해석, API 키 존재 확인, 조립, run 시작
- 제외: 터미널 입출력·시그널·종료 코드 (L3), 키 값 저장 (AD-C2: 키는 환경변수로만), 시스템 프롬프트 편집 (R16)

### 1.3 참조

| 값 | 출처 |
|---|---|
| 설정 파일 `<home>/.dynamic_react/config.json`, 중첩 형식 | 사용자 확정 (impl-03, R17) |
| 기본 sessionDir `~/.dynamic_react/sessions`, `DR_SESSION_DIR` | impl-01 사용자 확정, `session.DefaultDir` (HN-FR-15) |
| 키 없음 → 필요한 환경변수 이름 안내 | 옛 sets ST-FR-31, adapters AD-FR-50 |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 L3 ── agent.New(ctx, Options{Cwd, ConfigPath, Model, Observer})
            │ config.json ─┬─ "agent"                 → sets 설정 (Start 때 harness 에 raw JSON 으로)
            │              ├─ "providers", "models"   → adapters.ParseConfig → adapters.New(Getenv)
            │              └─ "sessionDir"
            ├─ osenv.New(cwd)
            └─ harness.New(Options{Env, Model, SessionDir, Observer})
       ── Agent.Start(ctx, sessionID, input) → *core.Run
```

모듈 의존: agent → harness, osenv, adapters, sets, core, contract, 표준 라이브러리.

### 2.2 제약
- AG-C1. 키 값을 읽는 설정 필드가 없고, 에러 문구에는 환경변수 **이름**만 쓴다 (AD-C1, C2).
- AG-C2. 환경변수·파일은 이 모듈이 읽어 하위 모듈에 값으로 넘긴다. adapters 에는 `Getenv` 를 넘긴다 (AD-C6).

---

## 3. 상세 요구사항

### 3.1 설정 파일

```json
{
  "sessionDir": "~/work/sessions",
  "agent": {"model": {"provider": "anthropic", "id": "claude-sonnet-5-5"}, "thinking": "medium"},
  "providers": {"mine": {"api": "openai-chat", "baseUrl": "http://localhost:8000/v1", "apiKeyEnv": ""}},
  "models": {"mine/qwen": {"contextWindow": 32768}}
}
```

| id | 요구사항 |
|---|---|
| AG-FR-01 | 경로: `Options.ConfigPath` 가 있으면 그것(상대 경로면 Cwd 기준), 없으면 `<home>/.dynamic_react/config.json`. **기본 경로에 파일이 없으면 빈 설정** `{}`, 명시한 경로에 없으면 에러 |
| AG-FR-02 | 최상위 키는 `sessionDir`·`agent`·`providers`·`models` 뿐이다. 모르는 키, JSON 이 아닌 내용, 뒤따르는 데이터는 에러 |
| AG-FR-03 | `providers`·`models` 는 `{"providers":…, "models":…}` 로 묶어 `adapters.ParseConfig` 로 검사한다 (모르는 키 에러는 adapters 가 낸다) |
| AG-FR-04 | `agent` 는 sets 설정 JSON 이다 (없으면 `{}`). `Options.Model` 이 있으면 `agent.model` 을 `{"provider", "id"}` 로 덮는다. 합친 결과를 `New` 에서 `sets.ParseConfig` 로 검사하고(모델 없음 등은 여기서 에러) **raw JSON 그대로** Start 때 넘긴다 (sets.Config 값을 쓰지 않음, HN-FR-27) |
| AG-FR-05 | `Options.Model` 형식은 `provider/id` 다. **첫** `/` 에서 나눈다 (id 에 `/` 허용, 예: `openrouter/anthropic/claude-x`). 어느 쪽이 비면 에러 |

### 3.2 환경

| id | 요구사항 |
|---|---|
| AG-FR-06 | `Options.Getenv` nil 이면 `os.Getenv`. `Options.Cwd` 비면 `os.Getwd()`, 상대 경로면 `os.Getwd()` 기준 절대 경로. home 은 `osenv.Env.Home()` |
| AG-FR-07 | sessionDir 우선순위: 환경변수 `DR_SESSION_DIR` > 설정 `sessionDir` > `<home>/.dynamic_react/sessions`. `~` 또는 `~/` 로 시작하면 home 으로 바꾼다. 결과가 절대 경로가 아니면 에러 (home 이 "" 인데 필요할 때 포함). 디렉터리는 미리 만들지 않는다 (세션 첫 쓰기의 `AppendFile` 이 만듦, R17 보호 없음) |
| AG-FR-08 | 모델 provider 를 `adapters.Client.Profile` 로 찾는다. 없으면 에러 `unknown provider "<name>"`. 프로필의 `APIKeyEnv` 가 비어 있지 않은데 `Getenv` 값이 비면 에러 `missing API key: set <ENV>` (값은 출력하지 않음) |

### 3.3 조립·실행

```go
type Options struct {
    Cwd        string              // "" → os.Getwd
    ConfigPath string              // "" → <home>/.dynamic_react/config.json
    Model      string              // "provider/id", 설정의 agent.model 을 덮음
    Observer   core.Observer
    Getenv     func(string) string // nil → os.Getenv
    NoRemoteCatalog bool           // adapters.Options.NoRemoteCatalog 로 전달 (테스트·오프라인)
}

func New(ctx context.Context, o Options) (*Agent, error)
func (a *Agent) Start(ctx context.Context, sessionID string, input []contract.Message) (*core.Run, string, error)
func (a *Agent) Harness() *harness.Agent
func (a *Agent) SessionDir() string
```

| id | 요구사항 |
|---|---|
| AG-FR-09 | `New` 순서: Cwd → `osenv.New` → 설정 파일(AG-FR-01~05) → sessionDir(AG-FR-07) → `adapters.New(Options{Getenv, Config, NoRemoteCatalog})` → 키 확인(AG-FR-08) → `harness.New(ctx, harness.Options{Env, Model, SessionDir, Observer})`. 어느 단계든 실패하면 에러 (앞 단계 이름을 접두어로: `agent: config: …`) |
| AG-FR-10 | `Start` = `harness.Agent.Start(ctx, RunRequest{SessionID, Input, Config: AG-FR-04 의 raw})`. 반환값은 그대로 (두 번째 = 쓰인 sessionID) |
| AG-FR-11 | `Harness()` 는 조립된 `*harness.Agent` (System·Tools 조회용), `SessionDir()` 은 AG-FR-07 의 결과 |

### 3.4 비기능

| id | 요구사항 |
|---|---|
| AG-NFR-01 | 종단 테스트는 실제 osenv(`t.TempDir()`) + httptest 로 띄운 Chat Completions 호환 서버(설정 `providers` 로 등록, `apiKeyEnv: ""`) + `HOME` 을 임시 디렉터리로 한다. 외부 네트워크를 쓰지 않는다 (`Options.NoRemoteCatalog`) |
| AG-NFR-02 | 실제 provider 종단 테스트는 환경변수 `DR_E2E_MODEL`(`provider/id`)과 그 provider 의 키가 있을 때만 돈다 (옛 HN-NFR-03). 없으면 skip |
| AG-NFR-03 | `go test -race` 통과, `GOWORK=off` 단독 테스트 통과 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| AG-FR-01 | 기본 경로 없음 → 성공(모델은 `Options.Model`), 명시 경로 없음 → 에러, 상대 ConfigPath → Cwd 기준 |
| AG-FR-02~03 | 모르는 최상위 키, 깨진 JSON, 뒤따르는 데이터, `providers` 안의 모르는 키 → 에러 |
| AG-FR-04~05 | 모델 덮어쓰기 우선, `openrouter/a/b` → provider `openrouter`·id `a/b`, `x/`·`/y`·`xy` → 에러, 모델 없음 → 에러 |
| AG-FR-06~07 | `DR_SESSION_DIR` > 설정 > 기본, `~/s` 확장, 상대 경로 에러 |
| AG-FR-08 | 없는 provider, 키 없음 → 에러 문구에 환경변수 이름만 (가짜 키 값이 문구에 없음) |
| AG-FR-09~11 | 종단: httptest 서버가 bash 도구 호출 → 텍스트 응답, 실제 osenv 로 명령 실행, 세션 파일이 sessionDir 에 생김, 같은 sessionID 로 재개 시 History 이어짐 |
| AG-NFR-02 | `DR_E2E_MODEL` 있을 때만 "ok 라고만 답하라" run → completed |

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| R17 | 1.1, AG-C2, AG-FR-06~09 |
| AD-C1, C2 | AG-C1, AG-FR-08 |
| HN-FR-15 | AG-FR-07 |
| HN-FR-27 | AG-FR-04, 10 |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.1 | 설정·env 해석, ModelCaller 생성은 L3 (R14) | `L2_harness/agent` | R17 |
