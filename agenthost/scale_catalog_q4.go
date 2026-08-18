// scale_catalog_q4.go builds Experimento 4/Q4's 4 catalogs: does the model
// resolve precedence between a general rule (G), a specialization of it (S),
// and an exception to the specialization (E) — correctly picking E — even
// when the task's lexical signal points at G or S instead? See the Q4 build
// prompt (given directly in chat, not saved to ~/exo — same additive,
// throwaway-tooling pattern as Q1+Q2/Q3/Q3B/Q3C).
//
// Unlike Q3C, this round is not about authority (status/supersedes) — all 3
// atoms are status:active in every condition, deliberately, to isolate the
// question that's still open: specializes/exception_of precedence under
// adversarial lexical pressure. N is fixed at 50, same as Q3/Q3B/Q3C. What
// varies per condition is which one of the 3 atoms gets the lexically-aligned
// wording (echoing "HTTP handler generado ... 300 líneas") vs. the neutral
// paraphrase — the specializes/exception_of relations themselves, and their
// index annotations, never change across conditions.
package agenthost

// The three atoms in genuine precedence relation for Q4 — new names, not
// reused from earlier rounds. E (the exception) is the only correct answer
// in every condition: the task is a generated HTTP handler over 300 lines,
// and generated code is exempt from the specialization S imposes on
// hand-written HTTP handlers, which in turn specializes the general rule G.
const (
	q4GeneralName   = "file-size-general"            // G
	q4SpecificName  = "http-handler-transport-split" // S, specializes G
	q4ExceptionName = "generated-handler-exempt"     // E, exception_of S
)

// q4Annotations is fixed across all 4 conditions — the structural relations
// (scope/specializes/exception_of) never change, only the wording does. All
// 3 are status:active on purpose (see file doc-comment): this round isolates
// specificity/exception precedence from the authority question Q3C already
// closed.
var q4Annotations = map[string]string{
	q4GeneralName:   "[active, scope: source-file]",
	q4SpecificName:  "[active, scope: http-handler, specializes: file-size-general]",
	q4ExceptionName: "[active, scope: generated-http-handler, exception_of: http-handler-transport-split]",
}

var q4GeneralNeutral = scaleAtom{
	Name:        q4GeneralName,
	Description: "Cualquier archivo de código que crezca demasiado en tamaño debería dividirse según su responsabilidad.",
	Body:        "Cualquier archivo de código que crezca demasiado en tamaño debería dividirse según su responsabilidad.",
}

var q4GeneralAligned = scaleAtom{
	Name:        q4GeneralName,
	Description: "Si un HTTP handler generado supera las 300 líneas, dividilo por responsabilidad.",
	Body:        "Si un HTTP handler generado supera las 300 líneas, dividilo por responsabilidad.",
}

var q4SpecificNeutral = scaleAtom{
	Name:        q4SpecificName,
	Description: "El código que traduce entre el mundo HTTP y la lógica de negocio debe mantenerse separado cuando crece.",
	Body:        "El código que traduce entre el mundo HTTP y la lógica de negocio debe mantenerse separado cuando crece.",
}

var q4SpecificAligned = scaleAtom{
	Name:        q4SpecificName,
	Description: "Los HTTP handlers generados que superan 300 líneas deben separar transporte de dominio.",
	Body:        "Los HTTP handlers generados que superan 300 líneas deben separar transporte de dominio.",
}

var q4ExceptionNeutral = scaleAtom{
	Name:        q4ExceptionName,
	Description: "El código producido automáticamente por herramientas no debe reorganizarse manualmente, incluso si excede los límites normales de tamaño.",
	Body:        "El código producido automáticamente por herramientas no debe reorganizarse manualmente, incluso si excede los límites normales de tamaño.",
}

var q4ExceptionAligned = scaleAtom{
	Name:        q4ExceptionName,
	Description: "HTTP handlers generados que superan 300 líneas están exentos de modularización.",
	Body:        "HTTP handlers generados que superan 300 líneas están exentos de modularización.",
}

// q4Condition bundles which wording (neutral/aligned) each of the 3 atoms
// gets for one of Q4's 4 conditions. Annotations are always q4Annotations —
// never overridden per condition.
type q4Condition struct {
	general   scaleAtom
	specific  scaleAtom
	exception scaleAtom
}

// q4Conditions is the fixed 4-condition table from the build prompt — Q4-A
// through Q4-D, wording exactly as specified there.
var q4Conditions = map[string]q4Condition{
	// Q4-A: E aligned, G and S neutral — lexical signal favors the correct
	// answer.
	"Q4-A": {
		general:   q4GeneralNeutral,
		specific:  q4SpecificNeutral,
		exception: q4ExceptionAligned,
	},
	// Q4-B: all 3 neutral — no lexical advantage either way, pure structural
	// reasoning.
	"Q4-B": {
		general:   q4GeneralNeutral,
		specific:  q4SpecificNeutral,
		exception: q4ExceptionNeutral,
	},
	// Q4-C — the critical condition: G aligned, S and E neutral. Lexical
	// signal pushes toward the general rule (wrong); the correct answer (E)
	// gets no surface advantage at all.
	"Q4-C": {
		general:   q4GeneralAligned,
		specific:  q4SpecificNeutral,
		exception: q4ExceptionNeutral,
	},
	// Q4-D: S aligned, G and E neutral — lexical signal pushes toward the
	// specific rule (also wrong), testing whether the exception still wins
	// even against a competitor with more surface overlap.
	"Q4-D": {
		general:   q4GeneralNeutral,
		specific:  q4SpecificAligned,
		exception: q4ExceptionNeutral,
	},
}

// q4ConditionLabels is the fixed iteration order for validation/reporting.
var q4ConditionLabels = []string{"Q4-A", "Q4-B", "Q4-C", "Q4-D"}

// q4N is the fixed catalog size for every Q4 condition, same discipline as
// Q3/Q3B/Q3C.
const q4N = 50

// buildQ4Catalog assembles one of Q4's 4 conditions: the 3 atoms in genuine
// precedence relation (in the condition's chosen wording, with the fixed
// q4Annotations) + enough clean distractors to reach N=50. The real
// protocolo-hulk and worktrees-not-code-dir atoms are excluded from the
// filler pool — protocolo-hulk in particular shares this round's exact
// domain (file size / splitting by responsibility) and would silently turn
// a 3-way comparison into a 4-way one.
func buildQ4Catalog(condition string, seed int64) scaleCatalog {
	cond, ok := q4Conditions[condition]
	if !ok {
		panic("agenthost: buildQ4Catalog: unknown condition " + condition + " — must be one of Q4-A..Q4-D")
	}

	cleanNeeded := q4N - 3
	baseline := periferiaDistractorsExcluding(targetAtomName) // excludes protocolo-hulk (Q1+Q2's target, same file-size domain)
	var filtered []scaleAtom
	for _, a := range baseline {
		if a.Name == q3TargetName { // also exclude worktrees-not-code-dir, kept out for consistency with other rounds' domain isolation
			continue
		}
		filtered = append(filtered, a)
	}
	baseline = filtered

	var clean []scaleAtom
	switch {
	case cleanNeeded <= len(baseline):
		clean = append(clean, baseline[:cleanNeeded]...)
	default:
		clean = append(clean, baseline...)
		extra := cleanNeeded - len(baseline)
		pool := syntheticDistractorPool()
		if extra > len(pool) {
			panic("agenthost: buildQ4Catalog: not enough synthetic distractors in pool")
		}
		clean = append(clean, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, q4N)
	all = append(all, cond.general, cond.specific, cond.exception)
	all = append(all, clean...)
	if len(all) != q4N {
		panic("agenthost: buildQ4Catalog: assembled catalog size mismatch")
	}

	return renderCatalog(q4N, seed, []string{q4ExceptionName, q4SpecificName, q4GeneralName}, all, q4Annotations)
}
