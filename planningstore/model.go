// Package planningstore implements Exo's Planning domain model — the
// frozen v1 conceptual model from PLANNING_MANIFESTO.md, translated into
// Go types. It intentionally does not implement the canvas/UI layer: Board
// here is just a name and an ordered list of Knowledge entries surfaced on
// it, not visual objects (frame/arrow/rectangle/...) — those are drawing
// primitives that belong to the frontend, not to the knowledge model.
//
// One JSON file per Planning, persisted by Store (store.go), mirroring
// chatstore's write-tmp-then-rename pattern.
package planningstore

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// KnowledgeType is the user-facing vocabulary from the manifesto. The word
// "Knowledge" itself is architecture and must never reach the UI — callers
// render these as "Notes", "Decisions", "Questions", "Research",
// "Principles", "References".
type KnowledgeType string

const (
	KnowledgeDecision  KnowledgeType = "decision"
	KnowledgePrinciple KnowledgeType = "principle"
	KnowledgeNote      KnowledgeType = "note"
	KnowledgeResearch  KnowledgeType = "research"
	KnowledgeReference KnowledgeType = "reference"
	KnowledgeQuestion  KnowledgeType = "question"
)

// KnowledgeStatus tracks the lifecycle manifesto rule 6: "nada se pierde,
// todo evoluciona" — a Decision or Question is never deleted, only moved
// forward in status.
type KnowledgeStatus string

const (
	StatusOpen       KnowledgeStatus = "open"       // question awaiting an answer, decision awaiting acceptance
	StatusAccepted   KnowledgeStatus = "accepted"   // canonical, currently in force
	StatusSuperseded KnowledgeStatus = "superseded" // replaced by a newer entry, kept for history
	StatusArchived   KnowledgeStatus = "archived"
)

// AuthorState is the authorship/trust state a client renders as border
// style/badge, per the "AI never owns the planning" rule: AI output starts
// as a proposal, never lands as canonical until a human accepts it.
type AuthorState string

const (
	AuthorHuman       AuthorState = "human"
	AuthorAISuggested AuthorState = "ai_suggested"
	AuthorAccepted    AuthorState = "accepted" // an ai_suggested entry a human accepted
	AuthorRejected    AuthorState = "rejected"
	AuthorArchived    AuthorState = "archived"
)

// Knowledge is the one canonical entity behind every Note/Decision/
// Principle/Research/Reference/Question — see manifesto rule 5: new
// concepts are added as a Type, never as a new top-level entity, unless
// they prove to need their own lifecycle and relations.
type Knowledge struct {
	ID     string          `json:"id"`
	Type   KnowledgeType   `json:"type"`
	Title  string          `json:"title"`
	Body   string          `json:"body"` // statement/rationale/content, depending on Type
	Status KnowledgeStatus `json:"status"`
	Author AuthorState     `json:"author"`

	// BoardID is where this entry surfaces via contextual disclosure — the
	// manifesto's "la pantalla por defecto está vacía" rule: a board does
	// not list its Knowledge, it only reveals entries when the user selects
	// a node that references them. Empty means not yet placed on any board.
	BoardID string `json:"board_id,omitempty"`

	// SupersededBy links an accepted Decision/Principle to the entry that
	// replaced it. Set only on the old entry; never delete on supersession.
	SupersededBy string `json:"superseded_by,omitempty"`

	// ResolvedBy links a Question to the Decision that answered it.
	ResolvedBy string `json:"resolved_by,omitempty"`

	// DerivedFrom links a distilled entry (Decision/Principle/Question) back
	// to the raw Research it came from — manifesto rule "lo crudo y lo
	// concluido no se mezclan". Empty for Research itself and for anything
	// captured directly, not distilled from a longer source.
	DerivedFrom string `json:"derived_from,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Board is a named visual space inside a Planning. Its content ownership
// stops at layout/visual objects (out of scope here); Knowledge entries
// reference it via Knowledge.BoardID, not the other way around, so a single
// Decision can surface on more than one board without duplication.
type Board struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectLink is how a Project inherits from its Planning without copying
// it — manifesto rule 2: "Projects nunca modifican Planning directamente;
// solo lo consultan o proponen cambios." Empty Boards/Knowledge IDs means
// "inherit everything" (the common case); non-empty scopes it down.
type ProjectLink struct {
	ProjectID    string    `json:"project_id"`
	Name         string    `json:"name"`
	BoardIDs     []string  `json:"board_ids,omitempty"`
	KnowledgeIDs []string  `json:"knowledge_ids,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Planning is the root aggregate: one JSON file, one source of truth.
type Planning struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Boards    []Board       `json:"boards"`
	Knowledge []Knowledge   `json:"knowledge"`
	Projects  []ProjectLink `json:"projects"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// AddBoard appends a new Board and returns it.
func (p *Planning) AddBoard(name string) Board {
	now := time.Now()
	b := Board{ID: newID("board"), Name: name, CreatedAt: now, UpdatedAt: now}
	p.Boards = append(p.Boards, b)
	p.UpdatedAt = now
	return b
}

// AddKnowledge appends a new Knowledge entry. Decisions default to
// StatusOpen ("proposed") until Accept is called; Notes/Research/
// References/Principles default to StatusAccepted since they don't go
// through a propose/accept flow the same way a Decision does.
func (p *Planning) AddKnowledge(k Knowledge) Knowledge {
	now := time.Now()
	k.ID = newID("kn")
	k.CreatedAt = now
	k.UpdatedAt = now
	if k.Status == "" {
		if k.Type == KnowledgeDecision || k.Type == KnowledgeQuestion {
			k.Status = StatusOpen
		} else {
			k.Status = StatusAccepted
		}
	}
	if k.Author == "" {
		k.Author = AuthorHuman
	}
	p.Knowledge = append(p.Knowledge, k)
	p.UpdatedAt = now
	return k
}

// Supersede marks oldID as superseded by newID. Both must already exist and
// be Decision or Principle entries — this is the manifesto's "nada se
// pierde, todo evoluciona" in code: the old entry is never removed, only
// flagged. Returns false if oldID/newID weren't found.
func (p *Planning) Supersede(oldID, newID string) bool {
	old := p.find(oldID)
	if old == nil || p.find(newID) == nil {
		return false
	}
	old.SupersededBy = newID
	old.Status = StatusSuperseded
	old.UpdatedAt = time.Now()
	p.UpdatedAt = old.UpdatedAt
	return true
}

// ResolveQuestion links a Question to the Decision that answered it and
// closes the question out.
func (p *Planning) ResolveQuestion(questionID, decisionID string) bool {
	q := p.find(questionID)
	if q == nil || q.Type != KnowledgeQuestion || p.find(decisionID) == nil {
		return false
	}
	q.ResolvedBy = decisionID
	q.Status = StatusAccepted
	q.UpdatedAt = time.Now()
	p.UpdatedAt = q.UpdatedAt
	return true
}

// ForBoard returns the Knowledge entries that surface on a given board —
// the data behind contextual disclosure ("selecciona un nodo, aparece su
// Decision/Principle"), not a persistent visible list.
func (p *Planning) ForBoard(boardID string) []Knowledge {
	out := make([]Knowledge, 0)
	for _, k := range p.Knowledge {
		if k.BoardID == boardID {
			out = append(out, k)
		}
	}
	return out
}

// ScopeFor resolves what a linked Project actually sees: everything, unless
// the ProjectLink narrows it down.
func (p *Planning) ScopeFor(projectID string) (boards []Board, knowledge []Knowledge) {
	var link *ProjectLink
	for i := range p.Projects {
		if p.Projects[i].ProjectID == projectID {
			link = &p.Projects[i]
			break
		}
	}
	if link == nil {
		return nil, nil
	}
	if len(link.BoardIDs) == 0 {
		boards = p.Boards
	} else {
		set := toSet(link.BoardIDs)
		for _, b := range p.Boards {
			if set[b.ID] {
				boards = append(boards, b)
			}
		}
	}
	if len(link.KnowledgeIDs) == 0 {
		knowledge = p.Knowledge
	} else {
		set := toSet(link.KnowledgeIDs)
		for _, k := range p.Knowledge {
			if set[k.ID] {
				knowledge = append(knowledge, k)
			}
		}
	}
	return boards, knowledge
}

func (p *Planning) find(id string) *Knowledge {
	for i := range p.Knowledge {
		if p.Knowledge[i].ID == id {
			return &p.Knowledge[i]
		}
	}
	return nil
}

func toSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}
