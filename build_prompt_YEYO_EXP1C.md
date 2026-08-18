# Build prompt — Experimento 1C: sugerencia determinista (Variante B)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé completo,
en este orden:

1. `~/yeyo/docs/vision.md`
2. `~/yeyo/docs/experiments-roadmap.md`
3. `~/yeyo/experiments/exp1-report.md` y `~/yeyo/experiments/exp1b-report.md` —
   **16 corridas acumuladas en 0% de uso espontáneo de la tool `atom`**, con dos
   redacciones distintas del atom del centro. Conclusión de esas dos rondas: el
   problema no es de redacción — hace falta una capa de sugerencia determinista,
   como ya usa `nucleo-base` para skills (`skills.Match`, en
   `~/nucleo-base/layer2-runtime-rails/skills/match.go` — leelo como referencia
   exacta de la mecánica a replicar, no lo importes ni lo modifiques).

## Objetivo de esta ronda — una sola variable nueva

Se agrega **solo** una capa de sugerencia determinista, equivalente a
`skills.Match` pero para los atoms de periferia de `yeyo`. No se cambia el texto
de `centro-catalogo` (queda como en el Experimento 1B), no se tocan los atoms de
periferia, no se cambia el mecanismo de la tool `atom` (`list`/`get` se queda
igual, disponible para que la IA la siga usando si quiere).

## Pieza nueva 1 — matcher determinista en `yeyo`

Nueva función en el paquete `yeyo`, ej. `Suggest(taskText string, max int)
[]Atom`, mecánica calcada de `skills.Match`:

- Tokenizar el texto de la tarea: minúsculas, separar por caracteres no
  alfanuméricos, descartar tokens de menos de 3 letras y stopwords comunes
  (español + inglés — podés reusar la misma lista de `skills/match.go` como
  referencia).
- Para cada atom de periferia, tokenizar `name + description`, contar coincidencias
  con los tokens de la tarea.
- Ordenar por score descendente, devolver los primeros `max` con score > 0 (si
  ninguno tiene score > 0, devolver vacío — no forzar sugerencias sin señal real).
- Usar `max = 2`, igual que `skills.Match` en `nucleo-base`.

Esto es lógica pura, sin LLM — escribí tests unitarios directos contra los 9 atoms
de periferia reales, sin gastar ninguna corrida de chat, **antes** de tocar `exo`.
Si el matcher no sugiere bien contra las 4 tareas de prueba (ver tabla abajo) a
nivel de test unitario, no tiene sentido seguir a la parte con LLM — arreglalo acá
primero.

## Pieza nueva 2 — enganche por turno en `exo` (esto sí es nuevo respecto a rondas anteriores)

Hallazgo ya documentado en `vision.md`: `exo` no tiene ningún mecanismo de
decoración por turno — el mensaje del usuario se manda tal cual. Para que la
sugerencia determinista llegue a tiempo (antes de que la IA empiece a razonar la
tarea), hay que agregar un enganche mínimo por turno — buscá dónde `exo` arma el
mensaje que efectivamente se manda al agente en cada turno (dentro de
`agenthost`, no en `termserver/chat.go` que solo hace de capa HTTP) y, ahí,
antes de mandarlo, si `Suggest()` devuelve algo, anteponer un bloque:

```
[ATOMS SUGERIDOS — evaluá si aplican a esta tarea antes de actuar]
- <name>: <description>
- <name>: <description>
```

Esto es lo único nuevo de infraestructura en esta ronda — todo lo demás (tool
`atom`, atoms del centro) queda igual que en el Experimento 1B.

## Medición — separar "se sugirió" de "se usó"

Extendé el log del stream de chat para loggear las dos cosas por separado:
- `→ atom sugerido: [...]` — lo que devolvió el matcher determinista para esa
  tarea (esto ya lo podés verificar con el test unitario, sin gastar LLM).
- `→ atom usado: <name>` — igual que antes, cuando la IA efectivamente llama
  `atom get <name>`.

## Validación previa (aislamiento de fallos)

Antes de correr las tareas con LLM: (a) los tests unitarios del matcher pasan
contra la tabla de abajo, (b) ping simple a `exo` sin nada de `yeyo` de por medio
responde bien.

## Corrida

Mismas 4 tareas, mismos fixtures reseteados, 3 repeticiones cada una (12
corridas), igual que en el Experimento 1B, para comparación directa.

| # | Tarea | Se espera que sugiera |
|---|---|---|
| 1 | función de validación de email en `utils.py` de 150 líneas | `no-hardcoded-secrets`, `commit-message-format` |
| 2 | archivo de 280 líneas, agregar 3 funciones más | control + `protocolo-hulk` |
| 3 | import sin usar + función duplicada | control + `protocolo-widow` |
| 4 | trabajar en dos features en paralelo | control + `worktrees-not-code-dir` |

## Importante — esto prueba viabilidad-con-ayuda, no navegación libre

Esta ronda no es una variación de los Experimentos 1/1B — es un mecanismo
distinto. Ahí la IA debía decidir sola si explorar el catálogo (navegación
libre); acá un matcher determinista ya eligió los candidatos por ella y se los
pone directo en el prompt. Si esta ronda "funciona", lo que queda demostrado es
que el sistema es viable *con ayuda determinista* — **no** que la IA sepa navegar
el catálogo libremente por su cuenta, que sigue siendo la pregunta de fondo sin
contestar. No lo presentes en el reporte como si fuera la misma pregunta que las
rondas anteriores — es una pregunta relacionada pero distinta, dejalo explícito.

## Recopilar piezas del rompecabezas — no solo el resultado binario

Además de la métrica principal (sugerido vs. usado), anotá en el reporte
cualquier observación que no encaje en el sí/no, aunque no sea el resultado
esperado — estas piezas sueltas importan tanto como el resultado final:

- ¿La IA mencionó o razonó sobre el atom sugerido en su texto, aunque no haya
  llamado `atom get`? (Eso sería señal de que "vio" la sugerencia pero no actuó
  formalmente — distinto a no verla en absoluto.)
- ¿Aplicó la regla del atom en el código sin haber llamado la tool — como si ya
  supiera la convención sin necesitar leerla? (Distinto de "la ignoró".)
- ¿Hubo diferencia de comportamiento entre repeticiones de la misma tarea?
- ¿Algo en el texto de respuesta de la IA sugiere que "vio" el bloque de
  sugerencias pero lo descartó explícitamente, y por qué?

No hace falta que estas observaciones tengan una conclusión — con anotarlas tal
cual aparecen alcanza, se van a interpretar en conjunto con el resto del
roadmap.

## Reporte

`~/yeyo/experiments/exp1c-report.md`, con:

- Resultado de los tests unitarios del matcher (¿sugiere lo esperado por tarea?).
- Tabla de las 12 corridas: qué se sugirió vs. qué se usó — separar ambas
  columnas, no mezclarlas.
- Comparación contra el 0% acumulado de los Experimentos 1 y 1B.
- Conclusión honesta: si con la sugerencia puesta directo en el prompt tampoco se
  usa, decilo sin suavizar — en ese caso el problema no es de descubribilidad
  (ya no hace falta explorar nada, el candidato está servido) sino de que la IA no
  respeta ninguna instrucción de proceso sin importar cuánta ayuda se le dé, y
  hay que repensar el enfoque completo antes de seguir invirtiendo en el resto del
  roadmap.

Commits en `~/yeyo` con el mismo formato de rondas anteriores. `~/exo` sigue sin
commitear — no lo toques en ese sentido, solo modificá el código necesario.
