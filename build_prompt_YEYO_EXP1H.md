# Build prompt — Experimento 1H: L en modo raw (sin loop de fases del Coordinator)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, los
reportes de Exp1 a Exp1G (especialmente `exp1g-report.md`, la ronda
inmediatamente anterior), y `~/yeyo/docs/codex_consult_navegacion_libre.md`
completo.

**Resultado clave de Exp1G que motiva esta ronda**: en el Experimento L (juicio
semántico puro — el modelo debía responder únicamente `project_rules_relevant:
yes|no`, sin tools, sin poder ejecutar la tarea), **0/14 corridas respetaron el
formato** — en las 14, el modelo intentó leer el archivo (sin tener ninguna
tool para eso) y, al no poder, le pidió el contenido al usuario en vez de
clasificar. Pasó incluso en una tarea que no era de código.

## Pregunta que esta ronda tiene que contestar, antes que nada

**¿Ese comportamiento es genuino del modelo, o es un artefacto del harness?**

Investigá primero, leyendo código, si `exo` — en el camino que usaron los
Experimentos 1 a 1G — tiene algún paso determinista de "investigar antes de
actuar" que se dispare **sin que el modelo lo decida**. Pista concreta: en la
investigación original de este proyecto se encontró que el `Coordinator`
nativo de `nucleo-base` (`layer2-runtime-rails/runtime/coordinator.go`) tiene
una función `autoInvestigate`, gateada por `shouldInvestigateFirst(input)` —
heurísticas de palabras clave que activan una sub-llamada de investigación
*antes* de la llamada principal al modelo, sin que sea el modelo quien decide
activarla. Confirmá primero si `exo` pasa por ese camino (o algo equivalente
propio) en el flujo normal que usaron todos los experimentos anteriores —
esto es en sí mismo un hallazgo importante para el reporte, se confirme o se
descarte.

## El experimento — misma pregunta de L, sin ningún loop de fases

Repetí exactamente la misma tarea de clasificación del Experimento L (mismas 7
tareas: T1-T5 "yes" esperado, I1-I2 "no" esperado), pero esta vez con una
**llamada de generación de una sola pasada** — system prompt (la misma
instrucción estricta de responder solo `project_rules_relevant: yes|no`) +
mensaje de usuario (la tarea) → una única respuesta del proveedor, sin loop de
turnos, sin fases, sin ningún paso de investigación automática, y sin ninguna
tool disponible (ni siquiera las 2 de plumbing que quedaron en L).

Buscá el punto más bajo posible en el código de `exo`/`nucleo-base` para hacer
esta llamada — el método de envío de una sola generación al proveedor, no el
loop completo del agente. Si no existe un camino así hoy, construí el mínimo
necesario para poder hacer esta llamada aislada, sin arrastrar nada del
pipeline normal.

14 corridas (2 repeticiones × 7 tareas), igual que en L, para comparación
directa.

## Validación previa

Ping simple a `exo` en su modo normal (con todo el pipeline), confirmá que
sigue sano, antes y después de esta ronda — para no dejar nada roto por probar
el camino de una sola pasada.

## Reporte

`~/yeyo/experiments/exp1h-report.md`, con:

- Respuesta a la pregunta de investigación de arriba: ¿`exo` tiene un paso
  determinista de auto-investigación en su camino normal, sí o no, con cita de
  código si existe.
- Tabla comparativa: las 14 corridas de esta ronda (una sola pasada) vs. las 14
  de Exp1G (con el pipeline normal) — misma tarea, mismo formato esperado.
- Conclusión, sin suavizar:
  - Si en modo raw el modelo **sí** responde `yes|no` correctamente (mejora
    clara respecto al 0/14 de L): confirma que el comportamiento de "querer
    leer el archivo" era, al menos en parte, un artefacto del pipeline —
    replantea la lectura de **todas** las rondas anteriores, no solo esta, y
    hay que decidir cómo re-testear el resto con esto en cuenta antes de
    construir K/H/M/N/O.
  - Si en modo raw el comportamiento **persiste** (sigue intentando leer o
    pidiendo el archivo, incluso sin ningún loop de por medio): confirma que
    es un comportamiento genuino del modelo, no del harness — y ahí sí,
    seguimos con la recomendación de Codex (H contrastivo, K con variantes
    K1a/K1b) con más confianza de que estamos midiendo lo que creemos medir.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
