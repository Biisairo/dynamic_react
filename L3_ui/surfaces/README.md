# surfaces — 입출력

완성된 에이전트(`L2_harness/agent`) 위의 입출력: TUI, rpc, 웹, 메신저 게이트웨이 (R17).

surface 공통 계층(도구 세트 상한, 승인 규칙·`Approver`, Abort·Steer·FollowUp, 이벤트, 세션 키·목록, 세션 잠금, 콜백 panic 격리)은 L2 agent 에 있다 (R18, `L2_harness/agent/SPEC.md` 3.4~3.9). surface 는 `Approver` 와 `OnEvent` 를 구현하고, 입력을 `Start`·`Steer`·`FollowUp`·`Abort` 로 잇고, 이벤트를 그린다. 전송 속도 제한·묶기(메신저 편집 제한 등)는 surface 가 콜백 안의 자기 큐로 한다.

inject O3 는 R18 로 종결됐다.

| 모듈 | 내용 |
|---|---|
| `terminal` | 대화형 터미널 surface 라이브러리 (`terminal/SPEC.md`). 진입점은 `L3_ui/cmd/dr` |
