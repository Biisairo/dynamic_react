// Package adapters 는 contract 의 중립 요청을 provider 방언으로 보내고, 응답 스트림을 contract 이벤트로 되돌린다.
// contract 와 표준 라이브러리만 import 한다 (AD-C5).
package adapters

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
)

// DefaultRemoteCatalogURL 은 원격 카탈로그 기본 주소다 (AD-FR-36b).
const DefaultRemoteCatalogURL = "https://models.dev/api.json"

const anthropicVersion = "2023-06-01"

type Options struct {
	HTTPClient       *http.Client
	Getenv           func(string) string
	Now              func() time.Time
	Config           Config
	RemoteCatalogURL string
	NoRemoteCatalog  bool
}

// Client 는 방언 어댑터와 카탈로그다. 동시 사용에 안전하다 (AD-FR-48).
type Client struct {
	http     *http.Client
	getenv   func(string) string
	now      func() time.Time
	profiles map[string]Profile
	models   map[string]ModelPatch
	remote   string

	mu      sync.Mutex
	lookups map[string]lookupResult // provider 조회 캐시 (AD-FR-39)
	snap    map[string]map[string]record

	remoteMu    sync.Mutex // 원격 카탈로그를 한 번만 받기 위함 (AD-FR-36b)
	remoteCache map[string]map[string]record
}

func New(o Options) *Client {
	c := &Client{http: o.HTTPClient, getenv: o.Getenv, now: o.Now, models: o.Config.Models, lookups: map[string]lookupResult{}}
	if c.http == nil {
		c.http = http.DefaultClient
	}
	if c.getenv == nil {
		c.getenv = func(string) string { return "" } // 환경을 읽지 않는다 (AD-C6)
	}
	if c.now == nil {
		c.now = time.Now
	}
	if !o.NoRemoteCatalog {
		c.remote = o.RemoteCatalogURL
		if c.remote == "" {
			c.remote = DefaultRemoteCatalogURL
		}
	}
	c.profiles = BuiltinProfiles()
	for name, patch := range o.Config.Providers {
		p, ok := c.profiles[name]
		if !ok && patch.API != nil && *patch.API == APIChat {
			p = chatDefaults(Profile{API: APIChat}) // 새 provider: 기본값 위에 설정을 덮는다
		}
		p = patch.apply(p)
		if p.Lookup == "" {
			p.Lookup = LookupNone
		}
		c.profiles[name] = p
	}
	c.snap = loadSnapshot()
	return c
}

// Profile 은 provider 의 적용된 프로필이다.
func (c *Client) Profile(provider string) (Profile, bool) {
	p, ok := c.profiles[provider]
	return p, ok
}

// Call 은 요청을 보내고 이벤트 스트림을 돌려준다. 스트림은 정확히 하나의 done 또는 error 로 끝난다 (AD-C4).
// iterator 는 error 를 내지 않는다.
func (c *Client) Call(ctx context.Context, req contract.Request) iter.Seq2[contract.StreamEvent, error] {
	return func(yield func(contract.StreamEvent, error) bool) {
		s := &stream{yield: yield, now: c.now, model: req.Model}
		c.call(ctx, req, s)
	}
}

// dialect 는 방언 하나의 요청 생성과 스트림 해석이다.
type dialect interface {
	path() string
	build(req contract.Request, p Profile, info record) ([]byte, *contract.CallError)
	auth(h http.Header, key string)
	parse(r io.Reader, s *stream) error
}

func dialectOf(a API) dialect {
	switch a {
	case APIAnthropic:
		return anthropicDialect{}
	case APIChat:
		return chatDialect{}
	case APIResponses:
		return responsesDialect{}
	}
	return nil
}

func invalid(format string, args ...any) *contract.CallError {
	return &contract.CallError{Class: contract.ClassInvalidRequest, Message: fmt.Sprintf(format, args...)}
}

func (c *Client) call(ctx context.Context, req contract.Request, s *stream) {
	p, ok := c.profiles[req.Model.Provider]
	if !ok {
		s.fail(*invalid("unknown provider %q", req.Model.Provider))
		return
	}
	if req.Model.API != "" && API(req.Model.API) != p.API {
		s.fail(*invalid("provider %q speaks %s, request says %s", req.Model.Provider, p.API, req.Model.API))
		return
	}
	d := dialectOf(p.API)
	if d == nil {
		s.fail(*invalid("unknown api %q", p.API))
		return
	}
	var key string
	if p.APIKeyEnv != "" {
		if key = c.getenv(p.APIKeyEnv); key == "" {
			s.fail(contract.CallError{Class: contract.ClassAuth, Message: "missing API key: set " + p.APIKeyEnv})
			return
		}
	}
	s.secrets = []string{key}
	s.provider = req.Model.Provider
	var info record
	if req.Options.Thinking != "" || req.Options.MaxOutputTokens == 0 {
		info = c.lookup(ctx, req.Model.Provider, req.Model.ID)
	}
	body, ce := d.build(req, p, info)
	if ce != nil {
		s.fail(*ce)
		return
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+d.path(), bytes.NewReader(body))
	if err != nil {
		s.fail(*invalid("%s", mask(err.Error(), s.secrets)))
		return
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "text/event-stream")
	if key != "" {
		d.auth(hr.Header, key)
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		s.failTransport(ctx, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		s.fail(classifyHTTP(resp.StatusCode, resp.Header, b, s.secrets))
		return
	}
	err = d.parse(resp.Body, s)
	switch {
	case s.ended:
	case ctx.Err() != nil:
		s.fail(aborted())
	case err != nil:
		s.failTransport(ctx, err)
	default:
		s.fail(transportError(errors.New("stream ended before completion"), s.secrets))
	}
}

func aborted() contract.CallError {
	return contract.CallError{Class: contract.ClassUnknown, Message: "aborted"}
}

// stream 은 이벤트 방출과 최종 메시지 조립이다.
type stream struct {
	yield    func(contract.StreamEvent, error) bool
	now      func() time.Time
	model    contract.ModelRef
	provider string
	secrets  []string

	started bool
	ended   bool // 종료 이벤트를 냈거나 소비자가 그만둠
	id      string
	blocks  map[int]*contract.Block
	args    map[int]*strings.Builder
	keys    map[string]int // 방언의 블록 키 → Index
	usage   contract.Usage
}

func (s *stream) emit(ev contract.StreamEvent) bool {
	if s.ended {
		return false
	}
	if !s.yield(ev, nil) {
		s.ended = true
		return false
	}
	return true
}

func (s *stream) start() bool {
	if s.started {
		return !s.ended
	}
	s.started = true
	return s.emit(contract.StreamEvent{Kind: contract.EventStart})
}

func (s *stream) fail(ce contract.CallError) {
	if s.ended {
		return
	}
	if !ce.Class.Valid() {
		ce.Class = contract.ClassUnknown
	}
	s.emit(contract.StreamEvent{Kind: contract.EventError, Error: &ce})
	s.ended = true
}

func (s *stream) failTransport(ctx context.Context, err error) {
	if ctx.Err() != nil {
		s.fail(aborted())
		return
	}
	s.fail(transportError(err, s.secrets))
}

// index 는 방언 블록 키에 Index 를 준다 (처음 본 순서).
func (s *stream) index(key string) int {
	if s.keys == nil {
		s.keys = map[string]int{}
	}
	if i, ok := s.keys[key]; ok {
		return i
	}
	i := len(s.keys)
	s.keys[key] = i
	return i
}

func (s *stream) block(i int, kind contract.BlockKind) *contract.Block {
	if s.blocks == nil {
		s.blocks = map[int]*contract.Block{}
	}
	b := s.blocks[i]
	if b == nil {
		b = &contract.Block{Kind: kind}
		s.blocks[i] = b
	}
	return b
}

func (s *stream) text(i int, delta string) bool {
	if delta == "" {
		return !s.ended
	}
	s.block(i, contract.BlockText).Text += delta
	return s.emit(contract.StreamEvent{Kind: contract.EventTextDelta, Index: i, Delta: delta})
}

func (s *stream) thinking(i int, delta string) bool {
	b := s.block(i, contract.BlockThinking)
	if delta == "" {
		return !s.ended
	}
	b.Text += delta
	return s.emit(contract.StreamEvent{Kind: contract.EventThinkingDelta, Index: i, Delta: delta})
}

func (s *stream) providerData(payload []byte) *contract.ProviderData {
	return &contract.ProviderData{Provider: s.provider, Model: s.model.ID, Payload: payload}
}

func (s *stream) toolStart(i int, id, name string) bool {
	b := s.block(i, contract.BlockToolCall)
	b.ToolCall = &contract.ToolCall{ID: id, Name: name}
	if s.args == nil {
		s.args = map[int]*strings.Builder{}
	}
	s.args[i] = &strings.Builder{}
	return s.emit(contract.StreamEvent{Kind: contract.EventToolCallStart, Index: i, ToolCall: &contract.ToolCall{ID: id, Name: name}})
}

func (s *stream) toolDelta(i int, delta string) bool {
	if delta == "" {
		return !s.ended
	}
	if s.args[i] == nil {
		s.args[i] = &strings.Builder{}
	}
	s.args[i].WriteString(delta)
	return s.emit(contract.StreamEvent{Kind: contract.EventToolCallDelta, Index: i, Delta: delta})
}

// toolEnd 는 누적한 인자(또는 full)를 파싱해 toolcall_end 를 낸다.
func (s *stream) toolEnd(i int, full string) bool {
	b := s.blocks[i]
	if b == nil || b.ToolCall == nil {
		return !s.ended
	}
	raw := full
	if raw == "" && s.args[i] != nil {
		raw = s.args[i].String()
	}
	args, err := compactObject(raw)
	if err != nil {
		s.fail(contract.CallError{Class: contract.ClassUnknown, Message: fmt.Sprintf("invalid tool call arguments for %s: %v", b.ToolCall.Name, err)})
		return false
	}
	b.ToolCall.Args = args
	c := *b.ToolCall
	return s.emit(contract.StreamEvent{Kind: contract.EventToolCallEnd, Index: i, ToolCall: &c})
}

func compactObject(raw string) (json.RawMessage, error) {
	if strings.TrimSpace(raw) == "" {
		return json.RawMessage("{}"), nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(raw)); err != nil {
		return nil, err
	}
	if buf.Len() == 0 || buf.Bytes()[0] != '{' {
		return nil, errors.New("not an object")
	}
	return buf.Bytes(), nil
}

func (s *stream) usageEvent() bool {
	u := s.usage
	return s.emit(contract.StreamEvent{Kind: contract.EventUsage, Usage: &u})
}

// done 은 블록을 Index 순서로 모아 최종 메시지를 낸다 (AD-FR-51).
func (s *stream) done(stop contract.StopReason) {
	if s.ended {
		return
	}
	idx := make([]int, 0, len(s.blocks))
	for i := range s.blocks {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	m := contract.Message{ID: s.id, Role: contract.RoleAssistant, Timestamp: s.now()}
	if m.ID == "" {
		m.ID = randomID()
	}
	for _, i := range idx {
		b := *s.blocks[i]
		if b.Kind == contract.BlockText && b.Text == "" {
			continue
		}
		m.Content = append(m.Content, b)
	}
	u := s.usage
	m.Usage, m.StopReason = &u, &stop
	s.emit(contract.StreamEvent{Kind: contract.EventDone, Message: &m})
	s.ended = true
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}

// normalizeID 는 tool call id 를 [a-zA-Z0-9_-] 64자 이하로 바꾼다 (AD-FR-03).
func normalizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if b.Len() == 64 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// samePD 는 ProviderData 를 이 요청에 다시 보내도 되는지 본다 (CT-FR-04, AD-FR-04).
func samePD(pd *contract.ProviderData, m contract.ModelRef) bool {
	return pd != nil && pd.Provider == m.Provider && pd.Model == m.ID
}

// joinText 는 text 블록을 합친다.
func joinText(bs []contract.Block, sep string) string {
	var parts []string
	for _, b := range bs {
		if b.Kind == contract.BlockText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, sep)
}

func hasImage(bs []contract.Block) bool {
	for _, b := range bs {
		if b.Kind == contract.BlockImage {
			return true
		}
	}
	return false
}

func dataURL(img *contract.Image) string {
	return "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

// withExtra 는 Options.Extra 를 최상위에 싣는다 (Extra 가 이긴다).
func withExtra(body map[string]any, extra map[string]json.RawMessage) ([]byte, *contract.CallError) {
	for k, v := range extra {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, invalid("encode request: %v", err)
	}
	return b, nil
}

var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}

func validLevel(l string) bool {
	for _, v := range effortLevels {
		if v == l {
			return true
		}
	}
	return false
}
