# L2_harness — 에이전트

L1(inject + sets) 위에서 사용자 환경에 올라가는 **실제 에이전트**다 (R13, R17).

| 모듈 | 내용 | 스펙 |
|---|---|---|
| `L2_harness/harness` | 에이전트 조립: 도구·세션·프롬프트·요약기 (패키지 `harness`, `env`, `envtest`, `tools`, `session`, `prompt`, `summary`). 실제 입출력은 하지 않고 `env.Env`·`sets.ModelCaller` 로 받는다 | [`SPEC.md`](SPEC.md) |
| `L2_harness/osenv` | `env.Env` 의 OS 구현 | [`osenv/SPEC.md`](osenv/SPEC.md) |
| `L2_harness/agent` | 완성된 에이전트: 설정 파일·환경변수 해석 + osenv + adapters → `harness.New` | [`agent/SPEC.md`](agent/SPEC.md) |

검증용 CLI 는 L3 (`L3_ui/cmd/dr-print`) 이고 agent 만 쓴다.
