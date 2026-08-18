// scale_catalog_q3c.go builds Experimento 1-bis Q3C's 5 catalogs: does the
// lexical-attraction bias Q3B surfaced (a redundant atom whose wording
// echoes the task's words ranked #1 over the canonical one, 3/3 corridas)
// actually flip the *final applied behavior* when the two competing atoms
// disagree — one deprecated, one active — and does status/supersedes
// metadata in the index neutralize it? See the Q3C build prompt (given
// directly in chat, not saved to ~/exo — same additive, throwaway-tooling
// pattern as Q1+Q2/Q3/Q3B).
//
// N is fixed at 50 in every condition, same as Q3/Q3B. What varies per
// condition is which of the two competing atoms gets the lexically-aligned
// wording vs. the paraphrased one, and what (if any) status/supersedes
// annotation the index shows next to each.
package agenthost

// The two atoms genuinely competing in Q3C — deliberately new names, not
// reused from Q3/Q3B, so this round's "target" is unambiguous: worktrees is
// the currently-correct recommendation, branches is the deprecated one the
// project no longer wants. Both content variants (aligned/paraphrased) are
// the exact wording from the build prompt, not paraphrased further here.
const (
	q3cBranchesName  = "parallel-work-branches"  // deprecated
	q3cWorktreesName = "parallel-work-worktrees" // active/correct
)

var q3cBranchesAligned = scaleAtom{
	Name:        q3cBranchesName,
	Description: "Para trabajar en dos features en paralelo sin pisarte, usá ramas (branches) separadas.",
	Body:        "Para trabajar en dos features en paralelo sin pisarte, usá ramas (branches) separadas.",
}

var q3cBranchesParaphrased = scaleAtom{
	Name:        q3cBranchesName,
	Description: "Cuando dos líneas de desarrollo necesiten permanecer activas y ejecutables al mismo tiempo, mantenelas en ramas independientes del mismo checkout.",
	Body:        "Cuando dos líneas de desarrollo necesiten permanecer activas y ejecutables al mismo tiempo, mantenelas en ramas independientes del mismo checkout.",
}

var q3cWorktreesAligned = scaleAtom{
	Name:        q3cWorktreesName,
	Description: "Para trabajar en dos features en paralelo sin pisarte, usá git worktrees.",
	Body:        "Para trabajar en dos features en paralelo sin pisarte, usá git worktrees.",
}

var q3cWorktreesParaphrased = scaleAtom{
	Name:        q3cWorktreesName,
	Description: "Cuando dos líneas de implementación deben permanecer simultáneamente ejecutables, creá árboles de trabajo (working trees) adicionales.",
	Body:        "Cuando dos líneas de implementación deben permanecer simultáneamente ejecutables, creá árboles de trabajo (working trees) adicionales.",
}

// q3cCondition bundles what varies per condition: the two atoms in their
// chosen wording, and any index annotation keyed by atom name.
type q3cCondition struct {
	branches    scaleAtom
	worktrees   scaleAtom
	annotations map[string]string
}

// q3cConditions is the fixed 5-condition table from the build prompt —
// C0 through C4, wording and metadata exactly as specified there.
var q3cConditions = map[string]q3cCondition{
	// C0 — control: both paraphrased, roughly even lexical overlap. Expect
	// worktrees (the correct one) to win on its own.
	"C0": {
		branches:  q3cBranchesParaphrased,
		worktrees: q3cWorktreesParaphrased,
	},
	// C1 — lexical trap, no metadata: branches gets the aligned wording
	// (echoes the task's words), worktrees gets paraphrased.
	"C1": {
		branches:  q3cBranchesAligned,
		worktrees: q3cWorktreesParaphrased,
	},
	// C2 — same trap + simple status metadata in the index.
	"C2": {
		branches:  q3cBranchesAligned,
		worktrees: q3cWorktreesParaphrased,
		annotations: map[string]string{
			q3cBranchesName:  "[deprecated]",
			q3cWorktreesName: "[active]",
		},
	},
	// C3 — same trap + explicit supersedes relation instead of a bare
	// status (branches gets no annotation at all, to isolate the effect of
	// the relation from the effect of a status label).
	"C3": {
		branches:  q3cBranchesAligned,
		worktrees: q3cWorktreesParaphrased,
		annotations: map[string]string{
			q3cWorktreesName: "[active, supersedes: parallel-work-branches]",
		},
	},
	// C4 — inverted trap, no metadata: worktrees (the correct one) gets the
	// aligned wording this time, branches gets paraphrased — confirms the
	// effect measured in C1 is lexical attraction, not a content quirk.
	"C4": {
		branches:  q3cBranchesParaphrased,
		worktrees: q3cWorktreesAligned,
	},
}

// q3cConditionLabels is the fixed iteration order for validation/reporting.
var q3cConditionLabels = []string{"C0", "C1", "C2", "C3", "C4"}

// buildQ3CCatalog assembles one of Q3C's 5 conditions: the two competing
// atoms (in the condition's chosen wording, with the condition's
// annotations) + enough clean distractors to reach N=50. The real
// worktrees-not-code-dir atom (Q3/Q3B's target) is deliberately excluded
// from the filler pool here — it's the same domain as this round's two
// competing atoms and would silently turn a 2-way comparison into a 3-way
// one, which isn't what this round measures.
func buildQ3CCatalog(condition string, seed int64) scaleCatalog {
	cond, ok := q3cConditions[condition]
	if !ok {
		panic("agenthost: buildQ3CCatalog: unknown condition " + condition + " — must be one of C0..C4")
	}

	cleanNeeded := q3N - 2
	baseline := periferiaDistractorsExcluding(q3TargetName) // excludes worktrees-not-code-dir specifically
	var clean []scaleAtom
	switch {
	case cleanNeeded <= len(baseline):
		clean = append(clean, baseline[:cleanNeeded]...)
	default:
		clean = append(clean, baseline...)
		extra := cleanNeeded - len(baseline)
		pool := syntheticDistractorPool()
		if extra > len(pool) {
			panic("agenthost: buildQ3CCatalog: not enough synthetic distractors in pool")
		}
		clean = append(clean, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, q3N)
	all = append(all, cond.branches, cond.worktrees)
	all = append(all, clean...)
	if len(all) != q3N {
		panic("agenthost: buildQ3CCatalog: assembled catalog size mismatch")
	}

	return renderCatalog(q3N, seed, []string{q3cWorktreesName, q3cBranchesName}, all, cond.annotations)
}
