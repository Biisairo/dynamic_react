# dr-print — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D16, R17)
- 의존 명세: `L2_harness/agent/SPEC.md`, `L0_core/core/SPEC.md` (Observer, Settle)
- 사용하는 쪽: 개발자 (검증용 CLI)

---

## 1. 소개

### 1.1 목적
완성된 에이전트(`L2_harness/agent`)를 터미널에서 프롬프트 하나로 실행하는 검증용 CLI 다. L3 이므로 **입출력만** 한다: 인자·stdin 읽기, 스트림 표시, 시그널, 종료 코드 (R17).

### 1.2 범위
- 포함 (모듈 `L3_ui/cmd/dr-print`, 패키지 `main`)
- 제외: 대화형 입력·follow-up, 이미지 첨부, 도구 결과 본문 표시, 색상·TUI, 설정·키 파일 해석 (agent 의 몫)

### 1.3 참조

| 값 | 출처 |
|---|---|
| 플래그, 출력 채널, 종료 코드 0/1/2/130, 키 없음 안내 | 옛 sets ST-FR-28~31 (e0dc207), 사용자 확정 (impl-03) |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
dr-print [-model provider/id] [-session id] [-cwd dir] [-config file] [-json] [prompt ...]
echo "prompt" | dr-print ...
```

```text
 main ── run(ctx, args, stdin, stdout, stderr) int
          ├─ agent.New(ctx, Options{Cwd, ConfigPath, Model, Observer: printer})
          ├─ Agent.Start(runCtx, session, [user message])
          └─ Run.Wait() → Settle → 종료 코드
```

모듈 의존: dr-print → agent, core, contract, 표준 라이브러리.

### 2.2 제약
- DP-C1. 키 값을 출력하지 않는다 (agent 에러 문구를 그대로 씀, AG-C1).
- DP-C2. 로직은 `run` 함수에 두고 `main` 은 `os.Exit(run(signalCtx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))` 만 한다 (테스트 가능성).

---

## 3. 상세 요구사항

| id | 요구사항 |
|---|---|
| DP-FR-01 | 플래그: `-model`(→ `agent.Options.Model`), `-session`(비면 새 세션), `-cwd`(→ `Options.Cwd`), `-config`(→ `Options.ConfigPath`), `-json`. 플래그 파싱 에러는 사용법을 stderr 에, 종료 코드 1 |
| DP-FR-02 | 프롬프트: 플래그 뒤 인자들을 공백 하나로 잇는다. 인자가 없으면 stdin 전체를 읽는다. 앞뒤 공백을 뺀 결과가 비면 `dr-print: empty prompt` 를 stderr 에, 종료 코드 1 |
| DP-FR-03 | 입력은 user 메시지 하나(`contract.Message{Role: user, Content: [text 블록]}`, `ID` = `crypto/rand` 16 byte 의 hex, `Timestamp` = 현재 시각 — contract 가 둘 다 요구)다 |
| DP-FR-04 | `agent.New`·`Start` 에러는 `dr-print: <에러>` 를 stderr 에, 종료 코드 1 (키 없음 → `Start` 의 `missing API key for "<provider>"` 가 그대로 보임, 옛 ST-FR-31) |
| DP-FR-05 | Start 성공 직후 `session: <sessionID>` 한 줄을 stderr 에 쓴다 (`-json` 이어도) |
| DP-FR-06 | 기본 출력: `text_delta` 의 Delta 를 stdout 에 바로 쓴다. `toolcall_end` 는 stderr 에 `[tool] <name> <arguments JSON, 200 byte 초과 시 자르고 …>`. `error` 스트림 이벤트는 stderr 에 `[error] <class>: <message>`. run 이 끝났을 때 stdout 에 쓴 텍스트가 줄바꿈으로 끝나지 않았으면 줄바꿈 하나를 더 쓴다 |
| DP-FR-07 | `-json`: Observer 이벤트마다 stdout 에 JSON 한 줄 (3.1). 기본 출력(DP-FR-06)은 하지 않는다. stderr 에는 DP-FR-04·05·08 만 |
| DP-FR-08 | 종료: `Wait()` 의 Settle 로 — `completed`·`stopped` 0, `failed` 1, `guard` 2, `aborted` 130. `failed`·`guard` 이면 `dr-print: <reason>: <Err>` 를 stderr 에 |
| DP-FR-09 | SIGINT·SIGTERM 을 받으면 run 의 ctx 를 취소한다 (run 은 aborted 로 settle → 130). `run` 은 신호 대신 전달받은 ctx 의 취소로 같은 동작을 한다 |

### 3.1 `-json` 이벤트 형식

`core.ObserverEvent` 하나 = 한 줄. 빈 값·nil 필드는 뺀다 (core 는 `stream_event` 에 step·seq·unit 을 싣지 않으므로 그 줄에는 없다).

```json
{"kind":"stream_event","runId":"r1","stream":{"kind":"text_delta","index":0,"delta":"hi"}}
{"kind":"step_ended","runId":"r1","step":"tool.execute","seq":7,"unit":"tool","durationMs":12,
 "signals":[{"kind":"next"}],"err":"..."}
{"kind":"run_settled","runId":"r1","settle":{"reason":"completed"}}
```

| 필드 | 값 |
|---|---|
| `kind`, `runId`, `seq`, `unit` | ObserverEvent 의 같은 이름 필드 |
| `step` | `Step.String()`. 0 이 아닌 경우만 |
| `durationMs` | `Duration` 의 밀리초, 0 이면 뺌 |
| `err` | `Err.Error()` |
| `signals` | `[{kind, reason?, err?}]` — Edit·Message·Call·Results 본문은 싣지 않는다 |
| `stream` | `contract.StreamEvent` 의 JSON 그대로 |
| `settle` | `{reason, err?}` |

---

## 4. 검증

`run` 을 직접 호출한다. 에이전트는 실제 `agent.New` + httptest Chat Completions 호환 서버(설정 파일 `providers`, 키 불필요) + `HOME`·cwd 임시 디렉터리 + `NoRemoteCatalog` 에 해당하는 설정(4.1).

| 요구사항 | 방법 |
|---|---|
| DP-FR-01~02 | 모르는 플래그 → 1, 인자 프롬프트, stdin 프롬프트, 빈 프롬프트 → 1 |
| DP-FR-04 | 키가 필요한 provider + 키 없음 → 1, stderr 에 `missing API key for "<provider>"` |
| DP-FR-05~06 | 서버가 bash 도구 호출 후 텍스트 → stdout = 텍스트(+줄바꿈), stderr 에 `session:`·`[tool] bash` |
| DP-FR-07 | `-json` → stdout 모든 줄이 JSON, 첫 줄 `run_started`, 끝 줄 `run_settled` |
| DP-FR-08 | completed → 0, 서버 400(재시도 안 되는 에러) → 1 |
| DP-FR-09 | 서버가 스트림을 멈춘 상태에서 ctx 취소 → 130 |
| `-session` | 같은 id 로 두 번 → 두 번째 요청에 첫 대화가 들어 있음 |

### 4.1 테스트용 원격 카탈로그 차단
`run` 은 `agent.Options.NoRemoteCatalog` 를 노출하는 플래그를 두지 않는다. 테스트는 `run` 의 내부 변형 `runWith(ctx, args, stdin, stdout, stderr, agentOptions func(*agent.Options))` 로 `NoRemoteCatalog=true` 를 넣는다 (`run` 은 nil 로 호출).

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| D16, R17 | 1.1, 전체 |
| 옛 ST-FR-28 | DP-FR-01~05 |
| 옛 ST-FR-29 | DP-FR-06~07 |
| 옛 ST-FR-30 | DP-FR-08~09 |
| 옛 ST-FR-31 | DP-FR-04 |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | 키 없음 문구에 환경변수 이름 | provider 이름 (`Start` 에러) | R21 |
| 0.1 | 옛 sets 3.8 (`Bind` 직접 사용) | `L2_harness/agent` 만 사용, 입출력만 | R17 |
