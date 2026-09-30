# adapters — 모델 방언 어댑터 (스펙 미작성)

`model.call` step 에 주입되는 provider 방언 어댑터와 모델 카탈로그. contract 의 중립 형식 ↔ 방언 변환, 스트림 파싱, usage 정규화, 에러 분류, capability 선언을 맡는다.

- 1차 대상: Anthropic Messages, OpenAI Chat Completions (`hermes_study/analysis/05-provider-wire-formats.md`)
- 합격 기준: contract CT-NFR-01 (05 문서 예시의 무손실 왕복)

스펙은 core·inject 구현 후 작성한다.
