# Hallazgo: el anclaje nunca se activó — investigación y opciones de arreglo

Surgió durante el retest de `canvas_qa_retest_checklist.md`'s test #1 (`canvas_edit_object`): al
pedirle al mini-chat que renombrara un nodo, el agente reescribió el payload completo y perdió 2 de
6 nodos sin que nadie lo pidiera. Investigar por qué (no solo parchar el síntoma) llevó a algo más
grande que ese bug puntual.

## La cadena real, confirmada en código, no especulada

1. `canvasstore.Materialize()` (`canvasstore/mutate.go:70`) deja el objeto con `Activation: ""`
   (inactivo) — materializar **no** activa automáticamente. Decisión de diseño explícita.
2. Activar un objeto solo pasa vía `SetActivation(objectID, ActivationActive)`
   (`canvasstore/mutate.go:80`), expuesto únicamente por
   `POST /api/canvases/objects/{id}/activate` (`termserver/canvas.go:173`).
3. **Nada en toda la app llama ese endpoint.** No existe tool de agente (`canvas_activate_object`
   no existe en `agenthost/canvas_tools.go`) y el frontend (`app.js`) solo *lee* `obj.activation`
   para pintar un badge (línea 1648-1649) — nunca hace `fetch` a `/activate` ni `/deactivate`.
4. Consecuencia: `ProjectCanvas.ActiveObjectIDs` queda `null` para siempre, en todo proyecto.
5. `dynamicCentro` (`agenthost/canvas_centro.go`) — el mecanismo entero de anclaje diseñado en
   Round 2/4 con Codex — **solo itera sobre `pc.ActiveObjectIDs`**. Como esa lista nunca tiene
   nada, ha sido un no-op silencioso desde que se construyó, en cada turno, en cada proyecto.

**El anclaje nunca funcionó, ni una sola vez, para ningún objeto materializado hasta hoy.** Lo que
parecía funcionar en pruebas anteriores (la primera edición vía IA que salió bien) coincidía con
seguir en la misma sesión de chat — el historial normal de mensajes cubría lo que el anclaje
debería haber estado haciendo. El mini-chat del panel flotante es su propia sesión nueva, sin ese
historial de respaldo, así que fue la primera vez que la ausencia real del anclaje se hizo visible:
el agente no tenía el payload actual en su contexto y fabricó una reconstrucción plausible pero
incompleta (4 nodos en vez de 6, dos IDs renombrados sin que se pidiera).

## Por qué esto no es solo "arreglar canvas_edit_object"

Parchar `canvas_edit_object` para que sea más cuidadoso (o para que rechace payloads que dropean
contenido inesperadamente) reduciría el síntoma, pero el problema de fondo — que la IA nunca tiene
el objeto realmente anclado — seguiría intacto y afectaría a cualquier otra interacción que dependa
de "la IA tiene presente el diagrama": mini-chat, futuras tools, lo que sea. Vale más cerrar la
brecha real.

## Opciones de arreglo

**A — Auto-activar al materializar.** `canvas_materialize_draft` llama `SetActivation(Active)` en
el mismo paso. Simple, cero piezas nuevas. **Pero contradice una decisión ya cerrada en Round 2/4**:
"Keep the active set small and human-curated by design — no automatic accumulation" — el costo de
mantener átomos anclados es el punto más caro de todo el diseño (`build_prompt_CANVAS_HOME_V1.md`),
y activar todo automáticamente reabre exactamente el riesgo de presupuesto de contexto sin techo
que esa decisión evitaba a propósito.

**B — Completar la activación explícita ya diseñada, que hoy no tiene forma de dispararse.**
Agregar lo que falta, nada más:
- Un tool de agente `canvas_activate_object` / `canvas_deactivate_object` (mismo patrón que
  `canvas_edit_object`, reusa el mismo endpoint/lógica ya construida) — para que la IA pueda
  anclar algo cuando el humano lo pida explícitamente ("ancla esto", "ya no lo necesito anclado").
- Un control real en el frontend conectado a `/activate`/`/deactivate` (hoy el badge es de solo
  lectura) — para que el humano lo pueda hacer directo desde el panel, sin pasar por el chat.

Esto no es una decisión de diseño nueva — es terminar de construir lo que Round 2/4 ya decidió y el
build spec ya documentó, y que quedó a medias (el endpoint y el badge existen; el disparador no).

**C — Auto-activar solo el objeto recién materializado, con deactivate manual disponible.**
Intermedio: el objeto que acabás de crear queda activo por default (matching la intuición original
de "en cuanto lo plasmo, la IA lo tiene presente"), pero no se acumula sin límite porque el humano
puede desactivarlo cuando ya no lo necesite (vía B). Requiere las mismas piezas de B más el cambio
de A solo en el momento de materializar.

## Recomendación

**B, con C como refinamiento a considerar después de que B exista** — activar/desactivar tiene que
existir como acción real antes de decidir si además debería pasar por default al materializar. Ir
directo a A o C sin que B exista sería resolver el síntoma de hoy sin dejar ninguna forma de que el
humano controle el costo más adelante, que es justo la garantía que Round 2/4 dejó documentada.

## Qué llevar al build prompt

1. `canvas_activate_object` / `canvas_deactivate_object` — tools de agente, mismo patrón que
   `canvas_edit_object` (`agenthost/canvas_tools.go`), reusando `SetActivation` directamente.
2. Botón real en el panel flotante (`app.js`) conectado a `/activate`/`/deactivate` — el badge deja
   de ser de solo lectura.
3. Una vez B esté construido: retest #1 de `canvas_qa_retest_checklist.md` de nuevo, esta vez
   activando el objeto explícitamente antes de pedirle la edición al mini-chat — recién ahí ese
   test mide lo que se diseñó medir.
4. Decidir C como paso aparte, después, no en el mismo build — mismo criterio que ya se usó para no
   mezclar decisiones independientes en un solo prompt.
