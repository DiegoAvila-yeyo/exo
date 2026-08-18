# The Planning Manifesto

> Frozen after a full design pass with Claude (Aug 15, 2026). This is the
> north star for the Planning section of Exo — any feature that doesn't fit
> here doesn't belong in Planning, no matter how good the idea is in the
> abstract.

## Qué es

Planning es el lugar donde un proyecto decide quién quiere ser, antes de que
exista una sola línea de código.

No es una pizarra. No es un chat. No es documentación.

Es la memoria de por qué el proyecto terminó siendo lo que es.

## Qué problema resuelve

Todas las herramientas que usamos hoy —Figma, Miro, Notion, Obsidian,
ChatGPT— guardan el resultado del pensamiento. Ninguna guarda el pensamiento
en sí.

Seis meses después de una decisión, nadie recuerda por qué se tomó, qué
alternativas se descartaron, ni qué principio la sostenía. Esa pérdida se
paga en cada discusión repetida, cada decisión que se revierte sin saber que
ya se había probado antes, cada persona nueva que tarda semanas en entender
por qué el proyecto se ve como se ve.

Planning existe para que esa pregunta — **¿por qué terminó siendo así?** —
siempre tenga respuesta.

## Qué nunca debe hacer

- Nunca construye ni ejecuta código por su cuenta. Eso es trabajo de Project.
- Nunca decide por el usuario. Propone, el humano decide.
- Nunca se convierte en un historial de chat que hay que scrollear para
  entender algo.
- Nunca obliga al usuario a llenar formularios o clasificar cosas antes de
  poder pensar.
- Nunca muestra su propia arquitectura interna. El usuario piensa en notas,
  decisiones, preguntas — no en esquemas de datos. La palabra "Knowledge" es
  arquitectura, nunca aparece en la UI.
- Nunca pierde una decisión. Puede quedar superada, nunca borrada.

## Principios que nunca deben romperse

**Planning decide. Project ejecuta.**
Si algo ayuda a definir qué construir y por qué, vive aquí. Si ayuda a
construirlo, no.

**El humano siempre dirige.**
La IA organiza, sugiere y conecta. Nunca es dueña del proyecto.

**Nada se pierde, todo evoluciona.**
Una decisión que cambia no se edita en silencio — se reemplaza dejando
rastro de la anterior (`superseded_by`).

**Lo crudo y lo concluido no se mezclan.**
Una conversación no es una decisión. Se convierte en una cuando alguien la
destila (`derived_from`).

**La pantalla por defecto está vacía.**
El usuario entra a pensar, no a administrar. Lo demás (decisiones,
principios) aparece solo cuando el nodo seleccionado tiene contexto que
importar.

---

## Modelo conceptual v1 (congelado)

```
Workspace
 └── Planning                     (fuente de verdad)
      ├── Board                   (espacio visual)
      │     └── objetos visuales  (frame, arrow, rectangle, image, connector)
      ├── Knowledge               (tipos: Decision, Principle, Note, Research, Reference, Question)
      └── Project (N)             (scope ⊆ Knowledge/Boards; solo lee, nunca escribe directo)
```

Reglas:

1. Planning es la fuente de verdad del proyecto.
2. Projects nunca modifican Planning directamente; solo lo consultan o
   proponen cambios.
3. Boards son espacios visuales de trabajo, y son dueños únicamente de los
   objetos que no cargan conocimiento (dibujo puro).
4. Decision y Principle son el conocimiento canónico; el resto de tipos de
   Knowledge (Note, Research, Reference, Question) existen para apoyarlo, no
   para competir con él.
5. Todo tipo nuevo de Knowledge que surja se agrega como tipo, no como
   entidad — salvo que demuestre necesitar ciclo de vida y relaciones
   propias (como pasó con Decision y Principle).

Implementación de este modelo: ver [`planningstore/`](planningstore/model.go)
y [`build_prompt_PLANNING_UI.md`](build_prompt_PLANNING_UI.md) para el
siguiente round (UI).
