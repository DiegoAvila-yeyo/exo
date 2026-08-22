Codex's response to `planning_design_canvas_next_round5_prompt.md`. Verbatim recommendation, kept as
the record of this round — no build followed from this round by the human's own choice (see
`CANVAS_STATUS.md`).

## Veredicto — ¿es el diagrama funcional?

Sí, funcional para seguir construyendo encima, pero no "cerrado". El núcleo que importaba ya existe y
está verificado en vivo: `canvasstore` separado y versionado, draft→materialize, anclaje por
activación, dual edit, y el fallback del `Payload` cuando aún no hay `CanvasAtom`
(`CANVAS_STATUS.md:12`, `canvasstore/model.go:1`, `agenthost/canvas_centro.go:23`,
`agenthost/canvas_centro_test.go:94`).

Los bugs de leak #3/#4 no deberían bloquear más trabajo de Canvas, pero tampoco son cosméticos: son
sistémicos y afectan la confiabilidad del chat como superficie de control, porque hoy se redirige
*todo* `stdout` del turno al stream visible (`agenthost/stdout.go:12`), y ya hay tools que imprimen a
`stdout` a propósito (`agenthost/atom_tool.go:18`, `agenthost/atoms_decision_tool.go:78`).
Recomendación: seguir con Canvas y arreglar esto en paralelo, pero no abrir más UX centrada en chat
sin resolverlo pronto.

## Atom Group — no wrong turn, pero necesita reshaping

`dynamicCentro` hoy inyecta "el contenido actual del propio objeto" desde `CanvasAtom` o `Payload`
(`agenthost/canvas_centro.go:41`). `CanvasObject`/`CanvasAtom` están modelados para eso: cuerpo JSON
propio, una cadena por objeto, `AnchorAtomIDs` apuntando a esa historia interna
(`canvasstore/model.go:46`). Un `atom_group` standalone haría otra cosa: resolver N punteros a otro
store, con cuerpos markdown/prosa, autoridad `status`/`supersedes` propia del catálogo, posible scope
por proyecto (`yeyo/atom.go:41`, `yeyo/catalog.go:168`) — eso ya no es "el mismo mecanismo", es una
segunda semántica metida en el mismo canal.

**Recomendación concreta:**
- No como nuevo object type v1.
- Sí como propiedad adjunta a cualquier objeto materializado: "este objeto además arrastra estos
  atoms del catálogo."
- Mantener separado el contenido propio del objeto de la guía reutilizable adjunta.

Encaja mejor con la intención real: el conocimiento reusable contextualiza un diagrama, no compite
con él como otro objeto activo más.

## Pointer vs. Snapshot — posición fuerte: snapshot

El catálogo `yeyo` tiene nombre, `status`, `supersedes`, `updated_at`, pero no expone una
identidad/versionado inmutable para "esta versión exacta del body" en el runtime de Canvas —
`GetFor` da el atom actual por nombre (`yeyo/catalog.go:168`). Anclar por puntero vivo significaría
que un cambio futuro en `yeyo` reescribe retroactivamente el contexto de objetos viejos sin pasar por
`CanvasAtom`, rompiendo la promesa de versionado explícito de Canvas.

- Guardar en Canvas la referencia de procedencia (nombres de atoms, metadata).
- Resolverlos a un snapshot materializado dentro de la historia del objeto.
- Un cambio futuro en el catálogo no cambia silenciosamente el objeto ya anclado.
- "Refresh desde catálogo" es una acción explícita que genera una nueva versión.

## Multi-object y scope

El fix actual de scope (`canvas_object_id` obligatorio, tools rechazan fuera de scope —
`agenthost/canvas_context.go:17`, `termserver/chat.go:47`, `app.js:1923`) sí cubre bien el riesgo de
"editar el objeto equivocado". Un bundle de atoms adjunto a un diagrama no rompe ese write-scoping.

El riesgo nuevo no es de mutación incorrecta, es de **contexto conflictivo y costo**:
`dynamicCentro` concatena todos los activos tal cual, en orden, sin roles más ricos que un header
(`agenthost/canvas_centro.go:32`). Con un diagrama más instrucciones de prosa del catálogo, el borde
pasa a ser "qué manda" y cuánto prompt estás pagando — no a qué objeto se editó. Esto refuerza (no
reemplaza) la recomendación de propiedad-adjunta sobre objeto-standalone-separado.

## Standalone vs. property — property gana

Un standalone `atom_group` solo vale si se quiere un "knowledge pack" autónomo, reusable y activable
sin relación con ningún objeto visual — eso puede existir después, pero abriría otra categoría de
activación global justo cuando el sistema acaba de aprender a no dejar que el modelo adivine scopes
(bug #8). Para v1: objeto principal = diagrama, propiedad adjunta = atoms de apoyo, una sola unidad
de activación, una sola historia de versiones visible al humano.

## Alternativa más simple (recomendación final)

No un nuevo tipo — extender el contrato de objeto activo a dos bloques:
- contenido propio actual del objeto
- referencias de conocimiento adjuntas, resueltas y snapshotteadas

Si más adelante aparece la necesidad real de packs autónomos, ahí sí crear una entidad separada — no
meterla primero en Canvas como si fuera "otro objeto igual".

## Ideas originales (no pedidas, agregadas por iniciativa propia)

1. **"Anchor budget" visible por objeto activo** — el diseño ya reconoce que el costo importa; hoy no
   hay ninguna señal operativa de cuánto pesa cada activo.
2. **Flujo explícito de "refresh attached knowledge"** cuando un atom referenciado queda
   `deprecated`/`superseded` en `yeyo` — el catálogo ya modela autoridad (`yeyo/render.go:32`);
   Canvas debería aprovechar eso de forma visible, no silenciosa.

No se modificó código en esta ronda.
