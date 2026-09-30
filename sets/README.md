# sets — 기본 함수 세트 (스펙 미작성)

22개 step 의 기본 구현을 함수 세트로 제공하는 레이어 (D6). core·inject 는 기본값을 갖지 않으므로, 실행 가능한 에이전트는 이 레이어의 세트를 바인딩해야 한다.

예정된 세트 (`hermes_study/analysis/06-build-your-own.md` 6장 단계와 대응):

| 세트 | 내용 |
|---|---|
| `core` | JSONL 세션 저장, 투영, 고정 시스템 프롬프트, AGENTS.md, 스킬 목록, 도구 4개(read/bash/edit/write), 결과 자르기, 짝 없는 tool call 복구(aborted), 요청 전 짝 검증 |
| `resilient` | 에러 분류 + 재시도, overflow 복구 `[rewrite, rebuild]`, 임계치 압축, 압축 직후 규칙, 입력함 소비(steering/follow-up) |
| `quality` | 병렬·선행 실행 스케줄러, LSP, 앵커 편집, `task` 서브에이전트(실행기 주입: goroutine / 프로세스), soft tool requirement |
| `assistant` | 메모리, 백그라운드 리뷰, fallback provider, 메신저 surfaceProfile |

스펙은 core·inject 구현 후 작성한다.
