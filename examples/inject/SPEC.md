# examples/inject — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D12, R22, R26)
- 의존 명세: `L2_harness/agent/SPEC.md` (3.14 주입), `L2_harness/SPEC.md` (3.6c), `L1_inject/sets/SPEC.md` (Base 생성자)
- 사용하는 쪽: 개발자 (주입 사용법 예시)

---

## 1. 소개

### 1.1 목적
완성된 에이전트(`agent.New`)에 **사용자 함수 세트를 끼우는 방법**을 실행 가능한 코드로 보여 준다 (R22 ⑥). 제품 진입점이 아니라 예시다.

### 1.2 범위
- 포함 (모듈 `examples/inject`, 패키지 `main`): 세트 하나(`example`)를 기본 세트 위에 겹친 에이전트, 프롬프트 하나를 실행하는 CLI
- 제외: 대화형 입력, 승인 묻기(Approver 없음 = 허용, AG-FR-18), 이벤트의 상세 표시, 설정·키 파일 해석 (agent 의 몫)

---

## 2. 전체 설명

```text
inject-example [-model provider/id] [-cwd dir] [-config file] prompt ...
```

```text
 main ── run(ctx, args, stdout, stderr) int
          └─ agent.New(ctx, Options{Cwd, ConfigPath, Model, OnEvent, Sets: Sets})
               Sets(d) = append(harness.DefaultSets(d), harness.SetEntry{Set: Example(d), On: true})
```

모듈 의존: examples/inject → agent, harness, sets, inject, core, contract, 표준 라이브러리. go.work 에 포함하고 독립 go.mod(`v0.0.0` + replace, R8)를 갖는다.

### 2.2 제약
- EX-C1. 키 값을 출력하지 않는다 (agent 에러 문구를 그대로 씀, AG-C1).
- EX-C2. 로직은 `run` 에 두고 `main` 은 `os.Exit(run(signalCtx, os.Args[1:], os.Stdout, os.Stderr))` 만 한다.
- EX-C3. `run` 은 `agent.Options.NoRemoteCatalog` 를 노출하는 플래그를 두지 않는다. 테스트는 내부 변형 `runWith(ctx, args, stdout, stderr, mod func(*agent.Options))` 로 넣는다 (`run` 은 nil, dr-print 와 같음).

---

## 3. 상세 요구사항

```go
const Rule = "End every reply with the line: -- checked by example"

func Sets(d sets.Deps) []harness.SetEntry // append(harness.DefaultSets(d), {Example(d), On})
func Example(d sets.Deps) inject.FunctionSet
```

| id | 요구사항 |
|---|---|
| EX-FR-01 | `Example(d)` 는 이름 `example` 인 세트이고 `ContextInject`·`ToolBefore` 두 step 만 채운다 (나머지는 기본 세트가 채움, IN-FR-19) |
| EX-FR-02 | `ContextInject`: 매 턴 user 메시지 하나(text = `Rule`, ID = 고정 `example-rule` — 저장·편집 대상이 아닌 임시 메시지라 고유할 필요가 없다, Timestamp = `d.Now()` — harness 가 항상 채운다, HN-FR-38 ⑤)를 돌려준다. 이번 턴의 요청에만 들어가고 세션에 저장되지 않는다 (core `context.inject`) |
| EX-FR-03 | `ToolBefore`: 호출 이름이 `bash` 이고 인자 `command` 에 `rm -rf` 가 들어 있으면 실행하지 않고 `Status: error`, 내용 `blocked by example: rm -rf` 인 ShortCircuit 결과를 돌려준다. 그 밖의 호출은 `ctx.Next(call)` 로 아래 층(기본 구성에서는 guard → base)에 넘긴다 — 기본 검사(없는 도구·스키마, ST-FR-38)를 유지하는 감싸기 예시다 (HN-FR-41, IN-FR-60) |
| EX-FR-04 | 플래그: `-model`(→ `agent.Options.Model`), `-cwd`(→ `Options.Cwd`), `-config`(→ `Options.ConfigPath`). 플래그 뒤 인자들을 공백 하나로 이은 것이 프롬프트다. 플래그 에러·빈 프롬프트는 stderr 에 사용법 또는 `inject-example: empty prompt`, 종료 코드 1 |
| EX-FR-05 | 출력: `OnEvent` 의 `message_update` 중 `text_delta` 의 Delta 를 stdout 에, `tool_end` 를 stderr 에 `[tool] <name> <status>` 한 줄로 쓴다. run 이 끝나면 stdout 에 쓴 텍스트가 줄바꿈으로 끝나지 않았을 때 줄바꿈 하나를 더 쓴다 |
| EX-FR-06 | 종료: `agent.New`·`Start` 에러는 `inject-example: <에러>` 를 stderr 에, 1. Settle `completed`·`stopped` 0, `aborted` 130, 그 밖은 `inject-example: <reason>: <Err>` 를 stderr 에, 1. ctx 취소(SIGINT·SIGTERM)는 run 을 취소한다 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| EX-FR-01~03 | httptest Chat Completions 호환 서버(설정 `providers` 로 등록, 키 불필요) + 임시 HOME·cwd 로 `run` 종단: 서버가 받은 요청마다 마지막 메시지 중 `Rule` 이 있음·다음 run 의 세션에는 `Rule` 이 없음, 모델이 `rm -rf x` bash 호출 → 다음 요청의 tool 결과가 `blocked by example: rm -rf`·파일 그대로, `echo ok` bash 호출 → 실제 실행 결과, 없는 도구 호출 → `unknown tool` (base 검사 유지) |
| EX-FR-04~06 | 빈 프롬프트·잘못된 플래그 → 1, 텍스트 응답 → stdout·0, 명시한 설정 파일 없음 → 1 |
| 실제 모델 | deepseek 로 수동 확인: 답 끝에 `-- checked by example` 줄, `rm -rf` 를 요청하면 차단 결과 (R22 종단, 저장소 밖 임시 HOME) |

---

## 5. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | `sets.BaseToolBefore(d)` 를 직접 부름, `Sets` 는 `[]inject.FunctionSet` | `ctx.Next(call)`, `[]harness.SetEntry` | R26 |
| 0.1 | 주입 예시 없음 | 이 모듈 | R22 ⑥ |
