# L2_harness — 하네스

L1(inject + sets) 위의 **에이전트 함수 하나**다 (R13, R14). 스펙: [`SPEC.md`](SPEC.md).

- 모듈: `L2_harness/harness` (패키지 `harness`, `tools`, `session`, `prompt`, `summary`, `envtest`)
- 실제 입출력은 하지 않는다. 파일·프로세스는 `harness.Env`, 모델은 `sets.ModelCaller` 로 받는다.
- Env 의 OS 구현(`L3_ui/osenv`)과 검증용 CLI(`L3_ui/cmd/dr-print`)는 L3 에 있다.
