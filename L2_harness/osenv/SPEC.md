# osenv — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (R8, R14, R17)
- 의존 명세: `L2_harness/SPEC.md` 3.1 (Env 계약 HN-FR-01~04)
- 사용하는 쪽: `L2_harness/agent`

---

## 1. 소개

### 1.1 목적
`harness/env.Env` 의 OS 구현이다. 도구·세션·프롬프트 수집이 실제 파일시스템과 셸 위에서 동작하게 한다 (R17: L2 는 사용자 환경 위의 실제 에이전트).

### 1.2 범위
- 포함 (모듈 `L2_harness/osenv`, 패키지 `osenv`): `FileSystem`(`os`), `Shell`(`os/exec`, 프로세스 그룹), `Cwd`·`Home`
- 제외: Windows (프로세스 그룹 kill 이 `Setpgid` 전제, unix 빌드 태그), 권한 승인·샌드박스 (HN-C3), 파일 권한 보호 (R17: 보호 없음), 환경변수 필터링 (프로세스 환경을 그대로 상속)

### 1.3 참조

| 값 | 출처 |
|---|---|
| 셸 = `$SHELL -c`, 없으면 `/bin/sh` | 사용자 확정 (impl-03, R17). pi `nodejs.ts` 는 bash 우선이지만 채택하지 않음 |
| 프로세스 그룹으로 실행, 취소·timeout 시 그룹 SIGKILL | pi `packages/agent/src/harness/env/nodejs.ts` `killProcessTree` (`process.kill(-pid, "SIGKILL")`, `detached`) |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 agent ── osenv.New(cwd) : env.Env ── harness.New(Options{Env, ...})
                │
                ├─ FileSystem → os.ReadFile / os.WriteFile / os.OpenFile(O_APPEND) / os.Stat / os.ReadDir / os.CreateTemp
                └─ Shell      → exec.Cmd{$SHELL -c cmd, Setpgid} → kill(-pgid, SIGKILL)
```

모듈 의존: osenv → `L2_harness/harness`(패키지 `env`, 테스트는 `envtest`), 표준 라이브러리.

### 2.2 제약
- OS-C1. Env 계약(HN-FR-01~03)을 지킨다. `envtest.ConformanceFS`·`envtest.ConformanceShell` 을 통과하는 것이 계약 충족의 기준이다.
- OS-C2. 전역 상태를 바꾸지 않는다 (`os.Chdir`, `os.Setenv` 금지). 셸 경로는 `New` 에서 한 번 읽는다.

---

## 3. 상세 요구사항

```go
type Env struct{ /* cwd, home, shell */ }

func New(cwd string) (*Env, error)
// *Env 는 env.Env 를 구현한다.
```

| id | 요구사항 |
|---|---|
| OS-FR-01 | `New(cwd)`: cwd 가 절대 경로가 아니거나 존재하는 디렉터리가 아니면 에러. `Cwd()` = `path.Clean(cwd)`. `Home()` = `os.UserHomeDir()`, 실패하면 "". 셸 = 환경변수 `SHELL`, 비면 `/bin/sh` |
| OS-FR-02 | 모든 FileSystem 경로 인자와 `ExecRequest.Dir` 는 절대 경로여야 한다. 아니면 `*fs.PathError{Err: fs.ErrInvalid}` (envtest 와 같음) |
| OS-FR-03 | FileSystem 연산은 시작 전에 `ctx.Err()` 를 검사해 취소됐으면 그 에러를 돌려준다. 연산 도중에는 끊지 않는다 |
| OS-FR-04 | 없는 경로의 `ReadFile`·`Stat`·`ListDir` 는 `os` 에러를 그대로(또는 `%w` 로 감싸) 돌려줘 `errors.Is(err, fs.ErrNotExist)` 가 참이다 |
| OS-FR-05 | `WriteFile`: 상위 디렉터리를 `MkdirAll(0755)`, `os.WriteFile(0644)`. 기존 파일은 내용만 바뀌고 권한은 유지된다. umask 가 적용된다 |
| OS-FR-06 | `AppendFile`: 상위 디렉터리를 `MkdirAll(0755)`, `OpenFile(O_WRONLY\|O_APPEND\|O_CREATE, 0644)` 후 **`Write` 한 번**, `Close`. Close 에러도 돌려준다 (HN-FR-03 세션 줄 원자성) |
| OS-FR-07 | `Stat`: 심볼릭 링크를 따라간다 (`os.Stat`). `FileInfo{Name: 경로의 마지막 요소, IsDir, Size}` |
| OS-FR-08 | `ListDir`: 이름순. 항목이 심볼릭 링크면 대상을 `os.Stat` 해 `IsDir`·`Size` 를 채운다 (스킬 디렉터리 링크 지원). 깨진 링크는 `IsDir=false`, `Size=0` 으로 포함 |
| OS-FR-09 | `CreateTemp(pattern)`: `os.CreateTemp("", pattern)`. 반환 경로가 절대 경로가 아니면 파일을 지우고 에러. writer 는 그 `*os.File` |
| OS-FR-10 | `Exec`: `exec.Cmd{Path: 셸, Args: [셸, "-c", Command], Dir: req.Dir (비면 Cwd()), Env: 프로세스 환경 그대로, Stdin: 없음(null 장치)}`. `Stdout` 과 `Stderr` 에 같은 `out` 을 넣어 합친다 (한 번에 한 goroutine 만 Write) |
| OS-FR-11 | 자식은 새 프로세스 그룹(`Setpgid`)으로 시작한다. ctx 취소 또는 `Timeout`(>0) 경과 시 `kill(-pgid, SIGKILL)` 로 그룹 전체를 끝낸다. 그룹을 벗어난 자손이 파이프를 잡고 있어도 `WaitDelay`(상수 `waitDelay` = 1초) 뒤 반환한다 |
| OS-FR-12 | 결과: 정상 종료 → `ExitCode` = 종료 코드, error nil. 우리가 보내지 않은 신호로 종료 → `ExitCode` = 128 + 신호 번호, error nil. Timeout → `TimedOut=true`, `ExitCode=-1`, error nil. ctx 취소(Timeout 아님) → error = `ctx.Err()`. 시작 실패(셸 없음, Dir 없음) → error |

---

## 4. 검증

모든 테스트는 `t.TempDir()` 아래의 실제 파일시스템과 실제 셸을 쓴다.

| 요구사항 | 방법 |
|---|---|
| OS-C1, FR-04~09 | `envtest.ConformanceFS(t, e, t.TempDir())` |
| OS-C1, FR-10~12 | `envtest.ConformanceShell(t, e, dir)` (실제 셸에서 처음 검증) |
| OS-FR-01 | 상대 cwd, 없는 cwd, 파일 cwd → 에러. `SHELL=""` → `/bin/sh` (`echo $0`), `SHELL=/bin/sh` 명시 → 그 셸 |
| OS-FR-02 | 상대 경로 인자 → `fs.ErrInvalid` (FileSystem 6종, Exec Dir) |
| OS-FR-03 | 취소된 ctx → `context.Canceled`, 파일이 만들어지지 않음 |
| OS-FR-05 | 0600 파일을 WriteFile 로 덮어써도 0600 유지 |
| OS-FR-08 | 디렉터리를 가리키는 링크 → `IsDir=true`, 깨진 링크 → 포함·`IsDir=false` |
| OS-FR-10 | Dir 비면 Cwd 에서 실행(`pwd`), stdin 이 비어 있음(`cat` 즉시 종료) |
| OS-FR-12 | `kill -TERM $$` → 143, 취소 → `context.Canceled`, 없는 Dir → error, 없는 셸(`SHELL=/nonexistent`) → error |
| 공통 | `go test -race -count=3`, `GOWORK=off` 단독 테스트, 뮤테이션 확인 |

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| R17 | 1.1, 1.2, 전체 |
| HN-FR-01 | OS-FR-02 |
| HN-FR-02 | OS-FR-04 |
| HN-FR-03 | OS-FR-06, 11, 12 |
| HN-FR-04 | 4 (Conformance) |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.1 | `L3_ui/osenv` (R14) | `L2_harness/osenv` 별도 모듈 | 사용자: L2 부터 사용자 환경 위의 실제 에이전트 (R17), osenv 는 분리 모듈 |
