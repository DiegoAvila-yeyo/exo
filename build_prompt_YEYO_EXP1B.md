# Build prompt — Experimento 1B: `centro-catalogo` imperativo (aislado)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé completo,
en este orden, antes de hacer nada:

1. `~/yeyo/docs/vision.md`
2. `~/yeyo/docs/experiments-roadmap.md`
3. `~/yeyo/experiments/exp1-report.md` — **el resultado del Experimento 1**: 0% de
   uso espontáneo de la tool `atom` en las 4 tareas de prueba. El mecanismo
   funciona (probado con invocación explícita), pero la IA nunca decidió usarlo
   sola. Se descartó interferencia de `AGENTS.md` u otro archivo — no había ninguno
   en la ruta de las tareas de prueba.

## Objetivo de esta ronda — una sola variable

Esta ronda prueba **una sola hipótesis, aislada**: que el texto del atom
`centro-catalogo` era demasiado declarativo/blando, y una versión más imperativa sí
dispara el uso espontáneo de la tool.

**No cambies nada más.** No toques los 9 atoms de periferia, no toques el
mecanismo de la tool `atom`, no agregues ningún filtro determinista de sugerencia
(eso es la Variante B, explícitamente fuera de alcance de esta ronda — se aisló a
propósito). El único cambio permitido es el `body` del atom `centro-catalogo`.

## El único cambio: nuevo texto de `centro-catalogo`

Reemplazá el body actual por este, más imperativo y con gancho explícito a la
acción de escribir/editar código (no una sugerencia de proceso general):

> Antes de escribir o editar cualquier archivo de código, es obligatorio llamar a
> `atom list` primero — sin excepción, sin importar si la tarea parece simple.
> Saltarte este paso es un error de proceso aunque el resultado final sea
> correcto. Después de ver el catálogo, si algún atom aplica a la tarea, llamá a
> `atom get <name>` antes de escribir el código.

Mantené el mismo `name` (`centro-catalogo`), mismo `tier: centro`, misma
ubicación de archivo — solo cambia el `body`.

## Validación previa (aislamiento de fallos)

Antes de correr nada: confirmá que `exo` + el gateway responden bien con un mensaje
simple, sin la tool `atom` de por medio. Si algo falla ahí, es infraestructura —
no sigas hasta resolverlo o avisar.

## Corrida — mismas 4 tareas, con repetición

Esta vez, para tener más muestra que la ronda anterior (1 corrida por tarea era
poco), corré **cada una de las 4 tareas 3 veces**, cada corrida en un chat nuevo
(12 corridas en total). Mismas tareas que en el Experimento 1 — reusá exactamente
el mismo texto de tarea y los mismos archivos de prueba (`utils.py` de 150/280
líneas, etc.) que ya están en `~/yeyo/experiments/exp1_project/`, para que la
comparación contra el baseline sea limpia.

Registrá, igual que antes, cada `→ atom usado: <name>` del stream de chat.

## Reporte

Generá `~/yeyo/experiments/exp1b-report.md` con:

- Tabla por tarea: de las 3 repeticiones, ¿en cuántas se llamó `atom list`
  espontáneamente? ¿En cuántas se usó el atom de decisión correcto?
- Comparación explícita contra el baseline del Experimento 1 (0% en todo).
- Conclusión: ¿la reescritura imperativa movió la aguja o no?
  - Si mejoró de forma clara (aunque no sea perfecto): decilo, y proponé si vale la
    pena seguir afinando el texto o pasar al Experimento 2 (jerarquía) con este
    texto como base.
  - Si el resultado sigue en 0% o casi: decilo sin suavizarlo — la conclusión en
    ese caso es que el problema no es de redacción, y la Variante B (filtro
    determinista de sugerencia) pasa a ser necesaria, no opcional.

Commit en `~/yeyo` con el mismo formato que las rondas anteriores
(`feat(catalog): ...` / `docs(exp1b): ...`). No commitees nada en `~/exo` — ya
tiene cambios sin commitear de otras sesiones, no los toques.

Terminá el reporte final explicando qué se cambió, qué se corrió, y el resultado —
sin asumir que quien lo lee tiene el contexto de esta conversación.
