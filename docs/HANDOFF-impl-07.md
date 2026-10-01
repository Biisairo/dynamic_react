# 인계: impl-07 — impl-06(R22 주입 통로) 결과와 남은 일

## 착수 블록 (다음 세션의 지시)

`dynamic_react` 에서 진행한다. 정해진 다음 작업은 없다. 사용자에게 아래 "남은 미결" 중 무엇을 할지 묻고 시작한다 (한 번에 하나씩, 권장안과 함께).

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋은 이 문서 커밋).
2. 기준 문서: `docs/DECISIONS.md` (D1~D16, R1~R22), `docs/INJECT.md` (주입 가이드), 손댈 모듈의 `SPEC.md`.
3. 규약은 아래 "변하지 않는 규약" 그대로.

**하지 말 것**: 구독 계정 로그인(Claude Pro/Max·ChatGPT 플랜 OAuth) 구현 — 사용자가 "넘어가자" 로 정리했다 (다시 꺼내지 말 것). 환경변수 읽기 추가 (R21). L3 가 설정 파일을 직접 읽거나 쓰기 (R17).

## impl-06 에서 한 일

배경: impl-05 끝에서 사용자가 "초기의 목적은 달성한건가? 기본 루프 안에서 원하는대로 외부 주입을 통해 컨트롤 하는 에이전트라는?" 이라고 물었다. L0·L1 은 목적(D1~D6)을 달성했지만, 실제로 쓰는 완성된 에이전트(`agent.New`)에는 외부 함수를 끼울 통로가 없었다 (harness `Options.Extra`·`Tools` 는 있었으나 agent 가 넘기지 않았고 쓰는 곳도 테스트도 없었다).

### 인터뷰로 정한 것 (R22)

| # | 질문 | 사용자 답 |
|---|---|---|
| 1 | 주입 범위 | "core 를 제외한 부분은 완벽히 튜닝 가능하도록 만드는게 목적" → 세트 목록 + 부품(도구·프롬프트·요약기·저장소·모델 호출) 전부. 실행 중 교체는 뒤로 미룸 |
| 2 | 부품을 넘기는 형태 | 함수 방식 (기본값을 받아 바꾼 값을 돌려줌, nil = 기본) |
| 3 | 세션 저장소도 바꿀지 | 바꾼다. `harness.Store` = `sets.Store` + `List`, agent 의 `History`·`Sessions` 는 harness 저장소를 거친다 |
| 4 | 사용자 세트가 `tool.before` 를 덮을 때 | 승인·이벤트는 항상 유지, base 기본 검사 생략은 사용자 책임 (스펙 명시 + 테스트) |
| 5 | 추가 도구와 상한·승인 규칙 | `ToolSet` 적용 뒤 registry 기준. 패턴 규칙은 bash·read·write·edit 만, 같은 이름으로 바꾼 도구에 인자가 없으면 패턴 규칙이 맞지 않음 (스펙 명시) |
| 6 | 예시 위치 | README 주입 절 + 루트 `examples/inject` 모듈 |
| — | 커밋 | 스펙 확인 후 커밋 → 구현 확인 후 커밋 |

### 커밋

| 커밋 | 내용 |
|---|---|
| `d5f2a15` | R22 스펙: DECISIONS R22, harness SPEC v1.0 (3.6c, HN-FR-37~42), agent SPEC v0.6 (3.14, AG-FR-40~43, 절 번호 3.13 중복 정정), examples/inject SPEC v0.1 |
| `c152140` | R22 구현: harness `ToolSet`·`Sets`·`System`·`Summarizer`·`Store`·`DefaultSets`·`Store()`·`session.Store.List` (`Tools`·`Extra` 삭제), `Compact` 의 ID 검사·요약기 nil 에러, agent 의 같은 필드 + `ModelCaller`, `History`·`Sessions` 저장소 경유, `examples/inject` 모듈(go.work 포함), README 주입 절 |
| (이 문서 커밋) | `docs/INJECT.md` 주입 가이드, `docs/VERIFY-22step.md` R22 절, README 문서 링크, 이 인계 문서 |

### 동작 변경 (이전 / 새 / 이유)

| 위치 | 이전 | 새 | 이유 |
|---|---|---|---|
| `harness.Options` | `Tools []sets.Tool`, `Extra []inject.FunctionSet` | `ToolSet`·`Sets`·`System`·`Summarizer`·`Store` 함수 | R22. 옛 필드는 쓰는 곳이 없었고 새 필드가 포함 |
| `harness.Agent.Compact` | ID 검사를 저장소에 맡김, 요약기는 항상 있음 | 직접 `CheckID`, 요약기 nil 이면 `harness: compaction disabled: no summarizer` | 사용자 저장소는 ID 를 검사하지 않을 수 있음, `Summarizer` 가 nil 을 돌려주면 압축 끔 |
| HN-FR-30 Gate | "base 검사를 통과한 호출"에 불림 | 실제로 쓰이는 `tool.before` 가 ShortCircuit 없이 돌려준 호출에 불림 (기본 세트면 같음) | 사용자 세트가 `tool.before` 를 덮을 때의 의미 |
| `agent.History`·`Sessions` | `session.New` 로 sessionDir 파일을 직접 읽음 | `Harness().Store()` 의 `Load`·`List` | 저장소를 바꿨을 때 목록·이력이 어긋나지 않게. 기본 저장소면 결과 같음 |

구현 중 정해 스펙에 반영한 세부: `session.List` 순서 = `ListDir` 순서(파일 이름순), 예시의 `runWith`(테스트가 `NoRemoteCatalog` 를 넣음, dr-print 와 같음), 예시 규칙 메시지 ID 고정 `example-rule` (임시 메시지라 고유할 필요 없음).

### 검증 (`c152140`)

- 모듈 12개(기존 11 + `examples/inject`) 전부 `gofmt -l`·`go vet`·`go test -race -count=3`·`GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test` 통과. 테스트 아닌 코드에 환경변수 읽기 없음.
- RED: 새 API 가 없어 컴파일 실패하는 것을 확인한 뒤 구현.
- 커버리지: harness 패키지 97.7% (이전 97.1%), agent 94.3% (이전 93.6%), examples/inject 84.7% (`main`·`run` 은 0%, 테스트는 `runWith`). 나머지 모듈은 변경 없음.
- 뮤테이션 35개 (harness 10, compact 2, session 5, agent 8, examples 10): 31개 KILLED, 4개 SURVIVED → 2개는 테스트 보강 후 재실행으로 KILLED (`Sessions` 의 `List` 에러 전달, bash 가 아닌 도구의 `rm -rf` 인자), 2개는 동치라서 코드를 지움 (`session.List` 의 중복 `sort.Strings` — ListDir 가 이미 이름순, 예시 규칙 메시지의 ID 생성 — 고정 ID 로). 커버리지에서 드러난 예시의 쓰이지 않는 `now` 폴백도 지웠다.
- **deepseek 실제 확인** (`deepseek/deepseek-flash`, 임시 HOME `/tmp/e2e/h-*` 에 키 항목만 0600 사본, 끝난 뒤 삭제):
  - 세 run 모두 답의 마지막 줄이 `-- checked by example` (context.inject 효과)
  - `rm -rf ./junk` 요청 → `[tool] bash error`, 모델이 받은 결과 `blocked by example: rm -rf`, 디렉터리 그대로 (tool.before 효과)
  - `echo hi-from-bash` → `[tool] bash ok`, 출력 정상 (base 경로)
  - 세션 파일 3개 어디에도 규칙 메시지가 저장되지 않음

## 현재 구조 (주입 관련)

| 위치 | 내용 |
|---|---|
| `L2_harness/harness/harness.go` `Options`·`New` | HN-FR-38 순서: ToolSet → Store → System → Summarizer → Sets → surface 래핑 → BindWith |
| `harness.go` `Store`·`DefaultSets`·`Agent.Store()` | HN-FR-39·40·42 |
| `harness/session/session.go` `List` | HN-FR-37 |
| `harness/surface.go` `wrap` | step 마다 실제로 쓰이는 함수를 그 세트 안에서 감쌈 (변경 없음, HN-FR-41 이 의미를 정함) |
| `L2_harness/agent/agent.go` `Options`·`New` | 주입 필드 6개, `ModelCaller` 적용 후 harness 로 |
| `L2_harness/agent/history.go`·`surface.go` `Sessions` | harness 저장소 경유 |
| `examples/inject/main.go` | `Sets`·`Example`·`runWith` |
| `docs/INJECT.md` | 사용 가이드 (필드 표, 만드는 순서, 세트 다루기, 지켜지는 것과 사용자 책임) |

## 이 세션이 값을 치르고 배운 것

- macOS 에 `timeout` 명령이 없다 — 실제 모델 시험에서 그냥 실행하고 Bash 도구의 timeout 을 쓴다.
- `/tmp/mut.py` 는 이번에도 있었다 (다음에도 있다고 가정하지 말 것). 형식은 impl-06 인계 문서와 같다: `python3 /tmp/mut.py <모듈 디렉터리> <파일> <뮤턴트 JSON 파일>`. 쉘 프로세스 치환(`<(...)`)으로 넘기는 명령은 자동 모드 분류기가 판정하지 못한 적이 있어 heredoc 로 파일을 쓰는 편이 안전하다.
- `sets.Tool` 의 필드는 `Spec`·`Run(ctx, call) ToolResult`·`Truncate` 다 (`Execute` 아님). 결과에 `CallID` 를 채울 것.
- harness `Deps.Now` 는 항상 채워진다(`Options.Now` 기본 `time.Now`), `Deps.NewID` 는 agent 경로에서 nil 일 수 있다.
- envtest `ListDir` 는 이름순이라 정렬 누락을 잡지 못한다 — 정렬이 필요하면 Env 계약(`ListDir` 은 이름순)을 근거로 삼는다.
- 테스트 헬퍼 이름 충돌 주의 (agent 패키지): 기존 `newAgent`·`setup`·`writeConfig`·`writeAuth`·`mkAgent` 에 이번에 `memStore`·`failList`·`countCaller`·`countSummarizer`·`extraTool`·`requestTools` 가 더해졌다. harness 패키지에는 `memStore`·`listStore`·`countingSummarizer`·`objTool`·`ruleSet`·`textModel` 이 더해졌다.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 동작 변경 시 이전 / 새 동작 / 이유를 변경 기록에. 스펙에 없는 Go 표현(새 export 포함)은 구현 전에 스펙에 먼저. 구현 중 정한 세부는 스펙에 반영하고 보고 때 "검토 부탁".
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`. 외부 의존: x/text(harness·terminal), x/term(terminal). 새 외부 의존은 사용자 결정.
- 키를 코드·대화·문서·이미지에 노출하지 않음. 키는 `<home>/.dynamic_react/auth.json`(0600)에만 (R21). 구독 요금제 엔드포인트·구독 OAuth 경로 금지 (AD-C1). 환경변수를 읽지 않음 (R21, 예외: 홈 디렉터리·자식 프로세스 상속·테스트 전용 `DR_E2E_*`).
- 라이선스: 서드파티 문안·데이터를 새로 가져오면 `THIRD_PARTY_NOTICES.md` 에 더한다.
- 사용자는 레이어 책임을 엄격히 본다(목록·검증·저장은 L2). 결정은 한 번에 하나씩, 권장안과 설명을 먼저. 질문이 추상적이면 "지금 상태 / 목표 / 안" 표로 다시 설명한다 (impl-06 에서 첫 질문이 이해되지 않는다는 답을 받음).

## 사용자 결정 중 유효한 것

- D1~D16, R1~R22 (R10 폐기, R14 는 R17 로 정정).
- R22: core 외 전부 생성 시점 Go 함수로 튜닝, 부품은 함수 방식, 저장소 교체 포함, 승인·이벤트 항상 유지·기본 검사는 사용자 책임, 패턴 규칙은 bash·파일 도구만, 예시 = README + `examples/inject`. 비목표: 설정 파일로 세트 고르기, 실행 중 교체.
- 이 저장소 자체의 LICENSE 는 미정 (사용자 결정 대기 — 묻지 않았으면 손대지 말 것).

## 남은 미결

| 위치 | 내용 |
|---|---|
| R22 비목표 | 실행 중(run 도중·run 사이) 세트·부품 교체 — 사용자: "런타임 경로 수정은 뒤에 진행" |
| agent | 사용자 도구에 패턴 규칙 비교 값을 꺼내는 함수 (`Target func(args) string`) — 필요해지면 |
| L3 | 이미 빌드된 `dr`·`dr-print` 에 주입하는 방법 없음 (D12 상 의도된 것). 대화형으로 쓰려면 자기 main 에서 `dr` 처럼 `agent.Options{…, Approver: t.Approver(), OnEvent: t.OnEvent}` 에 주입 필드를 더해 `agent.New` 하고 `terminal` 라이브러리에 넘긴다 (`L3_ui/cmd/dr/main.go:76`). 이 조합의 예시는 없다 |
| 저장소 | 자체 LICENSE 미정 |
| adapters | Anthropic·OpenAI 방언 실제 응답 검증 (API 키·크레딧 필요) |
| sets/harness | provider overflow 에러 복구의 실제 모델 확인 (가짜 서버로만 검증) |
| 뮤테이션 | impl-05 에서 관측 불가로 남은 2개 (agent `NoRemoteCatalog` 전달, terminal 종료 중 경합) |
