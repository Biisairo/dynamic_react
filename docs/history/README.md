# 개발 기록

이 폴더에는 dynamic_react 를 만드는 동안 남긴 작업 기록이 있습니다. 작업은 모두 같은 순서를 따랐습니다. 스펙을 먼저 고치고, 실패하는 테스트를 쓴 뒤에 구현했습니다. 세션이 끝날 때마다 다음 세션이 읽을 인계 문서를 남겼습니다.

문서 안의 경로와 커밋 번호는 작성 당시 기준입니다. 문서를 이 폴더로 옮긴 뒤에도 본문의 경로는 고치지 않았습니다.

## 인계 문서

| 문서 | 다룬 내용 |
|---|---|
| [HANDOFF-impl-01](HANDOFF-impl-01.md) | L0(contract, core)와 L1 구현을 시작했습니다. 폴더마다 독립 모듈로 나누기로 정했습니다. |
| [HANDOFF-impl-02](HANDOFF-impl-02.md) | L1 sets 를 구현해서 L0·L1 을 마쳤습니다. |
| [HANDOFF-impl-03](HANDOFF-impl-03.md) | 시스템 프롬프트를 편집할 수 있게 했고, OS 환경(osenv)과 비대화형 CLI(dr-print)를 만들었습니다. |
| [HANDOFF-impl-04](HANDOFF-impl-04.md) | 대화형 터미널 surface 의 스펙을 인터뷰로 정하고 구현했습니다. |
| [HANDOFF-impl-05](HANDOFF-impl-05.md) | 모델을 설정하지 않아도 실행되게 했고, 앱 안에서 모델을 고를 수 있게 했습니다. |
| [HANDOFF-impl-06](HANDOFF-impl-06.md) | 완성된 에이전트까지 외부 함수를 주입하는 통로를 이었습니다. |
| [HANDOFF-impl-07](HANDOFF-impl-07.md) | 주입 통로의 결과를 정리했고, 실제 모델로 확인한 결과를 기록했습니다. |
| [HANDOFF-impl-08](HANDOFF-impl-08.md) | 세트 배치(켜기, 끄기, 순서)를 구현하기 시작했습니다. |
| [HANDOFF-impl-09](HANDOFF-impl-09.md) | 세션 모드를 구현했고, 대기 제어는 하지 않기로 정했습니다. |
| [HANDOFF-impl-10](HANDOFF-impl-10.md) | 프롬프트 캐시를 다루었고, 개선 목록(GOALS)을 만들었습니다. |
| [HANDOFF-impl-11](HANDOFF-impl-11.md) | 뮤테이션 검사를 했고, 모드와 goal 판정을 개선했습니다. 레이어 원칙을 정리했습니다. |
| [HANDOFF-impl-12](HANDOFF-impl-12.md) | 반복 감지 세트를 넣고 프로젝트를 종료했습니다. |

## 그 밖의 기록

| 문서 | 내용 |
|---|---|
| [GOALS](GOALS.md) | 목표(G1~G6)와 개선 목록입니다. 남은 항목은 모두 `보류` 상태입니다. |
| [VERIFY-22step](VERIFY-22step.md) | Hermes, pi, oh-my-pi 의 기능을 22 step 과 신호로 표현할 수 있는지 하나씩 대조한 표입니다. |
| [SRS-portfolio](SRS-portfolio.md) | 저장소를 공개용으로 정비한 작업의 요구사항입니다. |
| [LINT-REPORT](LINT-REPORT.md) | 린트를 도입하면서 지적을 처리한 결과입니다. 결정을 기다리는 항목이 하나 있습니다. |
