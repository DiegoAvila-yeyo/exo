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
- [ ] #1 — `canvas_edit_object` (edición vía IA de objeto materializado, versiona con `supersedes`)
- [ ] #2 — auto-layout de diagramas (nodos ya no se apilan en `(0,0)`)
- [ ] #5 — referencias de edge rotas se rechazan al guardar (manual y vía tool)
- [ ] #6 — placeholder de canvas vacío se oculta correctamente

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
