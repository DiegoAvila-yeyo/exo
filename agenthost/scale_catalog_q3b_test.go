package agenthost

import "testing"

// TestBuildQ3BCatalogSizeAndGroup is the mandatory "verificá por código que
// los 4 atoms del grupo target+redundantes están efectivamente en el
// catálogo con el contenido exacto" check from the Q3B build prompt's
// "Validación previa".
func TestBuildQ3BCatalogSizeAndGroup(t *testing.T) {
	c := buildQ3BCatalog(1)
	if got := len(c.byName); got != q3N {
		t.Fatalf("got %d atoms, want %d", got, q3N)
	}
	if c.TargetPosition < 1 || c.TargetPosition > q3N {
		t.Errorf("target position %d out of range [1,%d]", c.TargetPosition, q3N)
	}

	real, ok := yeyoAtomsForQ3BSanityCheck()
	if !ok {
		t.Fatal("yeyo.Get(worktrees-not-code-dir) failed — is ~/yeyo/atoms/periferia intact?")
	}

	wantBodies := map[string]string{
		q3TargetName:                     real.Body, // reused verbatim from yeyo, not re-typed
		"worktrees-parallel-feature":     "Cuando desarrollás una feature en paralelo con otra, preferí worktrees en vez de cambiar de rama.",
		"worktrees-multiple-fs-state":    "Cuando te sirve tener múltiples estados de filesystem disponibles a la vez, creá un worktree.",
		"worktrees-avoid-stash-juggling": "Si te encontrás yendo y viniendo con `stash` entre dos tareas activas al mismo tiempo, un worktree es mejor opción.",
	}
	for name, wantBody := range wantBodies {
		got, ok := c.get(name)
		if !ok {
			t.Errorf("catalog missing atom %q", name)
			continue
		}
		if got.Body != wantBody {
			t.Errorf("atom %q body = %q, want %q", name, got.Body, wantBody)
		}
	}
}

// TestBuildQ3BCatalogPositionVariesAcrossSeeds mirrors Q1+Q2/Q3's
// equivalent checks — same discipline, not relaxed for Q3B.
func TestBuildQ3BCatalogPositionVariesAcrossSeeds(t *testing.T) {
	positions := map[int]bool{}
	texts := map[string]bool{}
	for seed := int64(1); seed <= int64(20); seed++ {
		c := buildQ3BCatalog(seed)
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

// TestBuildQ3BCatalogDeterministicPerSeed: reruns of a logged Q3B corrida
// must be reconstructible from its seed.
func TestBuildQ3BCatalogDeterministicPerSeed(t *testing.T) {
	a := buildQ3BCatalog(42)
	b := buildQ3BCatalog(42)
	if a.IndexText != b.IndexText || a.TargetPosition != b.TargetPosition {
		t.Fatalf("buildQ3BCatalog(42) not deterministic across calls")
	}
}
