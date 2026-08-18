# Build prompt — Experimento 1E: confirmar el patrón + reformulación epistémica

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, y los 4
reportes anteriores (`exp1-report.md`, `exp1b-report.md`, `exp1c-report.md` si
existe, `exp1d-report.md`).

**Hallazgo clave del Experimento 1D** (catálogo forzado, siempre visible): 9/12
en cero, 3/12 perfecto — los 3 aciertos fueron todos la misma tarea, formulada
como pregunta abierta de proceso ("¿cómo hago para...?"). Las tareas formuladas
como órdenes directas de código ("agregá X", "arreglá Y") siguieron en cero,
incluso con el trigger objetivamente presente (archivo cruzó 300 líneas,
duplicación identificada por el propio modelo sin leer el atom).

**Hipótesis a probar en esta ronda**: no es la forma gramatical de la tarea lo
que importa — es si el modelo **percibe que le falta conocimiento para
responder bien**. En preguntas abiertas de proceso, no tiene respuesta segura
propia, así que busca. En órdenes directas de código, su conocimiento general de
buenas prácticas le alcanza para producir algo razonable, así que no siente la
necesidad de validar contra nada externo — aunque en la práctica sí le falte la
convención específica del proyecto.

## Esta ronda hace dos cosas, en dos partes separadas — no las mezcles

### Parte A — confirmar el patrón con más datos

Con el texto de `centro-catalogo` **tal como quedó en el Experimento 1D/1B, sin
cambios**, correr un set de tareas más amplio que las 4 originales, para
verificar que el patrón (orden directa = cero, pregunta abierta = funciona) se
sostiene y no es casualidad de solo 3 tareas de cada tipo. Agregá estas 4 tareas
nuevas a las 4 originales (8 en total), 2 repeticiones cada una (16 corridas):

| # | Tarea | Tipo | Control (importante) |
|---|---|---|---|
| 5 | "Configurá dos worktrees de git para que pueda trabajar en dos features en paralelo sin pisarme." | orden directa | **Misma necesidad que la tarea 4 original (que sí funcionó), pero como orden en vez de pregunta.** Si esta cae a cero, confirma que es la forma de la tarea, no el tema. |
| 6 | "¿Cómo debería estructurar este archivo `config.py` de 290 líneas si le agrego una función de parseo más?" | pregunta abierta | **Mismo trigger que `protocolo-hulk` (tarea 2, que falló), pero como pregunta en vez de orden.** Si esta sí funciona, confirma la hipótesis desde el otro lado. |
| 7 | "Este archivo `helpers.py` tiene una función copiada dos veces con nombres distintos, corregilo." | orden directa | Segunda instancia de trigger de `protocolo-widow`, archivo distinto — para no depender de una sola instancia. |
| 8 | "Este archivo `parser.py` tiene 295 líneas, agregale una función más de validación." | orden directa | Segunda instancia de trigger de `protocolo-hulk`, archivo distinto. |

### Parte B — reformulación epistémica del centro

Reescribí el body de `centro-catalogo` a esto (reemplaza el texto imperativo de
1B/1D):

> Tu conocimiento general de buenas prácticas puede no coincidir con las
> convenciones específicas de este proyecto — incluso en tareas que parezcan
> simples y directas. No asumas que tu respuesta por defecto es suficiente solo
> porque la tarea parece clara. Antes de escribir o editar código, llamá a
> `atom list` para verificar si hay una convención de este proyecto que no
> conocés.

Con este texto nuevo, correr **las mismas 8 tareas** (las 4 originales + las 4
de la Parte A), 2 repeticiones cada una (16 corridas más).

## Validación previa

Ping simple a `exo` sin `yeyo` de por medio, confirmá que responde bien. Para la
Parte B, verificá por código (no preguntándole al modelo) que el nuevo texto
llegó al system prompt real antes de correr las 16 tareas.

## Medición y piezas del rompecabezas

Mismo log que rondas anteriores (`→ atom usado:`), más las mismas anotaciones de
observaciones no binarias (razonó sobre el atom sin usarlo, aplicó la convención
sin leerla, diferencias entre repeticiones).

Además, para esta ronda específicamente: anotá si las tareas 5 y 6 (los
controles cruzados) confirman o contradicen la hipótesis — son las más
importantes de las 8 para la conclusión.

## Reporte

`~/yeyo/experiments/exp1e-report.md`, con:

- Parte A: tabla de las 16 corridas — ¿se sostiene el patrón orden/pregunta con
  más datos? Resaltar especialmente las tareas 5 y 6.
- Parte B: tabla de las 16 corridas con el texto nuevo — comparación directa
  contra la Parte A, tarea por tarea. ¿Mejoraron específicamente las órdenes
  directas (1, 2, 3, 5, 7, 8), o no cambió nada?
- Conclusión honesta: ¿la reformulación epistémica mueve la aguja en las
  órdenes directas, que es donde falla todo hasta ahora? Si sí, en qué medida.
  Si no, decilo sin suavizar — en ese caso, el patrón orden/pregunta parece ser
  un límite real del enfoque de instrucción pura (sin importar cómo se redacte),
  y ahí sí correspondería evaluar la Variante B en serio, o escribirle a Codex
  con este hallazgo específico como pregunta.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
