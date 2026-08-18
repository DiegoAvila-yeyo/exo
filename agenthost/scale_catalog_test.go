package agenthost

import "testing"

// TestBuildScaleCatalogSizes checks the assembled catalog actually has N
// atoms (target + distractors) at every condition size this round uses, and
// that the target is present with its real yeyo content.
func TestBuildScaleCatalogSizes(t *testing.T) {
	for _, n := range []int{10, 20, 50, 100, 200} {
		c := buildScaleCatalog(n, 1)
		if got := len(c.byName); got != n {
			t.Errorf("n=%d: got %d atoms in catalog, want %d", n, got, n)
		}
		target, ok := c.get(targetAtomName)
		if !ok {
			t.Fatalf("n=%d: target atom %q missing from catalog", n, targetAtomName)
		}
		if target.Body == "" {
			t.Errorf("n=%d: target atom has empty body", n)
		}
		if c.TargetPosition < 1 || c.TargetPosition > n {
			t.Errorf("n=%d: target position %d out of range [1,%d]", n, c.TargetPosition, n)
		}
	}
}

// TestBuildScaleCatalogPositionVariesAcrossSeeds is this round's mandatory
// "verificá por código que la randomización de posición/orden realmente
// varía entre corridas" check (build_prompt_YEYO_Q1Q2.md, "Validación
// previa") — a fixed position across seeds would silently contaminate every
// result this experiment produces.
func TestBuildScaleCatalogPositionVariesAcrossSeeds(t *testing.T) {
	const n = 50
	positions := map[int]bool{}
	for seed := int64(1); seed <= int64(20); seed++ {
		c := buildScaleCatalog(n, seed)
		positions[c.TargetPosition] = true
	}
	if len(positions) < 5 {
		t.Fatalf("target position only took %d distinct values across 20 seeds at n=%d — randomization looks broken (want real spread, not a fixed or near-fixed position)", len(positions), n)
	}
}

// TestBuildScaleCatalogOrderVariesAcrossSeeds checks that it isn't just the
// target's position moving while everything else stays fixed — the full
// index text (distractor order too) must differ across seeds, per the same
// "Precaución" as above.
func TestBuildScaleCatalogOrderVariesAcrossSeeds(t *testing.T) {
	const n = 50
	texts := map[string]bool{}
	for seed := int64(1); seed <= int64(10); seed++ {
		c := buildScaleCatalog(n, seed)
		texts[c.IndexText] = true
	}
	if len(texts) < 8 {
		t.Fatalf("index text only took %d distinct forms across 10 seeds at n=%d — distractor order isn't varying, only (at best) the target's position", len(texts), n)
	}
}

// TestBuildScaleCatalogDeterministicPerSeed: same (n, seed) must reproduce
// the exact same catalog — reruns of a logged corrida should be
// reconstructible from its logged seed.
func TestBuildScaleCatalogDeterministicPerSeed(t *testing.T) {
	a := buildScaleCatalog(50, 42)
	b := buildScaleCatalog(50, 42)
	if a.IndexText != b.IndexText || a.TargetPosition != b.TargetPosition {
		t.Fatalf("buildScaleCatalog(50, 42) not deterministic across calls")
	}
}

// TestSyntheticDistractorPoolLargeEnoughAndUnique guards the N=200 condition
// (needs 191 synthetic distractors on top of the real 8) and checks there
// are no accidental name collisions, which would silently drop atoms from
// the index (map keyed by name).
func TestSyntheticDistractorPoolLargeEnoughAndUnique(t *testing.T) {
	pool := syntheticDistractorPool()
	const need = 190 // N=200 - 1 target - 9 real baseline distractors
	if len(pool) < need {
		t.Fatalf("synthetic distractor pool has %d atoms, need >= %d for the N=200 condition", len(pool), need)
	}
	seen := map[string]bool{}
	for _, a := range pool {
		if seen[a.Name] {
			t.Errorf("duplicate synthetic atom name %q", a.Name)
		}
		seen[a.Name] = true
		if a.Name == targetAtomName {
			t.Errorf("synthetic pool collides with target atom name %q", targetAtomName)
		}
	}
}

// TestBuildScaleCatalogBaselineMatchesRealCatalog: the N=10 condition must
// be exactly the real, already-validated periferia catalog — target + its 9
// real distractors, nothing synthetic mixed in. (10, not 9: Exp1F added 3
// "no-obvious" atoms since docs/vision.md's "9 atoms" description was
// written — see build_prompt_YEYO_Q1Q2.md's note on this and the report's
// "Nota metodológica" for the full explanation.)
func TestBuildScaleCatalogBaselineMatchesRealCatalog(t *testing.T) {
	c := buildScaleCatalog(10, 7)
	if len(c.byName) != 10 {
		t.Fatalf("n=10: got %d atoms, want 10", len(c.byName))
	}
	for _, real := range baselineDistractors() {
		if _, ok := c.get(real.Name); !ok {
			t.Errorf("n=10: real distractor %q missing from baseline catalog", real.Name)
		}
	}
}
