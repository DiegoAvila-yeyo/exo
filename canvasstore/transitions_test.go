package canvasstore

import "testing"

func TestValidatePhaseTransition(t *testing.T) {
	allPhases := []Phase{PhaseDraft, PhaseMaterialized, PhaseDeleted}
	allowed := map[[2]Phase]bool{
		{PhaseDraft, PhaseMaterialized}:   true,
		{PhaseDraft, PhaseDeleted}:        true,
		{PhaseMaterialized, PhaseDeleted}: true,
	}
	for _, from := range allPhases {
		for _, to := range allPhases {
			wantOK := allowed[[2]Phase{from, to}]
			err := ValidatePhaseTransition(from, to)
			gotOK := err == nil
			if gotOK != wantOK {
				t.Errorf("ValidatePhaseTransition(%s, %s) err=%v, want ok=%v", from, to, err, wantOK)
			}
		}
	}
}

func TestValidatePhaseTransition_NeverDeletedToAnything(t *testing.T) {
	for _, to := range []Phase{PhaseDraft, PhaseMaterialized, PhaseDeleted} {
		if err := ValidatePhaseTransition(PhaseDeleted, to); err == nil {
			t.Errorf("ValidatePhaseTransition(deleted, %s) = nil, want error (deleted is terminal)", to)
		}
	}
}

func TestValidatePhaseTransition_NeverMaterializedToDraft(t *testing.T) {
	if err := ValidatePhaseTransition(PhaseMaterialized, PhaseDraft); err == nil {
		t.Errorf("ValidatePhaseTransition(materialized, draft) = nil, want error")
	}
}

func TestValidateActivation(t *testing.T) {
	cases := []struct {
		phase      Phase
		activation Activation
		wantOK     bool
	}{
		{PhaseMaterialized, ActivationActive, true},
		{PhaseMaterialized, ActivationInactive, true},
		{PhaseDraft, ActivationActive, false},
		{PhaseDraft, ActivationInactive, false},
		{PhaseDeleted, ActivationActive, false},
		{PhaseDeleted, ActivationInactive, false},
	}
	for _, c := range cases {
		err := ValidateActivation(c.phase, c.activation)
		gotOK := err == nil
		if gotOK != c.wantOK {
			t.Errorf("ValidateActivation(%s, %s) err=%v, want ok=%v", c.phase, c.activation, err, c.wantOK)
		}
	}
}
