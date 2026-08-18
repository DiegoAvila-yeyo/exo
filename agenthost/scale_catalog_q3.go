// scale_catalog_q3.go builds Experimento 1-bis Q3's catalogs: does
// P(get correcto|inspect) degrade with genuinely confusing distractors
// (same domain, similar surface, different precondition) instead of clean
// ones? See build_prompt for Q3 (given directly in chat, not saved to
// ~/exo — same "throwaway experiment tooling, additive only" pattern as
// scale_catalog.go/scale_catalog_test.go for Q1+Q2).
//
// N is fixed at 50 for every condition in this round (that was Q1's
// variable, already answered) — what varies is how many of the 7
// hand-written near-neighbor atoms below replace clean filler distractors,
// with the total kept at 50 throughout.
package agenthost

import "github.com/yeyoos/yeyo"

// q3TargetName is Q3's target atom — different from Q1+Q2's
// protocolo-hulk, but the same "pull the real atom, don't invent content"
// rule applies.
const q3TargetName = "worktrees-not-code-dir"

// q3N is the fixed catalog size for every Q3 condition.
const q3N = 50

// q3TargetAtom fetches the real worktrees-not-code-dir atom from yeyo.
func q3TargetAtom() scaleAtom {
	a, ok := yeyo.Get(q3TargetName)
	if !ok {
		panic("agenthost: yeyo atom \"worktrees-not-code-dir\" not found — is ~/yeyo/atoms/periferia intact?")
	}
	return scaleAtom{Name: a.Name, Description: a.Description, Body: a.Body}
}

// q3Neighbors returns the 7 near-neighbor atoms in the exact order and
// content given in the Q3 build prompt — verbatim, not paraphrased. Each
// shares the general domain (git, multiple lines of work) with the target
// but differs on the specific precondition, so a correct pick requires
// reading the precondition, not domain-level keyword matching.
func q3Neighbors() []scaleAtom {
	return []scaleAtom{
		{
			Name:        "git-temp-branch",
			Description: "Rama temporal para trabajo especulativo.",
			Body:        "Usá una rama temporal cuando el trabajo es especulativo y no necesita mantener estado de filesystem simultáneo.",
		},
		{
			Name:        "git-stash-context",
			Description: "git stash para interrupciones breves.",
			Body:        "Usá `git stash` cuando interrumpís brevemente una tarea para atender otra, sin necesidad de trabajo paralelo extendido.",
		},
		{
			Name:        "git-second-clone",
			Description: "Segundo clone para aislar dependencias.",
			Body:        "Usá un segundo clone solo cuando necesites estado de dependencias completamente aislado, no solo archivos distintos.",
		},
		{
			Name:        "git-cherry-pick-scratch",
			Description: "Rama de scratch con cherry-pick para probar un commit aislado.",
			Body:        "Usá una rama de scratch con cherry-pick cuando necesites probar un commit específico de forma aislada, sin afectar tu rama actual.",
		},
		{
			Name:        "git-shallow-clone",
			Description: "Clone superficial para inspección rápida de un repo grande.",
			Body:        "Usá un clone superficial (`--depth`) cuando necesites inspeccionar un repo grande rápido, sin el historial completo, para una revisión puntual.",
		},
		{
			Name:        "git-detached-head",
			Description: "Detached HEAD para probar un commit viejo temporalmente.",
			Body:        "Usá un checkout en detached HEAD cuando necesites probar temporalmente un commit viejo, sin crear una rama nueva.",
		},
		{
			Name:        "git-rebase-onto",
			Description: "rebase --onto para reorganizar commits antes de cambiar de foco.",
			Body:        "Usá `rebase --onto` cuando necesites reorganizar commits antes de cambiar de foco, no para mantener dos líneas de trabajo corriendo a la vez.",
		},
	}
}

// q3NeighborNames is the name-only lookup q3 report tooling needs to tell
// "picked a near-neighbor" apart from "picked an unrelated clean
// distractor" when a get-target misses.
func q3NeighborNames() map[string]bool {
	out := make(map[string]bool)
	for _, n := range q3Neighbors() {
		out[n.Name] = true
	}
	return out
}

// buildQ3Catalog assembles one of Q3's 4 conditions: target +
// nNeighbors near-neighbors (a prefix of q3Neighbors(), in the given order)
// + enough clean distractors (real periferia first, synthetic pool topping
// up) to reach exactly q3N=50 atoms total, then shuffles by seed exactly
// like buildScaleCatalog does (position + full order together). Panics if
// nNeighbors is out of [0,7] — a programmer error, not a runtime condition.
func buildQ3Catalog(nNeighbors int, seed int64) scaleCatalog {
	neighbors := q3Neighbors()
	if nNeighbors < 0 || nNeighbors > len(neighbors) {
		panic("agenthost: buildQ3Catalog: nNeighbors must be in [0,7]")
	}
	chosenNeighbors := neighbors[:nNeighbors]

	target := q3TargetAtom()
	cleanNeeded := q3N - 1 - nNeighbors

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
			panic("agenthost: buildQ3Catalog: not enough synthetic distractors in pool")
		}
		clean = append(clean, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, q3N)
	all = append(all, target)
	all = append(all, chosenNeighbors...)
	all = append(all, clean...)
	if len(all) != q3N {
		panic("agenthost: buildQ3Catalog: assembled catalog size mismatch")
	}

	return renderCatalog(q3N, seed, []string{q3TargetName}, all, nil)
}
