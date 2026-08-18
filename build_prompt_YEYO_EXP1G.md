# Build prompt — Experimento 1G: control de tool-aversion (P) + juicio sin acción (L)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, los
reportes de Exp1/1B/1D/1E/1F, y **`~/yeyo/docs/codex_consult_navegacion_libre.md`
completo — tiene las dos respuestas de Codex, la segunda es la que motiva
directamente esta ronda (sección final del archivo).**

Resumen del estado: 6 rondas acumuladas muestran `P(list)=0`, `P(get|índice
visible)=0`, `P(apply|atom completo servido)≈100%` — el problema está acotado a
que la IA nunca entra en la rama de "considerar conocimiento externo" para
tareas donde ya tiene una respuesta directa segura. Esta ronda corre **solo**
los dos experimentos más baratos y más informativos de la lista que propuso
Codex (P y L) — no construyas M, N, H, K ni O todavía, quedan para después
según lo que salga acá.

## Experimento P — control de aversión general a tools

Objetivo: descartar que el cero acumulado sea un problema general de uso de
tools en `exo` (no específico de `yeyo`).

Agregá una tool nueva y trivial, `secret_value()`, que devuelve un string fijo
sin ninguna otra forma de obtenerlo (ej. `"XYLOPHONE-7734"` — no derivable ni
adivinable). Tarea: **"¿Qué valor exacto devuelve la función `secret_value`?"**
— no hay forma de responder correcto sin llamarla.

Corré esta tarea 10 veces, cada una en sesión nueva, mismo entorno que el resto
de los experimentos (con el catálogo de `yeyo` disponible pero irrelevante a
esta tarea puntual). Medí dos cosas por corrida: ¿llamó `secret_value()`?, y
¿el valor que reportó es el correcto?

Si esto da ~10/10 en ambas: confirma que no hay aversión general a tools — la
falla es específica de la situación donde existe una respuesta paramétrica
competitiva (el modelo "ya sabe" qué hacer sin consultar nada). Si da bajo
también: hay un problema más general de configuración de tool-use en `exo`
que hay que resolver antes de seguir con nada de `yeyo`.

## Experimento L — juicio semántico sin acción

Objetivo: separar "¿el modelo reconoce que podría haber una regla del proyecto
relevante?" de "¿actúa sobre ese reconocimiento?" — hasta ahora medíamos las
dos cosas mezcladas en un solo número.

Agregá un modo especial de ejecución (solo para esta prueba, no para uso
normal): antes de que el modelo pueda tocar cualquier archivo o llamar
cualquier tool de `atom`, tiene que responder **únicamente**:

```
project_rules_relevant: yes|no
```

Y ahí termina la corrida — el harness no debe dejar que continúe a ejecutar la
tarea después de esa respuesta. Nada de tools disponibles en este modo, ni
`atom`, ni nada de código — solo la clasificación.

Corré esto sobre 7 tareas, 2 repeticiones cada una (14 corridas):

| Tarea | Se espera |
|---|---|
| T1 (archivo 280 líneas) | yes |
| T2 (import + duplicación) | yes |
| T3 (función exportada en paquete Go interno) | yes |
| T4 (mensaje de error) | yes |
| T5 (leer config) | yes |
| I1 — "Explicame qué hace esta función en `utils.py`" (nueva, solo lectura/explicación, sin convención de proyecto involucrada) | no |
| I2 — "¿Qué versión de Python parece usar este proyecto?" (nueva, inspección simple) | no |

## Validación previa

Ping simple a `exo` sin nada de `yeyo`, confirmá que responde bien. Verificá
por código (no preguntándole al modelo) que el modo de "solo juicio, sin
acción" de L realmente corta la ejecución después de la respuesta — que no siga
de largo por error.

## Piezas del rompecabezas

Igual que rondas anteriores: anotá cualquier observación que no sea binaria —
si en L el modelo dudó entre yes/no en el texto de razonamiento antes de
decidir, si en P llamó la tool pero reportó mal el valor, cualquier patrón
entre repeticiones.

## Reporte

`~/yeyo/experiments/exp1g-report.md`, con:

- **P**: tasa de tool-call correcto (10 corridas) y tasa de valor reportado
  correcto.
- **L**: tabla de las 14 corridas — accuracy de la clasificación yes/no contra
  lo esperado, separado por tarea.
- Conclusión explícita, siguiendo el árbol de decisión de Codex:
  - Si P es alto: no hay aversión general a tools, la falla es específica de
    `yeyo`.
  - Si L acierta alto en las tareas "yes" (reconoce relevancia
    correctamente) pero seguimos sabiendo que `P(list)` real es 0 (de rondas
    anteriores): la falla está en la transición juicio→acción, y el checkpoint
    (K) pasa a ser el candidato principal.
  - Si L falla en reconocer relevancia (muchos "no" donde correspondía "yes"):
    la falla es más profunda, anterior a cualquier tool, y el few-shot
    contrastivo (H) se vuelve más urgente que el checkpoint.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
