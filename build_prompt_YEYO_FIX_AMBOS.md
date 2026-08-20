# Build prompt — Arreglar los dos bugs confirmados en uso real del gate

## Contexto

Sesión de Claude Code nueva. Leé, en orden: `~/yeyo/docs/vision.md`
completo (especialmente "Uso real (post Fase A)"), y
`~/yeyo/experiments/uso-real-report.md` + datos crudos en
`~/yeyo/experiments/uso-real-data/`.

**Chequeo previo obligatorio**: puede haber otra sesión (Canvas/planning)
trabajando en paralelo sobre el mismo `~/exo` sin commitear. Corré
`git status` antes de tocar nada — si hay cambios que no reconocés, no los
toques ni los revisas, son de la otra sesión.

Dos bugs confirmados con uso real (no sintético), independientes entre sí —
arreglalos en orden, cada uno con su propio ciclo de reproducir→arreglar→
confirmar→control, no los mezcles en un solo cambio.

---

## Bug 1 — pérdida de contexto del gate entre turnos

**El bug, con precisión**: el gate (`atoms_decision`) se resetea a "solo
esta tool disponible" al arrancar cada turno y decide `inspect`/`skip`
mirando, en la práctica, solo el contenido de ese turno. Caso confirmado: un
archivo verificablemente en 305 líneas recibió `skip` en el turno donde el
usuario solo respondió "la ruta es X" a una pregunta del propio agente, sin
repetir el pedido original.

**Pista concreta, no diseñes desde cero**: `Coordinator` ya tiene mecanismos
que trackean tareas activas (`ensureTask`, `tasks_file`/`progress`,
encontrados al migrar el gate en Fase A — ver `Coordinator.BootstrapTools`).
Investigá primero si alguno ya mantiene algo parecido a "hay una tarea sin
resolver, y esta es su descripción" — si existe, dale acceso a esa señal al
gate antes de decidir, no la reinventes. Si no existe nada usable,
documentalo antes de construir algo nuevo.

**Lo que NO hacer**: no cambies el diseño del gate en sí (sigue siendo la
única tool disponible en ese punto, sin campo de texto libre); no le mandes
el historial completo de la conversación (reintroduce el costo que el
índice liviano evitó — buscá la señal mínima suficiente); no toques
`atom_tool.go` ni la selección después de `inspect` (`P(get correcto|
inspect)` nunca falló, no es el problema).

**Validación**:
1. Reproducí el bug primero con el mismo tipo de escenario del reporte,
   para confirmar que se arregla algo real, no una hipótesis.
2. Implementá el arreglo.
3. Repetí el mismo escenario — confirmá que ahora dispara `inspect`.
4. Control: una conversación que genuinamente cambia de tema (código →
   pregunta sin relación) — confirmá que el arreglo no deja al gate
   "pegado" a la primera tarea para siempre.

---

## Bug 2 — sesgo léxico en la decisión inicial del gate

**Por qué NO es el mismo sesgo de Q3B/Q3C, leelo con cuidado**: Q3B/Q3C
encontraron sesgo en la *selección* entre atoms ya visibles (el índice,
después de `inspect`). Esto es antes de eso — la decisión `inspect`/`skip`
ocurre **antes** de ver el índice, así que no hay ningún atom con el que
comparar palabras. Caso confirmado: "300 líneas" (fraseo literal) disparó
`inspect` correcto; "se puso gigante" (parafraseado, mismo significado) dio
`skip`, con una respuesta que contradijo la convención real del proyecto.

**Pista concreta**: en el diseño original
(`docs/codex_consult_navegacion_libre.md`, tercera/cuarta consulta) se
había propuesto el **Experimento H — few-shot contrastivo**: ejemplos en el
centro de trayectorias donde, ante una tarea que *parece* resolverse con
conocimiento general (fraseo variado), el agente igual consulta el
catálogo. Se descartó correr H porque K1a/K2 dieron resultado perfecto en
sintético — la evidencia de uso real ahora sugiere que sí hace falta.
Empezá por ahí.

**Diseño de los ejemplos**: pares contrastivos, no solo positivos (al menos
un ejemplo de `skip` correcto — nada de ceremonia obligatoria de siempre
inspeccionar); fraseo variado, sin calcar las palabras de los atoms reales
(que el ejemplo no termine siendo keyword-matching disfrazado); al menos un
ejemplo con fraseo casual tipo "se puso gigante".

**Lo que NO hacer**: no reescribas las descripciones de los 10 atoms (el
problema no es ahí, es antes de verlas); no toques el schema del gate; no
agregues un matcher determinista.

**Validación**:
1. Reproducí el caso real primero, para confirmar el fallo antes de tocar
   nada.
2. Agregá los ejemplos few-shot al centro.
3. Repetí el mismo caso — ¿dispara `inspect` ahora?
4. Probá varias variantes de fraseo casual nuevas (no solo la conocida) —
   ¿generalizó, o memorizó el caso puntual?
5. Corré las tareas control/distractor de rondas anteriores — confirmá que
   no se volvió "gatillo fácil" en general (`P(skip|irrelevante)` no debería
   empeorar mucho).

---

## Protocolo compartido, una sola vez para los dos bugs

Mismo procedimiento de siempre para no tocar producción: build aparte
(`go build -o /tmp/exo-fix-test .`), bajar el servicio real
(`launchctl bootout gui/$(id -u)/com.diegoavila.exo`), probar con
`EXO_YEYO_GATE=1`, y al terminar **los dos arreglos** (no entre uno y otro):
matar el proceso de prueba, `launchctl bootstrap` para restaurar, confirmar
sano con un ping antes de cerrar el build.

## Reporte

`~/yeyo/experiments/fix-contexto-y-lexico-report.md`, con una sección por
bug: qué se cambió, evidencia de que el caso reproducido ahora funciona,
evidencia de que el control correspondiente sigue bien. Si alguno de los
dos no tiene un arreglo simple, decilo así — no fuerces una solución débil
para cerrar el build.

Commits en `~/yeyo` si corresponde. `~/exo` sigue sin commitear salvo que
decidas explícitamente lo contrario.
