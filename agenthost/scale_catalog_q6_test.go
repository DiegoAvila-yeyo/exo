package agenthost

import (
	"strings"
	"testing"
)

// TestBuildQ6CatalogContent is Q6's "verificá por código que los 3 atoms
// están en el catálogo sin ninguna relación de precedencia/autoridad entre
// ellos" check from the build prompt's "Validación previa".
func TestBuildQ6CatalogContent(t *testing.T) {
	c := buildQ6Catalog(1)

	if got := len(c.byName); got != q6N {
		t.Fatalf("got %d atoms, want %d", got, q6N)
	}

	wantDesc := map[string]string{
		q6SplitName:    q6Split.Description,
		q6PreserveName: q6Preserve.Description,
		q6DocsName:     q6Docs.Description,
	}
	for name, want := range wantDesc {
		a, ok := c.get(name)
		if !ok {
			t.Fatalf("%s missing from catalog", name)
		}
		if a.Description != want {
			t.Errorf("%s description = %q, want %q", name, a.Description, want)
		}
	}

	// No authority/specificity/exception metadata anywhere in the rendered
	// index — this is what distinguishes Q6 from Q3C/Q4.
	for _, forbidden := range []string{"status:", "supersedes:", "specializes:", "exception_of:", "[active", "[deprecated"} {
		if strings.Contains(c.IndexText, forbidden) {
			t.Errorf("index text contains %q — Q6 must have no precedence/authority metadata", forbidden)
		}
	}

	// Domain-isolation exclusions: protocolo-hulk (same 300-line-split domain
	// as split-large-file) and worktrees-not-code-dir (excluded for
	// consistency with Q4) must not appear.
	for _, excluded := range []string{targetAtomName, q3TargetName} {
		if _, ok := c.get(excluded); ok {
			t.Errorf("catalog must not contain %q (excluded distractor)", excluded)
		}
	}
}

// TestBuildQ6CatalogPositionVariesAcrossSeeds confirms the 3 target atoms'
// positions aren't pinned to one spot — same discipline as every prior
// round's position-bias check.
func TestBuildQ6CatalogPositionVariesAcrossSeeds(t *testing.T) {
	seeds := []int64{7001, 7002, 7003, 7004, 7005}
	seenSplit := map[int]bool{}
	seenPreserve := map[int]bool{}
	seenDocs := map[int]bool{}
	for _, seed := range seeds {
		c := buildQ6Catalog(seed)
		seenSplit[c.Positions[q6SplitName]] = true
		seenPreserve[c.Positions[q6PreserveName]] = true
		seenDocs[c.Positions[q6DocsName]] = true
	}
	if len(seenSplit) < 2 || len(seenPreserve) < 2 || len(seenDocs) < 2 {
		t.Errorf("expected positions to vary across seeds, got split=%v preserve=%v docs=%v", seenSplit, seenPreserve, seenDocs)
	}
}

// TestBuildQ6CatalogDeterministicPerSeed confirms the same seed always
// produces the same catalog — needed so corrida logs are reproducible.
func TestBuildQ6CatalogDeterministicPerSeed(t *testing.T) {
	a := buildQ6Catalog(42)
	b := buildQ6Catalog(42)
	if a.IndexText != b.IndexText {
		t.Errorf("same seed produced different index text")
	}
}
