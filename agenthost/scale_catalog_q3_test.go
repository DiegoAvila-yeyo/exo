package agenthost

import (
	"testing"

	"github.com/yeyoos/yeyo"
)

// TestBuildQ3CatalogConditions is the mandatory "verificá por código que
// cada condición realmente contiene la cantidad correcta de vecinos
// cercanos y que sigue sumando N=50" check from the Q3 build prompt's
// "Validación previa".
func TestBuildQ3CatalogConditions(t *testing.T) {
	neighborNames := q3NeighborNames()
	for _, nNeighbors := range []int{0, 1, 3, 7} {
		c := buildQ3Catalog(nNeighbors, 1)
		if got := len(c.byName); got != q3N {
			t.Errorf("nNeighbors=%d: got %d atoms, want %d", nNeighbors, got, q3N)
		}
		if c.TargetPosition < 1 || c.TargetPosition > q3N {
			t.Errorf("nNeighbors=%d: target position %d out of range [1,%d]", nNeighbors, c.TargetPosition, q3N)
		}
		target, ok := c.get(q3TargetName)
		if !ok || target.Body == "" {
			t.Fatalf("nNeighbors=%d: target atom missing or empty body", nNeighbors)
		}

		gotNeighbors := 0
		for name := range neighborNames {
			if _, ok := c.get(name); ok {
				gotNeighbors++
			}
		}
		if gotNeighbors != nNeighbors {
			t.Errorf("nNeighbors=%d: catalog actually contains %d of the 7 neighbor atoms", nNeighbors, gotNeighbors)
		}

		// The included neighbors must be exactly the expected prefix of
		// q3Neighbors() — D1 must be git-temp-branch, D3 must add
		// git-stash-context and git-second-clone next, etc. Order in the
		// build prompt is fixed, not arbitrary.
		expected := q3Neighbors()[:nNeighbors]
		for _, want := range expected {
			if _, ok := c.get(want.Name); !ok {
				t.Errorf("nNeighbors=%d: expected neighbor %q missing from catalog", nNeighbors, want.Name)
			}
		}
	}
}

// TestBuildQ3CatalogPositionVariesAcrossSeeds mirrors Q1+Q2's equivalent
// check — the position/order randomization discipline is not allowed to
// relax for Q3.
func TestBuildQ3CatalogPositionVariesAcrossSeeds(t *testing.T) {
	positions := map[int]bool{}
	texts := map[string]bool{}
	for seed := int64(1); seed <= int64(20); seed++ {
		c := buildQ3Catalog(7, seed)
		positions[c.TargetPosition] = true
		texts[c.IndexText] = true
	}
	if len(positions) < 5 {
		t.Fatalf("target position only took %d distinct values across 20 seeds — randomization looks broken", len(positions))
	}
	if len(texts) < 15 {
		t.Fatalf("index text only took %d distinct forms across 20 seeds — full-catalog order isn't varying", len(texts))
	}
}

// TestBuildQ3CatalogDeterministicPerSeed: reruns of a logged Q3 corrida
// must be reconstructible from (nNeighbors, seed).
func TestBuildQ3CatalogDeterministicPerSeed(t *testing.T) {
	a := buildQ3Catalog(3, 42)
	b := buildQ3Catalog(3, 42)
	if a.IndexText != b.IndexText || a.TargetPosition != b.TargetPosition {
		t.Fatalf("buildQ3Catalog(3, 42) not deterministic across calls")
	}
}

// TestQ3NeighborsDontCollideWithRealCatalogOrTarget: the 7 synthetic
// neighbor names must not accidentally shadow a real periferia atom or the
// target itself — a collision would silently drop an atom from the index
// (map keyed by name).
func TestQ3NeighborsDontCollideWithRealCatalogOrTarget(t *testing.T) {
	real := map[string]bool{}
	for _, a := range yeyo.Periferia() {
		real[a.Name] = true
	}
	for _, n := range q3Neighbors() {
		if n.Name == q3TargetName {
			t.Errorf("neighbor %q collides with the Q3 target name", n.Name)
		}
		if real[n.Name] {
			t.Errorf("neighbor %q collides with a real periferia atom name", n.Name)
		}
	}
}
