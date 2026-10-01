# terminal — 소프트웨어 요구사항 명세 (SRS)

- 문서 형식: ISO/IEC/IEEE 29148:2018 SRS 구조를 따른다.
- 버전: 0.2 (초안)
- 상위 근거: `docs/DECISIONS.md` (R8, R17, R18, R19, R20)
- 의존 명세: `L2_harness/agent/SPEC.md` v0.4 (3.4~3.12), `L2_harness/SPEC.md` v0.8 (3.6a Event)
- 사용하는 쪽: `L3_ui/cmd/dr` (`L3_ui/cmd/dr/SPEC.md`)

---

## 1. 소개

### 1.1 목적
완성된 에이전트(`L2_harness/agent`)를 쓰는 **대화형 터미널 surface** 라이브러리다. L3 이므로 입출력만 한다 (R17, R18): 키 입력을 `Start`·`Steer`·`FollowUp`·`Abort`·`Compact` 로 잇고, `OnEvent` 를 그리고, `Approver` 를 터미널 질문으로 구현하고, 세션 목록·이어가기 화면을 낸다. 승인 판정·세션 잠금·FollowUp 연쇄·압축은 agent 의 몫이다.

### 1.2 범위
- 포함 (모듈 `L3_ui/surfaces/terminal`, 패키지 `terminal`): raw mode 진입(`Open`), 키보드 프로토콜 협상, 키 해석, 입력 줄 편집, 출력 렌더링, 승인 프롬프트, 슬래시 명령, 세션 선택
- 제외: 플래그·`agent.New`·시그널·종료 코드 (`cmd/dr`), 마크다운 렌더링, 이미지 첨부, 풀스크린(대체 화면), 마우스, 방향키 목록 선택, 모델 전환, 키 바인딩 설정

### 1.3 참조

| 값 | 출처 |
|---|---|
| 형태 B (REPL + raw mode, 덧붙이기 출력 + 맨 아래 입력 줄), 모듈 위치, 키 바인딩, 승인 UX, 렌더링, 세션, 슬래시 명령, 비TTY | 사용자 확정 (impl-04 인터뷰 ①~⑧) |
| kitty 키보드 프로토콜 → modifyOtherKeys 협상, 줄바꿈 `Shift+Enter`·`Ctrl+J` | pi `packages/tui/src/terminal.ts`·`keybindings.ts` |
| 실행 중 Enter = steer, Alt+Enter = follow-up, Esc = 중단 | pi `packages/coding-agent/src/core/keybindings.ts` |

---

## 2. 전체 설명

### 2.1 제품 관점

```text
 cmd/dr ── restore, width, err := terminal.Open(os.Stdin)        (TTY 확인 + raw mode)
        ── t := terminal.New(Options{In, Out, Width, Color})
        ── a := agent.New(ctx, agent.Options{..., Approver: t.Approver(), OnEvent: t.OnEvent})
        ── err := t.Run(ctx, a, Start{Session, Continue, Pick})   (사용자가 끝낼 때까지)
        ── restore()

 Run ── 키 읽기 고루틴: In → 키 해석(3.2) → 키 이벤트
     ── 메인 루프: 상태(대기 / 실행 중 / 승인 / 압축 / 세션 선택) × 키 → 동작(3.3)
     ── OnEvent(agent 고루틴) ─┐
     ── Approve(agent 고루틴) ─┼─ 출력 잠금 하나로 직렬화 → 화면(3.4)
     ── 입력 줄 다시 그리기 ───┘
```

화면 구조: 위쪽은 터미널 스크롤백에 **덧붙이기만** 하는 출력, 맨 아래는 다시 그리는 영역(줄바꿈으로 끝나지 않은 출력 꼬리 + 상태 줄 + 입력 줄). 이미 줄바꿈으로 끝난 출력은 고치지 않는다.

모듈 의존: terminal → agent, harness(Event 타입), contract, `golang.org/x/term` v0.34.0(+`x/sys` v0.35.0, R19), `golang.org/x/text` v0.28.0(`width`, R15), 표준 라이브러리.

### 2.2 제약
- TM-C1. 에이전트의 동작(승인 판정, 잠금, 연쇄, 압축, 세션 형식)을 구현하지 않는다. agent 공개 API 만 쓴다 (R17).
- TM-C2. 키 값·환경변수를 읽지 않는다. 색 사용 여부는 `Options.Color` 로 받는다.
- TM-C3. 키 해석·입력 줄 편집·렌더링은 `io` 위에서 동작해 TTY 없이 테스트할 수 있다. TTY 의존은 `Open` 에만 있다.
- TM-C4. Go 1.23 호환. 외부 의존은 2.1 의 둘 뿐.

---

## 3. 상세 요구사항

### 3.1 공개 API

```go
type Options struct {
    In         io.Reader        // 키 바이트 (raw mode TTY)
    Out        io.Writer
    Width      func() int       // 터미널 열 수. nil 이거나 1 미만이면 80
    Color      bool             // ANSI SGR 색 사용
    Now        func() time.Time // 세션 목록의 상대 시각, 메시지 Timestamp. nil → time.Now
    EscTimeout time.Duration    // 단독 Esc 판별 대기 (3.2). 0 → 50ms
}

type Start struct {
    Session  string // 이 세션으로 시작 (없는 ID 면 그 ID 의 새 세션)
    Continue bool   // 가장 최근 세션 (없으면 새 세션)
    Pick     bool   // 시작 시 세션 목록에서 고른다
}

func New(o Options) *Terminal
func (t *Terminal) Approver() agent.Approver
func (t *Terminal) OnEvent(ev harness.Event)
func (t *Terminal) Resize()
func (t *Terminal) Run(ctx context.Context, a *agent.Agent, s Start) error

func Open(f *os.File) (restore func() error, width func() int, err error)
func IsTerminal(fd int) bool
var ErrNotTerminal error
```

| id | 요구사항 |
|---|---|
| TM-FR-01 | `Open(f)`: `f` 가 TTY 가 아니면 `ErrNotTerminal`. 맞으면 `term.MakeRaw` 로 raw mode 에 들어가고, 원래 모드로 되돌리는 `restore`(여러 번 불러도 됨)와 `term.GetSize` 의 열 수를 돌려주는 `width`(실패하면 0)를 돌려준다. stdout 이 TTY 인지는 호출자가 `Open` 과 같은 판별(`term.IsTerminal`)로 따로 본다 — `IsTerminal(fd int) bool` 을 함께 공개한다 |
| TM-FR-02 | `Start` 의 Session·Continue·Pick 은 하나만 쓸 수 있다. 둘 이상이면 `Run` 은 아무것도 출력하지 않고 에러를 돌려준다 |
| TM-FR-03 | `Run` 은 사용자가 끝내면(3.3 종료) `nil`, ctx 가 취소되면 실행 중인 run 을 `Abort` 하고 `Wait` 가 끝난 뒤 `ctx.Err()` 를 돌려준다. 어느 경우든 돌아가기 전에 3.2 의 키보드 프로토콜과 bracketed paste 를 해제하고, 다시 그리는 영역을 지우고, 커서를 새 줄 시작에 둔다. `In` 읽기 에러(EOF 포함)는 사용자 종료와 같다 |
| TM-FR-04 | `Run` 은 한 Terminal 에서 한 번만 부를 수 있다. 두 번째 호출은 에러 |
| TM-FR-05 | `OnEvent`·`Approver().Approve` 는 `Run` 중이 아닐 때 불리면 `OnEvent` 는 무시하고, `Approve` 는 `Decision{Kind: "deny", Reason: "terminal not running"}` 을 돌려준다 |
| TM-FR-06 | `Resize()`: 다음 그리기부터 `Width()` 를 다시 읽고 다시 그리는 영역을 새 폭으로 다시 그린다. 이미 덧붙인 출력은 건드리지 않는다. 아무 고루틴에서나 불러도 된다 |

### 3.2 터미널 모드와 키 해석

| id | 요구사항 |
|---|---|
| TM-FR-07 | `Run` 시작 시 `Out` 에 bracketed paste 켜기(`CSI ? 2004 h`)와 kitty 키보드 프로토콜 질의(`CSI > 1 u` `CSI ? u` `CSI c`)를 쓴다. 응답으로 `CSI ? <flags> u` 가 DA 응답(`CSI ? … c`)보다 먼저 오면 **kitty 모드**. DA 만 오면 `CSI > 4 ; 2 m` 을 써서 **modifyOtherKeys 모드**. 1초 안에 DA 가 오지 않으면 **기본 모드**(아무것도 켜지 않음). 협상 응답은 키 해석기가 받아 협상에만 쓰고 키로 처리하지 않는다. 협상 중 들어온 다른 키는 바로 처리한다 (해석기가 세 인코딩을 모두 받으므로 협상 결과와 무관하게 같은 키다) |
| TM-FR-08 | 해제: kitty 모드면 `CSI < u`, modifyOtherKeys 모드면 `CSI > 4 ; 0 m`, 그리고 `CSI ? 2004 l` |
| TM-FR-09 | 키 해석은 세 인코딩을 모두 받는다 (모드와 무관하게 — 터미널이 섞어 보내도 같은 키): 기본(제어 바이트, `ESC` 접두 Alt, `CSI`/`SS3` 방향키·Home·End·Delete), kitty `CSI <code> [; <mod>] u`, modifyOtherKeys `CSI 27 ; <mod> ; <code> ~`. 키 하나가 여러 번의 read 로 쪼개져 와도 같은 키다. UTF-8 문자도 쪼개져 올 수 있다 |
| TM-FR-10 | 해석 결과 키 (아래 표 외의 키·시퀀스는 무시) |

| 키 | 기본 인코딩 | kitty / modifyOtherKeys |
|---|---|---|
| Enter | `\r` (`\n` 은 Ctrl+J) | `\r`, `CSI 13 u` / `CSI 27;1;13~` |
| Shift+Enter | (구분 불가 = Enter) | `CSI 13;2 u` / `CSI 27;2;13~` |
| Alt+Enter | `ESC \r` | `CSI 13;3 u` / `CSI 27;3;13~` |
| Ctrl+J | `\n` | `CSI 106;5 u` / `CSI 27;5;106~` |
| Esc | `ESC` 뒤 EscTimeout 동안 다음 바이트 없음 | `CSI 27 u` / `CSI 27;1;27~` |
| Ctrl+C, Ctrl+D | `0x03`, `0x04` | `CSI 99;5 u`·`CSI 100;5 u` / `CSI 27;5;99~`·`CSI 27;5;100~` |
| Ctrl+A, Ctrl+E | `0x01`, `0x05` | `CSI 97;5 u`·`CSI 101;5 u` / `CSI 27;5;97~`·`CSI 27;5;101~` |
| Backspace | `0x7f`, `0x08` | `0x7f`, `CSI 127 u` |
| Delete | `CSI 3 ~` | 같음 |
| ←→↑↓ | `CSI D/C/A/B`, `SS3 D/C/A/B` | 같음 (수식자 붙은 형태 `CSI 1;<mod> X` 도 같은 키) |
| Home, End | `CSI H`·`CSI F`·`CSI 1 ~`·`CSI 4 ~`·`SS3 H/F` | 같음 |
| 문자 | 출력 가능한 UTF-8 (Tab 포함) | 같음, kitty `CSI <code> u`(수식자 없음)도 문자 |
| 붙여넣기 | `CSI 200 ~` … `CSI 201 ~` 사이 전부 = 문자열 하나 (안의 `\r\n`·`\r`·`\n` 은 줄바꿈 하나, `\t` 외의 제어 바이트는 버림) | 같음 |

### 3.3 상태와 키 동작

상태: **대기**(run 없음), **실행 중**(`Start` 한 run 의 `Wait` 가 아직 안 끝남), **승인**(Approve 대기), **압축**(`/compact` 진행), **선택**(세션 번호 입력), **모델 선택**(3.9).

| id | 요구사항 |
|---|---|
| TM-FR-11 | 입력 줄 편집 (대기·실행 중 공통): 문자·붙여넣기 = 커서 위치에 삽입, Shift+Enter·Ctrl+J = 줄바꿈 삽입, Backspace·Delete = 커서 앞·뒤 문자 하나 삭제, ←→ = 문자 단위 이동, Home·Ctrl+A / End·Ctrl+E = 현재 줄의 처음·끝. ↑↓ = 이 프로세스에서 보낸 입력의 기록(최신이 ↑ 첫 번째, 기록 끝에서 ↓ 는 쓰던 입력으로 복귀). 문자는 rune 단위 |
| TM-FR-12 | 대기 중: Enter·Alt+Enter = 입력 전송(TM-FR-15). Ctrl+C = 입력 줄이 비어 있지 않으면 비우고, 비어 있으면 상태 줄에 `Ctrl+C 를 한 번 더 누르면 종료` 를 표시하고 바로 다음 키가 Ctrl+C 이면 **종료**. Ctrl+D = 입력 줄이 비어 있으면 **종료**, 아니면 무시. Esc 무시 |
| TM-FR-13 | 실행 중: Enter = `Run.Steer`, Alt+Enter = `Run.FollowUp` (TM-FR-15). Esc·Ctrl+C = `Run.Abort` (입력 줄 유지). Ctrl+D = 입력 줄이 비어 있으면 `Abort` 후 run 이 끝나면 **종료**, 아니면 무시. 입력이 비었으면 Enter·Alt+Enter 는 무시 |
| TM-FR-14 | 종료 = `Run` 이 `nil` 을 돌려주는 것 (TM-FR-03 정리 포함) |
| TM-FR-15 | 전송: 앞뒤 공백을 뺀 입력이 비면 무시. `/` 로 시작하면 명령(3.6)이고 모델에 보내지 않는다. `//` 로 시작하면 앞 `/` 하나를 떼고 보낸다. 보낼 메시지 = `contract.Message{Role: user, Content: [text 블록 = 원문(앞뒤 공백 제거)], ID: crypto/rand 16 byte hex, Timestamp: Now()}`. 보낸 입력은 기록(TM-FR-11)에 더하고 입력 줄을 비운다. 출력에 user 줄(TM-FR-23)을 덧붙이고, Steer 면 끝에 ` (steer)`, FollowUp 이면 ` (follow-up)` 을 붙인다 |
| TM-FR-16 | 대기 중 전송 = `Agent.Start(ctx, 현재 세션, [메시지])`. 현재 세션이 없으면 `""`. 성공하면 상태는 실행 중, 현재 세션 = `Run.SessionID()` 이고 그것이 새로 생긴 ID 면 `── 세션 <id>` 줄을 덧붙인다. 에러면 `✗ <에러>` 줄 (상태 대기 유지), 그때 `Agent.Model()` 이 모델 문제를 돌려주면 줄 끝에 ` (/model)` 을 붙인다 |
| TM-FR-17 | 실행 중 `Steer`·`FollowUp` 이 `agent.ErrRunDone` 이면(run 이 막 끝난 경합) 그 run 의 `Wait` 가 끝나기를 기다린 뒤 같은 메시지로 `Start` 한다 |
| TM-FR-18 | `Wait` 가 돌아오면 상태는 대기. FollowUp 대기 수는 0 |

### 3.4 화면 그리기

| id | 요구사항 |
|---|---|
| TM-FR-19 | 모든 쓰기(OnEvent 렌더링, Approve 프롬프트, 명령 출력, 입력 줄 다시 그리기)는 잠금 하나로 직렬화한다. 덧붙이는 출력을 쓸 때는 다시 그리는 영역을 지우고(커서를 영역 첫 줄로 올리고 `CSI J`) 출력을 쓴 뒤 영역을 다시 그린다 |
| TM-FR-20 | 다시 그리는 영역 = ① 줄바꿈으로 끝나지 않은 출력 꼬리(다음 조각이 같은 줄에 이어 붙도록 영역에 포함해 매번 다시 찍는다) ② 상태 줄 ③ 입력 줄(프롬프트 `> ` + 입력, 여러 줄이면 이어지는 줄은 `  ` 들여쓰기). 줄 높이는 `Width()` 와 표시 폭(`x/text/width` 의 East Asian Wide·Fullwidth = 2 열, 나머지 1 열, 제어 문자 0)으로 계산한다. 커서는 입력 줄의 편집 위치에 둔다 |
| TM-FR-21 | 상태 줄: 대기 = 비움(빈 줄 없이 생략). 실행 중 = `● 실행 중 · Esc/Ctrl+C 중단 · Enter steer · Alt+Enter follow-up`, FollowUp 대기가 있으면 앞에 `+N `(이 Terminal 이 보낸 FollowUp 중 아직 run_end 를 받지 않은 수 — 연쇄의 run_end 를 받으면 0). 압축 = `⋯ 압축 중 · Esc/Ctrl+C 취소`. TM-FR-12 의 종료 안내는 그 키 한 번 동안만 |
| TM-FR-22 | 색 (Color 가 true 일 때만, 아니면 SGR 를 쓰지 않음): thinking·bash 출력·압축 요약 줄 = 흐림(`SGR 2`), `✓` = 초록, `✗` = 빨강, `⚠`·`↻` = 노랑, user 줄의 `> ` = 굵게. 각 색 구간은 같은 줄 안에서 `SGR 0` 으로 닫는다 |

### 3.5 이벤트 렌더링

| id | 요구사항 |
|---|---|
| TM-FR-23 | user 줄: `> ` + 텍스트. 여러 줄이면 이어지는 줄은 `  ` 들여쓰기 |
| TM-FR-24 | 그리는 모든 외부 텍스트(모델 출력, 도구 출력·결과, 에러 문구, 세션 제목)는 `\n` 외의 제어 문자를 버리고 탭을 공백 4칸으로 바꾼다 (터미널 시퀀스 주입과 폭 계산 어긋남 방지). 입력 줄의 탭도 표시만 공백 4칸이다. `message_update` 의 `text_delta` = Delta 의 텍스트를 그대로 덧붙인다. `thinking_delta` = 흐리게 덧붙인다. 한 메시지 안에서 text ↔ thinking 이 바뀌면 바뀌기 전 출력이 줄바꿈으로 끝나지 않았을 때 줄바꿈 하나를 넣는다. `toolcall_*` 는 그리지 않는다 |
| TM-FR-25 | `message_end` 와 run 의 끝(`run_end`)에서, 출력이 줄바꿈으로 끝나지 않았으면 줄바꿈 하나를 넣는다. `message_end` 의 `Message.Usage` 를 그 run 의 합계에 더한다 (입력 = Input + CacheRead + CacheWrite, 출력 = Output) |
| TM-FR-26 | `tool_start` 줄: `▸ <name>  <요약>`. 요약 = 인자 JSON 객체의 `command` 문자열 → 없으면 `path` 문자열 → 없으면 인자 JSON 그대로(공백 없는 직렬화). 요약은 첫 줄만, 줄 전체를 `Width()` 열에 맞게 잘라 끝에 `…`. 도구 이름으로 분기하지 않는다 |
| TM-FR-27 | `tool_update`: Output 을 `  ` 들여쓰기 + 흐림으로 덧붙인다. 한 호출(Call.ID)에서 줄바꿈으로 끝난 줄이 **20줄**에 이르면 그 뒤 출력은 버리고 버린 줄 수(줄바꿈 수 + 마지막 꼬리가 있으면 1)를 센다 |
| TM-FR-28 | `tool_end` 줄: 이 호출의 bash 출력 꼬리가 줄바꿈으로 끝나지 않았으면 줄바꿈을 먼저 넣고, 버린 줄이 있으면 `  … +N줄 생략`. 그다음 Status 별로 `✓` (ok) / `✗ error: <결과 텍스트 첫 줄>` / `⤼ skipped` (결과 텍스트가 `skipped:` 로 시작하는 error) / `■ aborted`. 결과 본문은 그리지 않는다. 한 줄은 `Width()` 로 자른다 |
| TM-FR-29 | `approval` 줄 (Approve 를 거친 결정과 규칙 거부 모두): Decision `allow` → `✓ 이번만 허용`, `allow_session` → `✓ 이 세션 동안 허용`, `deny` → `✗ 거부: <Reason>`, `rule_deny` → `✗ 규칙 거부: <Rule>` |
| TM-FR-30 | `retry` 줄: `↻ 재시도 <Attempt>회째 — <Delay 를 초 단위, 소수 첫째 자리까지>s 후 (<Error.Message>)` |
| TM-FR-31 | `compaction` 줄: Phase start → `⋯ 컨텍스트 압축 중`, end → Err 가 nil 이면 `✓ 압축 완료`, 아니면 `✗ 압축 실패: <Err>` |
| TM-FR-32 | `run_end` 줄: `── <Settle.Reason> · 입력 <n> / 출력 <n> 토큰`. 수는 1000 미만이면 그대로, 이상이면 `12.3k`(소수 첫째 자리, 반올림). `failed`·`guard` 면 ` · <Settle.Err>` 를 붙인다. `aborted` 면 `── 중단됨` 만. 합계는 줄을 그린 뒤 0 으로 (연쇄 run 마다 따로) |
| TM-FR-33 | `message_start`·`turn_end` 는 그리지 않는다 |

### 3.6 승인 프롬프트

| id | 요구사항 |
|---|---|
| TM-FR-34 | `Approve(ctx, req)` 는 한 번에 하나만 진행한다 (여러 고루틴이 부르면 차례로). 진행 중 상태는 승인이고, 입력 줄은 보존했다가 끝나면 되살린다 |
| TM-FR-35 | 프롬프트는 다시 그리는 영역에 그린다 (덧붙이지 않음): `⚠ <name> 승인 필요  (규칙: <Rule>)` — Rule 이 비면 `(규칙 없음 — 기본 묻기)`. 다음 줄 `  <Target>`(비면 인자 JSON 요약, TM-FR-26 규칙), 인자 중 `command`·`path` 가 아닌 문자열 값은 `  <key>: <값의 앞 5줄>`(더 있으면 `  …`), 마지막 줄 `  [y] 이번만  [a] 이 세션 동안  [n] 거부` |
| TM-FR-36 | 키: `y` → `Decision{allow}`, `a` → `Decision{allow_session}`, `n` → 마지막 줄이 `  거부 사유 (Enter 로 확정): ` + 입력 칸으로 바뀌고(TM-FR-11 편집, 줄바꿈·기록 없음, 붙여넣기의 줄바꿈은 공백), Enter → `Decision{deny, Reason: 입력(앞뒤 공백 제거), 비면 "user denied"}`. Esc·Ctrl+C (사유 입력 중 포함) → 현재 run `Abort` 후 ctx 취소를 기다려 `ctx.Err()` 를 돌려준다. 그 밖의 키는 무시. 대소문자 `Y`·`A`·`N` 도 같다 |
| TM-FR-37 | ctx 가 먼저 취소되면(예: 다른 경로의 Abort) 프롬프트를 지우고 `ctx.Err()` 를 돌려준다 |

### 3.7 세션

| id | 요구사항 |
|---|---|
| TM-FR-38 | 시작: `Start.Session` 이 있으면 그 세션을 이어간다(TM-FR-41, `Sessions()` 에 없으면 헤더 없이 그 ID 를 현재 세션으로만 둠). `Continue` 면 `Sessions()` 의 첫 항목을 이어가고 목록이 비면 새 세션. `Pick` 이면 목록 화면(TM-FR-40)을 띄운다. 셋 다 아니면 새 세션(현재 세션 없음). `Sessions` 에러는 `✗ <에러>` 줄 후 새 세션 |
| TM-FR-39 | 목록 줄: `<번호>  <상대 시각>  <Messages>msg  <Title>` (번호 1부터, 최대 20개, Title 이 비면 `(제목 없음)`, 줄은 `Width()` 로 자름). 상대 시각 = `Now() - Updated` 가 1분 미만 `방금`, 1시간 미만 `N분 전`, 1일 미만 `N시간 전`, 7일 미만 `N일 전`, 그 이상 `YYYY-MM-DD`(로컬). 목록이 비면 `(세션 없음)` |
| TM-FR-40 | 선택 상태 (`Pick` 시작, 또는 TM-FR-44 `/resume` 인자 없음): 목록을 덧붙이고 입력 줄 프롬프트를 `번호 (Enter = 새 세션): ` 로 바꾼다. Enter → 비면 새 세션, 1..목록 수의 정수면 그 세션 이어가기, 그 밖이면 `✗ 잘못된 번호` 후 다시 입력. Esc·Ctrl+C → 새 세션. 그 밖의 편집은 TM-FR-11 (줄바꿈·기록 없음). 새 세션을 고르면 `── 새 세션` 줄 |
| TM-FR-41 | 이어가기: 현재 세션 = ID. `── 세션 <id> · <Title> · <Messages> 메시지` 줄, 그리고 `Agent.History(ctx, id)` 를 그린다: `Omitted > 0` 이면 먼저 `⋯ 이전 <Omitted>개 메시지 압축됨`(흐림). 이어서 Messages 를 순서대로 — user = TM-FR-23, assistant = 블록 순서대로 text 그대로·thinking 흐림·tool_call 은 TM-FR-26 줄·image 는 `[image]`, tool_result = TM-FR-28 의 Status 줄. 메시지 사이 출력은 줄바꿈으로 끝나게 한다. History 에러는 `✗ <에러>` 줄 (현재 세션은 바꿈) |
| TM-FR-42 | 세션 전환(`/resume`·`/new`)은 대기 상태에서만 한다. 다른 상태면 `✗ 실행 중에는 세션을 바꿀 수 없습니다` |

### 3.8 슬래시 명령

명령 이름 뒤 인자는 공백으로 나눈다. 대소문자를 구분한다.

| id | 요구사항 |
|---|---|
| TM-FR-43 | `/help`: 키 바인딩(3.3 의 표 요약)과 명령 목록을 덧붙인다. 어느 상태에서나 |
| TM-FR-44 | `/sessions`: 목록(TM-FR-39)을 덧붙인다. 어느 상태에서나. `/resume <번호>` = 그 시점 `Sessions()` 순서의 번호, `/resume <id>` = 번호가 아니면 ID 로 보고 `Sessions()` 에 있어야 한다(없으면 `✗ 없는 세션: <id>`). `/resume` 인자 없음 = 선택 상태(TM-FR-40). `/new` = 현재 세션 없음 + `── 새 세션` 줄 |
| TM-FR-45 | `/compact`: 대기 상태에서만(아니면 `✗ 실행 중에는 압축할 수 없습니다`). 현재 세션이 없으면 `✗ 압축할 세션이 없습니다`. 상태 압축으로 바꾸고 `Agent.Compact(cctx, 현재 세션)` 을 부른다(cctx 는 Esc·Ctrl+C 로 취소). `(false, nil)` → `압축할 구간이 없습니다`, 에러 → ctx 취소면 `■ 압축 취소`, 그 밖은 `✗ <에러>` (진행 표시는 compaction 이벤트가 TM-FR-31 로 그림). 끝나면 대기 |
| TM-FR-46 | `/quit`·`/exit`: 대기 중이면 종료, 실행 중이면 `Abort` 후 run 이 끝나면 종료 |
| TM-FR-47a | `/model`: 대기 상태에서만(아니면 `✗ 실행 중에는 모델을 바꿀 수 없습니다`). 인자가 있으면 `Agent.SetModel(ctx, 인자)` — 성공 `── 모델 <ref> (저장됨)`, 에러 `✗ <에러>`. 인자가 없으면 모델 선택 상태(3.9) |
| TM-FR-47 | 그 밖의 `/…`: `✗ 알 수 없는 명령: /<이름> (/help)` |

### 3.9 모델 선택 (R20)

| id | 요구사항 |
|---|---|
| TM-FR-48 | 시작: 세션 시작(TM-FR-38) 뒤 `Agent.Model()` 이 모델 문제를 돌려주면 `⚠ <문제>` 줄(`ErrNoModel` 이면 `⚠ 모델이 설정되지 않았습니다`)을 덧붙이고 모델 선택 상태로 들어간다. 세션 선택(`Pick`)이 있으면 그것이 끝난 뒤에 들어간다 |
| TM-FR-49 | 모델 선택 상태: 목록 = 들어갈 때의 `Agent.Models()`. 입력 줄 프롬프트 `모델: ` 의 입력이 필터다 — 공백으로 나눈 낱말이 모두 `provider/id` 에 (대소문자 무시) 들어 있는 항목. 다시 그리는 영역의 상태 줄과 입력 줄 사이에 일치 항목을 최대 **10개** 그리고, 더 있으면 `  … +N개`, 하나도 없으면 `  (일치하는 모델 없음)`. 항목 줄 = 하이라이트면 `▶ `, 아니면 `  ` + `provider/id`, KeyMissing 이면 뒤에 흐리게 ` (키 없음: <KeyEnv>)`. 줄은 `Width()` 로 자른다. 상태 줄 = `모델 선택 · ↑↓ 이동 · Enter 선택 · Esc 취소` |
| TM-FR-50 | 키: 문자·붙여넣기·Backspace 등 = 필터 편집(TM-FR-11, 줄바꿈·기록 없음)이고 편집하면 하이라이트는 첫 항목. ↑↓ = 일치 목록 안에서 하이라이트 이동 (끝에서 멈춤, 화면의 10개 밖으로 가면 보이는 창을 옮김). Enter = 일치 항목이 없으면 무시, 하이라이트 항목이 KeyMissing 이면 `✗ missing API key: set <KeyEnv>` 줄(상태 유지), 아니면 `SetModel` — 성공 `── 모델 <ref> (저장됨)` 후 대기, 에러 `✗ <에러>`(상태 유지). Esc·Ctrl+C = 대기로 (모델 그대로) |
| TM-FR-51 | 모델 선택 중 Approve·이벤트는 오지 않는다 (대기 상태에서만 들어가므로) |

---

## 4. 검증

| 요구사항 | 방법 |
|---|---|
| TM-FR-07~10 | 키 해석기 단위 테스트(표 기반): 표 10 의 모든 인코딩, 1바이트씩 쪼갠 입력, 쪼개진 UTF-8, 붙여넣기 안의 `\r`·제어 바이트, 모르는 CSI 무시, 단독 ESC 는 시간 초과 뒤 Esc·ESC+`\r` 은 Alt+Enter. 협상: 가짜 In 이 kitty 응답+DA / DA 만 / 무응답일 때 Out 에 쓰인 켜기·해제 시퀀스, 협상 응답이 키로 새지 않음 |
| TM-FR-11 | 입력 줄 편집기 단위 테스트: 삽입·삭제·이동·줄 처음/끝·여러 줄·기록 ↑↓·한글(rune) |
| TM-FR-19~22 | 렌더 단위 테스트: 가짜 Out 의 바이트를 가상 화면(행 배열 + 커서, 처리 시퀀스는 이 모듈이 쓰는 것만)으로 재생해 최종 화면을 비교. 꼬리 이어 붙이기, 넓은 문자 줄 높이, 줄 넘김, Color false 면 SGR 없음 |
| TM-FR-23~33 | 이벤트 → 가상 화면: 각 종류, text↔thinking 줄바꿈, bash 20줄 상한과 `+N줄 생략`, tool_end 4종, run_end 토큰 형식(999·1000·12345)·aborted·failed |
| TM-FR-34~37 | 실제 agent + httptest 모델 서버(dr-print 테스트와 같은 방식, `NoRemoteCatalog`)로 bash 호출 → 가짜 In 에 y / a(다음 같은 호출은 묻지 않음) / n+사유(모델 다음 요청에 `denied: <사유>`) / Esc(run aborted) / 동시 승인 2개 직렬 |
| TM-FR-12~18 | 같은 종단 환경: 전송 → Start·`── 세션`, 실행 중 Enter → 다음 요청에 steer 메시지, Alt+Enter → 연쇄 run, Esc·Ctrl+C → `── 중단됨`, 대기 Ctrl+C 두 번·Ctrl+D·In EOF → Run nil, ctx 취소 → Run 이 ctx.Err() 와 해제 시퀀스, `//x` → 모델에 `/x` |
| TM-FR-38~42 | 세션 파일 둘을 만든 sessionDir 로: Continue → 최근 것 헤더 + History, Pick → 번호 2 선택·잘못된 번호·Enter 새 세션, 압축된 세션 → `이전 N개 메시지 압축됨` 과 요약 본문 없음 |
| TM-FR-47a~51 | 모델 없는 설정으로 시작 → `⚠ 모델이 설정되지 않았습니다` + 선택 화면, 필터 낱말 AND·대소문자, 10개 상한과 `+N개`, 일치 없음, ↑↓ 창 이동, KeyMissing 거부, Enter → 설정 파일 갱신 + `── 모델` 줄 + 다음 전송이 그 모델로, Esc 취소 후 전송 → `✗ no model configured (/model)`, `/model <ref>`, 실행 중 `/model` 거부 |
| TM-FR-43~47 | 각 명령, 실행 중 `/resume`·`/compact` 거부, `/compact` 짧은 세션 → `압축할 구간이 없습니다`, 긴 세션(작은 keepRecentTokens) → compaction 줄, 알 수 없는 명령 |
| TM-FR-01 | `Open` 에 일반 파일 → ErrNotTerminal (TTY 성공 경로는 수동 검증) |
| TM-FR-02~06 | Start 플래그 둘 → 에러, Run 두 번 → 에러, Run 밖 OnEvent·Approve, Resize 뒤 새 폭 |

비기능: `go test -race`, `GOWORK=off` 단독 통과. 종단 테스트는 외부 네트워크를 쓰지 않는다. 시간 의존(EscTimeout, 협상 1초)은 테스트에서 짧은 값 또는 해석기 직접 호출로 결정적으로 만든다.

---

## 5. 추적성

| 상위 | 요구사항 |
|---|---|
| R17 | TM-C1, 1.1 |
| R18 ②④ | TM-FR-23~37 |
| R18 ③ | TM-FR-12~18 |
| R18 ⑤, AG-FR-29~31 | TM-FR-38~45 |
| R19 | TM-C4, TM-FR-01, 07~10 |
| 인터뷰 ①~⑧ | 3.2~3.8 |

---

## 6. 미결 사항

없음.

---

## 7. 변경 기록

| 버전 | 이전 | 새 동작 | 이유 |
|---|---|---|---|
| 0.2 | 모델 문제면 dr 이 시작하지 못함, `/model` 없음 | 시작 시 모델 선택 화면, `/model` (3.9, TM-FR-47a) | R20 |
| 0.1 | 구체 surface 없음 (`surfaces/README.md` 만) | 대화형 터미널 surface 라이브러리 | 사용자: "대화형 터미널 해보자" (impl-04) |
