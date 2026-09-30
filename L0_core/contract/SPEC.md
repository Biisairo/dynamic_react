# contract — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md`
- 관련 명세: `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`

---

## 1. 소개

### 1.1 목적
core, inject, 그리고 이후 레이어(sets, adapters, surfaces)가 **공유하는 데이터 타입과 그 의미**를 정의한다. 이 계약이 고정돼 있어야 모델 API 가 바뀌어도 core 를 고치지 않는다 (07 문서 2.8).

### 1.2 범위
- 포함: 메시지·콘텐츠 블록, 도구 정의·호출·결과, 중립 요청, 스트림 이벤트, 정규화된 usage, stop reason, 에러 분류, 모델 capability, 컨텍스트 편집, 계약 버전.
- 제외: 방언 변환(adapters), 토큰 추정(sets), 저장 형식(sets).

### 1.3 용어

| 용어 | 뜻 |
|---|---|
| 방언 | provider API 형식 (Anthropic Messages, OpenAI Chat Completions, OpenAI Responses, Gemini, Bedrock 등) |
| 중립 형식 | 방언과 무관한 이 계약의 타입 |
| opaque | contract 가 해석하지 않고 운반만 하는 값 |
| 투영(projection) | 세션 기록에 편집(ContextEdit)을 적용해 모델이 보게 되는 메시지 목록 |

### 1.4 참조
- `hermes_study/analysis/05-provider-wire-formats.md` — 방언 5종의 형태와 정규화 규칙
- `hermes_study/analysis/04-context-window-and-usage.md` — usage 의미
- `hermes_study/analysis/07-injectable-loop-design.md` 2.8 — 계약 C1~C6

---

## 2. 전체 설명

### 2.1 제품 관점
contract 는 **데이터와 그 불변 규칙만** 가진다. 동작(함수, I/O)이 없다. 모든 레이어가 import 한다. contract 는 다른 레이어를 import 하지 않는다.

```text
surfaces   sets   adapters
    \       |       /
     \      |      /
       inject ─── core
           \     /
           contract            (depends on nothing but stdlib)
```

### 2.2 제약
- CT-C1. Go 표준 라이브러리 외 의존성 없음.
- CT-C2. 모든 타입은 JSON 직렬화가 가능하고, 같은 값은 항상 같은 바이트로 직렬화된다 (캐시·지문용).
- CT-C3. 모든 타입은 protobuf 로 1:1 대응 가능해야 한다 (D12, 별도 프로세스 경계).

---

## 3. 상세 요구사항

### 3.1 메시지와 콘텐츠 (C1)

| id | 요구사항 |
|---|---|
| CT-FR-01 | `Message` 는 `Role`(system / user / assistant / tool_result), `Content []Block`, `ID`, `Timestamp` 를 가져야 한다. `Role == tool_result` 이면 `Result *ToolResult`(3.2) 가 반드시 있고 `Content` 는 비운다. 다른 역할이면 `Result` 는 nil 이다 |
| CT-FR-02 | `Block` 은 닫힌 종류 `text` / `image` / `thinking` / `tool_call` / `opaque` 중 하나여야 한다 |
| CT-FR-03 | `thinking` 블록은 텍스트와 선택적 `ProviderData` 를 가진다. 서명·암호화 reasoning 은 `ProviderData` 에 담는다 |
| CT-FR-04 | `ProviderData` 는 `{Provider, Model, Payload []byte}` 이다. contract 는 Payload 를 해석하지 않는다. **같은 Provider·Model 로 가는 요청에만 재전송되어야 한다**는 규칙을 타입 문서에 명시한다 |
| CT-FR-05 | `opaque` 블록은 `{Provider, Kind string, Payload []byte}` 이다. adapter 가 모르는 블록을 손실 없이 보관·재생하는 용도다 |
| CT-FR-06 | 모든 Block 은 선택적 `ProviderData` 를 가질 수 있다 (Gemini 처럼 서명이 아무 part 에 붙는 경우) |

### 3.2 도구 (C1)

| id | 요구사항 |
|---|---|
| CT-FR-07 | `ToolSpec` 은 `Name`, `Description`, `InputSchema`(JSON Schema, raw JSON) 를 가진다 |
| CT-FR-08 | `ToolCall` 은 `ID`, `Name`, `Args`(raw JSON 객체) 를 가진다. `Args` 는 항상 객체다 (방언의 문자열 인자는 adapter 가 파싱) |
| CT-FR-09 | `ToolCall.ArgsDigest()` 는 Args 를 정규화(키 정렬)한 뒤의 해시를 돌려줘야 한다. 선행 실행 결과 재사용 판단의 기준이다 (core CORE-FR-15) |
| CT-FR-10 | `ToolResult` 는 `CallID`, `Content []Block`(text / image 만), `Status` 를 가진다. `Status` 는 닫힌 종류 `ok` / `error` / `aborted` / `skipped` |

### 3.3 요청 (중립)

| id | 요구사항 |
|---|---|
| CT-FR-11 | `Request` 는 `Model ModelRef`, `System []Block`, `Messages []Message`, `Tools []ToolSpec`, `Options` 를 가진다 |
| CT-FR-12 | `Options` 는 `MaxOutputTokens`, `Thinking`(level 문자열, 선택), `ToolChoice`(auto / none / required / 이름), `CacheHints []CacheHint`(메시지 인덱스 기준 캐시 지점), `Extra map[string]json.RawMessage`(방언 전용 값) 를 가진다 |
| CT-FR-13 | `ModelRef` 는 `Provider`, `ID`, `API`(방언 이름) 를 가진다 |

### 3.4 스트림 이벤트 (C2)

| id | 요구사항 |
|---|---|
| CT-FR-14 | `StreamEvent` 는 닫힌 종류 `start` / `text_delta` / `thinking_delta` / `toolcall_start` / `toolcall_delta` / `toolcall_end` / `usage` / `done` / `error` 중 하나다 |
| CT-FR-15 | 각 이벤트는 블록 인덱스(`Index`)를 가진다. `toolcall_end` 는 완성된 `ToolCall` 을 가진다 |
| CT-FR-16 | 스트림은 정확히 하나의 `done` 또는 `error` 로 끝난다. **adapter 는 panic·예외 대신 `error` 이벤트로 끝내야 한다** (pi 계약, 05 문서) |
| CT-FR-17 | `done` 이벤트는 확정된 assistant `Message`, `StopReason`, `Usage` 를 가진다 |

### 3.5 usage, stop reason, 에러 (C3, C4, C5)

| id | 요구사항 |
|---|---|
| CT-FR-18 | `Usage` 는 `Input`(캐시 **제외** 입력), `Output`(추론 **포함** 출력), `CacheRead`, `CacheWrite`, `Reasoning`(Output 의 일부, 선택), `ContextTokens`(provider 가 직접 보고한 점유량, 선택) 를 가진다 |
| CT-FR-19 | `Usage.Prompt()` = Input + CacheRead + CacheWrite, `Usage.Total()` = Prompt + Output 를 제공한다 |
| CT-FR-20 | `StopReason` 은 닫힌 종류 `stop` / `length` / `tool_use` / `error` / `aborted` 이고, 원문 값을 `Raw string` 으로 보존한다 |
| CT-FR-21 | `CallError` 는 `Class` 와 `Message`, 선택적 `RetryAfter`, `Status`(HTTP), `Raw` 를 가진다. `Class` 는 닫힌 종류 `overflow` / `rate_limit` / `overloaded` / `auth` / `payload_too_large` / `transient_network` / `invalid_request` / `permanent` / `unknown` |

### 3.6 capability (C6)

| id | 요구사항 |
|---|---|
| CT-FR-22 | `Capabilities` 는 불리언 플래그 집합이다: `LiveInput`, `MidConvoSystem`, `ToolChanges`, `ImagesInToolResult`, `NativeCompaction`, `ParallelToolCalls`, `Thinking`, `Vision` |
| CT-FR-23 | `ModelInfo` 는 `Ref ModelRef`, `ContextWindow int`(0 = 모름), `MaxOutputTokens int`, `Capabilities` 를 가진다 |

### 3.7 컨텍스트 편집

| id | 요구사항 |
|---|---|
| CT-FR-24 | `ContextEdit` 은 원본 메시지를 바꾸지 않고 투영만 바꾸는 편집이다. 종류는 닫힌 집합이다: `replace_range{From, To, Messages}`(From~To 구간을 Messages 로 대체: 압축) / `hide{Target}`(특정 메시지 숨김: 실패 응답 숨김) / `truncate_after{Target}`(Target 이후 제외: 되돌리기). 모든 편집은 `ID`, `Timestamp` 를 가진다 |
| CT-FR-25 | 모든 ContextEdit 은 대상 메시지를 `Message.ID` 로 가리킨다 (인덱스 금지: 저장·재개 후에도 유효해야 함) |

### 3.8 버전

| id | 요구사항 |
|---|---|
| CT-FR-26 | 패키지는 `ContractVersion` 상수(정수)를 가진다 |
| CT-FR-27 | 버전 증가 시 **추가만** 허용한다. 필드 삭제·의미 변경·닫힌 종류에서 값 제거는 금지한다 |
| CT-FR-28 | 닫힌 종류(enum)는 모르는 값을 역직렬화하면 에러를 돌려야 한다 (조용히 무시 금지) |

### 3.9 비기능

| id | 요구사항 |
|---|---|
| CT-NFR-01 | 05 문서의 방언 5종 request/response 예시를 **손실 없이** 중립 형식으로 왕복할 수 있어야 한다 (합격 기준, 검증은 adapters 레이어에서) |
| CT-NFR-02 | 공개 타입의 zero value 는 유효하지 않은 값으로 취급하고, `Validate() error` 로 검사할 수 있어야 한다 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| CT-FR-01~13, 18~25 | 단위 테스트: 생성·Validate·JSON 왕복 |
| CT-FR-09 | 속성 테스트: 키 순서만 다른 Args 는 같은 digest, 값이 다르면 다른 digest |
| CT-FR-14~17 | 단위 테스트: 스트림 종료 규칙 검사 헬퍼 (`done`/`error` 정확히 1회) |
| CT-FR-26~28 | 단위 테스트: 모르는 enum 값 역직렬화 시 에러 |
| CT-C2 | 속성 테스트: 같은 값 → 같은 바이트 |
| CT-C3 | protobuf 정의(inject 레이어)와 필드 대응 표 대조 |
| CT-NFR-01 | adapters 레이어 스펙에서 수행 (이번 범위 밖) |

---

## 5. 추적성

| 결정 | 요구사항 |
|---|---|
| D2, D12 | CT-C2, CT-C3 |
| 07 문서 C1 | CT-FR-01~13 |
| C2 | CT-FR-14~17 |
| C3~C5 | CT-FR-18~21 |
| C6 | CT-FR-22, 23 |
| D9, R3 | CT-FR-24, 25 |
| 계약 버전 규칙 | CT-FR-26~28 |
