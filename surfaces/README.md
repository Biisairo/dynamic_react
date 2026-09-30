# surfaces — 입출력 (스펙 미작성)

core 의 run API(`Start`/`Input`/`Wait`)와 Observer 이벤트 위에 올라가는 입출력 어댑터: TUI, print, rpc, 웹, 메신저 게이트웨이. surface 마다 다른 설정(도구 세트, 승인 정책, 인터럽트 방식, 출력 제약)은 바인딩할 함수 세트 조합으로 표현한다 (`hermes_study/analysis/06-build-your-own.md` 3.3).

스펙은 core·inject 구현 후 작성한다. inject 미결 사항 O3 을 이때 재검토한다.
