// Package terminal 은 대화형 터미널 surface 다 (L3_ui/surfaces/terminal/SPEC.md).
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L2_harness/agent"
	"github.com/Biisairo/dynamic_react/L2_harness/harness"
)

// negotiateTimeout 은 DA 응답을 기다리는 시간이다 (TM-FR-07). 테스트가 바꾼다.
var negotiateTimeout = time.Second

const (
	defaultEscTimeout = 50 * time.Millisecond
	maxSessions       = 20 // TM-FR-39

	seqPasteOn   = "\x1b[?2004h"
	seqPasteOff  = "\x1b[?2004l"
	seqQuery     = "\x1b[>1u\x1b[?u\x1b[c"
	seqKittyOff  = "\x1b[<u"
	seqModifyOn  = "\x1b[>4;2m"
	seqModifyOff = "\x1b[>4;0m"

	promptMain = "> "
	promptPick = "번호 (Enter = 새 세션): "
	exitHint   = "Ctrl+C 를 한 번 더 누르면 종료"
	statusRun  = "● 실행 중 · Esc/Ctrl+C 중단 · Enter steer · Alt+Enter follow-up"
	statusComp = "⋯ 압축 중 · Esc/Ctrl+C 취소"
)

type Options struct {
	In         io.Reader
	Out        io.Writer
	Width      func() int
	Color      bool
	Now        func() time.Time
	EscTimeout time.Duration
}

type Start struct {
	Session  string
	Continue bool
	Pick     bool
}

type state int

const (
	stIdle state = iota
	stRunning
	stCompacting
	stPick
	stChoose // 모델·provider 선택 (TM-FR-49, 52)
	stKey    // 키 입력 (TM-FR-53)
)

type approval struct {
	req       agent.ApprovalRequest
	ctx       context.Context
	reply     chan agent.Decision
	reasoning bool
}

type compactResult struct {
	ok  bool
	err error
	ctx context.Context
}

// Terminal 은 대화형 터미널 surface 다.
type Terminal struct {
	o Options

	mu  sync.Mutex // 출력·화면 상태 잠금 (TM-FR-19)
	scr screen
	ed  editor
	// 렌더링 상태 (mu)
	block     string
	calls     map[string]*callOut
	sent      map[string]bool // 이 Terminal 이 보낸 입력 메시지 ID (TM-FR-54)
	tokIn     int
	tokOut    int
	followUps int
	// 루프 상태 (Run 고루틴이 바꾸고, view 는 mu 아래에서 읽음)
	st       state
	hint     bool
	appr     *approval
	reason   editor
	pickList []agent.SessionInfo
	ch       *chooser
	keyFor   string
	secret   editor

	started  atomic.Bool
	running  atomic.Bool
	stopped  chan struct{}
	apprMu   sync.Mutex
	apprCh   chan *approval
	a        *agent.Agent
	ctx      context.Context
	sid      string
	run      *agent.Run
	runDone  chan struct{}
	pending  []contract.Message
	quitting bool
	compact  context.CancelFunc
	compDone chan compactResult
	kitty    bool
	modify   bool
	negStart time.Time
	negDone  bool

	modelChecked bool
}

func New(o Options) *Terminal {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.EscTimeout <= 0 {
		o.EscTimeout = defaultEscTimeout
	}
	t := &Terminal{o: o, calls: map[string]*callOut{}, sent: map[string]bool{}, stopped: make(chan struct{}), apprCh: make(chan *approval)}
	t.scr = screen{out: o.Out, width: o.Width}
	return t
}

// OnEvent 는 agent.Options.OnEvent 로 넘긴다. Run 중이 아니면 무시한다 (TM-FR-05).
func (t *Terminal) OnEvent(ev harness.Event) {
	if !t.running.Load() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.event(ev)
	t.redraw()
}

// Resize 는 다음 그리기부터 새 폭을 쓴다 (TM-FR-06).
func (t *Terminal) Resize() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.redraw()
}

// Approver 는 터미널에서 묻는 agent.Approver 다 (3.6).
func (t *Terminal) Approver() agent.Approver { return approver{t} }

type approver struct{ t *Terminal }

func (p approver) Approve(ctx context.Context, req agent.ApprovalRequest) (agent.Decision, error) {
	t := p.t
	if !t.running.Load() {
		return agent.Decision{Kind: "deny", Reason: "terminal not running"}, nil
	}
	t.apprMu.Lock()
	defer t.apprMu.Unlock()
	a := &approval{req: req, ctx: ctx, reply: make(chan agent.Decision, 1)}
	select {
	case t.apprCh <- a:
	case <-ctx.Done():
		return agent.Decision{}, ctx.Err()
	case <-t.stopped:
		return agent.Decision{Kind: "deny", Reason: "terminal not running"}, nil
	}
	select {
	case d := <-a.reply:
		return d, nil
	case <-ctx.Done():
		return agent.Decision{}, ctx.Err()
	}
}

// redraw 는 현재 상태로 영역을 다시 그린다. 잠금은 호출자가 잡는다.
func (t *Terminal) redraw() {
	t.scr.draw(t.view())
}

func (t *Terminal) view() view {
	var v view
	switch {
	case t.hint:
		v.status = exitHint
	case t.st == stRunning:
		v.status = statusRun
		if t.followUps > 0 {
			v.status = fmt.Sprintf("+%d %s", t.followUps, statusRun)
		}
	case t.st == stCompacting:
		v.status = statusComp
	case t.st == stChoose && t.ch.kind == chModel:
		v.status = statusModel
	case t.st == stChoose:
		v.status = statusProvider
	case t.st == stKey:
		v.status = statusKey
	}
	if t.appr != nil {
		lines, col := t.approvalView(t.appr)
		v.lines, v.line = lines, len(lines)-1
		v.col = col
		if col < 0 {
			v.col = len([]rune(lines[v.line]))
		}
		return v
	}
	prompt, ed := promptMain, &t.ed
	var above []string
	switch {
	case t.st == stPick:
		prompt = promptPick
	case t.st == stChoose && t.ch.kind == chModel:
		prompt, above = promptModel, t.chooseLines()
	case t.st == stChoose:
		prompt, above = promptProvider, t.chooseLines()
	case t.st == stKey:
		prompt, ed = t.keyFor+" 키: ", &t.secret
	}
	lines, line, col := ed.layout()
	if ed == &t.secret {
		for i, l := range lines {
			lines[i] = strings.Repeat("•", len([]rune(l)))
		}
	}
	for i, l := range lines {
		if i == line {
			col += 3 * strings.Count(string([]rune(l)[:col]), "\t")
		}
		lines[i] = strings.ReplaceAll(l, "\t", "    ")
		if i == 0 {
			lines[i] = prompt + lines[i]
		} else {
			lines[i] = "  " + lines[i]
		}
	}
	v.lines, v.line = append(above, lines...), line+len(above)
	v.col = col + 2
	if line == 0 {
		v.col = col + len([]rune(prompt))
	}
	return v
}

// say 는 한 줄을 덧붙이고 영역을 다시 그린다.
func (t *Terminal) say(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.line(s)
	t.redraw()
}

func (t *Terminal) fail(err error) { t.say(t.paint(sgrRed, "✗") + " " + sanitize(err.Error())) }

// update 는 잠금 아래에서 f 를 하고 다시 그린다.
func (t *Terminal) update(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f()
	t.redraw()
}

// Run 은 사용자가 끝낼 때까지 입력을 받는다 (TM-FR-03).
func (t *Terminal) Run(ctx context.Context, a *agent.Agent, s Start) error {
	n := 0
	for _, b := range []bool{s.Session != "", s.Continue, s.Pick} {
		if b {
			n++
		}
	}
	if n > 1 {
		return errors.New("terminal: Session, Continue and Pick are exclusive")
	}
	if !t.started.CompareAndSwap(false, true) {
		return errors.New("terminal: Run called twice")
	}
	t.a, t.ctx = a, ctx
	t.runDone, t.compDone = make(chan struct{}), make(chan compactResult, 1)
	in := make(chan []byte)
	go read(t.o.In, in)

	t.mu.Lock()
	io.WriteString(t.o.Out, seqPasteOn+seqQuery)
	t.negStart = time.Now()
	t.redraw()
	t.mu.Unlock()
	t.running.Store(true)
	defer t.finish()
	t.begin(s)
	if t.st != stPick {
		t.checkModel()
	}

	var p parser
	esc := time.NewTimer(time.Hour)
	esc.Stop()
	for {
		if t.quitting && t.run == nil && t.compact == nil {
			return nil
		}
		var apprDone <-chan struct{}
		if t.appr != nil {
			apprDone = t.appr.ctx.Done()
		}
		select {
		case b, ok := <-in:
			if !ok {
				in = nil
				t.quit()
				continue
			}
			t.keys(p.feed(b))
			if p.loneEsc() {
				esc.Reset(t.o.EscTimeout)
			}
		case <-esc.C:
			t.keys(p.flush())
		case a := <-t.apprCh:
			t.update(func() {
				t.appr = a
				t.reason.clear()
			})
		case <-apprDone:
			t.update(func() { t.appr = nil })
		case <-t.runDone:
			t.runDone = make(chan struct{})
			t.update(func() {
				t.run, t.st, t.followUps = nil, stIdle, 0
			})
			if !t.quitting && len(t.pending) > 0 {
				msgs := t.pending
				t.pending = nil
				t.start(msgs)
			}
		case r := <-t.compDone:
			t.compacted(r)
		case <-ctx.Done():
			if t.run != nil {
				t.run.Abort()
				<-t.runDone
				t.run = nil
			}
			if t.compact != nil {
				t.compact()
				<-t.compDone
				t.compact = nil
			}
			return ctx.Err()
		}
	}
}

func read(r io.Reader, out chan<- []byte) {
	defer close(out)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

// finish 는 터미널 모드를 되돌리고 영역을 지운다 (TM-FR-03, 08).
func (t *Terminal) finish() {
	t.running.Store(false)
	close(t.stopped)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.scr.erase()
	off := seqPasteOff
	if t.kitty {
		off = seqKittyOff + off
	}
	if t.modify {
		off = seqModifyOff + off
	}
	io.WriteString(t.o.Out, off)
}

// quit 은 종료를 요청한다. 실행 중이면 Abort 하고 끝나기를 기다린다 (TM-FR-13, 46).
func (t *Terminal) quit() {
	t.quitting = true
	if t.run != nil {
		t.run.Abort()
	}
	if t.compact != nil {
		t.compact()
	}
}

func (t *Terminal) keys(ks []key) {
	for _, k := range ks {
		t.key(k)
	}
}

func (t *Terminal) key(k key) {
	switch k.kind {
	case keyKittyFlags:
		if !t.negDone {
			t.kitty = true
		}
		return
	case keyDA:
		t.negotiated()
		return
	}
	switch {
	case t.appr != nil:
		t.approvalKey(k)
		return
	case t.st == stPick:
		t.pickKey(k)
		if t.st != stPick {
			t.checkModel()
		}
		return
	case t.st == stChoose:
		t.chooseKey(k)
		return
	case t.st == stKey:
		t.secretKey(k)
		return
	}
	var abort, quit, cancel, send bool
	t.mu.Lock()
	hint := t.hint
	t.hint = false
	running := t.st == stRunning
	switch {
	case t.st == stCompacting:
		cancel = k.kind == keyEsc || k.kind == keyCtrlC
	case k.kind == keyEnter || k.kind == keyAltEnter:
		send = true
	case k.kind == keyEsc:
		abort = running
	case k.kind == keyCtrlC:
		switch {
		case running:
			abort = true
		case !t.ed.empty():
			t.ed.clear()
		case hint:
			quit = true
		default:
			t.hint = true
		}
	case k.kind == keyCtrlD:
		quit = t.ed.empty()
	default:
		t.edit(&t.ed, k, true)
	}
	t.redraw()
	t.mu.Unlock()
	switch {
	case abort:
		t.run.Abort()
	case quit:
		t.quit()
	case cancel:
		t.compact()
	case send:
		t.submit(k.kind == keyAltEnter)
	}
}

// negotiated 는 DA 응답을 받았을 때다 (TM-FR-07). kitty 응답이 먼저 오지 않았으면 modifyOtherKeys 를 켠다.
func (t *Terminal) negotiated() {
	if t.negDone {
		return
	}
	t.negDone = true
	if t.kitty || time.Since(t.negStart) > negotiateTimeout {
		return
	}
	t.modify = true
	t.mu.Lock()
	io.WriteString(t.o.Out, seqModifyOn)
	t.mu.Unlock()
}

// edit 은 편집 키를 적용한다 (TM-FR-11). multiline 이 아니면 줄바꿈·기록을 쓰지 않는다.
func (t *Terminal) edit(e *editor, k key, multiline bool) {
	switch k.kind {
	case keyRune:
		e.insert(string(k.r))
	case keyPaste:
		if multiline {
			e.insert(k.text)
		} else {
			e.insert(strings.ReplaceAll(k.text, "\n", " "))
		}
	case keyNewline:
		if multiline {
			e.newline()
		}
	case keyBackspace:
		e.backspace()
	case keyDelete:
		e.del()
	case keyLeft:
		e.left()
	case keyRight:
		e.right()
	case keyHome:
		e.home()
	case keyEnd:
		e.end()
	case keyUp:
		if multiline {
			e.up()
		}
	case keyDown:
		if multiline {
			e.down()
		}
	}
}

func newMessage(text string, now time.Time) contract.Message {
	var b [16]byte
	rand.Read(b[:])
	return contract.Message{ID: hex.EncodeToString(b[:]), Role: contract.RoleUser, Timestamp: now,
		Content: []contract.Block{{Kind: contract.BlockText, Text: text}}}
}

// submit 은 TM-FR-15~17 이다.
func (t *Terminal) submit(followUp bool) {
	text := strings.TrimSpace(t.ed.text())
	if text == "" {
		t.update(func() {})
		return
	}
	t.mu.Lock()
	t.ed.submit()
	t.mu.Unlock()
	if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") {
		t.update(func() {})
		t.command(text)
		return
	}
	if strings.HasPrefix(text, "//") {
		text = text[1:]
	}
	msg := newMessage(text, t.o.Now())
	t.mu.Lock()
	t.sent[msg.ID] = true
	t.mu.Unlock()
	if t.st != stRunning {
		t.say(t.userLine(text, ""))
		t.start([]contract.Message{msg})
		return
	}
	suffix := " (steer)"
	if followUp {
		suffix = " (follow-up)"
	}
	t.say(t.userLine(text, suffix))
	var err error
	if followUp {
		err = t.run.FollowUp(msg)
	} else {
		err = t.run.Steer(msg)
	}
	switch {
	case errors.Is(err, agent.ErrRunDone):
		t.pending = append(t.pending, msg)
	case err != nil:
		t.fail(err)
	case followUp:
		t.update(func() { t.followUps++ })
	}
}

// start 는 TM-FR-16 이다.
func (t *Terminal) start(msgs []contract.Message) {
	r, err := t.a.Start(t.ctx, t.sid, msgs)
	if err != nil {
		t.say(t.paint(sgrRed, "✗") + " " + sanitize(err.Error()) + t.modelHint())
		return
	}
	fresh := t.sid == ""
	t.sid, t.run = r.SessionID(), r
	t.update(func() { t.st = stRunning })
	if fresh {
		t.say("── 세션 " + t.sid)
	}
	done := t.runDone
	go func() {
		r.Wait()
		close(done)
	}()
}

func (t *Terminal) approvalKey(k key) {
	a := t.appr
	switch {
	case k.kind == keyEsc || k.kind == keyCtrlC:
		if t.run != nil {
			t.run.Abort()
		}
	case a.reasoning && k.kind == keyEnter:
		reason := strings.TrimSpace(t.reason.text())
		if reason == "" {
			reason = "user denied"
		}
		t.answer(agent.Decision{Kind: "deny", Reason: reason})
	case a.reasoning:
		t.update(func() { t.edit(&t.reason, k, false) })
	case k.kind != keyRune:
	case k.r == 'y' || k.r == 'Y':
		t.answer(agent.Decision{Kind: "allow"})
	case k.r == 'a' || k.r == 'A':
		t.answer(agent.Decision{Kind: "allow_session"})
	case k.r == 'n' || k.r == 'N':
		t.update(func() { a.reasoning = true })
	}
}

func (t *Terminal) answer(d agent.Decision) {
	t.appr.reply <- d
	t.update(func() { t.appr = nil })
}

// begin 은 시작 세션을 정한다 (TM-FR-38).
func (t *Terminal) begin(s Start) {
	switch {
	case s.Session != "":
		list, err := t.a.Sessions(t.ctx)
		if err != nil {
			t.fail(err)
		}
		for _, info := range list {
			if info.ID == s.Session {
				t.resume(info)
				return
			}
		}
		t.sid = s.Session
	case s.Continue:
		list, err := t.a.Sessions(t.ctx)
		if err != nil {
			t.fail(err)
			return
		}
		if len(list) > 0 {
			t.resume(list[0])
		}
	case s.Pick:
		t.pick()
	}
}

func (t *Terminal) sessions() ([]agent.SessionInfo, bool) {
	list, err := t.a.Sessions(t.ctx)
	if err != nil {
		t.fail(err)
		return nil, false
	}
	return list[:min(len(list), maxSessions)], true
}

// listLines 는 TM-FR-39 의 목록이다.
func (t *Terminal) listLines(list []agent.SessionInfo) []string {
	if len(list) == 0 {
		return []string{"(세션 없음)"}
	}
	var out []string
	for i, s := range list {
		title := s.Title
		if title == "" {
			title = "(제목 없음)"
		}
		line := fmt.Sprintf("%d  %s  %dmsg  %s", i+1, t.ago(s.Updated), s.Messages, sanitize(firstLine(title)))
		out = append(out, truncate(line, t.scr.cols()))
	}
	return out
}

func (t *Terminal) ago(at time.Time) string {
	d := t.o.Now().Sub(at)
	switch {
	case d < time.Minute:
		return "방금"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d일 전", int(d/(24*time.Hour)))
	}
	return at.Local().Format("2006-01-02")
}

func (t *Terminal) showList(list []agent.SessionInfo) {
	t.update(func() {
		for _, l := range t.listLines(list) {
			t.line(l)
		}
	})
}

// pick 은 선택 상태로 들어간다 (TM-FR-40).
func (t *Terminal) pick() {
	list, ok := t.sessions()
	if !ok {
		return
	}
	t.showList(list)
	t.update(func() {
		t.pickList, t.st = list, stPick
		t.ed.clear()
	})
}

func (t *Terminal) pickKey(k key) {
	switch k.kind {
	case keyEsc, keyCtrlC:
		t.update(func() {
			t.st = stIdle
			t.ed.clear()
		})
		t.newSession()
	case keyEnter:
		text := strings.TrimSpace(t.ed.text())
		t.update(func() { t.ed.clear() })
		if text == "" {
			t.update(func() { t.st = stIdle })
			t.newSession()
			return
		}
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > len(t.pickList) {
			t.say(t.paint(sgrRed, "✗") + " 잘못된 번호")
			return
		}
		t.update(func() { t.st = stIdle })
		t.resume(t.pickList[n-1])
	default:
		t.update(func() { t.edit(&t.ed, k, false) })
	}
}

func (t *Terminal) newSession() {
	t.sid = ""
	t.say("── 새 세션")
}

// resume 은 TM-FR-41 이다.
func (t *Terminal) resume(info agent.SessionInfo) {
	t.sid = info.ID
	t.say(fmt.Sprintf("── 세션 %s · %s · %d 메시지", info.ID, sanitize(firstLine(info.Title)), info.Messages))
	h, err := t.a.History(t.ctx, info.ID)
	if err != nil {
		t.fail(err)
		return
	}
	t.update(func() {
		if h.Omitted > 0 {
			t.line(t.paint(sgrDim, fmt.Sprintf("⋯ 이전 %d개 메시지 압축됨", h.Omitted)))
		}
		for _, m := range h.Messages {
			t.message(m)
		}
	})
}

const helpText = `키: Enter 전송(실행 중 steer) · Alt+Enter follow-up · Shift+Enter/Ctrl+J 줄바꿈
     Esc/Ctrl+C 실행 중 중단 · Ctrl+C 입력 지우기(빈 줄에서 두 번 = 종료) · Ctrl+D 빈 줄에서 종료 · ↑↓ 기록
명령: /help · /sessions · /resume [번호|id] · /new · /compact · /quit · /exit · //글 = '/글' 전송
     /model [provider/id] · /login [provider] · /logout [provider]`

// command 는 3.8 이다.
func (t *Terminal) command(text string) {
	fields := strings.Fields(text)
	name, args := fields[0], fields[1:]
	idle := t.st == stIdle
	switch name {
	case "/help":
		t.update(func() {
			for _, l := range strings.Split(helpText, "\n") {
				t.line(l)
			}
		})
	case "/sessions":
		if list, ok := t.sessions(); ok {
			t.showList(list)
		}
	case "/resume", "/new":
		if !idle {
			t.say(t.paint(sgrRed, "✗") + " 실행 중에는 세션을 바꿀 수 없습니다")
			return
		}
		if name == "/new" {
			t.newSession()
			return
		}
		if len(args) == 0 {
			t.pick()
			return
		}
		list, ok := t.sessions()
		if !ok {
			return
		}
		if n, err := strconv.Atoi(args[0]); err == nil {
			if n < 1 || n > len(list) {
				t.say(t.paint(sgrRed, "✗") + " 잘못된 번호")
				return
			}
			t.resume(list[n-1])
			return
		}
		all, _ := t.a.Sessions(t.ctx)
		for _, info := range all {
			if info.ID == args[0] {
				t.resume(info)
				return
			}
		}
		t.say(t.paint(sgrRed, "✗") + " 없는 세션: " + sanitize(args[0]))
	case "/compact":
		switch {
		case !idle:
			t.say(t.paint(sgrRed, "✗") + " 실행 중에는 압축할 수 없습니다")
		case t.sid == "":
			t.say(t.paint(sgrRed, "✗") + " 압축할 세션이 없습니다")
		default:
			cctx, cancel := context.WithCancel(t.ctx)
			t.compact = cancel
			t.update(func() { t.st = stCompacting })
			sid := t.sid
			go func() {
				ok, err := t.a.Compact(cctx, sid)
				t.compDone <- compactResult{ok: ok, err: err, ctx: cctx}
			}()
		}
	case "/model":
		switch {
		case !idle:
			t.say(t.paint(sgrRed, "✗") + " 실행 중에는 모델을 바꿀 수 없습니다")
		case len(args) > 0:
			t.setModel(args[0])
		default:
			t.chooseModel()
		}
	case "/login", "/logout":
		t.keyCommand(name, args)
	case "/quit", "/exit":
		t.quit()
	default:
		t.say(t.paint(sgrRed, "✗") + " 알 수 없는 명령: " + sanitize(name) + " (/help)")
	}
}

func (t *Terminal) compacted(r compactResult) {
	t.compact()
	t.compact = nil
	t.update(func() { t.st = stIdle })
	switch {
	case r.err != nil && r.ctx.Err() != nil:
		t.say("■ 압축 취소")
	case r.err != nil:
		t.fail(r.err)
	case !r.ok:
		t.say("압축할 구간이 없습니다")
	}
}
