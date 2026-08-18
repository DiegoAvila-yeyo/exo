// scale_catalog_q3b.go builds Experimento 1-bis Q3B's single catalog: does
// *internal ambiguity* — several atoms that all genuinely apply, saying
// almost the same thing with no distinguishing precondition — degrade
// selection differently than Q3's well-differentiated near-neighbors did?
// See the Q3B build prompt (given directly in chat, not saved to ~/exo —
// same "throwaway experiment tooling, additive only" pattern as the Q1+Q2/
// Q3 files).
//
// N is fixed at 50, same target as Q3 (worktrees-not-code-dir), same task.
// The only variable this round is the presence of 3 redundant atoms that,
// unlike Q3's neighbors, have no real precondition separating them from the
// target — a correct pick still has to land on the canonical name, but
// "close enough" (one of the 3 redundants) is a materially different
// outcome than a genuine miss (a clean distractor). See
// checkpoint_scale.go's RunQ3BCheckpoint for how that distinction gets
// measured (top-5-before-get + multi-get detection), not just this file.
package agenthost

import "github.com/yeyoos/yeyo"

// q3bRedundantNames is the group this round measures internal ambiguity
// over: the real target plus its 3 functionally-equivalent redundants.
// Exported as a func (not a package var) so callers always get a fresh
// slice — same defensive pattern as q3Neighbors().
func q3bRedundantGroup() []scaleAtom {
	target := q3TargetAtom() // worktrees-not-code-dir, same target as Q3
	return []scaleAtom{
		target,
		{
			Name:        "worktrees-parallel-feature",
			Description: "Preferí worktrees en vez de cambiar de rama para features en paralelo.",
			Body:        "Cuando desarrollás una feature en paralelo con otra, preferí worktrees en vez de cambiar de rama.",
		},
		{
			Name:        "worktrees-multiple-fs-state",
			Description: "Creá un worktree cuando te sirve tener múltiples estados de filesystem a la vez.",
			Body:        "Cuando te sirve tener múltiples estados de filesystem disponibles a la vez, creá un worktree.",
		},
		{
			Name:        "worktrees-avoid-stash-juggling",
			Description: "Un worktree es mejor que ir y venir con stash entre dos tareas activas.",
			Body:        "Si te encontrás yendo y viniendo con `stash` entre dos tareas activas al mismo tiempo, un worktree es mejor opción.",
		},
	}
}

// q3bRedundantGroupNames is the name-only lookup the report tooling needs
// to classify a get-call as "target exacto" vs. "redundante equivalente"
// vs. neither.
func q3bRedundantGroupNames() []string {
	var out []string
	for _, a := range q3bRedundantGroup() {
		out = append(out, a.Name)
	}
	return out
}

// buildQ3BCatalog assembles the fixed N=50 catalog: target + its 3
// redundants + enough clean distractors (real periferia first, synthetic
// pool topping up — same pools Q1+Q2/Q3 use) to reach 50, then shuffles by
// seed with the same renderCatalog helper (position + full order together,
// no relaxed discipline).
func buildQ3BCatalog(seed int64) scaleCatalog {
	group := q3bRedundantGroup() // [target, +3 redundants], 4 atoms
	cleanNeeded := q3N - len(group)

	baseline := periferiaDistractorsExcluding(q3TargetName)
	var clean []scaleAtom
	switch {
	case cleanNeeded <= len(baseline):
		clean = append(clean, baseline[:cleanNeeded]...)
	default:
		clean = append(clean, baseline...)
		extra := cleanNeeded - len(baseline)
		pool := syntheticDistractorPool()
		if extra > len(pool) {
			panic("agenthost: buildQ3BCatalog: not enough synthetic distractors in pool")
		}
		clean = append(clean, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, q3N)
	all = append(all, group...)
	all = append(all, clean...)
	if len(all) != q3N {
		panic("agenthost: buildQ3BCatalog: assembled catalog size mismatch")
	}

	return renderCatalog(q3N, seed, []string{q3TargetName}, all, nil)
}

// yeyoAtomsForQ3BSanityCheck is a tiny escape hatch for the test file to
// confirm the target this round reuses is really the same real atom Q3
// used — not a re-typed copy that could silently drift.
func yeyoAtomsForQ3BSanityCheck() (yeyo.Atom, bool) {
	return yeyo.Get(q3TargetName)
}
