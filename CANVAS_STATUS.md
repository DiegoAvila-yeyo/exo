# Estado de Canvas

Roadmap vivo de la feature Canvas — mismo espíritu que `IMPLEMENTATION_PLAN.md`, pero para esta
pieza específica. Mantener actualizado; `IMPLEMENTATION_PLAN.md` no cubre Canvas y no debería
duplicarlo.

## Documentos de referencia (en orden de lectura)

1. `planning_design_canvas_home_prompt.md` — Round 1: qué es Canvas, dónde vive, tensión con
   Planning/Round 3 de navegación.
2. `planning_design_canvas_home_round2_prompt.md` — Round 2: arquitectura de anclaje
   (`canvasstore → object_id → activeObjectSet → dynamicCentro`), separado de `planningContext`.
3. `planning_design_canvas_object_model_round3_prompt.md` — Round 3: schema de `canvasstore`,
   Planning por composición, concurrencia CAS.
4. `planning_design_atoms_canvas_anchor_prompt.md` — Ronda paralela: cómo los átomos anclan un
   objeto de Canvas (centro dinámico, `supersedes`).
5. `planning_design_canvas_intent_render_round4_prompt.md` — Round 4: detección de intención
   (híbrido NL/slash/botón), motor de render (shape-graph estructurado).
6. `build_prompt_CANVAS_HOME_V1.md` — consolidado, instrucciones reales de build. **Es la fuente de
   verdad de qué se decidió construir**; los 5 documentos de arriba son el razonamiento detrás.
7. `canvas_live_qa_findings.md` — 6 hallazgos de la primera prueba en vivo.
8. `canvas_qa_retest_checklist.md` — checklist para reverificar los 4 fixes del commit `21ad82b`.
9. `canvas_activation_gap_findings.md` — hallazgo del retest 2026-08-20: el anclaje nunca se activó
   para ningún objeto (falta el disparador, no el mecanismo), con 3 opciones de arreglo y
   recomendación.

## Construido

- [x] Task 1 — `canvasstore`: modelo de objetos, transiciones de lifecycle, CAS store (`1f8a5ac`)
- [x] Task 2 — `appconfig.CanvasStoreDir()` (`9ee9dd3`)
- [x] Task 3 — tools `canvas_create_draft`, `canvas_materialize_draft`, `canvas_list_drafts`
      (`1e64c46`)
- [x] Task 4 — anclaje `dynamicCentro` + `RefreshCanvasCentro` (`f6b8aa3`)
- [x] Task 5 — HTTP layer `canvas.go` (`435ff7e`)
- [x] Task 6a — evento SSE `canvas_suggest` + slash command `/materialize` (`695926e`)
- [x] Task 6b — frontend: layout 3 columnas, panel flotante, render de diagrama (`67a9c68`)
- [x] Task 8 — fixes de QA en vivo: `canvas_edit_object`, auto-layout, validación de edges,
      empty-state (`21ad82b`) — **verificado en vivo por el build session, retest formal
      (`canvas_qa_retest_checklist.md`) todavía sin correr por el usuario**
- [x] Task 9 — cierre del gap de anclaje (`canvas_activation_gap_findings.md`, opción B): tools de
      agente `canvas_activate_object`/`canvas_deactivate_object` (reusan `SetActivation` tal cual) +
      toggle real en el panel flotante conectado a `/activate`/`/deactivate` — el badge de solo
      lectura y el endpoint HTTP ya existían, esto agrega el disparador que faltaba. `go build`/`go
      test` en verde, tests nuevos para materializado-ok / draft-o-deleted-falla /
      ya-inactivo-es-no-op / retry en conflicto CAS. **Verificado en vivo** — el fix llevó al
      hallazgo #8 (abajo), atacado en Task 10.
- [x] Task 10 — scoping real del mini-chat (bug #8, arreglado directo en esta sesión, sin build
      session aparte): `canvasCell.scopedObjectID` + `checkScope` (`agenthost/canvas_context.go`),
      `Host.BeginTurn` ahora recibe el object_id escopado, threadeado desde
      `POST /api/chat`'s nuevo campo `canvas_object_id` (`termserver/chat.go`) hasta
      `backend.go`'s runner. Los 5 tools (`canvas_edit_object`, `canvas_activate_object`,
      `canvas_deactivate_object`, `canvas_create_draft`, `canvas_materialize_draft`) rechazan
      actuar sobre cualquier objeto que no sea el escopado, cuando hay un scope activo. El mini-chat
      (`app.js`) ahora manda `canvas_object_id` en cada mensaje — mismo principio que
      `planning_id`/`board_id`: el navegador establece el contexto, no el modelo. `go build`/`go
      test` en verde, 6 tests nuevos (`TestCanvas*RejectsWhenScoped*`,
      `TestCanvasEditObjectAllowedWhenScopedToSameObject`). **Verificado en vivo** — el
      scoping sí apuntó al objeto correcto, lo que reveló #9 (abajo).
- [x] Task 11 — `dynamicCentro` nunca anclaba el `Payload` original de un objeto materializado
      pero jamás editado (cero átomos -> `CurrentAtom` devuelve ok=false -> se saltaba en
      silencio, aunque estuviera Active). Causa raíz real de la fabricación en #9: la primera
      edición de cualquier objeto siempre era ciega. Fix en `agenthost/canvas_centro.go`: fallback
      a `obj.Payload` cuando no hay átomo, mismo patrón que ya usa `app.js`
      (`currentAtomBody(...) || obj.payload`), ahora también del lado servidor. Test nuevo
      `TestDynamicCentroFallsBackToPayloadWhenNoAtomExists`. `go build`/`go test` en verde.
      **Verificado en vivo** — diagrama "CI/CD Pipeline" recién materializado y activado, primera
      edición preservó los 10 nodos originales completos, solo cambió el label pedido.

## Verificado en vivo (prueba manual del 2026-08-19)

Flujo completo probado de punta a punta contra `exo serve` real, con LLM real:
discutir diagrama → draft creado → banner `canvas_suggest` → materializar con botón → panel
flotante → edición manual (JSON crudo) → intento de edición vía mini-chat.

Encontró los 6 hallazgos de `canvas_live_qa_findings.md`. El build session reportó 4 arreglados
(`21ad82b`) — **pendiente de reverificar en vivo con el checklist antes de darlos por buenos**.

## Bugs abiertos, sin arreglar

- [ ] **#3** — JSON crudo de la tool call se filtra al chat al crear un draft. Causa raíz
      identificada: `agenthost/stdout.go`'s `redirectStdout`, captura trace del `agent.Agent`
      vendorizado de `nucleo-base`. **Sistémico — afecta a toda la app, no solo Canvas.** Necesita
      decisión de enfoque (filtrar patrones conocidos en el broadcaster vs. hacerlo específico a
      canvas tools) antes de poder pedirlo como fix.
- [ ] **#4** — marcador interno `=== FINAL ===` visible en respuestas de chat normales. Misma causa
      raíz que #3.

## Bugs reportados como arreglados, pendientes de reverificación

Usar `canvas_qa_retest_checklist.md` paso a paso — ninguno de estos se da por cerrado hasta correr
ese checklist:
- [x] #1 — `canvas_edit_object` (edición vía IA de objeto materializado, versiona con `supersedes`).
      Verificado en vivo 2026-08-20 con el diagrama "CI/CD Pipeline": los 10 nodos originales
      sobrevivieron, solo cambió el label pedido, `supersedes` correcto. Necesitó #7, #8 y #9
      arreglados primero — cada retest anterior de esto fue justo lo que reveló el siguiente bug.
- [x] #2 — auto-layout de diagramas (nodos ya no se apilan en `(0,0)`). Verificado en vivo, 3
      diagramas distintos, todos con posiciones separadas y edges visibles.
- [~] #5 — referencias de edge rotas se rechazan al guardar (manual y vía tool). **No reverificado
      en vivo hoy** (nos desviamos hacia #7-#9) — sí tiene cobertura de test automatizado en verde
      en ambos write paths (`TestCanvasManualEditRejectsDanglingDiagramEdge`,
      `TestCanvasEditObjectRejectsDanglingDiagramEdge`), pero eso no reemplaza un click-through
      real. Pendiente.
- [~] #6 — placeholder de canvas vacío se oculta correctamente. **No reverificado formalmente** —
      sí se observó funcionando de pasada varias veces hoy (desaparece cada vez que se materializa
      algo), pero el caso límite del checklist (desactivar el único objeto activo, ¿reaparece el
      placeholder?) nunca se probó. Pendiente.
- [x] #7 — el anclaje nunca se activó, para ningún objeto (Task 9). Verificado en vivo: activar un
      diagrama y pedir una edición sí lo mantiene presente — pero reveló #8 (abajo) al hacerlo con
      dos objetos activos a la vez.
- [x] #8 — con más de un objeto activo simultáneamente, el mini-chat editó el objeto equivocado y
      creó un objeto duplicado por confusión. Causa raíz: el scoping del mini-chat era solo un
      prefijo de texto advisory, nunca forzado. Arreglado en Task 10, verificado en vivo — el
      segundo intento sí apuntó al `object_id` correcto.
- [ ] #9 — **nuevo, encontrado verificando #8**: con el `object_id` correcto ya targeteado,
      `canvas_edit_object` reemplazó el diagrama por uno **completamente inventado** (IDs de nodo
      `"1"`, flujo distinto de pies a cabeza) en vez de editar el real. Causa raíz real, no era el
      scoping: el objeto nunca había sido editado antes (cero átomos), y `dynamicCentro` solo
      anclaba el átomo actual — si no existía ninguno, se saltaba el objeto en silencio aunque
      estuviera activo. La primera edición de cualquier objeto siempre era ciega. Arreglado en
      Task 11 (arriba) — pendiente de verificación en vivo: editar un objeto recién materializado
      (sin ediciones previas) y confirmar que preserva su contenido real, no lo inventa.

## Explícitamente fuera de alcance (diferido a propósito, no olvidado)

De `build_prompt_CANVAS_HOME_V1.md`:
- **Planning como objeto de Canvas** — el wrapper de composición (`type: "planning"`, payload
  `{"planning_id": "..."}`) está diseñado pero **nunca se construyó**. Solo existe como comentario
  en `canvasstore/model.go`. Esto era la decisión #2 original de toda la idea ("la IA controla
  todo, no solo diagramas") — sigue pendiente.
- Otros tipos de objeto (imagen, texto, música, aprendizajes) — el envelope genérico los soporta,
  cero schemas/UI reales.
- Sub-sesiones / manejo de presupuesto de contexto — diferido desde la primera ronda de
  planeación.
- Merge fino por concurrencia (per-shape/CRDT) — solo CAS a nivel de todo el objeto, aceptado
  como v1.
- Meta-DSL universal de payload entre tipos de objeto.

## Siguiente pieza — sin decidir todavía

Candidatos discutidos pero sin elegir uno: Planning-como-objeto-de-Canvas, un segundo tipo de
objeto real (imagen/texto), o cerrar #3/#4 primero por ser sistémicos. Decidir antes de generar el
próximo build prompt.
