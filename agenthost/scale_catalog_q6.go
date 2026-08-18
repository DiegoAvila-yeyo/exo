// scale_catalog_q6.go builds Experimento 1-bis Q6's single catalog: does the
// model bring back the *complete set* of atoms that simultaneously apply to
// one task, when all three are the same type (comportamiento) and none of
// them has any authority/specificity/exception relation to the others? See
// the Q6 build prompt (given directly in chat, not saved to ~/exo — same
// additive, throwaway-tooling pattern as Q1+Q2/Q3/Q3B/Q3C/Q4).
//
// Unlike every prior round in this series, Q6 is not about picking the one
// correct atom among distractors — it's about completeness (docs/vision.md,
// "Cuatro métricas de corrección", #4). All 3 target atoms are correct and
// independently relevant; the failure mode this round measures is
// under-selection (bringing a correct subset but forgetting one), not
// picking a wrong atom.
package agenthost

// The three atoms in genuine, conflict-free composition for Q6 — all apply
// at once to the same task, none specializes/excepts/supersedes any other.
// Content is exactly the build prompt's wording, unchanged.
const (
	q6SplitName    = "split-large-file"
	q6PreserveName = "preserve-public-api"
	q6DocsName     = "update-package-docs"
)

var q6Split = scaleAtom{
	Name:        q6SplitName,
	Description: "Si un archivo de código supera las 300 líneas, dividilo en módulos más chicos por responsabilidad.",
	Body:        "Si un archivo de código supera las 300 líneas, dividilo en módulos más chicos por responsabilidad.",
}

var q6Preserve = scaleAtom{
	Name:        q6PreserveName,
	Description: "Al dividir o reorganizar un archivo, preservá las funciones/símbolos públicos existentes — no rompas la superficie pública aunque cambie la organización interna.",
	Body:        "Al dividir o reorganizar un archivo, preservá las funciones/símbolos públicos existentes — no rompas la superficie pública aunque cambie la organización interna.",
}

var q6Docs = scaleAtom{
	Name:        q6DocsName,
	Description: "Cuando cambies la estructura de un paquete (dividir archivos, mover funciones), actualizá la documentación del paquete para que refleje la nueva organización.",
	Body:        "Cuando cambies la estructura de un paquete (dividir archivos, mover funciones), actualizá la documentación del paquete para que refleje la nueva organización.",
}

// q6TargetNames is the expected-set of 3 atoms every corrida should bring
// back — used both to build the catalog and, by the report, as the ground
// truth for precision/recall/F1.
var q6TargetNames = []string{q6SplitName, q6PreserveName, q6DocsName}

// q6N is the fixed catalog size — same discipline as every Q1+Q2..Q4 round.
const q6N = 50

// buildQ6Catalog assembles the 3 target atoms (no annotations — deliberately
// no status/specializes/exception_of metadata anywhere, since Q6 isolates
// pure composition from the authority/precedence questions Q3C/Q4 already
// closed) plus enough clean distractors to reach N=50. protocolo-hulk is
// excluded from the distractor pool even though it's a real, otherwise-clean
// periferia atom — its content ("300 líneas → dividir por responsabilidad")
// is near-identical to split-large-file's domain and would silently turn
// this round's clean-distractor baseline into a near-neighbor condition Q6
// isn't measuring. worktrees-not-code-dir is excluded too, for the same
// domain-isolation consistency Q4 already established (unrelated domain, but
// kept out to match every other round's baseline pool).
func buildQ6Catalog(seed int64) scaleCatalog {
	cleanNeeded := q6N - 3
	baseline := periferiaDistractorsExcluding(targetAtomName) // excludes protocolo-hulk
	var filtered []scaleAtom
	for _, a := range baseline {
		if a.Name == q3TargetName { // also exclude worktrees-not-code-dir
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
			panic("agenthost: buildQ6Catalog: not enough synthetic distractors in pool")
		}
		clean = append(clean, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, q6N)
	all = append(all, q6Split, q6Preserve, q6Docs)
	all = append(all, clean...)
	if len(all) != q6N {
		panic("agenthost: buildQ6Catalog: assembled catalog size mismatch")
	}

	// annotations is nil on purpose: no authority/specificity/exception
	// metadata anywhere in this round's index.
	return renderCatalog(q6N, seed, q6TargetNames, all, nil)
}
