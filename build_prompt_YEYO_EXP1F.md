# Build prompt — Experimento 1F: diagnóstico (F+G+J de Codex)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, los
reportes de Exp1/1B/1D/1E, y **`~/yeyo/docs/codex_consult_navegacion_libre.md`
completo — es la fuente de todo el diseño de esta ronda, junto con la respuesta
de Codex que motivó este prompt (pegada más abajo en este mismo archivo, sección
"Respuesta completa de Codex", para que tengas el razonamiento exacto).**

## Objetivo — descomponer el problema en tres probabilidades separadas

Hasta ahora medimos un solo número mezclado ("¿usó la tool?"). Codex señaló
correctamente que eso conflacionaba tres decisiones distintas:

- `P(list)` — ¿decide explorar el catálogo, sola?
- `P(get | índice visible)` — dado que ya ve el índice sin buscar, ¿decide traer
  el atom completo?
- `P(apply | atom completo visible)` — dado que ya tiene el atom completo
  enfrente, sin tool call de por medio, ¿lo aplica en el resultado?

Esta ronda mide las tres por separado, sobre el mismo set de tareas, y agrega
una variable que nunca controlamos: **si el atom coincide con lo que el modelo
ya haría por defecto (bajo valor informacional de buscarlo) vs. si es una
convención arbitraria del proyecto sin ninguna pista léxica (alto valor
informacional).**

## Atoms nuevos — familia "no obvia" (para el experimento J)

Agregá estos 3 atoms de periferia nuevos, con `role: no-obvious` (metadata
nuestra, no se la mostramos a la IA):

**`go-internal-test-location`** — descripción: "Ubicación de tests para
funciones exportadas en paquetes internos de Go." — body: "Cuando agregues una
función exportada a un paquete interno de Go (`internal/`), el test va en
`testcases/compat/`, nunca junto al archivo fuente."

**`error-message-prefix`** — descripción: "Formato de mensajes de error para el
usuario final." — body: "Todo mensaje de error mostrado al usuario final debe
empezar con el prefijo literal `[yeyo]: `."

**`config-load-typed`** — descripción: "Cómo leer configuración desde
`config/`." — body: "Cualquier función que lea archivos de `config/` debe usar
`LoadTyped[T]()`, nunca parsear el YAML/JSON directo."

Los 2 atoms ya existentes `protocolo-hulk` y `protocolo-widow` quedan como la
familia "redundante" (`role: redundant` en la metadata) — su contenido coincide
con lo que el modelo probablemente haría de todas formas por buenas prácticas
generales, a diferencia de los 3 nuevos, que son convenciones arbitrarias sin
pista léxica en el texto de la tarea.

## Tareas — 5 escenarios de trigger, cada uno en 3 condiciones

| Escenario | Tarea | Familia | Atom esperado |
|---|---|---|---|
| T1 | archivo `utils.py` de 280 líneas, agregar 3 funciones más | redundante | `protocolo-hulk` |
| T2 | import sin usar + función duplicada en `utils.py` | redundante | `protocolo-widow` |
| T3 | "Agregá una función exportada `Normalize` al paquete interno `pkgs/text/`" | no obvia | `go-internal-test-location` |
| T4 | "Agregá un mensaje de error cuando el archivo de config no exista" | no obvia | `error-message-prefix` |
| T5 | "Escribí una función que lea la config de `config/app.yaml`" | no obvia | `config-load-typed` |

Cada una de las 5, en 3 condiciones, 2 repeticiones (30 corridas):

- **Condición B (baseline)** — navegación libre, nada forzado (igual que
  rondas anteriores). Para T1/T2 podés reusar datos ya existentes de rondas
  previas si el texto de tarea es idéntico, en vez de re-correr — para T3/T4/T5
  hace falta correr desde cero porque son atoms nuevos.
- **Condición I (índice visible)** — mismo mecanismo del Exp1D: el índice
  completo de periferia (nombre+descripción de los 5, sin bodies) siempre
  presente, sin necesidad de `atom list`. Medí si llama `atom get`.
- **Condición F (atom completo visible)** — el body completo del atom
  correspondiente a esa tarea puntual (no los otros 4) se inyecta directo en el
  prompt de esa tarea, sin ningún tool call necesario. Acá no hay nada que
  "usar" — medí si el resultado final (código escrito, mensaje de error, etc.)
  **cumple** la regla o no.

## Medición — criterio de cumplimiento verificable por tarea (Condición F)

Como en la Condición F no hay tool call que loggear, definí un chequeo
automatizable por tarea:

- T1/T2: igual que antes (¿modularizó / limpió la duplicación?).
- T3: ¿el test quedó en `testcases/compat/` o junto al archivo fuente?
- T4: ¿el mensaje de error generado empieza literalmente con `[yeyo]: `?
- T5: ¿el código usa `LoadTyped[T]()` o parsea el archivo directo?

## Validación previa

Ping simple a `exo` sin nada de `yeyo`, confirmá que responde bien. Verificá por
código (no preguntándole al modelo) que cada condición realmente inyecta lo que
debe antes de correr las 30 tareas.

## Reporte

`~/yeyo/experiments/exp1f-report.md`, con:

- Tabla de las 30 corridas, separada por familia (redundante vs. no obvia) y
  condición (B/I/F).
- Los tres números que pedía Codex: `P(list)`, `P(get|índice)`,
  `P(apply|atom completo)` — comparando además redundante vs. no obvia en cada
  una.
- Conclusión honesta: ¿sube la navegación espontánea en la familia "no obvia"
  respecto a la "redundante"? ¿Dónde se concentra la caída — en `list`, en
  `get`, o incluso `apply` falla cuando el atom ya está servido?

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.

## Respuesta completa de Codex (contexto para diseñar bien esta ronda)

> Sí: la hipótesis que tienen va en la dirección correcta, pero yo la
> reformularía. No creo que lo que están midiendo sea literalmente "confianza
> introspectiva" del modelo. La explicación que mejor encaja es una
> prioridad/política aprendida de acción: ante ciertas clases de tareas, el
> modelo tiene una trayectoria de respuesta extremadamente dominante —"sé
> resolver esto directamente"— y la acción adicional atom list → atom get
> pierde la competencia de generación antes de que exista una evaluación
> explícita de "¿me falta conocimiento?".
>
> **1. Lectura de los datos.** Tres formulaciones semánticamente distintas
> (sugerencia, mandato, framing epistémico) produciendo prácticamente el mismo
> cero sobre el mismo subconjunto de tareas es buena evidencia de que seguir
> iterando wording tiene rendimiento esperado muy bajo. Coherente con
> Toolformer (no basta describir la herramienta; hace falta entrenar
> explícitamente cuándo insertar la llamada) y con ReAct (trayectorias
> demostradas de razonamiento+acción sí cambian el comportamiento sin tocar
> pesos). Corrección importante: Exp1D no demuestra que "una vez visible el
> candidato, el modelo elige bien" en general — solo lo demuestra para la
> tarea de git/worktrees. Los otros triggers estaban en el índice y no se
> usaron; quedan sin resolver dos explicaciones: iniciativa insuficiente para
> `atom get`, o el modelo ve el atom pero decide que su respuesta general ya
> alcanza. Reformulación de la hipótesis: de "training-default confidence" a
> "strong direct-policy prior" — más débil y más defendible con los datos.
>
> **2. Mecanismos mejores que reescribir el prompt.** Few-shot de
> *comportamiento de navegación* (no del contenido de los atoms): 3-5
> microtrayectorias en el system prompt donde, ante una tarea aparentemente
> trivial, el asistente igual consulta el catálogo, encuentra una regla
> específica, y actúa distinto. Crucial: incluir pares contrastivos (tareas
> donde SÍ hace falta consultar y tareas donde NO hace falta) — no solo
> ejemplos positivos, para enseñar la frontera de decisión y no una ceremonia
> obligatoria de siempre llamar `list`. Si se controlan los pesos del modelo,
> SFT/preference tuning sobre la política de navegación es la opción más
> potente, entrenando directamente `(task, catálogo) → {list/get/no_lookup}`
> en vez de depender de "hacer que dude".
>
> **3. Solución intermedia — el "checkpoint".** Forzar que exista una decisión
> sin forzar cuál es: antes de tocar código, el modelo debe producir
> explícitamente `atoms_decision(action="inspect"|"skip", reason=...)`. Si
> responde `skip`, no se consulta nada; si responde `inspect`, navega el
> catálogo normalmente. Esto es distinto de un matcher determinista: el
> matcher decide qué atom aplica (código decidiendo por la IA — viola el
> principio de `yeyo`); el checkpoint solo obliga a que la pregunta "¿necesito
> conocimiento local?" entre al ciclo de decisión — quien la contesta sigue
> siendo el modelo.
>
> **4. Experimentos propuestos**: F (forced discovery — `atom list`
> automático, el modelo solo decide `get`), G (forced relevant fetch — el atom
> completo servido sin pedir nada, para aislar `P(apply|atom)`), H (few-shot de
> trayectorias, 0 vs 2 vs 4 ejemplos, con subcondiciones positivo-solo vs.
> positivo+negativo), I (inversión del default entrenado — atoms que
> contradicen explícitamente la convención general, ej. "en migrations/, NO
> extraigas el código duplicado, dejalo tal cual"), J (atoms "redundantes" —
> coinciden con lo que el modelo haría solo — vs. "no obvios" — convención
> arbitraria sin pista léxica), K (comparar navegación libre vs. checkpoint
> explícito vs. matcher determinista, midiendo precision, recall, lookups
> innecesarios, selección correcta, cumplimiento de la regla, éxito de tarea,
> tokens y tool calls — con la hipótesis de que el checkpoint sea el punto de
> Pareto).
>
> **5. Variable faltante — costo de la acción.** Un tool call no es gratis
> desde la perspectiva de generación: compite contra una continuación mucho
> más probable ("Claro, voy a modificar..."). Vale la pena probar un
> `atom_search <descripción>` de un solo paso (en vez de `list` + `get`
> separados) para separar "no quiere consultar conocimiento externo" de "la
> trayectoria de dos pasos pierde la carrera contra actuar directo".
>
> **6. No conviene apuntar a "inducir incertidumbre" como objetivo en sí.**
> Verbalizar confianza no es lo mismo que la incertidumbre subyacente, y
> forzar que el modelo "dude más" puede tener efectos secundarios feos (más
> tool calls innecesarios, peor rendimiento en cosas obvias). La propiedad
> deseada es una política operacional aprendible directamente: "¿hay
> probabilidad suficiente de que este proyecto tenga una regla local aplicable
> como para justificar consultar el catálogo?" — no necesita fabricarse como
> una sensación de duda.
>
> **7. Pronóstico por mecanismo** (probabilidad de mejorar / si preserva
> selección por la IA): más wording → muy baja / sí. Framing de incertidumbre
> → baja / sí. Mejor descripción de la tool → baja-media / sí. Few-shot de
> lookup → media-alta / sí. Checkpoint inspect/skip → **alta / sí, casi
> completamente**. SFT de tool-navigation → muy alta / sí. Matcher
> determinista → muy alta para recall / no completamente. Inyectar siempre
> todos los atoms → alta / técnicamente sí, pero mata la periferia.
>
> **8. ¿Es viable la navegación 100% libre?** Depende de la definición. Si
> significa "instrucción + tool disponible, y esperar que un modelo genérico
> decida espontáneamente con alto recall" — con la evidencia de las 5 rondas,
> probablemente no es una meta razonable de producción (no matemáticamente
> imposible, pero suficiente evidencia para no seguir apostando la
> arquitectura a que emerja solo por prompting). Si significa "ningún matcher
> decide qué atom aplica; el modelo conserva la decisión semántica final" —
> ahí todavía vale la pena perseguirlo, probando primero few-shot contrastivo
> y checkpoint explícito (y SFT si se controlan los pesos). Si incluso el
> checkpoint da muchos `skip` falsos sobre reglas relevantes, ahí sí cerraría
> definitivamente la línea de navegación libre sin post-training — una
> falsación mucho más fuerte que otra ronda de wording.
