# 인계: impl-08 — impl-07 결과와 R26 구현 착수

## 착수 블록 (다음 세션의 지시)

`dynamic_react` 에서 진행한다. **할 일: R26 구현.** 스펙은 커밋 `70d8724` 에 확정돼 있다 (사용자 검토 "진행" 완료). 스펙 → 테스트(RED 확인) → 구현(GREEN) 순서를 지킨다.

1. `git status` 로 작업 트리가 깨끗한지 확인한다 (직전 커밋은 이 문서 커밋, 그 앞이 `70d8724`).
2. 먼저 읽을 것 (이 순서로):
   - `docs/DECISIONS.md` 의 **R26** (마지막 행) — 결정 전부
   - `L1_inject/inject/SPEC.md` 3.5 (IN-FR-18·19·21·60~63), 3.6 (IN-FR-23), 4절 IN-FR-60~63 행, 7절 0.4
   - `L1_inject/sets/SPEC.md` **3.0 세트 분해 표**(ST-FR-51~53), 3.1(동작 표, 그대로), ST-FR-03·04·41, 3.2a(verify, `Next` 옵션 삭제), 4절 ST-FR-51~53 행
   - `L2_harness/SPEC.md` 3.6c (HN-FR-38·39·41), **3.6d** (HN-FR-51~54), 4절 HN-FR-51~54 행
   - `L2_harness/agent/SPEC.md` AG-FR-02, 3.14 (Sets 타입), **3.14a** (AG-FR-44~46)
   - `L3_ui/surfaces/terminal/SPEC.md` TM-FR-55·56 (3.8), 4절
   - `examples/inject/SPEC.md` v0.2, `examples/verify/SPEC.md` v0.3
3. 구현 순서 (각 단계: 테스트 작성 → `go test` 로 RED 확인 → 구현 → GREEN → 다음 단계):
   1. **inject**: 생성기 `internal/gen/main.go` 에 step 마다 `XxxCtx.Next(...)` 생성 → `go generate` → `zz_generated.go`. `dispatcher.go`: `fns FunctionSet` + `owner map` 을 step 별 **층 스택**(함수, 세트 이름)으로. `Next` 는 같은 `invocation`·같은 `base` 의 ctx/view 로 아래 층을 부르되 **상태 ctx 는 아래 층 세트 이름의 네임스페이스**. dispatch 성공 시 쓰인 네임스페이스마다 commit·`StateStore.Save`. 맨 아래에서 `Next` → `ErrNoNext` 감싼 에러. `FunctionSet.Description`. `Binding()`·트레이스 `Set` = 맨 위 층. 기존 inject 테스트는 그대로 통과해야 한다 (Next 안 부르면 이전 동작)
   2. **sets**: `base.go` 를 `Base`(1·2·5·14·16 통과·17·18 통과·19), `Require`(1 = `ctx.Next` 후 짝 복구, 3·4·10·13·21·22 no-op, 6, 7 짝 복구만, 8, 9, 11, 12 fail, 15, 20), `Cache`(7 = Next 후 캐시 힌트), `Guard`(16 검사 후 Next), `Truncate`(18 = Next 결과 절단)로 분해. `Base<Step>` exported 생성자 삭제. `verify.go` 의 `VerifyOptions.Next` 삭제 → `ctx.Next()`. **동등성**: 기존 base·resilient·e2e 테스트의 바인딩 헬퍼(`helpers_test.go` `agent()`, `base_test.go` 의 step 단위 `step()` 헬퍼 등)를 `Base, Require, Cache, Guard, Truncate(, Resilient)` 로 바꿔 전부 통과 (ST-FR-51). 새 테스트는 SPEC 4절 ST-FR-51~53 행
   3. **harness**: `SetEntry`·`SetState`·`DefaultSets`(6개 전부 On)·`Options.Sets`/`Layout`·`Agent.Sets()`·`Arrange`·`ErrSetLayout`. `surface.go` `wrap` 은 켜진 세트 목록에서 step 마다 맨 위 층 함수를 감쌈(지금 "last" 로직 그대로 적용 가능). `Arrange` 성공 시 `a.disp`·`a.kernel` 교체(뮤텍스), `Start` 는 그 시점 것을 사용 (`harness.go` 179~ `Start` 는 `a.kernel`/`core.New(a.disp, opts)` 사용 중)
   4. **agent**: 설정 `sets` 파싱·검사 → `Layout`, `Sets()`·`ArrangeSets`(성공 후 설정 파일 `sets` 저장 — `SetModel` 의 파일 쓰기 코드 재사용, 실패 시 harness 배치 되돌림), `Options.Sets` 타입 변경
   5. **terminal**: `/sets`·`/set <name> on|off|to <n>`, `/help` 명령 목록에 추가
   6. **examples**: inject(`sets.BaseToolBefore` → `ctx.Next(call)`, `[]harness.SetEntry`), verify(`[]harness.SetEntry`)
   7. 문서: README(버전·DefaultSets 설명·`/sets`), `docs/INJECT.md`(Sets 타입·체인·배치), `docs/VERIFY-22step.md` 에 R26 절(선택)
4. 검증: 아래 "검증 절차". 끝나면 사용자에게 결과 보고 후 **커밋 확인**(사용자는 지금까지 "한 커밋으로"를 골랐다). 그다음 dr 실제 시연(`/sets`, `/set guard off` 등)을 dongminal 로 옆 칸에서 보여 주면 좋다 (아래 "시연 방법").

**하지 말 것**: 구독 계정 로그인 구현(다시 꺼내지 말 것). 환경변수 읽기 추가 (R21). L3 가 설정 파일을 직접 읽거나 쓰기 (R17 — 설정 저장은 agent). core(L0) 수정 (R26 은 core 를 건드리지 않는다). sets·inject 에 순서·기본 목록 넣기 (R26 ③ — 순서 지식은 harness `DefaultSets` 한 곳). 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃.

## impl-07 에서 한 일 (커밋)

| 커밋 | 내용 |
|---|---|
| `015aea8` | R23 탐색 도구 `grep`·`find`·`ls` (harness v1.1 3.2a, Env 위 순회·`respectIgnore`·glob, `env.FileInfo.Symlink`, osenv v0.3, agent v0.7 경로 패턴 규칙). 사용자 결정: 기본은 전부 탐색 + `respectIgnore`, 승인은 read 와 같게 |
| `1c59431` | R24 자기검증 세트 `sets.Verify` (sets v0.6 3.2a) + `examples/verify`. 결정: L1 세트·Sets 주입으로만, 변경 도구를 썼을 때만 검증, MaxAttempts 번째 실패 = run 실패 |
| `a20d8c7` | R25 harness `input` 이벤트(turn.start 메시지마다), terminal `» ` 줄(자기가 보낸 ID 는 건너뜀), `examples/verify -i` 대화형, `TestRunCompact` 대기 경합 보강 |
| `70d8724` | **R26 스펙** (inject v0.4, sets v0.7, harness v1.3, agent v0.8, terminal v0.5, examples) — 구현 전 |

실제 모델(deepseek-flash) 확인: verify 세트가 "Add 만 고쳐라" 프롬프트에서 Mul 테스트 실패를 잡아 보고 → 모델이 Mul 까지 고침 → 통과. 대화형(`-i`)에서도 `» Verification failed …` 줄이 보임. 관찰: 검증 보고("Fix the problems below")가 사용자 지시보다 우선됨 (모델이 답에 밝힘) — 바꿀지는 미결.

## R26 인터뷰 기록 (사용자 답 그대로)

| # | 질문 | 사용자 답 |
|---|---|---|
| 1 | 적용 시점 | a: run 사이 (다음 run 부터) |
| 2 | 바꾸는 단위 | "base 를 수정해서 입출력만 담당하는 완전 기본으로 만들고 규칙은 별개의 set 으로 … base 위에 이름 붙은 세트를 등록 … A 로" |
| 3 | base 분류 | "L2, L3 에 의존성이 있는것들은 base, 동작에 필수적인것들은 require, 그 외의 실제 로직들은 구분에 맞춰서" (그 전 답: "repair 는 base 에 남김" → require 로 들어감) |
| 4 | 겹치기 | 체인 바인딩 + "base, require 도 그냥 들어가는게 아닌 사용자가 주입하는거야. 그래야 완전 튜닝까지도 가능해지니까" |
| 5 | 체인 API | a: `ctx.Next()` |
| 6 | 저장 | b: 설정 파일에 저장 (R22 비목표 뒤집음) |
| 7 | 목록·순서 | "목록이나 순서같은건 전부 사용자가 정하는걸로, base, require 포함해서" / "이걸 core 가 해주면 위반" → 기본값은 L2 harness 프리셋(교체 가능), L0·L1 은 순서 모름 |
| 8 | 실행 중 범위 | "순서도" (켜기·끄기 + 순서, 저장) |
| 9 | 스펙 검토 | "진행" → 스펙 커밋 후 새 세션에서 진행 |

스펙에 적힌 가정(사용자에게 알렸음): 정책 세트는 `ctx.Next` 로 아래 층을 부르므로, 순서를 바꿔 같은 step 에 아래 층이 없으면 **실행 시점에** `ErrNoNext` 로 run 실패(Bind 는 알 수 없음). 저장된 배치로 Bind 실패 시 `New` 에러(조용한 폴백 없음). resilient 는 Next 없이 그대로.

## 현재 코드 구조 (R26 이 바꿀 곳)

| 위치 | 지금 |
|---|---|
| `L1_inject/inject/dispatcher.go` | `Dispatcher{fns FunctionSet; owner map[core.Step]string}`, `overlay(&d.fns, s, d.owner)` 로 마지막 함수만, `dispatch` 가 `base{…, st: newStateCtx(ns, run)}` 하나로 `invoke(&d.fns, b, step, input)`, 성공 시 `b.st.commit()` 한 번·`Save(ns)` 한 번 |
| `L1_inject/inject/internal/gen/main.go` | `zz_generated.go` 생성기 (401줄). `ctx.go:5` 에 `//go:generate go run ./internal/gen`. `gen_test.go` 가 생성 결과 최신 여부를 검사 |
| `L1_inject/sets/base.go` | `Base(d)` 가 22 step 을 `Base<Step>(d)` 로 채움. `BaseToolBefore`(352, unknown tool·스키마), `BaseToolExecute`(372, 없는 도구면 `unknown tool:` 결과 — guard 를 꺼도 이 경로가 남음), `BaseTurnFinish`(459) |
| `L1_inject/sets/verify.go` | `VerifyOptions.Next` (nil → `BaseTurnFinish`) |
| `L2_harness/harness/harness.go` | `Options.Sets func(sets.Deps) []inject.FunctionSet`, 140 `inject.BindWith`, 152 `Agent{kernel, disp, …}`, 179 `Start`. `DefaultSets` = `[Base, Resilient]` |
| `L2_harness/harness/surface.go` 175 `wrap` | step 마다 `last(...)` 로 마지막으로 그 step 을 채운 세트의 함수를 감쌈 (ToolBefore Gate·이벤트·EventInput 등) |
| `L2_harness/agent` | `SetModel` 이 사용자 설정 파일을 다시 쓰는 코드 (AG-FR-34) — `ArrangeSets` 저장에 재사용 |
| 사용처 | `examples/inject/main.go:40` `sets.BaseToolBefore(d)`. 테스트: `L1_inject/sets/base_test.go`, `L2_harness/harness/surface_test.go`·`inject_test.go` 가 `sets.Base<Step>` 사용 |

## 이 세션이 값을 치르고 배운 것

- **Transcript 는 History 로 시작한다** (core CORE-FR-31a). "이번 run 의 처음" 기준은 0 이 아니라 `len(Conv().History())`.
- **`TurnIndex()` 는 0 부터가 아니다** (첫 턴 판별에 쓰지 말 것 — 테스트에서 호출 횟수로 판별).
- **신호에는 `Kind()` 가 있어** 아래 함수의 신호를 들여다보고 settle 만 빼는 식의 감싸기가 된다 (verify 가 사용).
- **Bind 는 마지막 함수만 남긴다** → 이것이 R26 체인 바인딩의 동기.
- harness 이벤트를 정확한 순서로 비교하는 테스트가 있다(`TestEventsTextTurn`·`TestEventsStreamError`) — 이벤트를 더하면 기대값이 바뀐다.
- terminal 테스트 `TestRunCompact` 는 `── completed` 가 그려진 뒤 대기 상태로 바뀌기 전 틈이 있어 `waitFn("idle")` 이 필요했다 (같은 패턴 주의).
- 대화형 테스트(파이프 stdin + 가짜 `isTerminal`·`open`)에서 **Approver 가 붙으므로 write 등이 승인을 묻고 멈춘다** → 테스트에서는 `agent.Options.Permissions{Allow: …}` 로 허용.
- **dmctl `send-input` 은 bracketed paste 로 들어가서** 터미널 승인 프롬프트(y/a/n 키)를 누를 수 없다. 대화형 시연에서 승인은 사용자가 직접 누르거나, 시연용 프로젝트 설정 `<cwd>/.dynamic_react/config.json` `{"permissions":{"allow":["read","edit","write","bash"]}}` 로 미리 허용한다. 프롬프트 제출은 `send-input --execute ""`(자동 엔터)로.
- dmctl `read-screen` 은 다시 그리기까지 쌓인 스트림이라 스트리밍 답이 한 글자씩 반복돼 보인다 — 결과 확인은 세션 JSONL 로 하는 편이 정확하다.
- macOS 에 `timeout` 이 없다. 오래 걸리는 go test 는 `-timeout` 플래그와 Bash 도구 timeout 을 쓴다. 테스트가 멈추면 `go test -timeout 40s -run <이름>` 으로 스택을 본다.
- `go test` 를 10분 넘게 붙잡는 명령을 foreground 로 돌리면 백그라운드로 밀린다 — 전체 모듈 검증은 timeout 600000 으로.
- 새 모듈 의존은 `go mod edit -require=…@v0.0.0 -replace=…=../../…` 후 `GOWORK=off GOTOOLCHAIN=local go mod tidy` (원격 조회 실패 방지).
- `/tmp/mut.py` 는 있었다 (다음에도 있다고 가정하지 말 것): `python3 /tmp/mut.py <모듈 디렉터리> <파일> <뮤턴트 JSON 파일>`, JSON 은 `[[old,new],…]`, old 는 파일에서 정확히 1회. 프로세스 치환 대신 heredoc 으로 파일을 써서 넘길 것.
- 실제 모델 확인: 임시 HOME(`/tmp/e2e/h-*`)에 `~/.dynamic_react/auth.json` 의 provider 항목만 0600 으로 복사(키 값 출력 금지), 설정 `{"agent":{"model":{"provider":"deepseek","id":"deepseek-flash"}}}`, `GOCACHE`·`GOMODCACHE` 를 원래 값으로 넘김, 끝나면 `/tmp/e2e` 삭제. 외부 API 호출이므로 사용자 확인 후.

## 검증 절차 (지금까지 매번 한 것)

- 모듈 13개 전부: `gofmt -l`·`go vet ./...`·`go test -race -count=2 ./...`·`GOWORK=off GOTOOLCHAIN=local go mod tidy && GOWORK=off go test ./...`. 모듈 목록은 `go work edit -json` 의 `Use`.
- 테스트 아닌 코드에 `os.Getenv`·`os.LookupEnv`·`os.Environ` 없음 (`grep -rn … --include='*.go' . | grep -v _test.go`).
- 새 코드 커버리지 확인, 핵심 분기 뮤테이션(전부 KILLED 될 때까지 테스트 보강, 동치면 코드 삭제).
- 동작 변경은 SPEC 변경 기록에 이전 / 새 / 이유.

## 시연 방법 (dongminal)

`dmctl who-am-i` → `dmctl focus <자기 uuid>` → `dmctl split-h` → 새 칸 uuid 로 `dmctl send-input --at <uuid> --execute "<명령>"`, 끝은 `until dmctl read-screen … | grep -q …; do sleep 1; done` 를 `run_in_background` 로. 닫기는 `dmctl close-tab --at <uuid> --force`.

## 변하지 않는 규약

- 응답·문서·주석은 한국어. 식별자·기술 용어는 원문. 미사여구 없이 본론만.
- SDD → TDD 고정: 스펙 → 테스트(실패 확인) → 구현(통과). 모든 동작에 unit / edge / failure 테스트. 결정론적.
- 스펙은 IEEE 29148. 구현 중 정한 세부는 스펙에 반영하고 보고 때 "검토 부탁". 스펙에 없는 Go 표현(새 export)은 구현 전에 스펙에 먼저.
- 스펙과 코드가 충돌하거나 스펙으로 결정할 수 없으면 멈추고, 한 번에 하나씩 권장안과 함께 묻는다. 질문이 추상적이면 "지금 상태 / 목표 / 안" 표로.
- 커밋은 사용자 확인 후에만. 커밋 메시지에 AI 서명(`Co-Authored-By` 등) 금지.
- 사용자가 요청하지 않은 병렬 서브에이전트 팬아웃 금지.
- 심볼 탐색·수정: LSP → Serena → CLI.
- 모듈 간 require 는 `v0.0.0` + `replace`. 새 모듈은 go.mod 와 go.work 에 둘 다. `go 1.23.0`. 외부 의존: x/text(harness·terminal), x/term(terminal). 새 외부 의존은 사용자 결정.
- 키를 코드·대화·문서·이미지에 노출하지 않음. 키는 `<home>/.dynamic_react/auth.json`(0600)에만. 구독 요금제 엔드포인트·구독 OAuth 금지. 환경변수를 읽지 않음 (예외: 홈 디렉터리·자식 프로세스 상속·테스트 전용 `DR_E2E_*`).
- 라이선스: 서드파티 문안·데이터를 새로 가져오면 `THIRD_PARTY_NOTICES.md` 에 더한다.
- 사용자는 레이어 책임을 엄격히 본다 (L0 core 는 아무 기본값·순서도 모름, 목록·검증·저장은 L2, L3 는 입출력만).

## 사용자 결정 중 유효한 것

- D1~D16, R1~R26 (R10 폐기, R14 는 R17 로 정정, R22 의 비목표 두 개는 R26 이 뒤집음).
- 이 저장소 자체의 LICENSE 는 미정 (묻지 않았으면 손대지 말 것).

## 남은 미결 (R26 이후)

| 위치 | 내용 |
|---|---|
| verify | 검증 보고가 사용자 지시보다 우선되는 문구 — 바꿀지 사용자 미결정 |
| dr | 세트 등록은 `DefaultSets` 프리셋만 (verify 처럼 인자가 필요한 세트는 자기 main 에서) |
| 세트 후보 | 예산 가드(측정 기준 미결: 누적 토큰 vs 현재 컨텍스트 크기), plan→act, 반복 감지, 모델 라우팅 |
| R26 비목표 | run 도중 교체, 부품 실행 중 교체, 프로젝트별 배치, Bind 시점 Next 검사 |
| 기타 (impl-07 이전부터) | 저장소 LICENSE, Anthropic·OpenAI 방언 실제 응답 검증, overflow 복구 실제 모델 확인, agent 사용자 도구의 패턴 비교 값(`Target func`) |
