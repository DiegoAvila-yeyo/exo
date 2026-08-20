# Build prompt — Uso real del gate: máximo volumen de datos, chateando de verdad

## Contexto

Sesión de Claude Code nueva, con acceso al navegador (Browser pane). Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`
completo (especialmente "Fase A cerrada" — el gate y la telemetría ya están
migrados a `~/exo` real, verificados, detrás de `EXO_YEYO_GATE`), y el
hallazgo sin resolver de la última prueba en vivo: al probar el gate en una
sesión real de varios turnos, un segundo mensaje corto ("la ruta es X")
pareció hacer que el gate respondiera `skip` sin repetir el contexto de la
tarea original — pero el archivo de esa prueba no cruzó realmente las 300
líneas, así que quedó sin poder confirmarse si fue correcto o un fallo real.

## Objetivo — volumen y variedad máxima, no un checklist prolijo

Nada de `cmd/gatesmoke`, nada de tests de Go, nada de scripts que mandan
mensajes programáticamente. **Abrí el navegador, andá a la UI real de
`exo`, y escribí mensajes en el chat vos mismo** — muchas conversaciones
distintas (apuntá a 15-20 como mínimo, no 3 o 4), cada una de varios turnos,
cubriendo la mayor variedad posible de situaciones reales.

## Advertencia importante sobre un sesgo real, decilo así en el reporte

Sos una IA simulando ser un usuario — no sos Diego con fricciones genuinas.
Hay un riesgo concreto: sin darte cuenta, podés escribir mensajes que
"cooperan" con el sistema (usando palabras parecidas a las descripciones de
los atoms) de una forma que un usuario real, apurado o distraído, no
haría. Contramedida activa, no pasiva: **para al menos la mitad de las
conversaciones, parafraseá deliberadamente lejos de las palabras que usan
las descripciones de los atoms** — si un atom dice "límite de 300 líneas",
probá pedir algo como "este archivo se puso gigante" en vez de repetir
"300 líneas". Si notás que caíste en fraseo que calca la descripción de un
atom, anotalo en el reporte — es un dato en sí mismo, no lo escondas.

## Cómo correr esto sin tocar el `exo` de producción real de Diego

1. `go build -o /tmp/exo-uso-real .` en `~/exo` (con los cambios de Fase A
   ya en el working tree, sin commitear).
2. Bajar el servicio real (`launchctl bootout gui/$(id -u)/com.diegoavila.exo`)
   antes de arrancar tu binario de prueba en el mismo puerto — confirmá que
   el puerto queda libre antes de seguir.
3. Correr `EXO_YEYO_GATE=1 /tmp/exo-uso-real serve`, con las variables de
   `~/Library/Application Support/exo/agent.env` ya disponibles (se cargan
   solas).
4. Abrir el navegador en `localhost:45873` y chatear ahí, de verdad.
5. Al terminar TODA la sesión de chat (no antes, no a medias): matar el
   proceso de prueba, y `launchctl bootstrap gui/$(id -u)
   ~/Library/LaunchAgents/com.diegoavila.exo.plist` para restaurar el
   servicio real. Confirmá con un ping simple que quedó sano antes de cerrar
   este build.

## Qué conversar — cobertura amplia, como guía, no como guion letra por letra

- Los 10 atoms del catálogo actual, cada uno en al menos una conversación
  separada, con fraseo variado (mitad literal, mitad parafraseado como se
  pidió arriba).
- Varias tareas control/distractor, mezcladas entre las demás, no todas
  juntas.
- **La más importante**: al menos 2-3 conversaciones de varios turnos donde
  le pedís algo sin dar todos los datos, te pregunta, y respondés después —
  asegurate en al menos una de que el archivo real termine cruzando las 300
  líneas sin ambigüedad, para responder la pregunta pendiente.
- Varias conversaciones que cambian de tema a mitad de camino.
- Al menos 2-3 correcciones reales ("no, eso no es lo que quería") en
  distintos puntos de distintas conversaciones.
- Conversaciones largas (8-10+ turnos) y conversaciones cortas (1-2 turnos)
  — para ver si la longitud de la sesión afecta algo.
- Si se te ocurre algo que no está en esta lista pero se siente como uso
  real, hacelo — la cobertura de la lista es un piso, no un techo.

## Después de chatear — recopilar TODO, no solo un resumen

1. **Export completo de `yeyo_telemetry.db`** — volcá las tablas enteras
   (no solo agregados) a un archivo adjunto o a bloques de código en el
   reporte, para que quede el dato crudo disponible después, no solo tu
   interpretación.
2. Separá explícitamente, en dos secciones del reporte:
   - **Objetivo (de la telemetría)**: `P(inspect|relevante)`,
     `P(skip|irrelevante)`, estabilidad de `catalog_hash`, cualquier
     `turn_result: error`, orden de `get` por conversación.
   - **Juicio subjetivo tuyo, después del hecho**: para cada conversación,
     tu propia evaluación de si el gate acertó o no — dejá claro que esto
     es interpretación, no medición.
3. Foco especial en el escenario de contexto entre turnos — es la pregunta
   que sigue abierta desde la prueba anterior, respondela con la evidencia
   que juntes.
4. Notá explícitamente cualquier caso donde vos mismo caíste en fraseo
   calcado de una descripción de atom (la advertencia de arriba).

Guardalo en `~/yeyo/experiments/uso-real-report.md`, con los datos crudos
como archivo aparte si son muy largos para el cuerpo del reporte. Commit en
`~/yeyo` si corresponde. `~/exo` sigue sin commitear salvo que decidas
explícitamente lo contrario.

## Importante

Confirmá, antes de terminar, que el `exo` real de producción quedó
restaurado y sano — la verificación más importante de todo el build, porque
es el `exo` que Diego usa de verdad todos los días.
