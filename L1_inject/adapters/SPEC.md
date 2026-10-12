# adapters — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.8 (초안)
- 상위 근거: `docs/DECISIONS.md` (D2, D14, D15, R8, R9, R11, R12, R17, R31, R33)
- 의존 명세: `L0_core/contract/SPEC.md`
- 사용하는 쪽: `L1_inject/sets/SPEC.md`

---

## 1. 소개

### 1.1 목적
`model.call` step 에 주입되는 **모델 방언 어댑터**와, `model.select` 가 쓰는 **모델 카탈로그**를 정의한다. 어댑터는 contract 의 중립 `Request` 를 provider 방언으로 바꿔 보내고, 응답 스트림을 contract `StreamEvent` 로 되돌린다. API 가 바뀌면 이 레이어만 고친다 (07 문서 2.8).

### 1.2 범위
- 포함 (D15): 방언 3종 — Anthropic Messages, OpenAI Chat Completions(호환 서버 포함), OpenAI Responses. provider 프로필(3.4), 에러 분류, usage 정규화, stop reason 매핑, 동적 모델 카탈로그(3.6), API 키 해석.
- 제외: Gemini, Bedrock, Azure, Mistral 자체 API (다음 단계). 구독 요금제 엔드포인트 (AD-C1). 재시도·fallback 판단 (sets). 토큰 추정 (sets).

### 1.3 참조
- `hermes_study/analysis/05-provider-wire-formats.md` 2장(Anthropic), 3장(Chat Completions), 4장(Responses), 7장(비교표), 8장(형식 간 이동)
- pi 구현: `pi-mono/packages/ai/src/api/anthropic-messages.ts`, `openai-completions.ts`, `openai-responses*.ts`, `providers/*.ts`(프로필 원본), `utils/overflow.ts`
- Anthropic Models API (`GET /v1/models/{id}`: `max_input_tokens`, `max_tokens`, `capabilities`), 현재 모델의 thinking 규칙 (adaptive + `output_config.effort`, `budget_tokens` 거부) — claude-api 참조 자료, 2026-09-25

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 sets: model.select ──► client.Lookup(ctx, provider, id) -> ModelInfo   (dynamic, cached)
 sets: model.call   ──► client.Call(ctx, req) -> iter.Seq2[StreamEvent, error]
                             │ route by profile(req.Model.Provider).API
                             ├──► anthropic-messages
                             ├──► openai-chat       (+ compat per profile)
                             └──► openai-responses
                                     │
                                     ▼ net/http + SSE
                                 provider endpoint
```

- 어댑터는 contract 만 보는 단독 LLM 클라이언트다 (R9). `ModelCallFn` 으로 감싸 `core.CallStream` 을 만드는 것은 sets 몫이다.

### 2.2 제약
- AD-C1. provider 인증은 **API 키만** 지원한다. 구독 계정(Claude Pro/Max, ChatGPT 등)의 로그인·OAuth 토큰을 쓰는 경로와 **구독 요금제 전용 엔드포인트**(pi 의 openai-codex, github-copilot, kimi-coding, *-token-plan*, zai-coding 등)를 내장하지 않는다. OAuth 토큰을 받는 경로가 없다 (키는 `Options.APIKey` 하나). 키는 코드·로그·에러 메시지·트레이스에 노출하지 않는다.
- AD-C2. API 키는 `Options.APIKey(provider)` 로만 받는다. 키를 어디에 저장하는지는 L2 의 몫이다 (R21: agent 의 auth.json). `Config` 에 키 값 필드는 없다.
- AD-C3. HTTP 는 Go 표준 `net/http` 와 자체 SSE 파서로 구현한다. 이유: Chat Completions 호환 서버의 방언 차이(05 문서 3장)를 SDK 없이 직접 흡수해야 하고, core 와 같은 무의존 원칙을 L1 에도 가능한 한 적용한다. (재검토 가능: 6장 O1)
- AD-C4. 어댑터는 panic 하거나 스트림 도중 Go error 로 끝나지 않는다. 모든 실패는 contract `error` 이벤트(`CallError` 포함)로 끝낸다 (CT-FR-16).
- AD-C5. adapters 모듈은 contract 와 Go 표준 라이브러리 외 import 가 없다 (R8, R9). core·inject 를 import 하지 않는다.
- AD-C6. **사용자 환경을 읽지 않는다** (R17): 테스트가 아닌 코드는 `os` 를 import 하지 않는다 (환경변수·파일 접근 없음). 키는 `Options.APIKey`, 설정은 `ParseConfig` 의 바이트로만 받는다. 개발용 생성기 `internal/snapshot` 은 제외.

---

## 3. 상세 요구사항

### 3.1 공통

| id | 요구사항 |
|---|---|
| AD-FR-01 | `Client.Call(ctx, req)` 는 `req.Model.Provider` 의 프로필(3.4)로 방언·base URL·키를 정한다. `req.Model.API` 가 비어 있지 않으면 프로필의 API 와 같아야 한다. 모르는 provider·API 면 `error` 이벤트(`Class: invalid_request`) 하나로 끝나는 스트림을 돌려준다 |
| AD-FR-02 | ctx 가 취소되면 HTTP 요청·응답 본문을 닫고 `error` 이벤트(`Class: unknown`, 메시지 "aborted")로 끝낸다 |
| AD-FR-03 | tool call id 는 `[a-zA-Z0-9_-]`, 64자 이하로 정규화한다. 다른 문자는 `_` 로 바꾼다. 정규화 전후 대응을 한 요청 안에서 일관되게 유지한다 |
| AD-FR-04 | `ProviderData` 는 **같은 Provider·Model** 로 가는 요청에만 재전송하고, 아니면 버린다 (CT-FR-04). 서명 없는 thinking 블록은 text 로 바꿔 보낸다 (Anthropic) 또는 버린다 (Chat, 3.3 참조) |
| AD-FR-05 | 빈 text 블록과 내용이 없는 메시지는 보내지 않는다 |
| AD-FR-06 | 어댑터는 `Capabilities.LiveInput = false` 로 선언한다 (두 방언 모두 스트림 중 입력 미지원). core 는 이때 `inject_input` 을 입력함으로 보낸다 (CORE-FR-18) |

### 3.2 Anthropic Messages (`POST {base}/v1/messages`, SSE)

| id | 요구사항 |
|---|---|
| AD-FR-07 | 인증 헤더 `x-api-key`, `anthropic-version`. 기본 base URL `https://api.anthropic.com`, 키 필요 |
| AD-FR-08 | `System` 블록은 최상위 `system` 배열로 보낸다 (messages 에 넣지 않음) |
| AD-FR-09 | 도구 정의 `{name, description, input_schema}`. 도구 호출은 assistant 의 `tool_use{id, name, input(객체)}` 블록 |
| AD-FR-10 | 도구 결과는 **user 메시지 안의 `tool_result{tool_use_id, content, is_error}`** 블록. 연속된 결과는 user 메시지 하나로 묶는다 |
| AD-FR-11 | thinking 블록은 `{type:"thinking", thinking, signature}` 로 되돌려 보낸다 (signature = 같은 모델의 ProviderData). 가려진 추론은 opaque 블록 → `redacted_thinking{data}` |
| AD-FR-12 | `Options.CacheHints` 는 가리키는 위치(system 블록 / 메시지)의 **마지막 블록**에 `cache_control:{type:"ephemeral"}` 로 붙인다. 메시지가 여러 방언 블록으로 나뉘면(도구 결과 묶음) 그 메시지에서 나온 마지막 블록 |
| AD-FR-13 | `Options.Thinking`(level: `low`/`medium`/`high`/`xhigh`/`max`)이 비어 있으면 thinking 관련 필드를 보내지 않는다 (모델 기본값). 있으면: 카탈로그(3.6)가 그 모델의 `capabilities.thinking.types.adaptive` 를 지원한다고 하거나 capability 를 모르면 `thinking:{type:"adaptive"}` + `output_config:{effort: level}`. budget 방식만 지원하면 level 을 토큰 수로 환산하지 않고 `invalid_request` 로 끝낸다 — 그때는 `Options.Extra["thinking"]` 에 원문을 넣는다. 모르는 level 도 `invalid_request`. `Options.Extra` 의 키는 요청 최상위에 그대로 싣는다 (같은 키가 있으면 Extra 가 이긴다) |
| AD-FR-13a | `max_tokens` 는 필수다: `Options.MaxOutputTokens` 가 0 이면 카탈로그의 MaxOutputTokens, 그것도 0 이면 요청을 보내지 않고 `invalid_request` 로 끝낸다 |
| AD-FR-14 | 스트림 이벤트 매핑: `message_start` → `start` + usage(입력 쪽), `content_block_start/delta/stop` → `text_delta` / `thinking_delta` / `toolcall_start` / `toolcall_delta`(input_json_delta) / `toolcall_end`(누적 JSON 파싱), `signature_delta` → thinking 의 ProviderData, `message_delta` → 최종 usage·stop reason, `message_stop` → `done` |
| AD-FR-15 | usage: `input_tokens` → Input(이미 캐시 제외), `cache_read_input_tokens` → CacheRead, `cache_creation_input_tokens` → CacheWrite, `output_tokens` → Output. `message_delta` 에 input 이 없으면 `message_start` 값을 유지 |
| AD-FR-16 | stop reason: `end_turn`·`stop_sequence`·`pause_turn` → stop, `max_tokens` → length, `tool_use` → tool_use, `refusal` → error. 모르는 값 → error (Raw 보존) |

### 3.3 OpenAI Chat Completions (`POST {base}/chat/completions`, SSE)

| id | 요구사항 |
|---|---|
| AD-FR-17 | 인증 헤더 `Authorization: Bearer <key>`. base URL·키 필요 여부는 compat 프로필에서 온다 (3.4) |
| AD-FR-18 | system 은 `messages[0]` 이다. 역할은 프로필 `SupportsDeveloperRole` 이면 `developer`, 아니면 `system` (내장 모델 목록이 없어 reasoning 모델 여부로는 가르지 않는다) |
| AD-FR-19 | 도구 정의 `{type:"function", function:{name, description, parameters}}`. 도구 호출은 assistant 의 `tool_calls[{id, type:"function", function:{name, arguments(JSON 문자열)}}]` |
| AD-FR-20 | 도구 결과는 `{role:"tool", tool_call_id, content(문자열)}`. 결과에 이미지가 있고 모델이 vision 이면: 텍스트는 tool 메시지로("(see attached image)" 대체 문구 포함), 이미지는 뒤따르는 user 메시지의 `image_url`(data URL) 로 보낸다 |
| AD-FR-21 | `stream: true` 와 함께 compat 가 허용하면 `stream_options:{include_usage:true}` 를 보낸다 |
| AD-FR-22 | 출력 한도 필드는 compat 의 `MaxTokensField`(`max_tokens` / `max_completion_tokens`) |
| AD-FR-23 | thinking 요청 파라미터는 프로필의 `ThinkingFormat` 으로 보낸다 (Thinking 이 비면 아무것도 안 보냄, level 은 그대로 전달 — 모델별 변환 표 없음): `none` · `openai` = `reasoning_effort: level` · `deepseek` = `thinking:{type:"enabled"}` + `reasoning_effort: level` · `zai` = `thinking:{type:"enabled", clear_thinking:false}` · `openrouter`·`ant-ling` = `reasoning:{effort: level}` · `together` = `reasoning:{enabled:true}` · `chat-template` = `chat_template_kwargs:{enable_thinking:true}` (pi `openai-completions.ts` 875~972) |
| AD-FR-24 | 추론 텍스트 수신 필드는 `reasoning_content`, `reasoning`, `reasoning_text` 를 모두 확인한다. 되돌려 보낼 때는 compat 의 `ReasoningField` 로 assistant 메시지에 싣는다 (없으면 보내지 않음) |
| AD-FR-25 | 스트림: `choices[0].delta.content` → text_delta, 추론 필드 → thinking_delta, `delta.tool_calls[i]` → index 로 구분해 id·name 은 첫 조각, `arguments` 문자열을 이어 붙이고 finish 시 파싱 → toolcall_end. usage 는 `choices: []` chunk 또는 `choices[0].usage` 에서 |
| AD-FR-26 | usage: Input = `prompt_tokens − cached − cache_write`, CacheRead = `prompt_tokens_details.cached_tokens` → 없으면 `prompt_cache_hit_tokens` → 없으면 최상위 `cached_tokens`, CacheWrite = `prompt_tokens_details.cache_write_tokens`, Output = `completion_tokens`(추론 포함), Reasoning = `completion_tokens_details.reasoning_tokens` |
| AD-FR-27 | stop reason (`finish_reason`): `stop`·`end`·null → stop, `length` → length, `tool_calls`·`function_call` → tool_use, `content_filter` 외 → error |
| AD-FR-28 | assistant 메시지에 content 도 tool_calls 도 없으면 보내지 않는다 (일부 서버가 거부) |
| AD-FR-54 | 캐시 지점 켜기 (R31): 프로필 `CacheControl` 이 `anthropic` 이면 항상, `anthropic-models` 면 요청 모델 ID 가 `anthropic/` 으로 시작할 때만 AD-FR-55 를 적용한다. 비어 있으면 `CacheHints` 를 보내지 않는다 (기본). 원본: pi `openai-completions.ts` `cacheControlFormat`·`detectCompat` |
| AD-FR-56 | 캐시 키 (R33): 프로필 `PromptCacheKey` 가 true 이고 `Options.CacheKey` 가 비지 않으면 Responses·Chat 방언은 본문에 `prompt_cache_key` = CacheKey 의 앞 64 문자(rune)를 넣는다. 아니면 넣지 않는다. Anthropic 방언은 CacheKey 를 쓰지 않는다. 내장 프로필은 `openai` 만 true. 원본: pi `clampOpenAIPromptCacheKey` |
| AD-FR-55 | 캐시 지점 위치: `CacheHints` 마다 `cache_control:{type:"ephemeral"}` 을 붙인다. system 힌트 → system(또는 developer) 메시지. 메시지 힌트 → 그 메시지에서 나온 Chat 메시지 중 마지막 것. 대상에 텍스트가 없으면(도구 호출만 있는 assistant, 보내지 않은 메시지 AD-FR-28) system 을 뺀 앞쪽의 가장 가까운, 텍스트가 있는 Chat 메시지. 붙이는 방법: content 가 문자열이면 `[{type:"text", text, cache_control}]` 로 바꾸고, 파트 배열이면 마지막 text 파트에 붙인다. 같은 Chat 메시지에 두 번 붙지 않는다 |

### 3.3a OpenAI Responses (`POST {base}/responses`, SSE)

| id | 요구사항 |
|---|---|
| AD-FR-40 | 인증 `Authorization: Bearer <key>`. 본문 `stream: true`, `store: false`. 프로필이 `EncryptedReasoning` 이면 `include: ["reasoning.encrypted_content"]` |
| AD-FR-41 | system 은 `input[0]` 의 `developer`(프로필 `SupportsDeveloperRole`) 또는 `system` 역할 메시지 |
| AD-FR-42 | 도구 정의는 평평한 `{type:"function", name, description, parameters}` |
| AD-FR-43 | 메시지 → item: user 텍스트·이미지 → `input_text`·`input_image`(data URL), assistant 텍스트 → `{type:"message", role:"assistant", content:[output_text]}`, thinking 블록(같은 provider·model 의 ProviderData) → `{type:"reasoning", id, summary:[summary_text], encrypted_content}`, tool_call 블록 → `{type:"function_call", id(블록 ProviderData 의 item id, 같은 모델일 때만), call_id, name, arguments(문자열)}`, 도구 결과 → `{type:"function_call_output", call_id, output}` (이미지가 있으면 output 은 `input_text`·`input_image` 배열) |
| AD-FR-44 | `Options.Thinking` → `reasoning:{effort: level, summary:"auto"}`. `MaxOutputTokens` → `max_output_tokens`. ToolChoice `auto`/`none`/`required` → 문자열, `tool` → `{type:"function", name}`. CacheHints 는 보내지 않는다 (자동 캐시) |
| AD-FR-45 | 스트림: `response.created` → start, `output_item.added`(reasoning / message / function_call) → 블록 시작(`toolcall_start` 포함), `reasoning_summary_text.delta` → thinking_delta, `output_text.delta` → text_delta, `refusal.delta` → text_delta + 최종 stop reason error, `function_call_arguments.delta` → toolcall_delta, `output_item.done` → reasoning 의 ProviderData(`{id, encrypted_content}`) 확정 / `toolcall_end`(ProviderData 에 item id). 이벤트 `Index` = `output_index` |
| AD-FR-46 | 끝: `response.completed` → done (출력에 function_call 이 있으면 stop reason tool_use, 아니면 stop), `response.incomplete` → done (`max_output_tokens` 이면 length, 그 외 error), `response.failed`·`error` 이벤트 → error 이벤트 (`error` 이벤트는 `error.{type, code, message}`, 그 객체가 비면 최상위 `code`·`message`. `CallError` 는 3.5 로 분류, 코드 `context_length_exceeded` 는 overflow, `rate_limit_exceeded` 는 rate_limit) |
| AD-FR-47 | usage: Input = `input_tokens − input_tokens_details.cached_tokens`, CacheRead = `cached_tokens`, Output = `output_tokens`, Reasoning = `output_tokens_details.reasoning_tokens` |

### 3.4 provider 프로필

| id | 요구사항 |
|---|---|
| AD-FR-29 | provider 는 `Profile` 로 표현한다: `API`(방언), `BaseURL`, `RequiresKey`(false 면 키 없이 보낸다: 로컬 서버. 키가 있으면 어느 쪽이든 인증 헤더를 붙인다), `Lookup`(3.6 조회 방식), `CatalogID`(원격 카탈로그 provider 키), Chat·Responses 차이 `SupportsDeveloperRole`, `SupportsUsageInStreaming`, `MaxTokensField`, `ThinkingFormat`, `ReasoningField`, `RequiresToolResultName`, `EncryptedReasoning`, `CacheControl`(AD-FR-54), `PromptCacheKey`(AD-FR-56) |
| AD-FR-30 | 내장 프로필 = pi 의 provider 중 1차 방언을 API 키로 쓰는 것 + 로컬 서버 (R12). 값의 원본은 pi `providers/*.ts` (base URL, 키 필요 = pi 에 키 환경변수가 있음). z.ai 는 일반 API(`https://api.z.ai/api/paas/v4`, 공식 문서 확인). xiaomi 는 종량제 실시간 추론 OpenAI 호환 주소(`https://api.xiaomimimo.com/v1`, 공식 문서 mimo.mi.com "First API Call" 확인. 공식 예시가 OpenAI SDK 의 `api_key` 를 쓰므로 Bearer 인증) |

| provider | API | base URL | 키 필요 | Lookup |
|---|---|---|---|---|
| anthropic | anthropic-messages | `https://api.anthropic.com` | yes | anthropic |
| minimax | anthropic-messages | `https://api.minimax.io/anthropic` | yes | none |
| minimax-cn | anthropic-messages | `https://api.minimaxi.com/anthropic` | yes | none |
| fireworks | anthropic-messages | `https://api.fireworks.ai/inference` | yes | none |
| vercel-ai-gateway | anthropic-messages | `https://ai-gateway.vercel.sh` | yes | none |
| openai | openai-responses | `https://api.openai.com/v1` | yes | none |
| xai | openai-responses | `https://api.x.ai/v1` | yes | none |
| deepseek | openai-chat | `https://api.deepseek.com` | yes | none |
| zai | openai-chat | `https://api.z.ai/api/paas/v4` | yes | none |
| xiaomi | openai-chat | `https://api.xiaomimimo.com/v1` | yes | none |
| moonshotai | openai-chat | `https://api.moonshot.ai/v1` | yes | none |
| moonshotai-cn | openai-chat | `https://api.moonshot.cn/v1` | yes | none |
| openrouter | openai-chat | `https://openrouter.ai/api/v1` | yes | openrouter |
| groq | openai-chat | `https://api.groq.com/openai/v1` | yes | none |
| cerebras | openai-chat | `https://api.cerebras.ai/v1` | yes | none |
| together | openai-chat | `https://api.together.ai/v1` | yes | none |
| nvidia | openai-chat | `https://integrate.api.nvidia.com/v1` | yes | none |
| huggingface | openai-chat | `https://router.huggingface.co/v1` | yes | none |
| baseten | openai-chat | `https://inference.baseten.co/v1` | yes | none |
| ant-ling | openai-chat | `https://api.ant-ling.com/v1` | yes | none |
| vllm | openai-chat | `http://localhost:8000/v1` | no | vllm |
| llamacpp | openai-chat | `http://localhost:8080/v1` | no | llamacpp |
| ollama | openai-chat | `http://localhost:11434/v1` | no | ollama |

Chat·Responses 차이 필드 (원본: pi `openai-completions.ts` `detectCompat`. 표에 없는 값은 기본 — developer 역할 no, usage 스트리밍 yes, `max_completion_tokens`, thinking `openai`, 추론 필드 없음, 도구 결과 name 불필요):

| provider | developer 역할 | max tokens 필드 | thinking | 추론 필드(되돌려 보냄) | 기타 |
|---|---|---|---|---|---|
| openai (Responses) | yes | — | (Responses) | — | EncryptedReasoning yes, PromptCacheKey yes |
| xai (Responses) | no | — | (Responses) | — | EncryptedReasoning no |
| deepseek | no | `max_tokens` | deepseek | `reasoning_content` | |
| zai | no | `max_tokens` | zai | | |
| xiaomi | yes | | openai | | |
| moonshotai, moonshotai-cn | no | `max_tokens` | none | | |
| openrouter | no | | openrouter | | CacheControl `anthropic-models` (AD-FR-54) |
| groq, huggingface, baseten | yes | | openai | | |
| cerebras | no | | openai | | |
| together | no | `max_tokens` | together | | |
| nvidia | no | `max_tokens` | none | | |
| ant-ling | no | `max_tokens` | ant-ling | | |
| vllm, llamacpp, ollama | no (로컬 chat 템플릿 호환) | `max_tokens` | none | | |
|
| AD-FR-31 | 사용자 설정으로 프로필 값을 덮을 수 있다 (없는 provider 추가 포함) |

### 3.5 에러 분류

| id | 요구사항 |
|---|---|
| AD-FR-32 | HTTP 상태와 본문으로 `CallError.Class` 를 정한다: 429 → rate_limit(`Retry-After` → RetryAfter), 529·503 → overloaded, 401·403 → auth, 413 → payload_too_large, 400 + overflow 문구 → overflow, 그 외 400·404·422 → invalid_request, 5xx → transient_network, 연결·타임아웃·스트림 중단 → transient_network |
| AD-FR-33 | overflow 문구 목록 (대소문자 무시): `prompt is too long`, `maximum context length`, `context_length_exceeded`, `context length exceeded`, `prompt too long; exceeded`, `input is too long`, `too many tokens`. 목록은 한 곳(상수)에 두고 테스트로 고정한다 |
| AD-FR-34 | 에러 메시지·Raw 에 API 키나 Authorization 헤더 값이 들어가지 않게 제거한다 (AD-C1) |

### 3.6 모델 카탈로그 (동적, R11)

값은 **필드 단위**로, 아래 출처를 위에서부터 보고 처음 알려진 값을 쓴다. 어느 출처도 모르면 0(모름)·capability 없음이다.

| 순위 | 출처 | 필드 |
|---|---|---|
| 1 | 사용자 설정 `models["provider/id"]` (AD-FR-49) | contextWindow, maxOutputTokens, capabilities |
| 2 | provider 조회 API (프로필 `Lookup`) | 아래 AD-FR-36 |
| 3 | 원격 카탈로그 (기본 `https://models.dev/api.json`) | `limit.context`, `limit.output`, `modalities.input` 의 image, `reasoning`, `reasoning_options[].type` |
| 4 | 내장 스냅샷 (oh-my-pi `packages/catalog/src/models.json` 에서 프로필 provider 만 추린 것, MIT) | `contextWindow`, `maxTokens`, `input` 의 image, `reasoning`, `thinking.mode` |

| id | 요구사항 |
|---|---|
| AD-FR-35 | `Client.Lookup(ctx, provider, id) (contract.ModelInfo, bool)` 을 제공한다. 어느 출처에든 모델이 있으면 `ok=true` |
| AD-FR-36 | provider 조회 방식: `anthropic` = `GET {base}/v1/models/{id}` (`max_input_tokens`, `max_tokens`, `capabilities.image_input`, `capabilities.thinking` 과 `types.adaptive`) · `openrouter` = `GET {base}/models` (`context_length`, `top_provider.max_completion_tokens`, `architecture.input_modalities`, `reasoning` 존재) · `ollama` = `POST {host}/api/show {model}` (`model_info` 의 `*.context_length`, `capabilities`) · `llamacpp` = `GET {host}/props` (`default_generation_settings.n_ctx`) · `vllm` = `GET {base}/models` 의 `data[].max_model_len` (vLLM 소스 `ModelCard.max_model_len`, `vllm/entrypoints/serve/engine/protocol.py` main `ac9126e` 확인) · `none` = 조회하지 않음 |
| AD-FR-36a | 원격 카탈로그의 provider 키는 프로필 `CatalogID`(비면 provider 이름)다. thinking 방식: `effort` → adaptive, `budget_tokens` 만 → budget, `toggle` → adaptive 로 보지 않음. 내장 스냅샷의 `thinking.mode`: `anthropic-adaptive`·`effort` → adaptive, `budget`·`anthropic-budget-effort` → budget |
| AD-FR-36b | 원격 카탈로그는 Client 당 한 번 받아 캐시한다. 실패하면 캐시하지 않고 다음 Lookup 에서 다시 받는다. `Options.RemoteCatalogURL` 로 주소를 바꾸고 `Options.NoRemoteCatalog` 로 끈다. 요청에는 키·대화 내용을 싣지 않는다 (GET 만) |
| AD-FR-36c | 내장 스냅샷은 바이너리에 포함된 JSON 이고, 생성 도구(`internal/snapshot`)가 원본 파일에서 프로필 provider 의 모델만 필드를 추려 만든다. 데이터만 담는다 |
| AD-FR-37 | 사용자 설정(파일 경로는 sets 가 넘김)이 모델 단위로 덮거나 추가한다 (1순위) |
| AD-FR-38 | 추측하지 않는다 — 모든 출처가 모르면 0 이고, 기본값 판단은 sets 의 `model.select` 몫이다 |
| AD-FR-39 | provider 조회 결과는 Client 안에 (provider, id) 별로 캐시한다. 실패는 캐시하지 않는다. 동시 Lookup 에 안전하다 |

### 3.7 비기능

| id | 요구사항 |
|---|---|
| AD-NFR-01 | 05 문서 2·3·4장의 request JSON 예시를 중립 형식에서 **생성**하고, stream 예시를 **파싱**해 같은 의미가 되는지 골든 테스트로 고정한다 (CT-NFR-01 의 세 방언분). 2장의 thinking 은 AD-FR-13 의 새 형식으로 바꾼 값을 기대값으로 한다 |
| AD-NFR-02 | 실제 provider 호출 테스트는 테스트 전용 환경변수(pi 의 키 환경변수 이름)에 키가 있을 때만 실행하고, 없으면 건너뛴다 (CI 기본은 건너뜀). 테스트 코드만 읽는다 (R21 ⑥) |
| AD-NFR-03 | `httptest` 가짜 서버로 SSE 스크립트를 재생하는 테스트가 모든 매핑(3.2~3.5)을 덮는다 |

### 3.8 Go 표현

| id | 요구사항 |
|---|---|
| AD-FR-48 | `adapters.New(Options{HTTPClient, APIKey, Now, Config}) *Client`. nil 이면 `http.DefaultClient`, 모든 provider 에 키 없음(""), `time.Now` (AD-C6). `APIKey func(provider string) string` 은 요청·조회마다 부른다 (키가 바뀌면 다음 호출부터 반영). `Client` 는 동시 사용에 안전하다 |
| AD-FR-49 | `Config` JSON: `{"providers": {name: Profile 필드(부분 덮어쓰기·새 provider)}, "models": {"provider/id": {"contextWindow", "maxOutputTokens", "capabilities"}}}`. `ParseConfig([]byte)` 는 모르는 키를 에러로 거부한다. 파일을 읽는 함수는 두지 않는다 (AD-C6). 키 **값**을 담는 필드는 없다 (AD-C2). 프로필 필드 `requiresKey`(bool, 새 provider 기본 false) |
| AD-FR-50 | `RequiresKey` 인데 `APIKey(provider)` 가 "" 이면 요청을 보내지 않고 `error` 이벤트(`Class: auth`, 메시지 `missing API key for provider "<name>"`)로 끝낸다 |
| AD-FR-52 | `Client.Models() []ModelRef` (`type ModelRef struct{ Provider, ID string }`): 사용자 설정 `models` 의 키(`provider/id`, 첫 `/` 에서 나눔)와 내장 스냅샷의 모델을 합친 목록. provider 가 프로필(내장 + 설정 `providers`)에 없는 항목은 뺀다. `/` 가 없거나 id 가 빈 키(`ParseConfig` 를 거치지 않은 `Config`)는 뺀다. 중복 없이 Provider → ID 사전순. 네트워크를 쓰지 않는다 (provider 조회·원격 카탈로그 제외, R20) |
| AD-FR-53 | `Client.Providers() []string`: 프로필(내장 + 설정 `providers`) 이름, 사전순 |
| AD-FR-51 | done 이벤트의 Message: `ID` = provider 응답 id(없으면 무작위), `Timestamp` = `Now()`, `Role` assistant, `Usage`·`StopReason` 채움 (contract CT-FR-17). 스트림 도중 받은 블록은 이벤트 `Index` 순서로 Content 가 된다 |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| AD-FR-01~06 | 단위: 모르는 API, 취소, id 정규화, ProviderData 재전송 조건, 빈 블록 제거 |
| AD-FR-07~16 | 골든(요청 생성) + httptest SSE 재생(스트림·usage·stop) |
| AD-FR-17~28 | 같음 + compat 프로필별 차이(developer 역할, include_usage, max_tokens 필드, thinking 형식, 추론 필드, 이미지 분리) |
| AD-FR-29~31 | 프로필 덮어쓰기 |
| AD-FR-32~34 | 상태 코드·본문 표 기반 테스트, 키 마스킹 |
| AD-FR-35~39 | httptest 조회·원격 카탈로그 서버: 출처별 필드 매핑, 필드 단위 우선순위, 실패 시 다음 출처·재시도, 캐시, 동시 Lookup, 끄기 옵션. 스냅샷: 생성 결과 동기화, 프로필 provider 만 포함 |
| AD-FR-40~47 | 골든(요청 생성) + httptest SSE 재생 |
| AD-FR-30 | 표 대조 테스트, AD-C1 코드 검색(구독 엔드포인트·OAuth 경로 없음) |
| AD-FR-56 | Responses·Chat: 플래그 켬 + 키 → 본문에 키, 64자 넘는 키(한글 포함) → 앞 64 rune, 빈 키 → 없음, 플래그 끔 → 없음. Anthropic: 키가 있어도 없음. 내장 프로필 값(openai 만 true), 설정 덮어쓰기 `promptCacheKey` |
| AD-FR-54~55 | 표 기반: CacheControl 빈 값 → cache_control 없음(힌트가 있어도), `anthropic` → system·메시지 지점, `anthropic-models` → `anthropic/…` 모델만, 문자열 → 파트 배열, 이미지 user 메시지의 마지막 text 파트, 도구 호출만 있는 assistant → 앞쪽 텍스트 메시지, 이미지 도구 결과 → 나온 user 메시지, 힌트 없음 → 없음, 내장 openrouter 값, 설정 덮어쓰기 `cacheControl` |
| AD-FR-52~53 | 스냅샷만·설정 models 합침·프로필 없는 provider 제외·잘못된 키 제외·정렬·중복 없음·네트워크 없음, Providers 정렬·설정 provider 포함 |
| AD-C1, C2 | 코드 검색 테스트: OAuth·구독 토큰 경로 없음, 설정 스키마에 키 값 필드 없음 |
| AD-C5 | `go list -deps` 검사: contract·표준 라이브러리 외 없음 |
| AD-C6, FR-48 | 테스트 아닌 파일(`internal/` 제외)의 직접 import 에 `os` 없음. `APIKey` nil + RequiresKey provider → `auth` error 이벤트 (provider 이름), RequiresKey false 여도 `APIKey` 가 값을 주면 인증 헤더, 코드에 `Getenv`·키 환경변수 이름 없음 |

---

## 5. 추적성

| 근거 | 요구사항 |
|---|---|
| D2 (model.call 도 주입) | 2.1, AD-FR-01 |
| D15 | 1.2 범위 |
| 07 문서 2.8 계약 C2~C6 | AD-FR-14~16, 25~27, 32~33, 06 |
| 자격증명 보호 | AD-C1, AD-C2, AD-FR-34 |
| R31 | AD-FR-29, 54, 55, 3.4 표 |
| R33 | AD-FR-29, 56, 3.4 표 |

---

## 6. 미결 사항

| id | 내용 |
|---|---|
| O1 | 공식 Go SDK 사용 여부 → **net/http 직접 구현**으로 확정 (AD-C3, 사용자 진행 승인) |
| O2 | ~~내장 모델 목록~~ → **동적 조회**로 확정 (R11, 사용자) |

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | adapters 가 inject 의 `ModelCallFn` 을 직접 제공하고 `core.CallStream` 을 돌려줌 (contract·core·inject 의존) | `Call(ctx, req) iter.Seq2[contract.StreamEvent, error]`, contract 만 의존 (AD-C5). 감싸기는 sets | R8·R9: 독립 모듈로 단독 사용 가능하게 |
| 0.3 | 방언 2종, 내장 카탈로그(JSON), 프로필 5종 | 방언 3종(+Responses), 동적 카탈로그, pi 기반 프로필 23종 | 사용자: OpenAI·xAI 는 Responses, 모델은 매번 조회, pi 의 provider 기본 포함 |
| 0.3 | AD-FR-13: thinking 이면 temperature 제외 | adaptive + effort, budget 전용 모델은 Extra | 현재 Anthropic 모델은 budget_tokens 를 400 으로 거부. 사용자 확정 |
| 0.3 | AD-FR-18: reasoning 모델이면 developer | 프로필 값만 | 모델별 판단 근거가 카탈로그 밖에 없음 |
| 0.3 | AD-FR-39: 원격 카탈로그 조회 안 함 | 4단 출처 (설정 > provider 조회 > models.dev > 내장 스냅샷) | 조회 API 로 창 크기를 알 수 없는 provider 가 많음. 사용자 확정 |
| 0.3 | AD-C1: API 키만 | + 구독 요금제 엔드포인트 미내장, Anthropic OAuth 환경변수 무시 | 자격증명 보호 |
| 0.8 | 캐시 키를 보내지 않음 | 프로필 `PromptCacheKey` 가 켜져 있으면 `prompt_cache_key` (AD-FR-56), 내장 openai 켬 | R33 |
| 0.7 | Chat 방언은 `CacheHints` 를 버림 | 프로필 `CacheControl` 이 켜져 있으면 Anthropic 식 `cache_control` (AD-FR-54·55), 내장 openrouter 는 `anthropic/` 모델에서 켬 | R31 |
| 0.6 | 키 = `Options.Getenv(Profile.APIKeyEnv)`, 키 없음 문구에 환경변수 이름 | 키 = `Options.APIKey(provider)`, `Profile.RequiresKey`, 문구에 provider 이름. `Client.Providers()` (AD-FR-53) | R21: 환경변수를 쓰지 않고 키는 L2 의 auth.json |
| 0.5 | 모델 목록 API 없음 (id 로 Lookup 만) | `Client.Models()` (AD-FR-52) | R20: 등록한 모델 중에서 고르기 — 목록 원천은 L1, 키 상태·선택은 L2 |
| 0.4 | AD-FR-48: `Getenv` nil 이면 `os.Getenv`. AD-FR-49: `LoadConfig(path)` 가 파일을 읽음 | `Getenv` nil 이면 환경을 읽지 않음. `LoadConfig` 삭제 (`ParseConfig` 만). AD-C6 추가 | R17: L1 은 사용자 환경과 무관한 단독 라이브러리. 환경변수·파일 해석은 `L2_harness/agent` |
