# Build prompt — Experimento 1-bis, Q3: densidad semántica (vecinos cercanos)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`,
`~/yeyo/docs/codex_consult_escala_catalogo.md` completo (especialmente "Q3 —
densidad semántica"), y `~/yeyo/experiments/exp1bis-q1q2-report.md` (el
resultado anterior: escala pura con distractores limpios dio 100% en las 15
corridas, incluso en N=200 — el mecanismo aguanta escala bruta sin degradarse).

## Objetivo

Q1+Q2 confirmó que el tamaño bruto del catálogo no es el problema. La
hipótesis de Codex es que la degradación real, si existe, aparece por
**densidad semántica** — cuántos competidores plausibles y parecidos hay cerca
del atom correcto, no cuántos atoms hay en total. Esta ronda prueba
específicamente eso, con **N fijo en 50** (no varía en esta ronda — esa era la
variable de Q1, ya contestada).

## Atom objetivo y tarea

Mismo target de siempre: `worktrees-not-code-dir` — "Usá git worktrees cuando
necesites trabajar en dos líneas de desarrollo en paralelo, sin pisarte." Misma
tarea de prueba: *"Necesito trabajar en dos features en paralelo sin
pisarme."*

## Los 7 vecinos cercanos (contenido exacto — no improvises otros)

Todos hablan del mismo dominio general (manejar múltiples líneas de trabajo con
git) pero cada uno con una precondición distinta a la del target — el éxito acá
no es keyword matching, es que la IA lea la condición específica:

1. **`git-temp-branch`** — "Usá una rama temporal cuando el trabajo es
   especulativo y no necesita mantener estado de filesystem simultáneo."
2. **`git-stash-context`** — "Usá `git stash` cuando interrumpís brevemente
   una tarea para atender otra, sin necesidad de trabajo paralelo extendido."
3. **`git-second-clone`** — "Usá un segundo clone solo cuando necesites estado
   de dependencias completamente aislado, no solo archivos distintos."
4. **`git-cherry-pick-scratch`** — "Usá una rama de scratch con cherry-pick
   cuando necesites probar un commit específico de forma aislada, sin afectar
   tu rama actual."
5. **`git-shallow-clone`** — "Usá un clone superficial (`--depth`) cuando
   necesites inspeccionar un repo grande rápido, sin el historial completo,
   para una revisión puntual."
6. **`git-detached-head`** — "Usá un checkout en detached HEAD cuando
   necesites probar temporalmente un commit viejo, sin crear una rama nueva."
7. **`git-rebase-onto`** — "Usá `rebase --onto` cuando necesites reorganizar
   commits antes de cambiar de foco, no para mantener dos líneas de trabajo
   corriendo a la vez."

## Las 4 condiciones

Mismo catálogo base de distractores limpios (los 8 dominios de Q1) rellenando
hasta N=50 en las 4 condiciones — lo único que cambia es cuántos de los 7
vecinos cercanos de arriba se incluyen, reemplazando esa cantidad de
distractores limpios para mantener N=50 constante:

- **D0**: 0 vecinos cercanos (solo distractores limpios + target).
- **D1**: 1 vecino cercano (el primero de la lista).
- **D3**: 3 vecinos cercanos (los primeros tres).
- **D7**: los 7 vecinos cercanos.

3 repeticiones por condición (12 corridas). Randomizá posición del target y
orden de todo el catálogo en cada corrida, igual que en Q1+Q2 — no relajar esa
disciplina acá.

## Validación previa

Ping simple a `exo`, confirmá que responde bien. Verificá por código que cada
condición realmente contiene la cantidad correcta de vecinos cercanos y que
sigue sumando N=50 en las cuatro.

## Medición

Por cada corrida: `P(inspect|relevante)`, `P(get\ target|inspect)`,
`P(get\ vecino\ cercano\ en\ vez\ del\ target|inspect)` — **y en ese caso,
registrá cuál vecino específico eligió mal**, para ver si hay un patrón de qué
precondiciones confunden más — `P(sin\ get|inspect)`, `P(apply|get\
correcto)`.

## Reporte

`~/yeyo/experiments/exp1bis-q3-report.md`, con:

- Tabla de las 12 corridas, agrupada por condición (D0/D1/D3/D7).
- Curva de `P(get\ target|inspect)` vs. cantidad de vecinos cercanos — ¿se
  mantiene alta, o empieza a caer con D3/D7?
- Si hubo confusiones: qué vecino específico se eligió mal más seguido, y si
  hay un patrón legible (ej. ¿confunde más con el que tiene la precondición
  más parecida en superficie, como `git-second-clone`, que con uno más
  distinto como `git-rebase-onto`?).
- Comparación directa contra Q1+Q2 (100% con distractores limpios, cualquier N)
  — si D7 cae por debajo de eso, es la primera evidencia real de que la
  densidad semántica, no la escala, es la variable que importa.
- Conclusión: si D7 se mantiene alto, la próxima pregunta lógica es Q4
  (precedencia entre reglas generales y excepciones). Si D7 (o incluso D3) ya
  degrada notablemente, es un hallazgo fuerte — vale la pena reportarlo a
  Codex antes de seguir con Q4, porque cambiaría cómo diseñar el resto del
  programa.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
