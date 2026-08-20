# Build prompt — Primer catálogo real de `yeyo` (rollout + Experimento 3)

## Contexto

Sesión de Claude Code nueva. Leé, en orden: `~/yeyo/docs/vision.md` completo,
`~/yeyo/docs/experiments-roadmap.md` completo (especialmente "Fase
operacional", puntos 2-7), `~/yeyo/docs/codex_consult_catalogo_real.md`
completo (diseño y respuesta de Codex). Todo el diseño ya está decidido — no
rediseñes nada de esto, ejecutalo.

**Chequeo previo obligatorio**: puede haber otra sesión (Canvas/planning)
trabajando en paralelo sobre `~/exo`. Corré `git status` en `~/exo` antes de
tocar nada — no toques cambios que no reconozcas como tuyos.

## Objetivo — dos cosas a la vez, ver la nota al final de por qué van juntas

1. Ejecutar los pasos 2-4 del rollout ya diseñado (fixtures separadas,
   estructura de catálogo global+project, primeros 5 atoms reales).
2. Que uno de esos 5 atoms sea de **conocimiento destilado** — eso cubre el
   Experimento 3 del roadmap original, que estaba planeado para transferir
   sobre uso real en vez de correr como ronda sintética aparte.

## Paso 1 — separar los fixtures experimentales

Los 10 atoms usados en `Exp1`-`Q6b` salen del loader productivo por
completo (decisión ya tomada, roadmap punto 4): mové a algo inequívoco
(`fixtures/experimental-atoms/` o similar), fuera de
`Periferia()`/`RenderIndex()` en producción. `Role` sale del modelo `Atom`
productivo — queda solo en el código de fixtures, no en producción.

## Paso 2 — estructura de catálogo global + project

Scope determinado por `rootPath` (mismo patrón que `instructions.Load
(rootPath)`). Empezá centralizado si el loader actual lo hace más simple
(`~/yeyo/catalogs/{global,exo,...}/` o equivalente) — la frontera
global/project tiene que ser **estructural** (un campo real), no una
descripción de texto tipo "solo para X" dentro de un atom que igual entra a
todos los índices. Prueba para decidir si un atom es global: *"si mañana
abro un repo que nunca vi, ¿esta regla debería seguir aplicando?"*

## Paso 3 — primeros 5 atoms reales, diversos

Fuente primaria: `~/.claude/CLAUDE.md` (Diego, global — leelo vos mismo, no
inventes contenido). Migración = **preservación semántica, no copia
literal** — cada atom tiene que entenderse sin haber leído `CLAUDE.md`
completo (si hace falta preguntar "¿pero en qué contexto?", la atomización
falló). Diversidad obligatoria, no 5 versiones de lo mismo:

1. Un atom de **comportamiento global** (ej. de Protocolo Widow o Hulk).
2. Un atom de **convención de proyecto** — de `exo`/`yeyo` mismos (dogfooding,
   piloto elegido en el roadmap), no de otro repo todavía.
3. Un atom de **workflow** (ej. convención de branches/commits).
4. Un atom con **scope de lenguaje** (ej. convenciones de Python).
5. **Un atom de conocimiento destilado** — esto es lo que cubre el
   Experimento 3. Tiene que ser un hecho verificable sobre `yeyo`/`exo` (no
   una regla de comportamiento) donde un modelo sin el atom tienda a
   inventar — ej. detalles reales de la arquitectura del gate, o del
   formato de metadata de atoms.

Cada atom lleva las dos familias de metadata (ya decididas, no las
rediseñes): runtime/semántica (`type`, `scope`, `status`, `supersedes`,
`specializes`, `exception_of`, la que ve el modelo) y lifecycle/provenance
(`source_path`, `source_section`, `source_revision`/`source_hash`,
`created_at`, `updated_at`, para mantenimiento).

## Paso 4 — acceptance check por atom

Para cada uno de los 5: una tarea real donde debe aplicar, una donde no
debe aplicar. Si tiene relación (`supersedes`/`specializes`/`exception_of`),
un tercer caso donde esa relación importe.

**Para el atom de conocimiento (Experimento 3) específicamente, un cuarto
chequeo**: hacele al modelo, sin el atom disponible, una pregunta donde el
hecho real importa (para ver si inventa/alucina un detalle) — después la
misma pregunta con el atom disponible — comparar si citar el atom evita la
alucinación. Esto es lo que el Experimento 3 original quería medir, ahora
con contenido real en vez de sintético.

## Por qué las dos cosas van en el mismo build

El roadmap ya decía que el Experimento 3 debía "transferir directo" sobre
el mecanismo validado, sin ronda sintética aparte — y el rollout del
catálogo real necesita, de todos modos, al menos un atom de conocimiento
para tener diversidad real. Hacerlas por separado sería repetir trabajo.

## Validación

Mismo protocolo de siempre para no tocar producción real durante las
pruebas: build aparte, bajar el servicio real, probar con
`EXO_YEYO_GATE=1`, restaurar y confirmar sano al final.

## Reporte

`~/yeyo/experiments/catalogo-real-report.md` — qué atoms se escribieron
(contenido completo, no solo nombres), resultado del acceptance check de
cada uno, y el resultado específico del chequeo de alucinación del atom de
conocimiento. Documentá cualquier decisión de diseño que hayas tenido que
tomar sobre la marcha que no estuviera ya especificada.

Commits en `~/yeyo`. `~/exo` sigue sin commitear salvo que decidas
explícitamente lo contrario.
