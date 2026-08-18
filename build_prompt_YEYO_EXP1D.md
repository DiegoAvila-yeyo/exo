# Build prompt — Experimento 1D: catálogo forzado (aísla iniciativa de criterio)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé completo,
en este orden:

1. `~/yeyo/docs/vision.md`
2. `~/yeyo/docs/experiments-roadmap.md`
3. `~/yeyo/experiments/exp1-report.md` y `~/yeyo/experiments/exp1b-report.md` —
   **16 corridas acumuladas en 0% de uso espontáneo de la tool `atom`**, con dos
   redacciones distintas del atom del centro (blanda e imperativa). El mecanismo en
   sí funciona (probado con invocación explícita); la IA nunca decide sola
   explorarlo.

## Por qué esta ronda es distinta — qué pregunta aísla

Los Experimentos 1 y 1B midieron una sola cosa mezclada: si la IA decide explorar
el catálogo, **y** si, al explorarlo, elige bien. Como nunca lo exploró, no
sabemos nada de la segunda parte — podría elegir perfecto si lo tuviera enfrente,
o podría elegir mal incluso viéndolo. Esta ronda separa esas dos preguntas: **le
sacamos a la IA la decisión de si mirar el catálogo (se lo mostramos siempre, sin
que tenga que pedirlo) y medimos solo si, dado que ya lo tiene, elige
correctamente cuáles aplican.**

Esto **no es la Variante B** (matcher determinista que sugiere candidatos
filtrados) — acá se muestran los 9 atoms de periferia completos, sin ningún
filtro ni preselección. La IA sigue siendo la que decide cuáles son relevantes;
lo único que cambia es que ya no tiene que decidir si buscar el índice, porque
el índice ya está puesto.

## Único cambio respecto al Experimento 1B

No toques el texto de `centro-catalogo` (queda igual que en 1B), no toques los 9
atoms de periferia, no toques el mecanismo de `atom get`. El único cambio: el
**índice** de periferia (nombre + descripción de los 9 atoms, no el body
completo) se inyecta siempre, en el mismo lugar donde se inyecta el centro — no
hace falta llamar a `atom list` para verlo, ya está ahí. La tool `atom get
<name>` se mantiene disponible para traer el body completo de cualquiera que la
IA considere relevante.

Si encontrás que exponer el índice siempre requiere modificar el mismo punto de
enganche por turno que se agregó en el Experimento 1C, reusalo si ya existe en el
código; si no existe todavía (por ejemplo, si la Variante B nunca se corrió),
agregalo mínimo, sin construir nada más de lo necesario para esto.

## Validación previa (aislamiento de fallos)

Ping simple a `exo` sin nada de `yeyo` de por medio, confirmá que responde bien
antes de correr las tareas.

## Corrida

Mismas 4 tareas, mismos fixtures reseteados entre corridas, 3 repeticiones cada
una (12 corridas), para comparación directa con los baselines anteriores.

| # | Tarea | Se espera que use (`atom get`) |
|---|---|---|
| 1 | función de validación de email en `utils.py` de 150 líneas | `no-hardcoded-secrets`, `commit-message-format` |
| 2 | archivo de 280 líneas, agregar 3 funciones más | control + `protocolo-hulk` |
| 3 | import sin usar + función duplicada | control + `protocolo-widow` |
| 4 | trabajar en dos features en paralelo | control + `worktrees-not-code-dir` |

Los 2 atoms distractores (`rails-conventions`, `jira-pass-ticket-flow`) también
están en el índice forzado — si se usan en cualquiera de las 4 tareas, es un
falso positivo, igual criterio que en rondas anteriores.

## Piezas del rompecabezas — anotá lo que no sea binario

Mismo criterio que en la ronda anterior: anotá en el reporte cualquier
observación aunque no encaje en sí/no —

- ¿La IA razonó sobre atoms específicos en su texto sin haber llamado `atom get`?
- ¿Aplicó una convención sin haber leído el atom, como si ya la supiera?
- ¿Hubo diferencia de comportamiento entre repeticiones?
- Si eligió mal, ¿el error fue "no eligió nada" o "eligió el equivocado"? — son
  señales distintas (la primera sugiere que ni con el índice forzado hay
  iniciativa; la segunda sugiere que sí hay iniciativa pero falla el criterio).

## Reporte

`~/yeyo/experiments/exp1d-report.md`, con:

- Tabla de las 12 corridas: qué se usó vs. lo esperado.
- Comparación explícita contra el 0% acumulado de 1 y 1B.
- Conclusión, sin suavizar, respondiendo directamente: **dado el catálogo
  siempre visible, sin tener que decidir explorarlo — ¿la IA elige bien?** Si sí:
  el problema es 100% de iniciativa, no de criterio, y el foco siguiente debería
  ir ahí (few-shot, posición, disparador más específico). Si no: el problema es
  más profundo que la iniciativa — el catálogo mismo, tal como está redactado, no
  comunica bien la relevancia ni siquiera cuando está a la vista, y esa es una
  conversación distinta a la que veníamos teniendo.

Commits en `~/yeyo` con el mismo formato de rondas anteriores. `~/exo` sigue sin
commitear — no lo toques en ese sentido.
