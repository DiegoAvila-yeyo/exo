# Build prompt — Experimento 1-bis, Q3C: sesgo léxico vs. autoridad explícita

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`,
`~/yeyo/docs/codex_consult_escala_catalogo.md` completo (especialmente la
tercera respuesta de Codex, donde nombra el "lexical-attraction bias" y
propone este experimento exacto), y `~/yeyo/experiments/exp1bis-q3b-report.md`
(el hallazgo que motiva esto: en Q3B, un atom redundante con la palabra
"paralelo" en su descripción ganó el ranking #1 en 3/3 corridas sobre el
target canónico, pese a posiciones randomizadas — sospecha de sesgo léxico de
superficie, no de canonicidad).

## Por qué esta ronda es distinta a Q3B — acá sí importa quién gana

En Q3B los 4 atoms redundantes daban la misma recomendación final, así que el
sesgo de ranking nunca se tradujo en un error real. Acá los dos atoms en
competencia **dan recomendaciones genuinamente distintas y contradictorias**
— uno está deprecado (recomienda algo que el proyecto ya no quiere) y el otro
es el vigente (recomienda lo correcto). Si el deprecado gana por atracción
léxica, es un error de producción real, no solo un dato de ranking.

## Los dos atoms en competencia — dos versiones de redacción cada uno

**`parallel-work-branches`** (deprecado — la recomendación es incorrecta según
la convención actual del proyecto):
- *Redacción alineada léxicamente* (comparte palabras con la tarea de
  prueba): "Para trabajar en dos features en paralelo sin pisarte, usá ramas
  (branches) separadas."
- *Redacción parafraseada* (mismo significado, sin las palabras literales de
  la tarea): "Cuando dos líneas de desarrollo necesiten permanecer activas y
  ejecutables al mismo tiempo, mantenelas en ramas independientes del mismo
  checkout."

**`parallel-work-worktrees`** (vigente — la recomendación correcta, coherente
con todo lo usado en rondas anteriores):
- *Redacción alineada léxicamente*: "Para trabajar en dos features en
  paralelo sin pisarte, usá git worktrees."
- *Redacción parafraseada*: "Cuando dos líneas de implementación deben
  permanecer simultáneamente ejecutables, creá árboles de trabajo (working
  trees) adicionales."

**Tarea de prueba, fija en las 5 condiciones**: "Necesito trabajar en dos
features en paralelo sin pisarme."

## Las 5 condiciones

Mismo catálogo base N=50 (target + distractores limpios de rondas anteriores)
en todas — lo único que cambia es qué redacción usa cada uno de los dos atoms
en competencia, y si hay metadata de autoridad en el índice.

- **C0 — control**: las dos usan la redacción parafraseada (overlap léxico
  aproximadamente parejo). Se espera que gane `parallel-work-worktrees`
  (el correcto).
- **C1 — trampa léxica, sin metadata**: `parallel-work-branches` con
  redacción alineada, `parallel-work-worktrees` con redacción parafraseada.
  Sin ningún campo de estado en el índice.
- **C2 — misma trampa + `status`**: igual que C1, pero el índice muestra
  `parallel-work-branches [deprecated]` y `parallel-work-worktrees
  [active]`.
- **C3 — misma trampa + relación explícita**: igual que C1, pero el índice
  muestra `parallel-work-worktrees [active, supersedes:
  parallel-work-branches]` (sin mostrar `[deprecated]` en el otro, para
  aislar el efecto de la relación explícita vs. el simple estado).
- **C4 — trampa invertida, sin metadata**: `parallel-work-worktrees` con
  redacción alineada, `parallel-work-branches` con redacción parafraseada. Sin
  metadata. Sirve para confirmar que lo que se mide es atracción léxica y no
  una peculiaridad de contenido — acá el correcto debería ganar más fácil,
  porque la señal léxica y la respuesta correcta coinciden.

3 repeticiones por condición (15 corridas), randomizando posición y orden del
catálogo completo en cada corrida, igual disciplina que rondas anteriores.

## Medición — tres niveles separados, no un solo número

Por cada corrida, registrá los tres niveles por separado (pueden divergir):

1. **`P(worktrees\ rankeado\ #1)`** — en el top-5 pedido antes del `get`.
2. **`P(worktrees\ obtenido\ vía\ get)`** — cuál trajo efectivamente.
3. **`P(comportamiento\ final\ correcto)`** — si la respuesta/código final
   termina recomendando o usando `worktrees`, sin importar qué rankeó primero
   o qué trajo antes.

Es importante no colapsar estos tres en un solo resultado — puede pasar que
rankee mal pero se autocorrija (`get` correcto igual), o que rankee mal, traiga
mal, y aplique mal (ahí sí hay un failure mode de producción real).

## Validación previa

Ping simple a `exo`, confirmá que responde bien. Verificá por código que cada
condición tiene exactamente la redacción y metadata especificada arriba, sin
mezclarse entre condiciones.

## Reporte

`~/yeyo/experiments/exp1bis-q3c-report.md`, con:

- Tabla de las 15 corridas, los tres niveles de medición por condición.
- Comparación C1 vs. C2 vs. C3 — ¿el `status` simple alcanza para neutralizar
  la trampa léxica, o hace falta la relación explícita (`supersedes`)?
- Comparación C1 vs. C4 — confirma o descarta que el efecto sea
  específicamente léxico.
- Conclusión práctica y directa: ¿hace falta meter metadata de
  estado/autoridad en el formato real de los atoms de `yeyo` antes de seguir
  con Q4, o el efecto fue chico/no reprodujo? Si el efecto es real y la
  metadata lo neutraliza, eso es una mejora concreta al modelo de datos de
  `yeyo`, no solo un hallazgo de investigación — vale la pena decirlo así de
  claro en la conclusión.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
