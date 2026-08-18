# Build prompt — Experimento 1 de `yeyo`: navegación de catálogo de atoms

## Contexto

Este prompt es para una sesión de Claude Code nueva, sin memoria de la
investigación previa. Todo el contexto de diseño necesario está en dos documentos
ya escritos — **léelos completos antes de empezar**:

- `~/yeyo/docs/vision.md` — el modelo completo (tres capas concéntricas, tipos de
  atom, por qué no usamos MCP, cómo se decide qué va en el centro).
- `~/yeyo/docs/experiments-roadmap.md` — la secuencia completa de experimentos y el
  "principio de aislamiento de fallos".

Este build cubre **solo el Experimento 1**: probar si la IA navega bien un catálogo
de atoms de comportamiento, con jerarquía mínima (centro fijo / periferia por
catálogo), usando roles de prueba (control/decisión/distractor) para medir tanto
aciertos como falsos positivos.

## Objetivo de esta ronda

1. Construir el paquete `yeyo` (Go, nativo, sin MCP) en `~/yeyo`, con el contenido
   exacto de atoms de abajo.
2. Engancharlo a `~/exo` — el centro al `buildSystemPrompt`, la periferia como una
   tool nueva.
3. Construir la medición: qué atoms se usaron por turno, visible en el stream de
   chat de `exo`.
4. Correr las 4 tareas de prueba definidas abajo y generar un reporte comparando lo
   esperado vs. lo que realmente pasó.

## Restricciones — importantes, no las ignores

- **Nada de MCP.** El paquete `yeyo` es un módulo de Go normal, importado por `exo`
  como dependencia — no un servidor, no un proceso separado, no un protocolo.
- **No tocar la cáscara existente.** No modifiques `tool/skill.go`,
  `instructions/loader.go`, ni ningún otro archivo de `~/nucleo-base` — `yeyo` vive
  en su propio repo y se conecta desde `exo`, imitando el *patrón* de `skill.go`
  (catálogo `list` + fetch por nombre), no reutilizando su código.
- **No reutilices código de `~/flamen`.** Es un proyecto TypeScript distinto, ya
  descartado explícitamente en `docs/vision.md` — esto se construye desde cero.
- **Ningún archivo `.go` debe superar 300 líneas** (protocolo del ecosistema — ver
  `~/.claude/CLAUDE.md` si está disponible). Si una pieza lo requeriría, modulariza
  antes de escribir.
- **Antes de correr las 4 tareas de prueba, valida que `exo` + el gateway de
  proveedores están sanos**, sin nada de `yeyo` de por medio (principio de
  aislamiento de fallos — mandá un mensaje de chat simple y confirmá que responde
  antes de seguir). Si algo falla ahí, es infraestructura, no el diseño de `yeyo` —
  no sigas hasta que esté resuelto o avisado.
- Trabaja incremental — cada pieza (paquete `yeyo`, wiring en `exo`, medición) se
  prueba en local antes de pasar a la siguiente. No entregues todo junto sin haber
  verificado cada parte por separado.
- Commits en `~/yeyo` con mensajes en inglés, formato `feat(scope): descripción`.

## 1. Paquete `yeyo` — contenido exacto de los atoms

Crea en `~/yeyo` una estructura de atoms, formato frontmatter + body (similar al
schema real de `~/flamen/schemas/atom.schema.md` si querés referencia de formato,
pero contenido propio, no copiado). Campos mínimos: `name`, `description`, `tier`
(`centro` | `periferia`), y para los de periferia, `role` (`control` | `decision` |
`distractor`) — este campo `role` es solo para nuestra medición/reporte, no se lo
mostramos a la IA como tal.

### Centro (3 atoms, siempre inyectados, sin fetch)

**`centro-catalogo`**
> Existe un catálogo de atoms navegable, accesible con la tool `atom`. Antes de
> actuar sobre una tarea de código, consultá `atom list` para ver si algo del
> catálogo aplica a lo que estás por hacer.

**`centro-verify-before-claim`**
> No afirmes que algo funciona sin haberlo verificado. Si corriste algo, mostrá el
> resultado real; si no lo corriste, decilo explícitamente.

**`centro-ask-on-doubt`**
> Si hay ambigüedad real sobre el alcance de la tarea — qué archivos, qué se
> considera terminado — preguntá antes de actuar.

### Periferia (9 atoms, catálogo con fetch bajo demanda)

*Rol: control*

**`no-hardcoded-secrets`** — descripción para el catálogo: "Nunca hardcodear
credenciales, API keys o secretos en el código." — body: "Cualquier credencial,
token o secreto debe venir de variables de entorno o un gestor de secretos, nunca
como texto plano en el código fuente."

**`commit-message-format`** — descripción: "Formato de mensajes de commit del
proyecto." — body: "Usar `feat/fix/refactor/test/docs/chore(scope): descripción`
en inglés para el mensaje de commit."

*Rol: decision*

**`protocolo-hulk`** — descripción: "Límite de tamaño de archivo para evitar
God Objects." — body: "Si un archivo de código va a superar las 300 líneas con el
cambio propuesto, DETENTE antes de escribir más — proponé una modularización
(dividir por responsabilidad) antes de continuar."

**`protocolo-widow`** — descripción: "Qué hacer al encontrar código muerto o
duplicado." — body: "Si detectás código muerto, imports sin usar, o lógica
duplicada en 2 o más lugares mientras trabajás en una tarea, limpialo como parte
del mismo cambio, sin pedir permiso aparte."

**`worktrees-not-code-dir`** — descripción: "Cómo manejar trabajo en paralelo con
git." — body: "Si la tarea implica trabajar en dos líneas de desarrollo en
paralelo sin pisarse, usar git worktrees — no trabajar directo sobre el mismo
directorio de código con cambios sin commitear de dos features distintas a la
vez."

*Rol: distractor*

**`rails-conventions`** — descripción: "Convenciones de Ruby on Rails para
controllers y modelos." — body: "En proyectos Rails, los controllers deben ser
delgados y delegar lógica de negocio a los modelos o a service objects."
(irrelevante para las 4 tareas de prueba, que son Python/Go — no debería
dispararse nunca en este experimento).

**`jira-pass-ticket-flow`** — descripción: "Flujo de tickets Jira con prefijo
PASS-*." — body: "Todo cambio en `pacta-pay-rails` debe referenciar un ticket
PASS-* válido en el mensaje de commit." (irrelevante si la tarea no toca ese
repo/flujo — no debería dispararse en este experimento).

## 2. Mecanismo — tool `atom`, native en Go

En el paquete `yeyo`, implementar una tool con dos modos, mismo espíritu que
`tool/skill.go` de `nucleo-base` (léelo como referencia de patrón, no lo
importes/modifiques):

- `atom list` → devuelve el catálogo liviano de periferia: `name` + `description`
  de cada uno de los 9 atoms de periferia. (El centro no aparece acá — ya está
  siempre presente.)
- `atom get <name>` → devuelve el `body` completo de ese atom.

Exportar esto de forma que `exo` lo pueda registrar como una tool más en su lista
de tools disponibles para el agente.

## 3. Wiring en `exo`

- En `agenthost/host.go`, dentro de `buildSystemPrompt`, agregar el contenido de
  los 3 atoms del centro al string final — mismo lugar donde hoy se agrega el
  resultado de `instructions.Load()`.
- Registrar la tool `atom` (del paquete `yeyo`) en la lista de tools que arma el
  `Host` — buscá dónde se registran las tools existentes (cerca de donde se arma
  `buildSystemPrompt`) y agregala ahí.
- Agregar `yeyo` como dependencia en el `go.mod` de `exo`.

## 4. Medición

En `termserver/chat.go`, donde ya se escriben las líneas de fase al
`chatBroadcaster` (`→ phase: tasking`, `→ phase: acting`), agregar una línea
similar después de cada llamada a la tool `atom get <name>`: `→ atom usado:
<name>`. Esto deja un registro visible, en el mismo stream, de qué atoms pidió la
IA en cada tarea — sin necesidad de nada más sofisticado para este experimento.

## 5. Tareas de prueba y expectativas

Correr estas 4 tareas, una por vez, cada una en un chat nuevo (para que no se
contaminen entre sí), sobre un proyecto de prueba real en Python (podés armar un
`utils.py` chico con una función simple para las dos primeras tareas).

| # | Tarea (mensaje a mandar por el chat de `exo`) | Se espera que use |
|---|---|---|
| 1 | "Agregá una función de validación de email a este archivo `utils.py` de 150 líneas." | `no-hardcoded-secrets`, `commit-message-format`. Ningún otro. |
| 2 | "Este archivo `utils.py` tiene 280 líneas — agregále 3 funciones más." | los de control + **`protocolo-hulk`**. |
| 3 | "Hay un import sin usar y una función duplicada en este archivo, arreglalo." | los de control + **`protocolo-widow`**. |
| 4 | "Necesito trabajar en dos features en paralelo sin pisarme los cambios." | los de control + **`worktrees-not-code-dir`**. |

En ninguna de las 4 debería aparecer `rails-conventions` ni `jira-pass-ticket-flow`
— si aparecen, es un falso positivo.

## 6. Reporte final

Al terminar las 4 tareas, generar un reporte en Markdown
(`~/yeyo/experiments/exp1-report.md`) con:

- Tabla: por cada tarea, qué atoms se esperaban vs. cuáles se usaron realmente
  (según el log de `→ atom usado:`).
- Métricas: tasa de acierto en atoms de control (¿se usaron en las 4?), tasa de
  acierto en el atom de decisión correspondiente a cada tarea, tasa de falsos
  positivos (¿aparecieron distractores en alguna?).
- Una conclusión corta: ¿el mecanismo de navegación funciona razonablemente bien, o
  hay que ajustar algo antes de pasar al Experimento 2?

Terminá avisando qué se construyó, dónde (archivos y repos tocados), y el resultado
del reporte — no asumas que quien lee esto tiene el contexto de la conversación de
diseño, así que sé explícito.
