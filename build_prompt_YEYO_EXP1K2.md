# Build prompt — Experimento 1K2: telemetría de K1a + índice en la respuesta de `inspect`

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, los
reportes de Exp1 a Exp1K, y `~/yeyo/docs/codex_consult_navegacion_libre.md`
completo — especialmente la cuarta consulta al final, que motiva esta ronda.

**Resultado de K1a (Exp1K)**: el gate de protocolo (`atoms_decision(inspect|
skip)` como única tool en Fase 1) dio `P(inspect|relevante)=10/10=100%`, sin
alucinación, cumpliendo el protocolo en 14/14. Pero de esos 10 `inspect`,
**solo 3/10** siguieron con `atom get` en la Fase 2 (donde se abren `atom
list`/`atom get` junto con las tools normales de código, al mismo tiempo).
Donde sí llegó a `atom get`, selección y aplicación fueron 100% correctas.

## Parte 1 — telemetría de las 7 fallas de K1a (gratis, sin correr nada nuevo)

Antes de construir K2, volvé a los logs/transcripts crudos de las 14 corridas
de Exp1K (deberían estar en `~/yeyo/experiments/exp1k_logs/` o donde haya
quedado la instrumentación de esa ronda). Para cada uno de los 7 casos donde
hubo `inspect` pero no `atom get`, clasificá la **primera acción** que tomó el
modelo después de recibir la respuesta de `atoms_decision(inspect)`:

| Corrida | Primera acción tras `inspect` |
|---|---|
| ... | `atom get` / `Read` / `Grep`/`Search` / `Bash` / otra tool / respuesta de texto directa / declaración explícita de "ningún atom aplica" / otro |

Esto es puramente análisis de datos ya existentes — no correr nada de LLM
para esta parte. Agregalo al reporte final tal cual, sin interpretarlo todavía
(la interpretación va en la conclusión, después de tener también el resultado
de K2).

## Parte 2 — K2: índice entregado en la propia respuesta de `inspect`

Único cambio respecto a K1a: cuando el modelo responde `atoms_decision(action:
"inspect")`, el **resultado de esa misma llamada** debe incluir directamente
el índice completo de periferia (nombre + descripción de los 9 atoms — el
mismo contenido que hoy devuelve `atom list`, sin ranking ni preselección de
ningún tipo, eso introduciría selección externa y no es lo que queremos medir).
No hace falta que el modelo llame a `atom list` por separado — ya lo tiene en
el resultado del `inspect`.

Todo lo demás queda igual que K1a:

- Fase 1: `atoms_decision(action: enum["inspect","skip"])` como única tool
  disponible, sin campo `reason`.
- Si `skip`: se abren las tools normales, sin nada de `atom`.
- Si `inspect`: la respuesta de esa tool ya trae el índice; a partir de ahí se
  abren `atom get` + las tools normales **juntas** (todavía no exclusivo —
  eso sería K3, y solo se construye si esta ronda no alcanza).

Mismas 7 tareas, 2 repeticiones (14 corridas), igual que K1a y L, para
comparación directa fila por fila.

## Validación previa

Ping simple a `exo` en modo normal, confirmá que sigue sano. Verificá por
código que el índice efectivamente viaja dentro del resultado de la llamada a
`atoms_decision(inspect)` y no en un paso aparte.

## Reporte

`~/yeyo/experiments/exp1k2-report.md`, con:

- La tabla de telemetría de la Parte 1.
- `P(inspect|relevante)`, `P(skip|irrelevante)` de esta ronda — comparación
  directa contra K1a.
- **El número central**: `P(get|inspect)` de esta ronda vs. el 3/10 de K1a.
- Selección y aplicación correctas, para las que sí llegaron a `get` (esperado
  seguir ~100% si la teoría de competencia de acciones es correcta).
- Conclusión con umbrales explícitos, siguiendo el criterio de Codex:
  - Si `get/selección` ronda ~90-100%: parar acá — ya hay arquitectura
    (`atoms_decision` + índice en la respuesta + selección libre). No haría
    falta K3, ni few-shot, ni post-entrenamiento salvo como optimización
    posterior.
  - Si ronda ~50%: recomendar K3 — subfase exclusiva con `atom_select(name|
    none)` que devuelva el body directo en la misma llamada (no un `get`
    nominal aparte), como único paso disponible en esa subfase.
  - Si K2 sale peor que lo esperado o revela algo raro en la telemetría de la
    Parte 1 que contradiga la teoría de competencia de acciones: reportarlo
    sin suavizar, es información tan valiosa como una mejora.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
