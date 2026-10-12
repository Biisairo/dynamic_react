package harness

import (
	"crypto/sha256"
	"encoding/json"
	"sync"

	"github.com/Biisairo/dynamic_react/L0_core/contract"
	"github.com/Biisairo/dynamic_react/L1_inject/sets"
)

// EventPrefixBreak 는 요청 접두가 직전 요청과 달라졌다는 이벤트다 (R29, HN-FR-75).
const EventPrefixBreak EventKind = "prefix_break"

// PrefixPart 는 접두에서 처음 달라진 부분이다.
type PrefixPart string

const (
	PrefixTools    PrefixPart = "tools"
	PrefixSystem   PrefixPart = "system"
	PrefixMessages PrefixPart = "messages"
	PrefixModel    PrefixPart = "model"
)

// PrefixBreak 는 prefix_break 이벤트의 내용이다 (3.6f).
type PrefixBreak struct {
	Part     PrefixPart
	At       int
	Total    int
	Edits    []contract.EditKind
	Expected bool
	Model    contract.ModelRef
}

type digest [sha256.Size]byte

func hashOf(v any) digest {
	b, _ := json.Marshal(v)
	return sha256.Sum256(b)
}

// sentMessage 는 메시지에서 provider 에 보내는 필드다 (HN-FR-74 ③).
type sentMessage struct {
	Role    contract.Role    `json:"role"`
	Content []contract.Block `json:"content,omitempty"`
	Result  *sentResult      `json:"result,omitempty"`
}

type sentResult struct {
	CallID  string              `json:"callId"`
	Status  contract.ToolStatus `json:"status"`
	Content []contract.Block    `json:"content,omitempty"`
}

func hashMessage(m contract.Message) digest {
	s := sentMessage{Role: m.Role, Content: m.Content}
	if m.Result != nil {
		s.Result = &sentResult{CallID: m.Result.CallID, Status: m.Result.Status, Content: m.Result.Content}
	}
	return hashOf(s)
}

// fingerprint 는 맨 위 request.finalize 의 출력으로 만든 지문이다 (HN-FR-74).
type fingerprint struct {
	tools, system digest
	msgs          []digest
	stable        int
	edits         []contract.ContextEdit
}

func newFingerprint(req contract.Request, ephemeral []contract.Message, edits []contract.ContextEdit) *fingerprint {
	fp := &fingerprint{tools: hashOf(req.Tools), system: hashOf(req.System),
		stable: sets.StableLen(req.Messages, ephemeral), edits: edits}
	for _, m := range req.Messages {
		fp.msgs = append(fp.msgs, hashMessage(m))
	}
	return fp
}

// baseline 은 한 세션·모델의 직전 요청이다 (HN-FR-76).
type baseline struct {
	tools, system digest
	msgs          []digest
	edits         map[string]bool
}

type modelKey struct{ provider, id string }

// prefixTable 은 세션 → 모델 → 기준이다. Agent 메모리에만 둔다 (HN-FR-76).
type prefixTable struct {
	mu   sync.Mutex
	sess map[string]map[modelKey]*baseline
}

func newPrefixTable() *prefixTable {
	return &prefixTable{sess: map[string]map[modelKey]*baseline{}}
}

// check 는 fp 를 그 세션·모델의 기준과 비교하고 기준을 fp 로 바꾼다 (HN-FR-75, 76). 깨짐이 없으면 nil.
func (t *prefixTable) check(sid string, model contract.ModelRef, fp *fingerprint) *PrefixBreak {
	t.mu.Lock()
	defer t.mu.Unlock()
	models := t.sess[sid]
	if models == nil {
		models = map[modelKey]*baseline{}
		t.sess[sid] = models
	}
	key := modelKey{model.Provider, model.ID}
	base := models[key]
	var out *PrefixBreak
	switch {
	case base == nil && len(models) > 0:
		out = &PrefixBreak{Part: PrefixModel, At: -1}
	case base == nil:
	default:
		out = compare(base, fp)
	}
	if out != nil {
		out.Model = model
		if base != nil {
			for _, e := range fp.edits {
				if !base.edits[e.ID] {
					out.Edits = append(out.Edits, e.Kind)
				}
			}
		}
		out.Expected = out.Part == PrefixMessages && len(out.Edits) > 0
	}
	next := &baseline{tools: fp.tools, system: fp.system, msgs: fp.msgs[:fp.stable], edits: map[string]bool{}}
	for _, e := range fp.edits {
		next.edits[e.ID] = true
	}
	models[key] = next
	return out
}

func compare(base *baseline, fp *fingerprint) *PrefixBreak {
	total := len(base.msgs)
	switch {
	case base.tools != fp.tools:
		return &PrefixBreak{Part: PrefixTools, At: -1, Total: total}
	case base.system != fp.system:
		return &PrefixBreak{Part: PrefixSystem, At: -1, Total: total}
	}
	for i, h := range base.msgs {
		if i >= len(fp.msgs) || fp.msgs[i] != h {
			return &PrefixBreak{Part: PrefixMessages, At: i, Total: total}
		}
	}
	return nil
}
