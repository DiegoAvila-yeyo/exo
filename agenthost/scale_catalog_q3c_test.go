package agenthost

import (
	"strings"
	"testing"
)

// TestBuildQ3CCatalogConditions is the mandatory "verificá por código que
// cada condición tiene exactamente la redacción y metadata especificada
// arriba, sin mezclarse entre condiciones" check from Q3C's "Validación
// previa".
func TestBuildQ3CCatalogConditions(t *testing.T) {
	cases := []struct {
		condition         string
		wantBranchesDesc  string
		wantWorktreesDesc string
		wantAnnotations   map[string]string
	}{
		{"C0", q3cBranchesParaphrased.Description, q3cWorktreesParaphrased.Description, nil},
		{"C1", q3cBranchesAligned.Description, q3cWorktreesParaphrased.Description, nil},
		{"C2", q3cBranchesAligned.Description, q3cWorktreesParaphrased.Description, map[string]string{
			q3cBranchesName:  "[deprecated]",
			q3cWorktreesName: "[active]",
		}},
		{"C3", q3cBranchesAligned.Description, q3cWorktreesParaphrased.Description, map[string]string{
			q3cWorktreesName: "[active, supersedes: parallel-work-branches]",
		}},
		{"C4", q3cBranchesParaphrased.Description, q3cWorktreesAligned.Description, nil},
	}

	for _, tc := range cases {
		c := buildQ3CCatalog(tc.condition, 1)
		if got := len(c.byName); got != q3N {
			t.Errorf("%s: got %d atoms, want %d", tc.condition, got, q3N)
		}
		branches, ok := c.get(q3cBranchesName)
		if !ok {
			t.Fatalf("%s: %s missing from catalog", tc.condition, q3cBranchesName)
		}
		if branches.Description != tc.wantBranchesDesc {
			t.Errorf("%s: branches description = %q, want %q", tc.condition, branches.Description, tc.wantBranchesDesc)
		}
		worktrees, ok := c.get(q3cWorktreesName)
		if !ok {
			t.Fatalf("%s: %s missing from catalog", tc.condition, q3cWorktreesName)
		}
		if worktrees.Description != tc.wantWorktreesDesc {
			t.Errorf("%s: worktrees description = %q, want %q", tc.condition, worktrees.Description, tc.wantWorktreesDesc)
		}

		// Annotations show up in the rendered index text, not in the atom's
		// own Description field (that stays wording-only) — check the
		// index line itself.
		lineFor := func(name string) string {
			for _, line := range strings.Split(c.IndexText, "\n") {
				if strings.HasPrefix(line, "- "+name+":") {
					return line
				}
			}
			return ""
		}
		for name, suffix := range tc.wantAnnotations {
			full := lineFor(name)
			if full == "" {
				t.Fatalf("%s: no index line found for %q", tc.condition, name)
			}
			if !strings.Contains(full, suffix) {
				t.Errorf("%s: index line for %q = %q, want it to contain %q", tc.condition, name, full, suffix)
			}
		}
		// Conditions with no annotations must not leak the OTHER
		// conditions' bracket metadata onto either atom's line.
		if tc.wantAnnotations == nil {
			for _, name := range []string{q3cBranchesName, q3cWorktreesName} {
				full := lineFor(name)
				if strings.Contains(full, "[") {
					t.Errorf("%s: index line for %q unexpectedly has bracket metadata: %q", tc.condition, name, full)
				}
			}
		}

		// The real worktrees-not-code-dir atom must not leak into a Q3C
		// catalog — it's the same domain as this round's two competing
		// atoms and would turn a 2-way comparison into a 3-way one.
		if _, ok := c.get(q3TargetName); ok {
			t.Errorf("%s: catalog unexpectedly contains %q (Q3/Q3B's target) alongside this round's two competing atoms", tc.condition, q3TargetName)
		}

		if _, ok := c.Positions[q3cWorktreesName]; !ok {
			t.Errorf("%s: Positions missing %q", tc.condition, q3cWorktreesName)
		}
		if _, ok := c.Positions[q3cBranchesName]; !ok {
			t.Errorf("%s: Positions missing %q", tc.condition, q3cBranchesName)
		}
	}
}

// TestBuildQ3CCatalogUnknownConditionPanics guards against a typo'd
// condition label silently running the wrong catalog.
func TestBuildQ3CCatalogUnknownConditionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("buildQ3CCatalog(\"C99\", 1) did not panic")
		}
	}()
	buildQ3CCatalog("C99", 1)
}

// TestBuildQ3CCatalogPositionVariesAcrossSeeds mirrors Q1+Q2/Q3/Q3B's
// equivalent checks — same discipline, not relaxed for Q3C, and both
// tracked positions (not just one) must vary.
func TestBuildQ3CCatalogPositionVariesAcrossSeeds(t *testing.T) {
	worktreesPositions := map[int]bool{}
	branchesPositions := map[int]bool{}
	for seed := int64(1); seed <= int64(20); seed++ {
		c := buildQ3CCatalog("C1", seed)
		worktreesPositions[c.Positions[q3cWorktreesName]] = true
		branchesPositions[c.Positions[q3cBranchesName]] = true
	}
	if len(worktreesPositions) < 5 {
		t.Errorf("worktrees position only took %d distinct values across 20 seeds", len(worktreesPositions))
	}
	if len(branchesPositions) < 5 {
		t.Errorf("branches position only took %d distinct values across 20 seeds", len(branchesPositions))
	}
}

// TestBuildQ3CCatalogDeterministicPerSeed: reruns of a logged Q3C corrida
// must be reconstructible from (condition, seed).
func TestBuildQ3CCatalogDeterministicPerSeed(t *testing.T) {
	a := buildQ3CCatalog("C2", 42)
	b := buildQ3CCatalog("C2", 42)
	if a.IndexText != b.IndexText {
		t.Fatal("buildQ3CCatalog(\"C2\", 42) not deterministic across calls")
	}
}
