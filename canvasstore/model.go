// Package canvasstore implements Exo Canvas's object model — a new,
// separate store that sits beside planningstore, not instead of it.
// Composition, not absorption: Planning becomes one Canvas object type
// (type "planning", payload {"planning_id": "..."}), never a migration of
// Planning's own data. planningstore/model.go, store.go and the
// planning_* agent tools are untouched by this package.
//
// One JSON file per project (keyed by ProjectID, the project's absolute
// root path — the same identity already used as chatstore.ChatSession's
// ProjectPath), persisted by Store (store.go), mirroring planningstore's
// write-tmp-then-rename pattern. Unlike planningstore, writes go through
// optimistic concurrency (CAS on Version) rather than last-write-wins,
// because the Canvas's manual-edit HTTP path is the first write path in
// this codebase that does not run under Server.agentMu.
package canvasstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Phase is the existence axis of a CanvasObject: draft (not yet visible on
// the Canvas), materialized (visible), or deleted (never removed from the
// file, only flagged — mirrors planningstore's "nada se pierde" rule).
type Phase string

const (
	PhaseDraft        Phase = "draft"
	PhaseMaterialized Phase = "materialized"
	PhaseDeleted      Phase = "deleted"
)

// Activation is the anchoring axis — whether a materialized object's
// current content is injected into every turn's prompt via dynamicCentro.
// Only meaningful once Phase == PhaseMaterialized; independent of Phase,
// so deactivating an object never touches its Payload, Atoms, or Phase.
type Activation string

const (
	ActivationActive   Activation = "active"
	ActivationInactive Activation = "inactive"
)

// CanvasAtom is one immutable version of a materialized CanvasObject's
// content. Mirrors planningstore.Knowledge's SupersededBy idiom exactly:
// an edit never mutates a CanvasAtom in place, it appends a new one whose
// Supersedes points at the prior head of the chain. Named CanvasAtom (not
// bare Atom) to avoid colliding with the unrelated "yeyo atom"
// catalog/RAG concept already living in agenthost (agenthost/atom_tool.go,
// yeyo.Get) — this package never imports yeyo.
type CanvasAtom struct {
	AtomID     string          `json:"atom_id"`
	ObjectID   string          `json:"object_id"`
	Body       json.RawMessage `json:"body"`
	Supersedes string          `json:"supersedes,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

// CanvasObject is the generic envelope every object type (diagram today;
// image/text/planning-file/music/aprendizajes later) uses with a
// type-specific Payload. Type is an open vocabulary, not a fixed enum.
type CanvasObject struct {
	ObjectID string `json:"object_id"`
	Type     string `json:"type"`
	Name     string `json:"name"` // required at creation — no anonymous drafts

	Phase      Phase      `json:"phase"`
	Activation Activation `json:"activation,omitempty"` // meaningful only when Phase == PhaseMaterialized

	Payload json.RawMessage `json:"payload"`

	// AnchorAtomIDs generally resolves to the head of this object's atom
	// chain (see CurrentAtom) — the atom whose body dynamicCentro injects
	// while Activation == active.
	AnchorAtomIDs []string `json:"anchor_atom_ids,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProjectCanvas is the root aggregate — one JSON file per project.
type ProjectCanvas struct {
	ProjectID       string         `json:"project_id"`
	Objects         []CanvasObject `json:"objects"`
	Atoms           []CanvasAtom   `json:"atoms"` // flat across all objects in this project canvas
	ActiveObjectIDs []string       `json:"active_object_ids"`
	Version         int            `json:"version"` // CAS token — see Store.Save
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}
