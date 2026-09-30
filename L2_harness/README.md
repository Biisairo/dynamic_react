# L2_harness — 하네스 (스펙 미작성)

L1(inject + sets + adapters) 위에서 **실행 가능한 에이전트**를 만드는 레이어다 (R13, pi 의 coding-agent 에 해당). L1 sets 가 interface 로만 받는 것들의 구현을 둔다.

| 항목 | 출처 (sets v0.2 에서 옮김) |
|---|---|
| 도구 4종 read / write / edit / bash 구현 | 옛 sets 3.3 (ST-FR-09) |
| JSONL 세션 저장소 (`sets.Store` 구현) | 옛 sets 3.4 (ST-FR-12~14) |
| 시스템 프롬프트 조립: 기본 문안, `systemPrompt` 파일, AGENTS.md·CLAUDE.md, 스킬 목록 | 옛 sets 3.5 (ST-FR-16~18), 미결 O1 |
| 요약기 구현 (`sets.Summarizer`, pi 요약 프롬프트) | 옛 sets 3.7 (ST-FR-24 의 프롬프트 부분) |
| 설정 파일: sessionDir, systemPrompt, catalog(adapters Config) 경로 | 옛 sets 2.2 |
| 검증용 print CLI `cmd/dr-print` | 옛 sets 3.8 (ST-FR-28~31), D16 |
| 실제 provider 종단 테스트 | 옛 ST-NFR-03 |

확정된 값: 기본 sessionDir = `~/.dynamic_react/sessions` (환경변수 `DR_SESSION_DIR`, 설정 `sessionDir` 로 변경 가능) — impl-01 에서 사용자 확정 (옛 sets O3).

스펙은 L1 구현 후 작성한다.
