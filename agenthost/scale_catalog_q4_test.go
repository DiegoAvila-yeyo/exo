package agenthost

import (
	"strings"
	"testing"
)

// TestBuildQ4CatalogConditions is the mandatory "verificá por código que
// cada condición tiene exactamente la redacción especificada arriba para
// cada atom (alineada o neutral, según corresponda), y que las relaciones
// specializes/exception_of están presentes en las 4 condiciones sin
// cambios" check from Q4's "Validación previa".
func TestBuildQ4CatalogConditions(t *testing.T) {
	cases := []struct {
		condition     string
		wantGeneral   string
		wantSpecific  string
		wantException string
	}{
		{"Q4-A", q4GeneralNeutral.Description, q4SpecificNeutral.Description, q4ExceptionAligned.Description},
		{"Q4-B", q4GeneralNeutral.Description, q4SpecificNeutral.Description, q4ExceptionNeutral.Description},
		{"Q4-C", q4GeneralAligned.Description, q4SpecificNeutral.Description, q4ExceptionNeutral.Description},
		{"Q4-D", q4GeneralNeutral.Description, q4SpecificAligned.Description, q4ExceptionNeutral.Description},
	}

	lineFor := func(indexText, name string) string {
		for _, line := range strings.Split(indexText, "\n") {
			if strings.HasPrefix(line, "- "+name+":") {
				return line
			}
		}
		return ""
	}

	for _, tc := range cases {
		c := buildQ4Catalog(tc.condition, 1)
		if got := len(c.byName); got != q4N {
			t.Errorf("%s: got %d atoms, want %d", tc.condition, got, q4N)
		}

		general, ok := c.get(q4GeneralName)
		if !ok {
			t.Fatalf("%s: %s missing from catalog", tc.condition, q4GeneralName)
		}
		if general.Description != tc.wantGeneral {
			t.Errorf("%s: general description = %q, want %q", tc.condition, general.Description, tc.wantGeneral)
		}

		specific, ok := c.get(q4SpecificName)
		if !ok {
			t.Fatalf("%s: %s missing from catalog", tc.condition, q4SpecificName)
		}
		if specific.Description != tc.wantSpecific {
			t.Errorf("%s: specific description = %q, want %q", tc.condition, specific.Description, tc.wantSpecific)
		}

		exception, ok := c.get(q4ExceptionName)
		if !ok {
			t.Fatalf("%s: %s missing from catalog", tc.condition, q4ExceptionName)
		}
		if exception.Description != tc.wantException {
			t.Errorf("%s: exception description = %q, want %q", tc.condition, exception.Description, tc.wantException)
		}

		// specializes/exception_of/status annotations must be present,
		// identical, in every condition — never toggled by wording.
		for name, suffix := range q4Annotations {
			full := lineFor(c.IndexText, name)
			if full == "" {
				t.Fatalf("%s: no index line found for %q", tc.condition, name)
			}
			if !strings.Contains(full, suffix) {
				t.Errorf("%s: index line for %q = %q, want it to contain %q", tc.condition, name, full, suffix)
			}
		}

		// protocolo-hulk (same file-size domain) and worktrees-not-code-dir
		// must not leak into the filler pool — would silently turn the 3-way
		// comparison into a 4-way or 5-way one.
		if _, ok := c.get(targetAtomName); ok {
			t.Errorf("%s: catalog unexpectedly contains %q (Q1+Q2's target, same file-size domain)", tc.condition, targetAtomName)
		}
		if _, ok := c.get(q3TargetName); ok {
			t.Errorf("%s: catalog unexpectedly contains %q", tc.condition, q3TargetName)
		}

		for _, name := range []string{q4GeneralName, q4SpecificName, q4ExceptionName} {
			if _, ok := c.Positions[name]; !ok {
				t.Errorf("%s: Positions missing %q", tc.condition, name)
			}
		}
	}
}

// TestBuildQ4CatalogUnknownConditionPanics guards against a typo'd
// condition label silently running the wrong catalog.
func TestBuildQ4CatalogUnknownConditionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildQ4Catalog(\"Q4-Z\", 1) did not panic")
		}
	}()
	buildQ4Catalog("Q4-Z", 1)
}

// TestBuildQ4CatalogPositionVariesAcrossSeeds mirrors Q1+Q2/Q3/Q3B/Q3C's
// equivalent checks — same discipline, all 3 tracked positions must vary.
func TestBuildQ4CatalogPositionVariesAcrossSeeds(t *testing.T) {
	positions := map[string]map[int]bool{
		q4GeneralName:   {},
		q4SpecificName:  {},
		q4ExceptionName: {},
	}
	for seed := int64(1); seed <= int64(20); seed++ {
		c := buildQ4Catalog("Q4-C", seed)
		for name := range positions {
			positions[name][c.Positions[name]] = true
		}
	}
	for name, seen := range positions {
		if len(seen) < 5 {
			t.Errorf("%s position only took %d distinct values across 20 seeds", name, len(seen))
		}
	}
}

// TestBuildQ4CatalogDeterministicPerSeed: reruns of a logged Q4 corrida
// must be reconstructible from (condition, seed).
func TestBuildQ4CatalogDeterministicPerSeed(t *testing.T) {
	a := buildQ4Catalog("Q4-C", 42)
	b := buildQ4Catalog("Q4-C", 42)
	if a.IndexText != b.IndexText {
		t.Fatal("buildQ4Catalog(\"Q4-C\", 42) not deterministic across calls")
	}
}
