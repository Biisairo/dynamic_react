# adapters — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.1 (초안)
- 상위 근거: `docs/DECISIONS.md` (D2, D14, D15)
- 의존 명세: `L0_core/contract/SPEC.md`, `L0_core/core/SPEC.md`, `L1_inject/inject/SPEC.md`
- 사용하는 쪽: `L1_inject/sets/SPEC.md`

---

## 1. 소개

### 1.1 목적
`model.call` step 에 주입되는 **모델 방언 어댑터**와, `model.select` 가 쓰는 **모델 카탈로그**를 정의한다. 어댑터는 contract 의 중립 `Request` 를 provider 방언으로 바꿔 보내고, 응답 스트림을 contract `StreamEvent` 로 되돌린다. API 가 바뀌면 이 레이어만 고친다 (07 문서 2.8).

### 1.2 범위
- 포함 (D15): Anthropic Messages, OpenAI Chat Completions (및 호환 서버: OpenAI, OpenRouter, vLLM, llama.cpp, Ollama 의 OpenAI 호환 엔드포인트), 에러 분류, usage 정규화, stop reason 매핑, 모델 카탈로그, API 키 해석.
- 제외: OpenAI Responses, Gemini, Bedrock (다음 단계). 재시도·fallback 판단 (sets). 토큰 추정 (sets).

### 1.3 참조
- `hermes_study/analysis/05-provider-wire-formats.md` 2장(Anthropic), 3장(Chat Completions), 7장(비교표), 8장(형식 간 이동)
- pi 구현: `pi-mono/packages/ai/src/api/anthropic-messages.ts`, `openai-completions.ts`, `utils/overflow.ts`

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 sets: model.select ──► Catalog.Lookup(ref) -> ModelInfo
 sets: model.call   ──► adapters.Call(ctx, req) -> core.CallStream
                             │ route by req.Model.API
                             ├──► anthropic-messages
                             └──► openai-chat  (+ compat profile per provider)
                                     │
                                     ▼ net/http + SSE
                                 provider endpoint
```

- 어댑터는 inject 의 `ModelCallFn` 시그니처를 따르는 함수를 제공한다. sets 는 그 함수를 그대로 바인딩하거나 감싼다.

### 2.2 제약
- AD-C1. provider 인증은 **API 키만** 지원한다. 구독 계정(Claude Pro/Max, ChatGPT 등)의 로그인·OAuth 토큰을 쓰는 경로를 구현하지 않는다. 키는 코드·로그·에러 메시지·트레이스에 노출하지 않는다.
- AD-C2. API 키는 환경변수 또는 설정의 **환경변수 이름 참조**로만 받는다 (설정 파일에 키 값을 직접 쓰는 형식을 지원하지 않는다).
- AD-C3. HTTP 는 Go 표준 `net/http` 와 자체 SSE 파서로 구현한다. 이유: Chat Completions 호환 서버의 방언 차이(05 문서 3장)를 SDK 없이 직접 흡수해야 하고, core 와 같은 무의존 원칙을 L1 에도 가능한 한 적용한다. (재검토 가능: 6장 O1)
- AD-C4. 어댑터는 panic 하거나 스트림 도중 Go error 로 끝나지 않는다. 모든 실패는 contract `error` 이벤트(`CallError` 포함)로 끝낸다 (CT-FR-16).

---

## 3. 상세 요구사항

### 3.1 공통

| id | 요구사항 |
|---|---|
| AD-FR-01 | `adapters.Call` 은 `Request.Model.API` 로 방언을 고른다 (`anthropic-messages`, `openai-chat`). 모르는 API 면 `error` 이벤트(`Class: invalid_request`) 하나로 끝나는 스트림을 돌려준다 |
| AD-FR-02 | ctx 가 취소되면 HTTP 요청·응답 본문을 닫고 `error` 이벤트(`Class: unknown`, 메시지 "aborted")로 끝낸다 |
| AD-FR-03 | tool call id 는 `[a-zA-Z0-9_-]`, 64자 이하로 정규화한다. 다른 문자는 `_` 로 바꾼다. 정규화 전후 대응을 한 요청 안에서 일관되게 유지한다 |
| AD-FR-04 | `ProviderData` 는 **같은 Provider·Model** 로 가는 요청에만 재전송하고, 아니면 버린다 (CT-FR-04). 서명 없는 thinking 블록은 text 로 바꿔 보낸다 (Anthropic) 또는 버린다 (Chat, 3.3 참조) |
| AD-FR-05 | 빈 text 블록과 내용이 없는 메시지는 보내지 않는다 |
| AD-FR-06 | 어댑터는 `Capabilities.LiveInput = false` 로 선언한다 (두 방언 모두 스트림 중 입력 미지원). core 는 이때 `inject_input` 을 입력함으로 보낸다 (CORE-FR-18) |

### 3.2 Anthropic Messages (`POST {base}/v1/messages`, SSE)

| id | 요구사항 |
|---|---|
| AD-FR-07 | 인증 헤더 `x-api-key`, `anthropic-version`. 기본 base URL `https://api.anthropic.com`, 키 환경변수 기본 `ANTHROPIC_API_KEY` |
| AD-FR-08 | `System` 블록은 최상위 `system` 배열로 보낸다 (messages 에 넣지 않음) |
| AD-FR-09 | 도구 정의 `{name, description, input_schema}`. 도구 호출은 assistant 의 `tool_use{id, name, input(객체)}` 블록 |
| AD-FR-10 | 도구 결과는 **user 메시지 안의 `tool_result{tool_use_id, content, is_error}`** 블록. 연속된 결과는 user 메시지 하나로 묶는다 |
| AD-FR-11 | thinking 블록은 `{type:"thinking", thinking, signature}` 로 되돌려 보낸다 (signature = 같은 모델의 ProviderData). 가려진 추론은 opaque 블록 → `redacted_thinking{data}` |
| AD-FR-12 | `Options.CacheHints` 는 해당 블록에 `cache_control:{type:"ephemeral"}` 로 붙인다 |
| AD-FR-13 | `Options.Thinking` 이 있으면 `thinking` 파라미터를 보내고 `temperature` 는 보내지 않는다 |
| AD-FR-14 | 스트림 이벤트 매핑: `message_start` → `start` + usage(입력 쪽), `content_block_start/delta/stop` → `text_delta` / `thinking_delta` / `toolcall_start` / `toolcall_delta`(input_json_delta) / `toolcall_end`(누적 JSON 파싱), `signature_delta` → thinking 의 ProviderData, `message_delta` → 최종 usage·stop reason, `message_stop` → `done` |
| AD-FR-15 | usage: `input_tokens` → Input(이미 캐시 제외), `cache_read_input_tokens` → CacheRead, `cache_creation_input_tokens` → CacheWrite, `output_tokens` → Output. `message_delta` 에 input 이 없으면 `message_start` 값을 유지 |
| AD-FR-16 | stop reason: `end_turn`·`stop_sequence`·`pause_turn` → stop, `max_tokens` → length, `tool_use` → tool_use, `refusal` → error. 모르는 값 → error (Raw 보존) |

### 3.3 OpenAI Chat Completions (`POST {base}/chat/completions`, SSE)

| id | 요구사항 |
|---|---|
| AD-FR-17 | 인증 헤더 `Authorization: Bearer <key>`. base URL·키 환경변수 이름은 compat 프로필에서 온다 (3.4) |
| AD-FR-18 | system 은 `messages[0]` 이다. 역할은 compat 가 `developer` 를 지원하고 모델이 reasoning 모델이면 `developer`, 아니면 `system` |
| AD-FR-19 | 도구 정의 `{type:"function", function:{name, description, parameters}}`. 도구 호출은 assistant 의 `tool_calls[{id, type:"function", function:{name, arguments(JSON 문자열)}}]` |
| AD-FR-20 | 도구 결과는 `{role:"tool", tool_call_id, content(문자열)}`. 결과에 이미지가 있고 모델이 vision 이면: 텍스트는 tool 메시지로("(see attached image)" 대체 문구 포함), 이미지는 뒤따르는 user 메시지의 `image_url`(data URL) 로 보낸다 |
| AD-FR-21 | `stream: true` 와 함께 compat 가 허용하면 `stream_options:{include_usage:true}` 를 보낸다 |
| AD-FR-22 | 출력 한도 필드는 compat 의 `MaxTokensField`(`max_tokens` / `max_completion_tokens`) |
| AD-FR-23 | thinking 요청 파라미터는 compat 의 `ThinkingFormat` 으로 보낸다. 1차 지원: `none`, `openai`(`reasoning_effort`), `chat-template`(`chat_template_kwargs.enable_thinking`), `deepseek`(`thinking:{type}`) |
| AD-FR-24 | 추론 텍스트 수신 필드는 `reasoning_content`, `reasoning`, `reasoning_text` 를 모두 확인한다. 되돌려 보낼 때는 compat 의 `ReasoningField` 로 assistant 메시지에 싣는다 (없으면 보내지 않음) |
| AD-FR-25 | 스트림: `choices[0].delta.content` → text_delta, 추론 필드 → thinking_delta, `delta.tool_calls[i]` → index 로 구분해 id·name 은 첫 조각, `arguments` 문자열을 이어 붙이고 finish 시 파싱 → toolcall_end. usage 는 `choices: []` chunk 또는 `choices[0].usage` 에서 |
| AD-FR-26 | usage: Input = `prompt_tokens − cached − cache_write`, CacheRead = `prompt_tokens_details.cached_tokens` → 없으면 `prompt_cache_hit_tokens` → 없으면 최상위 `cached_tokens`, CacheWrite = `prompt_tokens_details.cache_write_tokens`, Output = `completion_tokens`(추론 포함), Reasoning = `completion_tokens_details.reasoning_tokens` |
| AD-FR-27 | stop reason (`finish_reason`): `stop`·`end`·null → stop, `length` → length, `tool_calls`·`function_call` → tool_use, `content_filter` 외 → error |
| AD-FR-28 | assistant 메시지에 content 도 tool_calls 도 없으면 보내지 않는다 (일부 서버가 거부) |

### 3.4 compat 프로필

| id | 요구사항 |
|---|---|
| AD-FR-29 | Chat Completions 의 서버별 차이는 `Compat` 구조체로 표현한다: `BaseURL`, `APIKeyEnv`, `SupportsDeveloperRole`, `SupportsUsageInStreaming`, `MaxTokensField`, `ThinkingFormat`, `ReasoningField`, `RequiresToolResultName` |
| AD-FR-30 | 1차 내장 프로필: `openai`, `openrouter`, `vllm`, `llamacpp`, `ollama`. 프로필은 카탈로그(3.6)에서 provider 별로 연결된다 |
| AD-FR-31 | 사용자 설정으로 프로필 값을 덮을 수 있다 (없는 provider 추가 포함) |

### 3.5 에러 분류

| id | 요구사항 |
|---|---|
| AD-FR-32 | HTTP 상태와 본문으로 `CallError.Class` 를 정한다: 429 → rate_limit(`Retry-After` → RetryAfter), 529·503 → overloaded, 401·403 → auth, 413 → payload_too_large, 400 + overflow 문구 → overflow, 그 외 400·404·422 → invalid_request, 5xx → transient_network, 연결·타임아웃·스트림 중단 → transient_network |
| AD-FR-33 | overflow 문구 목록 (대소문자 무시): `prompt is too long`, `maximum context length`, `context_length_exceeded`, `context length exceeded`, `prompt too long; exceeded`, `input is too long`, `too many tokens`. 목록은 한 곳(상수)에 두고 테스트로 고정한다 |
| AD-FR-34 | 에러 메시지·Raw 에 API 키나 Authorization 헤더 값이 들어가지 않게 제거한다 (AD-C1) |

### 3.6 모델 카탈로그

| id | 요구사항 |
|---|---|
| AD-FR-35 | `Catalog.Lookup(provider, id) (contract.ModelInfo, bool)` 을 제공한다 |
| AD-FR-36 | 내장 카탈로그는 바이너리에 포함된 JSON 이다: provider → API, compat 프로필, 모델 목록(`id`, `contextWindow`, `maxOutputTokens`, capability) |
| AD-FR-37 | 사용자 설정 파일(경로는 sets 가 넘김)이 내장 항목을 덮거나 추가한다. 모델 단위로 `contextWindow`, `maxOutputTokens`, capability 를 바꿀 수 있다 |
| AD-FR-38 | 카탈로그에 없는 모델은 `ok=false`. 창 크기를 추측하지 않는다 — 기본값 판단은 sets 의 `model.select` 몫이다 |
| AD-FR-39 | 1차는 원격 카탈로그 조회(models.dev 등)를 하지 않는다 |

### 3.7 비기능

| id | 요구사항 |
|---|---|
| AD-NFR-01 | 05 문서 2·3장의 request JSON 예시를 중립 형식에서 **생성**하고, stream 예시를 **파싱**해 같은 의미가 되는지 골든 테스트로 고정한다 (CT-NFR-01 의 두 방언분) |
| AD-NFR-02 | 실제 provider 호출 테스트는 환경변수 키가 있을 때만 실행하고, 없으면 건너뛴다 (CI 기본은 건너뜀) |
| AD-NFR-03 | `httptest` 가짜 서버로 SSE 스크립트를 재생하는 테스트가 모든 매핑(3.2~3.5)을 덮는다 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| AD-FR-01~06 | 단위: 모르는 API, 취소, id 정규화, ProviderData 재전송 조건, 빈 블록 제거 |
| AD-FR-07~16 | 골든(요청 생성) + httptest SSE 재생(스트림·usage·stop) |
| AD-FR-17~28 | 같음 + compat 프로필별 차이(developer 역할, include_usage, max_tokens 필드, thinking 형식, 추론 필드, 이미지 분리) |
| AD-FR-29~31 | 프로필 덮어쓰기 |
| AD-FR-32~34 | 상태 코드·본문 표 기반 테스트, 키 마스킹 |
| AD-FR-35~39 | 내장·사용자 덮어쓰기·없는 모델 |
| AD-C1, C2 | 코드 검색 테스트: OAuth·구독 토큰 경로 없음, 설정 스키마에 키 값 필드 없음 |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| D2 (model.call 도 주입) | 2.1, AD-FR-01 |
| D15 | 1.2 범위 |
| 07 문서 2.8 계약 C2~C6 | AD-FR-14~16, 25~27, 32~33, 06 |
| 자격증명 보호 | AD-C1, AD-C2, AD-FR-34 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | 공식 Go SDK(anthropic-sdk-go, openai-go) 사용 여부. 1차는 net/http 직접 구현 (AD-C3) |
| O2 | 내장 카탈로그의 초기 모델 목록 (구현 시 사용자에게 확인) |
