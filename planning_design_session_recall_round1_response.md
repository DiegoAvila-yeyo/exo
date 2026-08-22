Codex's response to `planning_design_session_recall_round1_prompt.md`. Verbatim, kept as the record
of this round.

Nota de contexto de Codex: la ruta `/Users/eltitoyeyo/YEYO/YEYO_ESTADO.md` no existe en este
workspace, así que el critique se apoya en el código real y en los docs cerrados que sí están en
disco.

## Veredicto

La dirección "separate store + atom-shaped pull tool" sí aguanta, pero el gap #1 es más grande de lo
que parece: acumular usage por sesión no mide presión de contexto. En el runtime actual el provider
vuelve a enviar el historial en cada turno, así que el `InputTokens` acumulado mide gasto acumulado,
no "cuánto cabe aún en la ventana" (`coordinator.go:191`, `agenthost/host.go:344`).

## 1. Token accounting: persistencia y ubicación

Separar captura de persistencia:
- **Captura**: en el path real del turno — `Host.Run` devolviendo metadata del turno, o un wrapper
  inmediato sobre `Coordinator.Run` — ahí ya hay acceso confiable al delta del provider
  (`agenthost/host.go:344`, `coordinator.go:229`).
- **Persistencia**: en `chatstore.ChatSession`, no en `Host` — la sesión ya es el límite de
  aislamiento y ya sobrevive restart como JSON por sesión (`chatstore/store.go:29`, `:114`).
- No persistir solo "totales acumulados": persistir `last_turn_usage`, `estimated_context_tokens`,
  `context_window_tokens`, `model_id`, y opcionalmente `usage_totals` como telemetría/costo. El
  estimado correcto para el meter v1 es `last_turn_input_tokens + last_turn_output_tokens`, **no**
  la suma acumulada de toda la sesión.

## 2. Threshold

**85% fijo en v1**, aviso one-shot por sesión cerrable. 90% llega tarde para turnos con
tools/salida larga; 80% castiga de más a modelos grandes sin necesidad. Configurable después, no
desde v1 — primero cerrar bien la semántica y la UX. Regla: avisar cuando
`estimated_context_tokens / context_window_tokens >= 0.85`.

## 3. Quién escribe el resumen y cómo

**Llamada separada de summarization**, no "el mismo turno se resume a sí mismo" — más consistente
con el patrón tipo librarian de memoria (`memoryservice/service.go:373`), permite prompt
estructurado sobre transcript + metadata, y evita el estado peligroso "sesión cerrada pero sin
summary" si el mismo turno falla a media respuesta.

El cierre debe ser secuencia explícita: **generate summary → persist recall entry → mark session
closed**. Si falla el resumen, la sesión sigue abierta.

## 4. Shape del nuevo store y lifecycle

**Un JSON por proyecto con CAS**, copiando el patrón de `canvasstore`, no un archivo por summary —
"project = absolute path" + CAS por versión ya es el patrón validado para aggregates project-scoped
(`canvasstore/model.go:1`, `store.go:64`).

Entradas keyed por `session_id`, con `title`, `description`, `summary_body`, `closed_at`,
`model_id`, `context_pct_at_close`, `status`, `supersedes`. Lifecycle: append-mostly, sin delete;
`supersedes` solo para re-summarización explícita. No duplicar transcript — solo referenciar el
`session_id` de `chatstore`.

## 5. Contrato del tool, scoping y acceso a transcript crudo

Tool espejo de `atom`: **`list` y `get`, nada más** (`agenthost/atom_tool.go:13`).
- `list` devuelve solo summaries cerrados del proyecto activo — el proyecto no viene del modelo,
  viene del contexto ya establecido por la app/host, mismo patrón que otros scopes
  browser-established (`app.js:548`, `:575`).
- `get` devuelve **solo summary + metadata, no transcript crudo**. Darle al agente un escape hatch
  al transcript completo rompe la decisión central de evitar full injection y convierte el tool en
  un bypass del diseño. El transcript queda como backup humano/manual vía `chatstore`, no como
  recall path del agente.

## 6. Session lifecycle, sidebar y reopen

`closed` es **terminal para continuación, pero no invisible**: el transcript sigue abrible en modo
lectura, con badge "Closed", y aparece en el sidebar debajo de las activas dentro del grupo del
proyecto — la sidebar ya agrupa por proyecto y abrir una sesión vieja no cambia el proyecto activo,
lo cual encaja con "historial navegable, no reanudable" (`app.js:703`, `:716`).

**No permitir "reopen and continue" sobre el mismo `session_id`**: invalida el meaning de closed,
vuelve stale el summary, mezcla dos épocas en un solo record. Si el humano quiere seguir, abre una
sesión nueva.

## Problema arquitectónico de fondo

El problema real no es "separate store + atom-shaped pull tool" — eso está bien. El riesgo profundo
es **dual-write consistency** entre `chatstore` y el nuevo recall store: si marcas `closed` en el
chat y falla la persistencia del summary, queda una sesión cerrada pero no recuperable; si guardas
el summary y no cierras el chat, se generan huérfanos. Ese workflow necesita orden transaccional
claro aunque siga siendo file-based.

## Conclusión

Mantener el store separado y el tool `list`/`get`, pero corregir la premisa del token meter: el
warning de cierre debe basarse en el **último prompt real estimado**, no en tokens acumulados por
sesión. Sin esa corrección, el feature nacería con una métrica equivocada.
