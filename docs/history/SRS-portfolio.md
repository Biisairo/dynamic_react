# SRS: 포트폴리오 정비 (PF)

- 문서 ID: SRS-PF, 버전 v0.3, 2026-10-16
- 형식: IEEE 29148 (Software Requirements Specification)
- 상태: 승인 뒤 구현 완료. 구현 중 바뀐 내용은 7장에 있다.

## 1. 소개

### 1.1 목적

이 문서는 종료된 dynamic_react 저장소(R42)를 포트폴리오로 공개할 수 있는 상태로 정비하는 작업의 요구사항을 정의한다.

### 1.2 범위

범위 안:

- 저장소 첫 화면 문서(README) 재작성
- 내부 작업 기록을 `docs/history/` 로 이동
- 린트 설정 추가 (CI 는 v0.3 에서 범위 밖으로 뺐다)
- 동작을 바꾸지 않는 린트 수정, flaky 테스트 수정
- 실행 데모 GIF 제작

범위 밖 (비목표):

- 라이선스 파일 추가. 사용자 결정에 따라 LICENSE 를 만들지 않는다.
- 영어 문서. README 는 한국어로만 작성한다.
- 런타임 동작 변경. 기능 추가, API 변경, 에러 문자열 변경을 하지 않는다.
- 각 모듈 `SPEC.md` 와 `docs/DECISIONS.md`·`docs/INJECT.md` 본문 수정. 링크 경로 수정은 예외로 허용한다.
- 원격 저장소 생성, push, 외부 커버리지 서비스(Codecov 등) 연동.
- GOALS 에서 `보류` 로 둔 항목의 재개.

### 1.3 정의

| 용어 | 뜻 |
|---|---|
| 모듈 | `go.work` 의 `use` 목록에 있는 14개 Go 모듈 |
| 전 모듈 검증 | 14개 모듈 각각에서 `gofmt -l`(출력 없음), `go vet ./...`, `go test -race ./...`, `golangci-lint run ./...` 이 모두 성공하는 상태 |
| 동작 보존 수정 | 컴파일된 프로그램의 관측 가능한 출력·반환값·에러 문자열·부수효과를 바꾸지 않는 수정 |
| 동작 위험 지적 | 고치면 관측 가능한 동작이 바뀔 수 있는 린트 지적 (예: 무시하던 `json.Unmarshal` 에러를 처리, ST1005 에러 문자열 수정) |
| 내부 작업 기록 | `docs/HANDOFF-impl-01~12.md`, `docs/VERIFY-22step.md`, `docs/GOALS.md` |

### 1.4 참고

- `README.md` (현재판, 2026-10-12)
- `docs/architecture.html`
- fluent-korean 작문 규칙 (https://github.com/snflkd/fluent-korean)
- ASD-STE100 (문서 산출물에 약 80% 완화 적용)

## 2. 전체 설명

### 2.1 현재 상태 (2026-10-16 측정)

- 14개 모듈 중 13개는 vet·test 를 통과한다. `L2_harness/agent` 의 `TestFollowUp` 은 `-count=20` 에서 4회 실패한다 (`agent: run is done`).
- 원인: `EventMessageStart` 를 받은 뒤 `FollowUp` 을 부르지만, 가짜 LLM 응답이 곧바로 끝나서 run 이 먼저 종료되는 경합이다.
- golangci-lint v2 (errcheck, staticcheck, unused, govet, ineffassign, misspell, unconvert, gocritic) 지적은 215건이다. errcheck 199건, staticcheck 10건, gocritic 6건이다. errcheck 중 테스트 밖 코드는 82건이다.
- README 는 내부 결정 번호(R·D·G)와 저장소 밖 경로(`../hermes_study`)에 기대어 있어, 처음 읽는 사람이 목적을 파악하기 어렵다.
- LICENSE·CI·원격 저장소가 없다.

### 2.2 제약

- C-1: 모듈 경로 `github.com/Biisairo/dynamic_react/<폴더>` 는 바꾸지 않는다.
- C-2: 파일 이동은 `git mv` 로 하여 이력을 보존한다.
- C-3: 커밋은 사용자 확인 뒤에만 하고, 커밋 메시지에 AI 서명을 넣지 않는다.
- C-4: API 키는 문서·GIF·로그·대화에 노출하지 않는다. 데모는 사용자가 등록한 `~/.dynamic_react/auth.json` 을 프로그램이 직접 읽게 하며, 작업자는 그 파일을 열지 않는다.
- C-5: 문서 문장은 fluent-korean 규칙과 ASD-STE100(약 80% 완화)을 따른다. 둘이 충돌하면 중요한 내용을 줄이지 않는 쪽을 택한다.

### 2.3 가정

- A-1 (중요): 데모에 쓰는 등록 모델은 API 키(`type: api_key`) 경로다. 코드가 이 형식만 지원한다 (`L2_harness/agent/model.go` 의 `authAPIKey`).
- A-2: GitHub 저장소는 나중에 `Biisairo/dynamic_react` 로 만든다. 배지 URL 은 이 경로를 기준으로 한다.
- A-3: Go 버전은 `go.work` 의 1.23 이상이다.

## 3. 요구사항

### 3.1 문서 (DOC)

- PF-FR-01: README 는 다음 순서의 장을 가진다. ① 한 줄 소개와 배지 ② 데모 GIF ③ 만든 이유(해결하려는 문제) ④ 핵심 아이디어(루프 고정 + 주입) ⑤ 아키텍처(레이어 그림, 의존 방향) ⑥ 빠른 시작(설치, 키 등록, 실행) ⑦ 주입 예시 코드 한 개 ⑧ 설계에서 내린 주요 결정과 이유 ⑨ 품질 지표(모듈 수, 코드 줄 수, 테스트 수, 모듈별 커버리지) ⑩ 개발 과정(SDD·TDD, 작업 기록 위치) ⑪ 문서 안내 ⑫ 서드파티 고지.
- PF-FR-02: README 본문은 내부 결정 번호(R·D·G·FR 번호)가 없어도 뜻이 통해야 한다. 번호는 근거 링크 용도로만 괄호 안에 쓴다.
- PF-FR-03: README 는 저장소 밖 경로를 참조하지 않는다.
- PF-FR-04: README 의 "빠른 시작" 명령은 깨끗한 클론에서 그대로 실행된다.
- PF-FR-05: 현재 README 에 있는 정보 중 사용법 정보(설정 파일, 세트 배치, 모드, 주입 방법)는 README 또는 `docs/` 의 기존 문서 링크로 남는다. 정보가 사라지지 않는다.
- PF-FR-06: 내부 작업 기록을 `docs/history/` 로 `git mv` 한다. `docs/history/README.md` 에 각 문서의 한 줄 설명을 둔다.
- PF-FR-07: 저장소 안의 모든 상대 링크(`.md`, `.html`)는 이동 뒤에도 실제 파일을 가리킨다.

### 3.2 저장소 (REPO)

- PF-FR-10: 루트에 golangci-lint v2 설정 `.golangci.yml` 을 둔다. 활성 린터는 2.1 절의 8종이다.
- PF-FR-11: 설정은 의도적으로 무시하는 표준 패턴(`fmt.Fprint*`, `io.Closer.Close`, 테스트 파일의 errcheck)만 제외한다. 제외 항목마다 이유를 주석으로 단다.
- PF-FR-12: (v0.3 에서 삭제) CI 워크플로우는 두지 않는다. 전 모듈 검증은 로컬에서 실행한다.
- PF-FR-13: `.gitignore` 에 커버리지 산출물을 추가한다.

### 3.3 코드 품질 (CODE)

- PF-FR-20: `TestFollowUp` 은 `go test -race -count=50` 에서 실패 0회다. 수정은 테스트 파일에만 한다 (기존 `blockingLLM` 도우미로 첫 스트림을 붙잡는다).
- PF-FR-21: PF-FR-11 제외 뒤 남은 지적 중 동작 보존 수정만 코드에 적용한다.
- PF-FR-22: 동작 위험 지적은 코드에 적용하지 않고 `docs/history/LINT-REPORT.md` 에 목록(파일:줄, 린터, 내용, 위험 이유)으로 남긴다. 사용자에게 건별로 결정을 받는다.
- PF-FR-23: 사용자가 결정하기 전까지 동작 위험 지적은 해당 줄의 `//nolint:<linter> // LINT-REPORT #n` 으로 표시한다. 이유 없는 `nolint` 는 쓰지 않는다.
- PF-FR-24: 모든 수정 뒤 전 모듈 검증이 통과한다.

### 3.4 데모 (DEMO)

- PF-FR-30: `docs/demo/demo.tape` (vhs 스크립트)와 그 결과 `docs/demo/demo.gif` 를 둔다.
- PF-FR-31: 데모는 임시 폴더의 작은 Go 예제 프로젝트에서 `dr` 를 실행하고, 에이전트가 파일을 읽고 수정한 뒤 테스트를 실행하는 한 번의 대화를 보여준다.
- PF-FR-32: GIF 는 40초 이하, 5MB 이하다.
- PF-FR-33: GIF·tape 에 API 키, 홈 경로의 사용자 이름, 개인 정보가 나타나지 않는다.
- PF-FR-34: `docs/demo/README.md` 에 GIF 재생성 방법을 적는다.

## 4. 검증

| 요구사항 | 방법 | 통과 기준 |
|---|---|---|
| PF-FR-01~03 | 검토 | 장 순서 일치, 내부 번호 없이 읽힘, `../` 밖 경로 없음 (`grep`) |
| PF-FR-04 | 시험 | 임시 폴더에 `git clone` 뒤 빠른 시작 명령 실행 성공 |
| PF-FR-05 | 검토 | 현재 README 장별 정보의 새 위치 대조표 |
| PF-FR-06·07 | 시험 | 링크 검사 스크립트: 모든 상대 링크 대상 존재 |
| PF-FR-10·11 | 시험 | 로컬에서 각 모듈 `golangci-lint run` 0건 |
| PF-FR-20 | 시험 | `go test -race -count=50 -run TestFollowUp` 실패 0 |
| PF-FR-21~24 | 시험·검토 | 전 모듈 검증 통과, 수정 diff 마다 동작 보존 근거 |
| PF-FR-30~34 | 시험·검토 | `vhs` 재생성 성공, 길이·크기 측정, 프레임 육안 점검 |

## 5. 구현 순서

1. PF-FR-20 (flaky 테스트) → 실패 재현 확인 → 수정 → `-count=50`.
2. PF-FR-10·11 (린트 설정) → 남은 지적 분류 → PF-FR-21 수정 → PF-FR-22·23 보고.
3. PF-FR-13 (.gitignore).
4. PF-FR-06·07 (문서 이동, 링크 검사).
5. PF-FR-30~34 (데모).
6. PF-FR-01~05 (README). 품질 지표 수치는 2~5단계가 끝난 뒤 측정한다.

## 6. 리스크

| 리스크 | 등급 | 대응 |
|---|---|---|
| 린트 수정이 동작을 바꿈 | MEDIUM | 동작 위험 지적은 보고만 한다 (PF-FR-22) |
| 데모 실행이 모델 응답에 따라 매번 다름, 비용 발생 | LOW | 짧은 프롬프트 한 개, 결과를 확인한 뒤 GIF 채택 |
| 데모 화면에 키·사용자 정보 노출 | MEDIUM | 임시 HOME 이 아닌 등록 설정을 쓰므로 프롬프트·경로를 tape 에서 고정하고 프레임을 점검한다 (PF-FR-33) |
| 문서 이동으로 링크 깨짐 | LOW | 링크 검사 스크립트 (PF-FR-07) |

## 7. 변경 기록

| 버전 | 내용 |
|---|---|
| v0.1 | 초안 |
| v0.2 | 구현 중 바뀐 내용을 반영했다. ① PF-FR-11: 설정 제외에 QF1001(가독성), `L2_harness/harness/tools/` 의 ST1005(pi 도구 문안), `envtest/` 의 errcheck(테스트 도우미)를 더했다. 제외마다 이유를 주석으로 달았다. ② PF-FR-23: 오탐 4건에도 이유를 단 `nolint` 를 썼다. 결정 대기 항목만 `LINT-REPORT #n` 을 단다. ③ PF-FR-31: 데모 설정은 예제 폴더에서 bash 를 모두 허용하고, `AGENTS.md` 로 `ls -la` 를 막는다 (PF-FR-33). ④ PF-FR-01: README 에 그림 두 장(`docs/images/`, `architecture.html` 캡처)과 "모드"·"상태" 장을 더했다. |
| v0.3 | 사용자 결정으로 CI 를 범위에서 뺐다 (PF-FR-12 삭제, README 의 CI 배지 제거). LINT-REPORT #1 은 권장안(`null` 을 `{}` 로 취급)대로 고친다. |
