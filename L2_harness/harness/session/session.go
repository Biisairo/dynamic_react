// Package session 은 JSONL 세션 형식과 그것을 env.FileSystem 위에 쓰는 sets.Store 다 (3.3).
package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/inject"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
	"github.com/Biisairo/dynamic_react/L2_harness/harness/env"
)

const (
	lineMessage = "message"
	lineEdit    = "edit"
	lineState   = "state"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// CheckID 는 sessionID 가 파일 이름으로 안전한지 검사한다 (HN-FR-14).
func CheckID(id string) error {
	if !idPattern.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("session: invalid session id %q", id)
	}
	return nil
}

// NewID 는 시간순 정렬 가능한 sessionID 다: 20060102-150405-<8 hex>. entropy 가 짧으면 0 으로 채운다.
func NewID(now time.Time, entropy io.Reader) string {
	var b [4]byte
	_, _ = io.ReadFull(entropy, b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// DefaultDir 은 기본 세션 디렉터리다 (HN-FR-15).
func DefaultDir(home string) string {
	return path.Join(home, ".dynamic_react", "sessions")
}

// Store 는 <dir>/<sessionID>.jsonl 에 append-only 로 쓰는 sets.Store 다.
type Store struct {
	fs  env.FileSystem
	dir string
	mu  sync.Mutex
}

func New(fsys env.FileSystem, dir string) (*Store, error) {
	if !path.IsAbs(dir) {
		return nil, fmt.Errorf("session: dir must be absolute: %q", dir)
	}
	return &Store{fs: fsys, dir: path.Clean(dir)}, nil
}

type stateDelta struct {
	Set     map[string]json.RawMessage `json:"set,omitempty"`
	Delete  []string                   `json:"delete,omitempty"`
	Publish []string                   `json:"publish,omitempty"`
}

type line struct {
	Type    string                `json:"type"`
	Message *contract.Message     `json:"message,omitempty"`
	Edit    *contract.ContextEdit `json:"edit,omitempty"`
	NS      string                `json:"ns,omitempty"`
	Delta   *stateDelta           `json:"delta,omitempty"`
}

func (s *Store) file(id string) (string, error) {
	if err := CheckID(id); err != nil {
		return "", err
	}
	return s.dir + "/" + id + ".jsonl", nil
}

// append 는 줄들을 AppendFile 한 번으로 쓴다 (HN-FR-12).
func (s *Store) append(ctx context.Context, id string, lines []line) error {
	p, err := s.file(id)
	if err != nil || len(lines) == 0 {
		return err
	}
	var buf bytes.Buffer
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("session: encode: %w", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fs.AppendFile(ctx, p, buf.Bytes())
}

func (s *Store) AppendMessages(ctx context.Context, sessionID string, msgs []contract.Message) error {
	lines := make([]line, len(msgs))
	for i := range msgs {
		lines[i] = line{Type: lineMessage, Message: &msgs[i]}
	}
	return s.append(ctx, sessionID, lines)
}

func (s *Store) AppendEdits(ctx context.Context, sessionID string, edits []contract.ContextEdit) error {
	lines := make([]line, len(edits))
	for i := range edits {
		lines[i] = line{Type: lineEdit, Edit: &edits[i]}
	}
	return s.append(ctx, sessionID, lines)
}

func (s *Store) SaveState(ctx context.Context, sessionID, ns string, d inject.StateDelta) error {
	return s.append(ctx, sessionID, []line{{Type: lineState, NS: ns, Delta: &stateDelta{Set: d.Set, Delete: d.Delete, Publish: d.Publish}}})
}

// List 는 dir 의 세션 ID 를 ListDir 순서(파일 이름순)로 돌려준다. dir 이 없으면 빈 목록이다 (HN-FR-37).
func (s *Store) List(ctx context.Context) ([]string, error) {
	entries, err := s.fs.ListDir(ctx, s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, ent := range entries {
		id, ok := strings.CutSuffix(ent.Name, ".jsonl")
		if !ent.IsDir && ok && CheckID(id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// Load 는 파일을 순서대로 재생한다. 개행 없는 마지막 조각은 복구한다 (HN-FR-13).
func (s *Store) Load(ctx context.Context, sessionID string) (sets.Session, error) {
	p, err := s.file(sessionID)
	if err != nil {
		return sets.Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.fs.ReadFile(ctx, p)
	if errors.Is(err, fs.ErrNotExist) {
		return sets.Session{State: map[string]inject.NamespaceState{}}, nil
	}
	if err != nil {
		return sets.Session{}, err
	}
	var lines []line
	rest := data
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			// 개행 없는 마지막 조각: 온전하면 개행을 붙이고, 깨졌으면 버린다.
			var l line
			fixed := data[:len(data)-len(rest)]
			if json.Unmarshal(rest, &l) == nil && l.valid() {
				lines = append(lines, l)
				fixed = append(bytes.Clone(data), '\n')
			}
			if err := s.fs.WriteFile(ctx, p, fixed); err != nil {
				return sets.Session{}, err
			}
			break
		}
		var l line
		if err := json.Unmarshal(rest[:i], &l); err != nil || !l.valid() {
			return sets.Session{}, fmt.Errorf("session: %s: line %d is invalid", p, len(lines)+1)
		}
		lines = append(lines, l)
		rest = rest[i+1:]
	}
	return replay(lines), nil
}

func (l line) valid() bool {
	switch l.Type {
	case lineMessage:
		return l.Message != nil
	case lineEdit:
		return l.Edit != nil
	case lineState:
		return l.Delta != nil && l.NS != ""
	}
	return false
}

func replay(lines []line) sets.Session {
	out := sets.Session{State: map[string]inject.NamespaceState{}}
	published := map[string]map[string]bool{}
	for _, l := range lines {
		switch l.Type {
		case lineMessage:
			out.Messages = append(out.Messages, *l.Message)
		case lineEdit:
			out.Edits = append(out.Edits, *l.Edit)
		case lineState:
			ns, ok := out.State[l.NS]
			if !ok {
				ns = inject.NamespaceState{Values: map[string]json.RawMessage{}}
				published[l.NS] = map[string]bool{}
			}
			for k, v := range l.Delta.Set {
				ns.Values[k] = v
			}
			for _, k := range l.Delta.Delete {
				delete(ns.Values, k)
			}
			for _, k := range l.Delta.Publish {
				published[l.NS][k] = true
			}
			out.State[l.NS] = ns
		}
	}
	for name, keys := range published {
		ns := out.State[name]
		ns.Published = nil
		for k := range keys {
			ns.Published = append(ns.Published, k)
		}
		sort.Strings(ns.Published)
		out.State[name] = ns
	}
	return out
}
