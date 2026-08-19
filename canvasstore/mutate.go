package canvasstore

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrNameRequired is returned by AddDraft when name is empty — every draft
// must be named before it's persisted, so a later ambiguous "ya, hazlo" has
// something to disambiguate against.
var ErrNameRequired = errors.New("canvasstore: draft name is required")

// ErrObjectNotFound is returned by any mutator given an ObjectID that
// doesn't exist in this ProjectCanvas.
var ErrObjectNotFound = errors.New("canvasstore: object not found")

// AddDraft appends a new Phase:"draft" CanvasObject and returns it. The
// object is not visible on the Canvas until Materialize is called on it.
func (pc *ProjectCanvas) AddDraft(objType, name string, payload json.RawMessage) (CanvasObject, error) {
	if name == "" {
		return CanvasObject{}, ErrNameRequired
	}
	now := time.Now()
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	obj := CanvasObject{
		ObjectID:  newID("object"),
		Type:      objType,
		Name:      name,
		Phase:     PhaseDraft,
		Payload:   payload,
		CreatedAt: now,
		UpdatedAt: now,
	}
	pc.Objects = append(pc.Objects, obj)
	pc.UpdatedAt = now
	return obj, nil
}

// Materialize flips an existing Phase:"draft" object to Phase:"materialized",
// making it visible on the Canvas. Only operates on an already-existing
// object — it cannot create and materialize in one call.
func (pc *ProjectCanvas) Materialize(objectID string) error {
	obj := pc.find(objectID)
	if obj == nil {
		return ErrObjectNotFound
	}
	if err := ValidatePhaseTransition(obj.Phase, PhaseMaterialized); err != nil {
		return err
	}
	obj.Phase = PhaseMaterialized
	obj.UpdatedAt = time.Now()
	pc.UpdatedAt = obj.UpdatedAt
	return nil
}

// Delete flags an object as Phase:"deleted" — it is never removed from the
// file, only marked, per "nada se pierde, todo evoluciona."
func (pc *ProjectCanvas) Delete(objectID string) error {
	obj := pc.find(objectID)
	if obj == nil {
		return ErrObjectNotFound
	}
	if err := ValidatePhaseTransition(obj.Phase, PhaseDeleted); err != nil {
		return err
	}
	obj.Phase = PhaseDeleted
	obj.Activation = ""
	obj.UpdatedAt = time.Now()
	pc.UpdatedAt = obj.UpdatedAt
	pc.removeFromActive(objectID)
	return nil
}

// SetActivation flips whether a materialized object is currently anchoring
// context (see dynamicCentro in agenthost). It never touches Phase,
// Payload, or the object's atoms.
func (pc *ProjectCanvas) SetActivation(objectID string, a Activation) error {
	obj := pc.find(objectID)
	if obj == nil {
		return ErrObjectNotFound
	}
	if err := ValidateActivation(obj.Phase, a); err != nil {
		return err
	}
	obj.Activation = a
	obj.UpdatedAt = time.Now()
	pc.UpdatedAt = obj.UpdatedAt
	if a == ActivationActive {
		pc.addToActive(objectID)
	} else {
		pc.removeFromActive(objectID)
	}
	return nil
}

// AppendAtom versions an object's content: it never mutates an existing
// CanvasAtom, it creates a new one whose Supersedes points at the current
// chain head (if any) and updates the object's AnchorAtomIDs to the new
// head. This is the one write path both manual edits and tool-driven edits
// use for content changes.
func (pc *ProjectCanvas) AppendAtom(objectID string, body json.RawMessage) (CanvasAtom, error) {
	obj := pc.find(objectID)
	if obj == nil {
		return CanvasAtom{}, ErrObjectNotFound
	}
	now := time.Now()
	var supersedes string
	if head, ok := pc.CurrentAtom(objectID); ok {
		supersedes = head.AtomID
	}
	atom := CanvasAtom{
		AtomID:     newID("atom"),
		ObjectID:   objectID,
		Body:       body,
		Supersedes: supersedes,
		CreatedAt:  now,
	}
	pc.Atoms = append(pc.Atoms, atom)
	obj.AnchorAtomIDs = []string{atom.AtomID}
	obj.UpdatedAt = now
	pc.UpdatedAt = now
	return atom, nil
}

// CurrentAtom walks objectID's atom chain to its un-superseded end — the
// "current" version dynamicCentro should inject while the object is
// active. Reports false if the object has no atoms yet.
func (pc *ProjectCanvas) CurrentAtom(objectID string) (CanvasAtom, bool) {
	var latest CanvasAtom
	found := false
	superseded := make(map[string]bool)
	for _, a := range pc.Atoms {
		if a.Supersedes != "" {
			superseded[a.Supersedes] = true
		}
	}
	for _, a := range pc.Atoms {
		if a.ObjectID != objectID || superseded[a.AtomID] {
			continue
		}
		if !found || a.CreatedAt.After(latest.CreatedAt) {
			latest = a
			found = true
		}
	}
	return latest, found
}

func (pc *ProjectCanvas) find(objectID string) *CanvasObject {
	for i := range pc.Objects {
		if pc.Objects[i].ObjectID == objectID {
			return &pc.Objects[i]
		}
	}
	return nil
}

func (pc *ProjectCanvas) addToActive(objectID string) {
	for _, id := range pc.ActiveObjectIDs {
		if id == objectID {
			return
		}
	}
	pc.ActiveObjectIDs = append(pc.ActiveObjectIDs, objectID)
}

func (pc *ProjectCanvas) removeFromActive(objectID string) {
	out := pc.ActiveObjectIDs[:0]
	for _, id := range pc.ActiveObjectIDs {
		if id != objectID {
			out = append(out, id)
		}
	}
	pc.ActiveObjectIDs = out
}
