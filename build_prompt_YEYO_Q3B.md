# Build prompt — Experimento 1-bis, Q3B: ambigüedad interna del catálogo (no similitud)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`,
`~/yeyo/docs/codex_consult_escala_catalogo.md` completo (especialmente la
segunda respuesta de Codex, donde reformula la hipótesis de "densidad
semántica" a "entropía de decisión" y propone este experimento como Q3B), y
los reportes `exp1bis-q1q2-report.md` / `exp1bis-q3-report.md`.

## Por qué esta ronda es distinta a Q3

Q3 probó vecinos **claramente distintos** entre sí (cada uno con una
precondición diferente — rama temporal, stash, clone, etc.) y la selección fue
perfecta incluso con 7 a la vez. Esta ronda prueba algo distinto: atoms que
**dicen casi lo mismo**, todos verdaderamente aplicables, sin una señal clara
de cuál es "el" canónico — la hipótesis de Codex es que la degradación
aparece acá, no con vecinos bien diferenciados.

## Catálogo — target + 3 atoms redundantes (contenido exacto)

**Target/canónico**, ya usado en rondas anteriores — `worktrees-not-code-dir`:
> "Usá git worktrees cuando necesites trabajar en dos líneas de desarrollo en
> paralelo, sin pisarte."

**Redundantes** (todos dicen esencialmente lo mismo con distinto enfoque, sin
ninguna precondición que los distinga realmente del target):

1. **`worktrees-parallel-feature`** — "Cuando desarrollás una feature en
   paralelo con otra, preferí worktrees en vez de cambiar de rama."
2. **`worktrees-multiple-fs-state`** — "Cuando te sirve tener múltiples
   estados de filesystem disponibles a la vez, creá un worktree."
3. **`worktrees-avoid-stash-juggling`** — "Si te encontrás yendo y viniendo
   con `stash` entre dos tareas activas al mismo tiempo, un worktree es mejor
   opción."

Mismo N=50 total, mismos distractores limpios de rondas anteriores rellenando
el resto. Misma tarea de prueba: *"Necesito trabajar en dos features en
paralelo sin pisarme."*

3 repeticiones, randomizando posición y orden de todo el catálogo (incluidos
los 3 redundantes) en cada corrida, igual disciplina que rondas anteriores.

## Medición — no todo desvío es una falla

Importante: elegir `worktrees-parallel-feature` en vez del target no es lo
mismo que elegir algo genuinamente incorrecto — los 4 dan, en la práctica,
la misma orientación al usuario. Por eso separá:

- **`get target exacto`** — trajo específicamente `worktrees-not-code-dir`.
- **`get redundante funcionalmente equivalente`** — trajo uno de los otros 3
  (no es una falla real, es información sobre cómo resuelve ambigüedad interna
  del catálogo).
- **`get múltiple`** — trajo más de uno de los 4 (¿nota la redundancia, o los
  trata como cosas separadas?).
- **falla real** — no llegó a `get`, o trajo algo de los distractores limpios
  sin relación.

Agregá también la métrica que sugirió Codex, barata de sumar en la misma
corrida: **antes de que llame `get`, pedile que liste su top-5 de candidatos
por nombre** (sin ejecutar `get` todavía), para poder ver en qué posición
quedó el target aunque el resultado final sea correcto — señal temprana de
degradación aunque no se traduzca en una falla visible.

## Validación previa

Ping simple a `exo`, confirmá que responde bien. Verificá por código que los 4
atoms del grupo target+redundantes están efectivamente en el catálogo con el
contenido exacto de arriba, y que la posición de cada uno se randomiza por
corrida.

## Reporte

`~/yeyo/experiments/exp1bis-q3b-report.md`, con:

- Tabla de las 3 corridas: qué trajo cada una (target exacto / redundante
  equivalente / múltiple / falla real), y el ranking top-5 reportado antes del
  `get`.
- Conclusión: ¿la ambigüedad interna (atoms redundantes) genera algo distinto
  a lo que vimos con vecinos bien diferenciados en Q3? ¿El modelo nota la
  redundancia (trae varios, o menciona que son equivalentes) o la trata como
  si fueran opciones independientes?
- Si aparece degradación real acá (no solo redundancia benigna): es la primera
  señal fuerte de un cuello de botella genuino, y justificaría replantear el
  orden del resto del programa (Q6 antes que Q4, como sugirió Codex). Si sigue
  sin haber fallas reales, reportarlo también — sería la tercera hipótesis de
  Codex cayendo seguida, y valdría la pena decírselo antes de construir nada
  más.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
