package agenthost

import (
	"context"
	"strings"
	"testing"

	"github.com/yeyoos/yeyo"
)

// TestPhase1RegistryHasOnlyAtomsDecision is exp1k's most important
// validation, run by code rather than by asking the model (lesson from
// exp1-report.md: the model's introspection about its own prompt/tools is
// not reliable). If this ever fails, the experiment's Phase 1 has a tool
// leak and any corrida run against it is invalid.
func TestPhase1RegistryHasOnlyAtomsDecision(t *testing.T) {
	phase1, _, _, _ := buildCheckpointToolsets(false)
	defs := phase1.Definitions()
	if len(defs) != 1 {
		t.Fatalf("phase 1 registry has %d tools, want exactly 1: %+v", len(defs), defs)
	}
	if defs[0].Name != "atoms_decision" {
		t.Fatalf("phase 1 tool = %q, want atoms_decision", defs[0].Name)
	}
}

func TestInspectOpensAtomToolInPlace(t *testing.T) {
	phase1, withAtom, _, result := buildCheckpointToolsets(false)

	_, isErr := phase1.Execute(context.Background(), "atoms_decision", `{"action":"inspect"}`)
	if isErr {
		t.Fatal("atoms_decision(inspect) returned an error result")
	}
	if result.Decision != "inspect" {
		t.Fatalf("result.Decision = %q, want %q", result.Decision, "inspect")
	}

	defs := phase1.Definitions()
	if len(defs) != len(withAtom.Definitions()) {
		t.Fatalf("phase1 has %d tools after inspect, want %d (withAtom's set)", len(defs), len(withAtom.Definitions()))
	}
	found := false
	for _, d := range defs {
		if d.Name == "atom" {
			found = true
		}
		if d.Name == "atoms_decision" {
			t.Fatal("atoms_decision is still registered after the decision was made — Phase 1 never actually closed")
		}
	}
	if !found {
		t.Fatal("atom tool not present in phase1 registry after inspect")
	}
}

func TestSkipDoesNotOpenAtomTool(t *testing.T) {
	phase1, _, withoutAtom, result := buildCheckpointToolsets(false)

	_, isErr := phase1.Execute(context.Background(), "atoms_decision", `{"action":"skip"}`)
	if isErr {
		t.Fatal("atoms_decision(skip) returned an error result")
	}
	if result.Decision != "skip" {
		t.Fatalf("result.Decision = %q, want %q", result.Decision, "skip")
	}

	defs := phase1.Definitions()
	if len(defs) != len(withoutAtom.Definitions()) {
		t.Fatalf("phase1 has %d tools after skip, want %d (withoutAtom's set)", len(defs), len(withoutAtom.Definitions()))
	}
	for _, d := range defs {
		if d.Name == "atom" {
			t.Fatal("atom tool leaked into the toolset after skip")
		}
		if d.Name == "atoms_decision" {
			t.Fatal("atoms_decision is still registered after the decision was made — Phase 1 never actually closed")
		}
	}
}

func TestInvalidActionIsRejectedWithoutOpeningEitherPhase(t *testing.T) {
	phase1, _, _, result := buildCheckpointToolsets(false)

	_, isErr := phase1.Execute(context.Background(), "atoms_decision", `{"action":"maybe"}`)
	if !isErr {
		t.Fatal("atoms_decision(maybe) should return an error result")
	}
	if result.Decision != "" {
		t.Fatalf("result.Decision = %q, want empty (invalid action must not record a decision)", result.Decision)
	}
	defs := phase1.Definitions()
	if len(defs) != 1 || defs[0].Name != "atoms_decision" {
		t.Fatalf("phase1 registry mutated on an invalid action: %+v", defs)
	}
}

// TestK2InspectResultCarriesTheIndex is exp1k2's own version of exp1k's
// "verify by code, not by asking the model" checkpoint: confirms the
// periferia index actually travels inside atoms_decision's own tool result
// on inspect — not in a separate step — before spending any of the 14
// corridas on it.
func TestK2InspectResultCarriesTheIndex(t *testing.T) {
	phase1, _, _, _ := buildCheckpointToolsets(true)

	result, isErr := phase1.Execute(context.Background(), "atoms_decision", `{"action":"inspect"}`)
	if isErr {
		t.Fatal("atoms_decision(inspect) returned an error result")
	}

	index := yeyo.RenderIndex()
	if !strings.Contains(result, index) {
		t.Fatalf("K2 inspect result does not contain the periferia index verbatim.\nresult: %s\nindex: %s", result, index)
	}
}

// TestK1aInspectResultDoesNotCarryTheIndex guards the K1a/K2 boundary: with
// indexOnInspect=false (K1a), the result must stay the short acknowledgement
// — no accidental index leak that would make the two rounds not comparable.
func TestK1aInspectResultDoesNotCarryTheIndex(t *testing.T) {
	phase1, _, _, _ := buildCheckpointToolsets(false)

	result, isErr := phase1.Execute(context.Background(), "atoms_decision", `{"action":"inspect"}`)
	if isErr {
		t.Fatal("atoms_decision(inspect) returned an error result")
	}

	// The index always starts with this literal header (see yeyo.RenderIndex).
	if strings.Contains(result, "Atom catalog (periferia)") {
		t.Fatalf("K1a inspect result unexpectedly carries the index: %s", result)
	}
}
