package canvasstore

import "fmt"

// ValidatePhaseTransition reports whether moving a CanvasObject from one
// Phase to another is allowed. Allowed moves: draft->materialized,
// draft->deleted, materialized->deleted. Never allowed: deleted->anything
// (deletion is terminal — "nada se pierde" means the record stays, but it
// never comes back to life), materialized->draft (materializing is a
// one-way commitment, mirroring the human's own two-phase mental model).
// from == to is not a transition and is rejected too — callers should
// no-op instead of calling this for an unchanged Phase.
func ValidatePhaseTransition(from, to Phase) error {
	switch {
	case from == PhaseDraft && to == PhaseMaterialized:
		return nil
	case from == PhaseDraft && to == PhaseDeleted:
		return nil
	case from == PhaseMaterialized && to == PhaseDeleted:
		return nil
	default:
		return fmt.Errorf("canvasstore: invalid phase transition %s -> %s", from, to)
	}
}

// ValidateActivation reports whether setting activation is legal for an
// object currently in the given phase — activation is only meaningful once
// an object is materialized.
func ValidateActivation(phase Phase, activation Activation) error {
	if phase != PhaseMaterialized {
		return fmt.Errorf("canvasstore: cannot set activation %q on an object in phase %q — activation only applies once materialized", activation, phase)
	}
	switch activation {
	case ActivationActive, ActivationInactive:
		return nil
	default:
		return fmt.Errorf("canvasstore: invalid activation %q", activation)
	}
}
