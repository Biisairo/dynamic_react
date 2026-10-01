# dr — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (R17, R19)
- 의존 명세: `L3_ui/surfaces/terminal/SPEC.md`, `L2_harness/agent/SPEC.md` v0.3
- 사용하는 쪽: 사용자 (대화형 진입점)

---

## 1. 소개

### 1.1 목적
대화형 터미널 surface(`L3_ui/surfaces/terminal`)를 실행하는 진입점이다. 플래그 해석, TTY 확인, `agent.New`, 시그널, 종료 코드만 한다. 화면·키·세션 선택은 terminal, 에이전트 동작은 agent 의 몫이다 (R17).

### 1.2 범위
- 포함 (모듈 `L3_ui/cmd/dr`, 패키지 `main`)
- 제외: 비대화형 실행 (`dr-print`), 설정·환경변수 해석 (agent, 단 `NO_COLOR` 는 표시 설정이라 여기서 읽는다)

### 1.3 참조

| 값 | 출처 |
|---|---|
| 플래그 `-session`·`-c`·`-r`, 비TTY 거부, 종료 코드 0/1/2/130 | 사용자 확정 (impl-04 인터뷰 ⑥⑧) |
| `-model`·`-cwd`·`-config` | dr-print DP-FR-01 과 같음 |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
dr [-model provider/id] [-cwd dir] [-config file] [-session id | -c | -r]
```

```text
 main ── os.Exit(run(signalCtx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
 run  ── 플래그 → TTY 확인 → terminal.Open(stdin) → terminal.New → agent.New(Approver, OnEvent)
      ── Terminal.Run(ctx, agent, Start) → 종료 코드
```

모듈 의존: dr → terminal, agent, 표준 라이브러리.

### 2.2 제약
- DR-C1. 키 값을 출력하지 않는다 (agent 에러 문구 그대로, AG-C1).
- DR-C2. 로직은 `run` 에 두고 `main` 은 시그널 ctx 와 `os.Exit` 만 한다. 테스트는 `runWith(ctx, args, stdin, stdout, stderr, hooks)` 로 TTY 판별·`Open`·agent 옵션을 바꿔 끼운다 (`run` 은 실제 구현으로 호출).

---

## 3. 상세 요구사항

| id | 요구사항 |
|---|---|
| DR-FR-01 | 플래그: `-model`(→ `agent.Options.Model`), `-cwd`(→ `Cwd`), `-config`(→ `ConfigPath`), `-session id`·`-c`·`-r`(→ `terminal.Start{Session, Continue, Pick}`). 위치 인자는 받지 않는다. 플래그 파싱 에러·위치 인자·`-session`/`-c`/`-r` 중 둘 이상 → 사용법을 stderr 에, 종료 코드 2 |
| DR-FR-02 | stdin 과 stdout 중 하나라도 TTY 가 아니면 stderr 에 `dr: 대화형 터미널이 필요합니다 (비대화형은 dr-print)`, 종료 코드 2. agent 를 만들기 전에 판별한다 |
| DR-FR-03 | `terminal.Open(stdin)` → `terminal.New(Options{In: stdin, Out: stdout, Width: Open 의 width, Color: 환경변수 NO_COLOR 가 비어 있음})` → `agent.New(ctx, Options{Model, Cwd, ConfigPath, Approver: t.Approver(), OnEvent: t.OnEvent})`. `agent.New` 에러는 raw mode 를 되돌린 뒤 stderr 에 `dr: <에러>`, 종료 코드 1 |
| DR-FR-04 | `Run` 이 `nil` → 0. 시그널로 ctx 가 취소되어 `Run` 이 돌아오면 → 130. 그 밖의 `Run` 에러 → stderr 에 `dr: <에러>`, 1. 어느 경로든 종료 전에 `restore` 를 부르고, panic 이어도 `restore` 를 부른 뒤 panic 을 다시 일으킨다 |
| DR-FR-05 | 시그널: SIGINT·SIGTERM·SIGHUP 은 ctx 를 취소한다 (`Run` 이 Abort·정리 후 돌아옴, TM-FR-03). SIGWINCH 는 `Terminal.Resize()` |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| DR-FR-01 | 모르는 플래그·위치 인자·`-c -r` → 2 |
| DR-FR-02 | 일반 파일 stdin/stdout → 2, stderr 문구 |
| DR-FR-03 | hooks 로 TTY 판별·Open 을 가짜로, 없는 config 경로 → 1 |
| DR-FR-04 | hooks + httptest 모델 서버: In 에 `/quit\r` → 0, ctx 취소 → 130, restore 호출 확인 |
| DR-FR-05 | 수동 검증 (시그널 배선) |

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| R17 | 1.1, DR-C1 |
| 인터뷰 ⑥ | DR-FR-01 |
| 인터뷰 ⑧ | DR-FR-02, 04, 05 |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.1 | 대화형 진입점 없음 | `dr` | 사용자: "대화형 터미널 해보자" (impl-04) |
