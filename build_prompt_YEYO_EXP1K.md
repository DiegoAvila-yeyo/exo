# Build prompt — Experimento 1K: checkpoint como gate de protocolo (K1a)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md`, `~/yeyo/docs/experiments-roadmap.md`, los
reportes de Exp1 a Exp1H, y `~/yeyo/docs/codex_consult_navegacion_libre.md`
completo — especialmente la tercera consulta al final, que es la que motiva
directamente esta ronda y tiene el diseño exacto que hay que seguir.

**Resumen del estado**: 8 rondas acumuladas confirman que el modelo tiene una
trayectoria procedural entrenada muy fuerte para tareas de código
(investigar→leer→editar→responder) que ejecuta incluso sin herramientas
disponibles (alucinando tool-calls falsos en Exp1H). Instrucciones de texto
pidiendo una interrupción de esa trayectoria fallan siempre (0/14 en L y en
1H). Una tool real, con schema, para algo objetivamente necesario, funciona
perfecto (10/10 en P). Esta ronda prueba si un checkpoint construido como
**gate de protocolo real** (no una tool más entre otras) activa la rama que
venimos midiendo en cero.

## Requisitos de diseño — no son opcionales, son la razón de ser de esta ronda

Si alguno de estos tres puntos no se implementa así, el resultado no sirve
como prueba válida de K — quedaría con la misma debilidad metodológica que ya
descartamos en rondas anteriores:

1. **Máquina de dos fases real.** En la Fase 1, la **única** tool disponible
   para el modelo es `atoms_decision`. Nada de `Read`, `Edit`, `Bash`, `atom
   list`, `atom get`, ni ninguna otra — ni siquiera las 2 de plumbing que
   quedaron en L. El modelo no tiene otra opción de acción que llamar esa
   tool. Recién después de esa llamada, el harness abre la Fase 2:
   - Si respondió `inspect`: se habilitan `atom list`/`atom get` + las tools
     normales de código.
   - Si respondió `skip`: se habilitan solo las tools normales de código.
2. **Schema mínimo, sin campo de texto libre.**
   ```
   atoms_decision(
     action: enum["inspect", "skip"]
   )
   ```
   Nada de `reason`, nada de campo de texto — solo el enum. Esto es
   deliberado: un campo abierto le daría al modelo espacio para reconstruir
   en texto el loop de agente que estamos tratando de suprimir en esta
   medición.
3. **No confundir esto con L.** L medía si el modelo obedecía una
   interrupción textual ajena a su forma de trabajar — quedó invalidado como
   medida de si sabe clasificar relevancia. Este experimento es la primera
   medición limpia de esa clasificación, expresada como acción de protocolo,
   no como texto.

## Corrida

Mismas 7 tareas que en L (T1-T5 relevantes, I1-I2 irrelevantes), 2
repeticiones cada una (14 corridas). Medí, para cada corrida:

- ¿Qué respondió en la Fase 1 (`inspect` o `skip`)?
- Si respondió `inspect`: ¿después llamó `atom list`? ¿`atom get`? ¿del atom
  correcto? (Esto empieza a darnos, de yapa, la segunda transición causal que
  señaló Codex — `P(get|inspect)` — aunque el foco principal de esta ronda es
  la primera puerta.)

## Validación previa

Ping simple a `exo` en modo normal, confirmá que sigue sano. Verificá por
código (no preguntándole al modelo) que en la Fase 1 **de verdad** no hay
ninguna otra tool disponible más que `atoms_decision` — esto es lo más
importante de verificar antes de gastar las 14 corridas, porque si hay una
fuga de tools en la Fase 1 el experimento entero pierde validez.

## Piezas del rompecabezas

Igual que rondas anteriores: cualquier observación no binaria — si el modelo
intentó de alguna forma "hacer trampa" contra la restricción de la Fase 1 (ej.
texto describiendo que va a investigar antes de llamar la tool disponible),
diferencias entre repeticiones, cualquier señal rara en las respuestas de
`inspect` que después no siguieron con `atom get`.

## Reporte

`~/yeyo/experiments/exp1k-report.md`, con:

- `P(inspect|relevante)` sobre T1-T5, `P(skip|irrelevante)` sobre I1-I2.
- De las que dieron `inspect`, tasa de `atom get` correcto después (dato de
  yapa para la segunda transición).
- Comparación explícita contra el 0/14 de L — ¿mejoró, y cuánto?
- Conclusión siguiendo el criterio de Codex: si esto da alta precisión y alto
  recall sobre un dataset limpio y balanceado, es evidencia fuerte a favor del
  checkpoint como arquitectura final. Si da muchos `skip` falsos **incluso con
  el gate implementado correctamente como fase exclusiva**, es la falsación
  fuerte que cerraría la línea de navegación libre sin post-entrenamiento —
  ya no puede explicarse por costo de descubrimiento, competencia con otras
  tools, `autoInvestigate`, instrucción de texto ignorada, falta de schema, ni
  ninguna de las causas ya descartadas en rondas anteriores.
- Si hay señal pero no perfecta: recomendar si conviene seguir con K1b (agregar
  `basis` categórico) o con K2 (el índice entregado directo en la respuesta
  del `inspect`, para separar la segunda transición).

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
