# sets — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (D6, D9, D11, D15, D16, R2, R3, R7, R9)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`, `L1_inject/adapters/SPEC.md`

---

## 1. 소개

### 1.1 목적
22개 step 의 **기본 동작을 함수 세트로** 제공해, core + inject + adapters + sets 만으로 **실행 가능한 코딩 에이전트**가 되게 한다 (D15). core·inject 는 기본값을 갖지 않으므로(D6), 기본값은 전부 여기에 있다.

### 1.2 범위
- 포함:
  - `base` 세트: 22 step 전부
  - `resilient` 세트: 에러 재시도, overflow 복구, 임계치 압축, follow-up
  - 도구 4개: read / write / edit / bash
  - JSONL 세션 저장소, StateStore, 토큰 추정, 압축 요약, 설정 스키마
  - 검증용 print CLI (D16)
- 제외 (다음 단계, `quality`·`assistant` 세트):
  - 병렬·선행 실행 스케줄러, 서브에이전트, soft tool requirement
  - 메모리, 백그라운드 리뷰, fallback provider, 권한 승인

### 1.3 참조 (수치의 출처)

| 값 | 출처 |
|---|---|
| 도구 결과 2000줄 / 50KB | pi `core/tools/` (02 문서 10장) |
| reserve = max(16384, 15%·W) | omp `effectiveReserveTokens` (04 문서 2.3) |
| keepRecent 20,000 토큰 | pi·omp 공통 기본값 (02 문서 8.1) |
| 요약 출력 = min(0.8·reserve, 모델 최대 출력, 16384) | pi `generateSummaryWithUsage`, omp `MAX_SUMMARY_TOKENS` |
| 추정: ASCII (len+3)/4, 비ASCII 바이트/4 + 한중일 1자=1토큰 | Hermes `estimate_tokens_rough` (04 문서 3.2) |
| 이미지 1200 토큰 | omp `IMAGE_TOKEN_ESTIMATE`, pi 4800 chars |
| overflow 복구 1회 | pi `_checkCompaction` compact-and-retry 1회 |
| 무효 압축 2회 연속 → 자동 압축 중단 | Hermes anti-thrash |
| 압축 이전 usage 무시 | pi `estimateProjectedContextTokens`, omp `assistantPredatesCompaction` |
| 요약 형식 (Goal / Constraints / Progress / Decisions / Next / Critical) | pi `SUMMARIZATION_PROMPT` (02 문서 8.3) |
| 재시도 3회, 2s·4s·8s, 상한 60s | **근거 없음 — 기본값(설정 가능)** |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 cmd/dr-print  ── core.New(inject.Bind(sets.Base(cfg), sets.Resilient(cfg)), ...)
                        │
 sets.Base      22 step defaults ── tools ── store ── estimate ── prompt
 sets.Resilient overrides: 3 context.maintain, 12 call.error, 13 response.classify, 21 run.beforeSettle
                        │ uses
 adapters       model.call, catalog
```

### 2.2 설정 (RunInput.Config)

```json
{
  "model":        { "provider": "anthropic", "id": "claude-sonnet-4-5" },
  "cwd":          "/path/to/project",
  "sessionDir":   "~/.dynamic_react/sessions",
  "systemPrompt": "",
  "catalog":      "",
  "maxOutputTokens": 0,
  "tools":        ["read", "write", "edit", "bash"],
  "compaction":   { "enabled": true, "reserveTokens": 0, "keepRecentTokens": 20000 },
  "retry":        { "maxAttempts": 3, "baseDelayMs": 2000, "maxDelayMs": 60000 }
}
```

| id | 요구사항 |
|---|---|
| ST-FR-01 | 설정은 위 스키마의 JSON 이다. 빈 값·0 은 "기본값 사용" 이다. 모르는 키는 에러 |
| ST-FR-02 | 설정에 API 키 값을 넣는 필드는 없다 (adapters AD-C2) |

### 2.3 제약
- ST-C1. API 키는 환경변수로만 받는다. 로그·세션 파일·트레이스에 기록하지 않는다.
- ST-C2. base 세트에는 권한 승인이 없다 — `bash` 는 제한 없이 실행된다 (pi 와 같음). print CLI 는 시작 시 이를 한 줄로 경고한다.

---

## 3. 상세 요구사항

### 3.1 base 세트 — step 별 기본 동작

| # | step | 기본 동작 | 신호 |
|---|---|---|---|
| 1 | run.start | 세션 JSONL(3.4)을 읽어 History 복원, StateStore 복원. **짝 없는 tool call 복구**: 결과 없는 호출마다 `Status: aborted` 결과 메시지를 History 에 붙이고 세션에도 기록 (D9). 초기 입력(`RunInput.Input`)을 상태에 보관 | next |
| 2 | turn.start | 첫 턴: 보관한 초기 입력. 이후 턴: 입력함에서 **아직 소비하지 않은** 메시지(소비 수는 상태에 기록). 반환한 메시지를 세션에 기록 | 첫 턴에 입력이 없으면 settle, 아니면 next |
| 3 | context.maintain | 아무것도 안 함 | next |
| 4 | context.inject | 빈 목록 | next |
| 5 | context.project | Transcript 에 편집 로그를 적용: `replace_range` → 구간을 요약 메시지 하나로, `hide` → 제외, `truncate_after` → 이후 제외. 그 뒤 Ephemeral 을 끝에 붙임. **아직 세션에 기록되지 않은 편집을 기록** | next |
| 6 | request.build | System = 시스템 프롬프트(3.5) · Tools = 설정의 도구 목록의 spec · Messages = 투영 · MaxOutputTokens = 설정값 또는 ModelInfo.MaxOutputTokens | next |
| 7 | request.finalize | **짝 검증·복구**: 결과 없는 tool call 뒤에 aborted 결과를 끼움. 캐시 힌트: system 끝, 마지막 메시지 | next |
| 8 | model.select | 설정 model → 카탈로그 조회. 없으면 `ContextWindow 0`(모름), capability 없음으로 ModelInfo 구성 | next |
| 9 | model.call | adapters.Call 을 감싸 `CallStream{Events: adapters.Call(...), Live: nil}` 반환 (R9) | next |
| 10 | stream.event | 아무것도 안 함 | next |
| 11 | stream.end | Response = 입력 그대로, Discard = 선행 실행 전부 (base 는 선행 실행을 요청하지 않음) | next |
| 12 | call.error | 재시도 없음 | fail(CallError) |
| 13 | response.classify | 아무것도 안 함 | next |
| 14 | response.commit | 응답을 세션에 기록 (usage 포함) | next |
| 15 | tools.plan | 응답의 호출마다 그룹 하나, `MaxParallel 1` (순차) | next |
| 16 | tool.before | 등록되지 않은 도구 → ShortCircuit(error "unknown tool: X"). 인자가 도구 스키마에 맞지 않으면 ShortCircuit(error, 검증 메시지) | next |
| 17 | tool.execute | 도구 레지스트리로 실행 (3.3) | next |
| 18 | tool.after | 결과 자르기 (3.3 도구별 규칙이 없으면 앞에서 2000줄 / 50KB) | next |
| 19 | tools.commit | 결과들을 세션에 기록 | next |
| 20 | turn.finish | 이번 턴에 도구 호출이 있었으면 continue_turn, 없으면 settle (R2) | continue_turn / settle |
| 21 | run.beforeSettle | 아무것도 안 함 | next |
| 22 | run.settled | 상태 flush | — |

| id | 요구사항 |
|---|---|
| ST-FR-03 | `sets.Base(cfg)` 는 위 표대로 22개 step 을 모두 채운 FunctionSet(`Name: "base"`)을 돌려준다 |
| ST-FR-04 | base 의 각 함수는 exported 로 따로 제공해, 사용자가 조합 함수(inject 3.7)로 감쌀 수 있게 한다 (예: `sets.BaseTurnFinish`) |

### 3.2 resilient 세트 — 덮어쓰는 step

`sets.Resilient(cfg)` 는 `Name: "resilient"` 이고 아래 step 만 채운다. `Bind(Base, Resilient)` 로 겹쳐 쓴다.

**3 context.maintain — 임계치 압축**

```text
W        = ModelInfo.ContextWindow            (0 이면 아무것도 안 함)
reserve  = cfg.reserveTokens > 0 ? cfg.reserveTokens : max(16384, floor(0.15 * W))
used     = estimateContext(projection)        (3.6)
if used > W - reserve and not blocked:
    edit = compact(projection, keepRecent)    (3.7)
    -> [rewrite(edit), next]
```

| id | 요구사항 |
|---|---|
| ST-FR-05 | 위 판정을 매 턴 한다. 도구 라운드마다 턴이 바뀌므로(20 → continue_turn → 2) 모델 호출 전마다 판정된다 |
| ST-FR-06 | 압축 후 다음 응답의 usage 가 여전히 임계치 이상이면 "무효 압축" 1회로 센다. **2회 연속**이면 이 run 에서 자동 압축을 멈추고(blocked) 경고 이벤트를 남긴다. 임계치 아래 응답이 오면 카운터를 0 으로 |

**12 call.error — 재시도와 overflow 복구**

| Class | 동작 |
|---|---|
| rate_limit, overloaded, transient_network | 같은 Class 의 이번 호출 재시도 횟수 < maxAttempts 이면 대기 후 `recall`. 대기 = RetryAfter(있으면, 상한 maxDelay) 또는 `min(baseDelay × 2^n, maxDelay)`. 대기는 ctx 취소를 따른다. 넘으면 `fail` |
| overflow | 이번 턴에 overflow 복구를 안 했으면 `[rewrite(compact), rebuild]` (R3). 했으면 `fail` |
| auth, invalid_request, payload_too_large, permanent, unknown | `fail` |

**13 response.classify — length 와 조용한 overflow**

| 조건 | 동작 |
|---|---|
| StopReason = length, 출력 < 요청한 MaxOutputTokens, 이번 턴 복구 미실시 | `[rewrite(compact), rebuild]` (pi 의 recoverable length) |
| StopReason = stop 인데 `usage.Input + usage.CacheRead > W` (W > 0) | overflow 로 보고 위와 같음 (pi 의 silent overflow) |
| 그 외 | next |

**21 run.beforeSettle — follow-up**

| 조건 | 동작 |
|---|---|
| 입력함에 소비하지 않은 메시지가 있음 | continue_turn |
| 없음 | next |

| id | 요구사항 |
|---|---|
| ST-FR-07 | resilient 는 위 네 step 만 채운다. 재시도·복구 횟수는 `ctx.State()` 에 사유별로 기록한다 (D11) |
| ST-FR-08 | 모든 대기·요약 호출은 ctx 취소를 따른다 |

### 3.3 도구

| 도구 | 인자 | 동작 |
|---|---|---|
| `read` | `path`, `offset?`(1부터), `limit?` | 텍스트: 2000줄 또는 50KB 중 먼저 닿는 곳에서 자르고 "offset 으로 이어 읽어라" 안내. png/jpg/gif/webp 는 image 블록 |
| `write` | `path`, `content` | 생성·덮어쓰기, 상위 디렉터리 생성 |
| `edit` | `path`, `edits:[{oldText, newText}]` | pi 알고리즘: LF 정규화 → 정확 매칭, 없으면 fuzzy(NFKC, 줄 끝 공백, 스마트 따옴표·유니코드 대시·특수 공백 정규화) → 없음/중복/겹침/무변화는 에러 → 모든 편집은 원본 기준 적용 → 원래 줄바꿈 복원. 결과에 unified diff |
| `bash` | `command`, `timeout?`(초) | `$SHELL -c`(없으면 `/bin/sh`), 프로세스 그룹으로 실행. stdout+stderr 합쳐 **마지막** 2000줄 / 50KB 반환, 잘리면 전체를 임시 파일에 쓰고 경로 안내. 타임아웃·취소 시 프로세스 그룹 kill. 종료 코드 표시 |

| id | 요구사항 |
|---|---|
| ST-FR-09 | 경로는 `cwd` 기준 상대 경로를 허용하고, 결과에는 cwd 기준 경로를 쓴다 |
| ST-FR-10 | 도구는 `ToolRegistry`(이름 → spec + 실행 함수)로 등록한다. 사용자가 도구를 추가·교체할 수 있다 |
| ST-FR-11 | 도구 실행 에러는 `Status: error` 결과로 돌려준다 (Go error 로 루프를 멈추지 않는다) |

### 3.4 세션 저장소 (JSONL)

| id | 요구사항 |
|---|---|
| ST-FR-12 | 세션 파일은 `<sessionDir>/<sessionID>.jsonl`, append-only |
| ST-FR-13 | 줄 종류: `{"type":"message", "message": Message}`, `{"type":"edit", "edit": ContextEdit}`, `{"type":"state", "ns":..., "delta":...}`, `{"type":"usage", "usage": Usage, "model": ModelRef}` |
| ST-FR-14 | 쓰기는 줄 단위 원자적(한 번의 write + fsync 옵션). 마지막 줄이 깨져 있으면 읽을 때 그 줄만 버리고 경고 |
| ST-FR-15 | `StateStore`(inject IN-FR-26) 구현은 같은 파일의 `state` 줄을 쓰고 읽을 때 순서대로 재적용한다 |

### 3.5 시스템 프롬프트

| id | 요구사항 |
|---|---|
| ST-FR-16 | 기본 프롬프트는 바이너리에 포함된 텍스트다. 설정 `systemPrompt`(파일 경로)가 있으면 그것으로 대체한다 |
| ST-FR-17 | 컨텍스트 파일: cwd 부터 상위로 git 루트(없으면 파일시스템 루트)까지 `AGENTS.md`, `CLAUDE.md` 를 모아, 루트 쪽부터 순서대로 붙인다 |
| ST-FR-18 | 스킬: `~/.agents/skills`, `<cwd>/.agents/skills`, `~/.claude/skills`, `<cwd>/.claude/skills` 의 `*/SKILL.md` frontmatter(`name`, `description`)를 모아 이름·설명·경로 목록만 넣는다. 본문은 모델이 `read` 로 읽는다. 같은 이름은 먼저 찾은 것이 이긴다 |
| ST-FR-19 | 시스템 프롬프트는 run 시작 시 한 번 조립하고 run 동안 바꾸지 않는다 (프롬프트 캐시 보호, 06 문서 4.4) |

### 3.6 토큰 추정

| id | 요구사항 |
|---|---|
| ST-FR-20 | `estimateText(s)`: ASCII 면 `(len+3)/4`, 아니면 한중일·한글·가나 글자 수 + 나머지의 `(UTF-8 바이트+3)/4` |
| ST-FR-21 | 메시지: text·thinking·도구 이름+인자 JSON·도구 결과 텍스트를 합산, 이미지 1개 1200, ProviderData·opaque payload 는 0 (provider usage 로만 가격이 매겨짐) |
| ST-FR-22 | `estimateContext(projection)`: 투영 안에서 **마지막으로 성공한 응답의 usage** 를 찾고, 그 응답이 **가장 최근 편집보다 뒤**이면 `usage.Total()` + 그 뒤 메시지 추정. 아니면(압축 이전 usage) 전부 추정 + 시스템 프롬프트·도구 스키마 추정 |

### 3.7 압축 (compact)

| id | 요구사항 |
|---|---|
| ST-FR-23 | 투영의 끝에서 거꾸로 추정 토큰을 쌓아 `keepRecentTokens` 이상이 되는 지점을 찾고, 그 앞쪽의 **유효 절단점**(user 메시지 또는 assistant 메시지. tool_result 는 절단점이 될 수 없음 — 호출과 결과를 떼지 않기 위함)에서 자른다 |
| ST-FR-24 | 절단점 앞 구간을 같은 모델로 요약한다 (adapters.Call 직접 사용). 요약 프롬프트는 pi 형식: `## Goal`, `## Constraints & Preferences`, `## Progress`(Done / In Progress / Blocked), `## Key Decisions`, `## Next Steps`, `## Critical Context`. 이전 요약이 구간에 있으면 "갱신" 프롬프트로 합친다 |
| ST-FR-25 | 요약 출력 한도 = `min(floor(0.8 × reserve), ModelInfo.MaxOutputTokens(>0 일 때), 16384)` |
| ST-FR-26 | 결과는 `ContextEdit{replace_range, from: 구간 첫 메시지 ID, to: 구간 끝 메시지 ID, messages: [요약을 담은 user 메시지]}` 하나다 |
| ST-FR-27 | 요약 호출이 실패하면 압축을 포기하고 편집 없이 진행한다 (3 에서는 next, 12·13 에서는 fail). 무효 압축 카운트(ST-FR-06)에 포함하지 않는다 |

### 3.8 print CLI (`L1_inject/cmd/dr-print`, D16)

```text
dr-print [-model provider/id] [-session id] [-cwd dir] [-config file] [-json] "prompt"
echo "prompt" | dr-print ...
```

| id | 요구사항 |
|---|---|
| ST-FR-28 | `Bind(Base, Resilient)` 로 에이전트를 만들고 프롬프트 하나를 run 으로 실행한다. `-session` 이 있으면 이어서, 없으면 새 세션 id 를 만들어 stderr 에 출력 |
| ST-FR-29 | 기본 출력: 응답 텍스트 delta 를 stdout 에 흘리고, 도구 호출·결과 요약·경고·에러는 stderr. `-json` 이면 Observer 이벤트를 JSON Lines 로 stdout |
| ST-FR-30 | 종료 코드: completed·stopped 0, failed 1, guard 2, aborted 130. Ctrl-C 는 run 취소 |
| ST-FR-31 | API 키가 없으면 어떤 환경변수가 필요한지 알려 주고 종료 코드 1 (키 값은 출력하지 않음) |

### 3.9 비기능

| id | 요구사항 |
|---|---|
| ST-NFR-01 | 모든 step 함수는 가짜 View(inject 테스트 도구)로 단위 테스트한다. 파일 I/O 는 임시 디렉터리 |
| ST-NFR-02 | 종단 테스트: 가짜 ModelCaller(스크립트된 스트림) + 실제 base/resilient + 임시 세션으로 "도구 2라운드 후 종료", "overflow → 압축 → 재호출", "429 → 대기 → 재시도", "세션 재개 시 짝 복구" 를 검증 |
| ST-NFR-03 | 실제 provider 종단 테스트는 키가 있을 때만 (adapters AD-NFR-02 와 같음) |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| ST-FR-01, 02 | 설정 파싱: 기본값, 모르는 키 에러, 키 필드 부재 |
| ST-FR-03, 04 | 22 step 모두 non-nil, exported 함수 목록 |
| 3.1 각 행 | step 별 단위 테스트 (정상 / 경계 / 실패) |
| ST-FR-05, 06 | 임계치 경계값(W − reserve ± 1), W=0, 무효 압축 2회 연속 → blocked |
| 3.2 call.error 표 | Class 별 분기, RetryAfter 상한, 재시도 소진, overflow 1회 제한, 취소 중 대기 |
| 3.2 classify 표 | length 회복 조건, silent overflow |
| ST-FR-09~11 | 도구별: read 자르기·이미지, write 디렉터리 생성, edit(정확·fuzzy·중복·겹침·무변화), bash(꼬리 자르기·임시 파일·타임아웃 kill·취소) |
| ST-FR-12~15 | JSONL 기록·재생, 깨진 마지막 줄, state 재적용 |
| ST-FR-16~19 | 프롬프트 대체, AGENTS.md 수집 순서, 스킬 목록·중복, run 중 불변 |
| ST-FR-20~22 | 추정: ASCII / 한글 / 이모지 / 이미지 / 압축 전후 usage 신뢰 |
| ST-FR-23~27 | 절단점이 tool_result 에 걸리지 않음, 요약 한도, 실패 시 포기 |
| ST-FR-28~31 | CLI: 종료 코드, 출력 채널, 키 없음, Ctrl-C |
| ST-NFR-02 | 종단 시나리오 4개 |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| D6 | 3.1 전체 (기본값의 위치) |
| D9 | 3.1 의 1·7 행 |
| D11 | ST-FR-07, 15 |
| D15 | 1.2 범위 |
| D16 | 3.8 |
| R2 | 3.1 의 20 행 |
| R3 | 3.2 call.error overflow, classify |
| R7 | 3.1 의 5 행 (Transcript 사용) |
| 자격증명 보호 | ST-C1, ST-FR-02, 31 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | 기본 시스템 프롬프트 문안 (구현 시 초안 작성 후 사용자 확인) |
| O2 | 재시도 기본값(3회, 2s 기준, 60s 상한)의 근거 확보 또는 사용자 확정 |
| O3 | 기본 `sessionDir` 경로 |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | 9 model.call = adapters 가 제공한 `ModelCallFn` 을 그대로 바인딩 | sets 가 `adapters.Call` 의 이벤트 시퀀스를 `core.CallStream` 으로 감쌈 | R9: adapters 가 contract 만 의존 |
