# examples/verify — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D12, R22, R24, R25)
- 의존 명세: `L1_inject/sets/SPEC.md` (3.2a verify), `L2_harness/agent/SPEC.md` (3.14 주입), `examples/inject/SPEC.md` (같은 CLI 형태)
- 사용하는 쪽: 개발자 (자기검증 세트 사용법 예시)

---

## 1. 소개

### 1.1 목적
완성된 에이전트에 **자기검증 세트(`sets.Verify`)를 셸 명령 검증으로 끼우는 방법**을 실행 가능한 코드로 보여 준다 (R24 ①). 제품 진입점이 아니라 예시다.

### 1.2 범위
- 포함 (모듈 `examples/verify`, 패키지 `main`): 기본 세트 위에 verify 세트를 겹친 에이전트, 셸 명령 Check, 프롬프트 하나를 실행하는 CLI
- 제외: 승인 묻기(Approver 없음 = 허용), 설정·키 파일 해석 (agent 의 몫), Check 의 시간 제한 (ctx 취소만)

---

## 2. 전체 설명

```text
verify-example -check "<shell command>" [-attempts N] [-model provider/id] [-cwd dir] [-config file] prompt ...
verify-example -i -check "<shell command>" [-attempts N] [-model provider/id] [-cwd dir] [-config file] [-no-color]
```

```text
 main ── run(ctx, args, stdout, stderr) int
          └─ agent.New(ctx, Options{Cwd, ConfigPath, Model, OnEvent, Sets})
               Sets(d) = append(harness.DefaultSets(d), sets.Verify(d, {Check: ShellCheck(dir, cmd, stderr), MaxAttempts}))
```

모듈 의존: examples/verify → agent, harness, sets, inject, core, contract, terminal(`-i`), 표준 라이브러리. go.work 에 포함하고 독립 go.mod(`v0.0.0` + replace, R8)를 갖는다.

### 2.2 제약
- VX-C1. 키 값을 출력하지 않는다 (agent 에러 문구를 그대로 씀).
- VX-C2. 로직은 `run` 에 두고 `main` 은 `os.Exit(run(signalCtx, os.Args[1:], os.Stdout, os.Stderr))` 만 한다. 테스트는 `runWith(ctx, args, stdout, stderr, mod func(*agent.Options))` 로 (examples/inject 와 같음).
- VX-C3. 환경변수를 읽지 않는다. Check 의 자식 프로세스는 OS 환경을 상속한다 (R21 ⑥ 예외).

---

## 3. 상세 요구사항

```go
func ShellCheck(dir, command string, log io.Writer) func(ctx context.Context) (sets.CheckResult, error)
```

| id | 요구사항 |
|---|---|
| VX-FR-01 | `ShellCheck`: `exec.CommandContext(ctx, "/bin/sh", "-c", command)`, Dir = dir, stdout·stderr 합침, stdin 없음. 실행 전 log 에 `[verify] <command>`. 종료 코드 0 → `{OK: true, Report: 출력}` 과 log `[verify] ok`. 0 이 아닌 종료 → `{OK: false, Report: 출력}` 과 log `[verify] failed (exit <n>)`. 실행 자체 실패(셸 없음, ctx 취소 등)는 error |
| VX-FR-02 | 플래그: `-check`(필수, 비면 `verify-example: -check is required`, 종료 코드 1), `-attempts`(→ `MaxAttempts`, 기본 0 = sets 기본 3), `-model`·`-cwd`·`-config` 는 examples/inject EX-FR-04 와 같다. Check 의 dir = agent 의 cwd (`-cwd` 가 비면 프로세스 cwd) |
| VX-FR-03 | `Sets(d)` = `append(harness.DefaultSets(d), verify)`. `sets.Verify` 는 Check 가 nil 일 때만 에러이고 `ShellCheck` 는 nil 을 돌려주지 않으므로 에러가 나지 않는다. 에러면 panic (프로그래밍 오류) |
| VX-FR-05 | `-i` (R25): 대화형. 위치 인자가 있으면 `verify-example: -i takes no prompt arguments`, 2. 승인은 터미널이 묻는다(Approver, 설정 `permissions` 그대로). stdin·stdout 이 TTY 가 아니면 `verify-example: -i needs an interactive terminal`, 2. 그 밖은 dr(`L3_ui/cmd/dr` 의 TTY 열기·`terminal.New{In, Out, Width, Color: !-no-color}`·`Approver`·`OnEvent`·SIGWINCH → `Resize`·`Run` 의 반환 → 0 / ctx 취소 130 / 그 밖 1)과 같고 agent.Options 에 `Sets`(VX-FR-03)를 더한다. Check 의 log 는 버린다(raw mode 화면을 깨지 않게) — 실패 보고는 terminal TM-FR-54 로 보인다. 새 세션으로 시작한다. 테스트용 내부 변형은 dr 과 같이 `isTerminal`·`open` 을 바꿔 넣는다 |
| VX-FR-04 | 출력·종료 코드는 examples/inject EX-FR-05·06 과 같고 접두어는 `verify-example`. 단 검증 실패로 턴이 이어지면 응답이 여럿이므로, `message_end` 때 stdout 출력이 줄바꿈으로 끝나지 않았으면 줄바꿈 하나를 쓴다. 검증 상한 도달은 Settle `failed` → `verify-example: failed: core: turn.finish: verify: checks still failing after N attempts: <첫 줄>` (core 가 step 문맥을 붙임, CORE-FR-49), 1 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| VX-FR-01 | 실제 `/bin/sh`: `true` → OK·log, `echo bad; exit 3` → 실패·보고 `bad`·`exit 3`, 취소된 ctx → error |
| VX-FR-05 | 파이프 stdin·stdout + 가짜 `isTerminal`·`open` 으로: 프롬프트 입력 → write → 끝냄 → 실패 보고가 화면에 `» Verification failed (attempt 1 of 2)` 로 나옴 → 고침 → 통과, `/quit` → 0. 비TTY → 2, 위치 인자 → 2 |
| VX-FR-02~04 | httptest Chat Completions 서버 + 임시 HOME·cwd 로 `runWith` 종단: 모델이 `write` 후 끝냄 + `-check "test -f ok"` → 첫 검증 실패 → 다음 요청에 `Verification failed (attempt 1 of 2)` → 모델이 ok 파일 write 후 끝냄 → 검증 성공 → 0. `-check false -attempts 2` → 1·`checks still failing after 2 attempts`. 텍스트만 답함 → Check 안 부름(log 없음)·0. `-check` 없음 → 1 |
| 실제 모델 | deepseek 로 수동 확인: 일부러 실패하는 테스트가 있는 임시 Go 모듈에서 "테스트 통과시켜" → 검증 실패 보고를 받고 고친 뒤 성공 (저장소 밖 임시 HOME·cwd) |

---

## 5. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | 한 번 실행하는 CLI 만 | `-i` 대화형 (VX-FR-05) | R25 ③ |
| 0.1 | 자기검증 예시 없음 | 이 모듈 | R24 ① |
