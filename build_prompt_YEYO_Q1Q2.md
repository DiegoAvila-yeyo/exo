# Build prompt — Experimento 1-bis, Q1+Q2: escala pura + control de posición

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md` (sección "Arquitectura validada"),
`~/yeyo/docs/experiments-roadmap.md` (sección "Experimento 1-bis"), y
`~/yeyo/docs/codex_consult_escala_catalogo.md` completo — tiene la respuesta de
Codex con el diseño exacto de este experimento (busca "Q1 — escala pura" y
"Q2 — posición").

**Mecanismo ya validado, no lo toques**: gate de protocolo obligatorio
(`atoms_decision(action: enum["inspect","skip"])`, única tool en Fase 1, sin
campo de texto libre) + índice de periferia entregado en la propia respuesta
de `inspect` + `atom_get`/selección libre después. Esta ronda reusa ese
mecanismo tal cual — el código de `~/exo` de Exp1K/1K2 debería servir como base
(sigue sin commitear, encontralo en el estado del working tree).

## Objetivo — una sola pregunta, con una precaución metodológica obligatoria

¿`P(get correcto|inspect)` se degrada a medida que crece el catálogo, con
distractores limpios (claramente irrelevantes, sin vecinos semánticamente
confusos — eso es la próxima ronda, Q3, no esta)?

**Precaución que Codex marcó como obligatoria, no opcional**: si no
randomizamos la posición del atom correcto dentro del índice en cada
repetición, cualquier caída (o ausencia de caída) que midamos puede deberse a
sesgo de posición, no al tamaño del catálogo — el resultado quedaría
contaminado y no serviría. Por eso esta ronda combina escala (Q1) y posición
(Q2) desde el arranque, no las separa.

## Diseño

**Atom objetivo (target)**: reusar `protocolo-hulk` (el de tamaño de archivo
>300 líneas) como único atom relevante, y la misma tarea de prueba ya usada en
rondas anteriores ("archivo `utils.py` de 280 líneas, agregale 3 funciones
más") — mantener esto fijo en todas las condiciones para que la comparación
sea limpia.

**Distractores limpios**: generá atoms de relleno, todos de tipo
comportamiento, con contenido variado y plausible pero **claramente
irrelevante** a la tarea de archivo/tamaño — cubrí varios dominios distintos
(convenciones de testing, CI/deploy, logging, manejo de dependencias, diseño de
API, migraciones de base de datos, documentación, seguridad no relacionada a
tamaño de archivo, etc.). Nada de vecinos semánticos cercanos al target
todavía — eso es explícitamente la próxima ronda.

**Tamaños de catálogo**: `N = 9, 20, 50, 100, 200` (contando el target). Usá el
catálogo de 9 ya existente como el primer punto (con sus distractores actuales,
sin los de rol "no-obvia" de Exp1F si generan ruido — mantené limpio el
criterio de "claramente irrelevante").

**Randomización obligatoria en cada repetición**:
- Posición del atom target dentro del índice — cambiala en cada corrida (no
  fija en el mismo lugar).
- Orden del resto de los distractores también randomizado, no solo la posición
  del target.

**Repeticiones**: 3 por cada valor de N (15 corridas en total).

## Validación previa

Ping simple a `exo` sin nada de `yeyo`, confirmá que responde bien. Verificá
por código que la randomización de posición/orden realmente varía entre
corridas (no quedó fija por accidente).

## Medición

Por cada corrida, registrá:
- `P(inspect|relevante)` — chequeo de cordura, se espera que se mantenga
  estable sin importar N (según la predicción de Codex).
- `P(get\ target|inspect)` — métrica principal.
- `P(get\ incorrecto|inspect)` — si trajo un atom equivocado.
- `P(sin\ get|inspect)` — inspeccionó pero no llegó a pedir nada.
- `P(apply|get\ correcto)`.
- Tokens y latencia de la corrida completa.
- La posición exacta del target en el índice para esa corrida (para poder
  chequear después si, a pesar de la randomización, hay algún patrón de
  posición dentro de este mismo experimento).

## Reporte

`~/yeyo/experiments/exp1bis-q1q2-report.md`, con:

- Tabla completa de las 15 corridas.
- Curva de `P(get\ target|inspect)` vs. N — ¿se mantiene plana, como predice
  Codex, o empieza a degradar ya en catálogos limpios?
- Curva de `P(inspect|relevante)` vs. N — confirmar que se mantiene estable
  (si no, es señal de contaminación experimental, revisar antes de seguir).
- Curva de tokens/latencia vs. N — cuantificar el costo real de crecer el
  catálogo, aunque la precisión aguante.
- Chequeo de posición dentro de este experimento — ¿hay algún patrón visible
  pese a la randomización?
- Conclusión: si `P(get\ target|inspect)` se mantiene alta incluso en N=200
  con distractores limpios, confirma la predicción de Codex (el problema real
  no es escala bruta, es densidad semántica) y motiva correr Q3 (vecinos
  semánticos) como siguiente paso, no antes probar jerarquía. Si ya degrada acá
  con distractores limpios, es un hallazgo más fuerte de lo esperado — repensar
  el orden del programa completo antes de seguir a Q3.

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
