# Build prompt — Fase A, cierre: telemetría del gate `atoms_decision`

## Contexto

Sesión de Claude Code nueva. Leé, en orden: `~/yeyo/docs/vision.md`,
`~/yeyo/docs/experiments-roadmap.md` (sección "Fase operacional — de
capability validation a producción real"), y
`~/yeyo/docs/codex_consult_instrumentacion.md` completo (el diseño exacto de
qué loguear).

**Estado real, no asumas nada de memoria**: el gate `atoms_decision(inspect|
skip)` ya está migrado a `~/exo` (`agenthost/host.go`), detrás del flag
`EXO_YEYO_GATE` (apagado por default). Se probó en vivo contra el gateway
real y funciona — confirmado en una sesión de chat real, no solo en el
arnés de experimentos. Encontrá vos mismo, leyendo el código actual, el
"seam para telemetría, vacío por ahora" que se dejó preparado (`onDecision`/
`onGet`, mencionado en el resumen de esa migración) — no lo redescubras
desde cero, ya está pensado el punto de enganche.

## Objetivo — implementar el consumidor de esos hooks, no rediseñar el gate

Según la respuesta de Codex ya volcada al roadmap (puntos 1-2 de "Fase
operacional"), lo que hay que loguear es:

- **Append-only, eventos, no filas mutables.**
- `catalog_hash`/`content_hash` por atom disponible en ese momento (sin
  esto, la deriva del catálogo con el tiempo es irreconstruible después).
- Snapshot de qué atoms estaban disponibles en el índice de ese turno
  (crece con el tiempo, a diferencia de los experimentos con N fijo).
- Decisión del gate (`inspect`/`skip`) por turno.
- Orden de `atom get` subsiguientes, con nombre.
- Tamaño textual del índice en ese momento (no solo el conteo N).
- Resultado técnico del turno (`completed`/`error`/`cancelled`).
- **Sin clasificación automática de "corrección del usuario"** — guardar
  la referencia al mensaje siguiente (ya existe en `chatstore`, según el
  diseño) y clasificar offline más adelante, no en este build.
- **Sin taxonomía de fallos todavía** — es prematuro sin datos reales, no la
  inventes en este paso.

## Almacenamiento

Candidato ya identificado en la consulta: sumar una tabla a la
infraestructura de `chatstore`/`sessionstore` (SQLite, `appconfig.go`) en
vez de levantar algo nuevo. Confirmá vos, leyendo esos paquetes, si tiene
sentido — si encontrás una razón real para no usarlos, documentala en el
reporte en vez de forzarlo.

## Restricciones

- Todo detrás del mismo flag `EXO_YEYO_GATE` — si está apagado, cero cambio
  de comportamiento, cero escritura de telemetría.
- No toques la lógica del gate en sí (`atoms_decision_tool.go`,
  `atom_tool.go`, el reset por turno) — este build es exclusivamente el
  consumidor de los hooks ya preparados.
- Si el seam para telemetría no está tan preparado como sugiere el resumen
  de la migración anterior (puede que falte algo), documentalo como
  hallazgo, no lo fuerces silenciosamente.
- Mismo patrón de aislamiento de fallos de siempre: verificá que el gate
  sigue funcionando igual que antes de tocar nada (ping simple, smoke test)
  antes de dar por buena la instrumentación.

## Reporte

Un resumen — qué tabla(s) se agregaron, dónde quedó el código, cómo se
verificó que loguea de verdad (una corrida real con el flag prendido,
mostrando los eventos guardados), y cualquier decisión de diseño que haya
hecho falta tomar sobre la marcha (ej. formato exacto de `catalog_hash`) que
no estuviera ya especificada en la consulta de Codex — dejalas explícitas
para revisar después, no las decidas en silencio si son ambiguas.

Commits en `~/yeyo` si corresponde (no debería hacer falta, esto es todo
código de `~/exo`). `~/exo` sigue sin commitear, mismo patrón de todas las
rondas anteriores — a menos que ahora sí quieras empezar a commitear
`~/exo`, en cuyo caso decilo explícito, no lo asumas.
