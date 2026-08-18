# Build prompt — Experimento 1-bis, Q6: composición pura (multi-select, un solo tipo)

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md` completo (especialmente "De 'selección de un
atom' a semantic planning" y "Cuatro métricas de corrección"),
`~/yeyo/docs/experiments-roadmap.md`, `~/yeyo/docs/codex_consult_escala_
catalogo.md` completo (la quinta respuesta de Codex diseña este experimento
exacto), y `~/yeyo/experiments/exp4-q4-report.md` (el resultado que motiva
que esta ronda vaya antes que Q5: precedencia/autoridad/escala salieron
todas mejor de lo esperado — la hipótesis fuerte ahora es composición, no
retrieval).

## Objetivo — la pregunta que ningún experimento anterior probó todavía

Todos los experimentos hasta ahora midieron "¿cuál atom aplica?" (top-1, o
top-1 entre confundibles). Esta ronda mide algo distinto: **cuando varios
atoms del mismo tipo aplican simultáneamente a la misma tarea, sin ningún
conflicto ni precedencia entre ellos, ¿el modelo trae todos, o se olvida de
alguno?**

**Deliberadamente la versión más simple posible de composición** — mismo
tipo de atom, sin mezclar comportamiento/conocimiento/integración todavía
(eso sería una ronda aparte, Q5-relacionada, solo si esto revela un problema)
— para aislar "¿recuerda que aplican varios?" de "¿distingue bien entre
tipos?".

## Los tres atoms (contenido exacto — todos aplican a la vez, sin conflicto)

**`split-large-file`** — "Si un archivo de código supera las 300 líneas,
dividilo en módulos más chicos por responsabilidad."

**`preserve-public-api`** — "Al dividir o reorganizar un archivo, preservá
las funciones/símbolos públicos existentes — no rompas la superficie pública
aunque cambie la organización interna."

**`update-package-docs`** — "Cuando cambies la estructura de un paquete
(dividir archivos, mover funciones), actualizá la documentación del paquete
para que refleje la nueva organización."

Ninguno contradice a otro, ninguno tiene relación de `specializes`/
`exception_of`/`supersedes` con los otros dos — son genuinamente
independientes y los tres aplican a la vez a la misma tarea.

**Tarea de prueba**: "Este archivo interno `handlers.go` tiene 320 líneas y
expone varias funciones públicas. Necesito dividirlo en módulos más chicos,
sin romper nada que ya se esté usando desde afuera, y que quede bien
documentado."

Catálogo N=50 (los 3 target + distractores limpios de rondas anteriores),
posición y orden randomizados en cada corrida, misma disciplina de siempre.

## Corrida

5 repeticiones (más que rondas anteriores — esta es la pregunta que Codex
marcó como la única todavía sin ninguna evidencia indirecta, vale la pena más
muestra).

## Medición — multi-label, no exact match

Por cada corrida, registrá el **conjunto** de atoms que trajo (`get`) contra
el conjunto esperado `{split-large-file, preserve-public-api,
update-package-docs}`:

- **Precision** — de lo que trajo, ¿cuánto era correcto?
- **Recall** — de lo que debía traer, ¿cuánto trajo?
- **F1**.
- Clasificá cada corrida como **under-selection** (trajo un subconjunto
  correcto pero incompleto — ningún atom equivocado, pero falta alguno),
  **over-selection** (trajo de más, algo irrelevante de los distractores),
  **completa y correcta**, o **incorrecta** (trajo algo que no corresponde en
  vez de o además de lo correcto, más allá de simple exceso).
- Anotá también si la respuesta final (el código/plan que propone) refleja
  las 3 preocupaciones (modularizar + no romper API + documentar) incluso en
  los casos donde no haya traído los 3 atoms explícitamente — para poder
  distinguir "no lo trajo pero igual lo hizo bien por conocimiento general"
  de "no lo trajo y se le pasó".

## Validación previa

Ping simple a `exo`, confirmá que responde bien. Verificá por código que los
3 atoms están en el catálogo sin ninguna relación de precedencia/autoridad
entre ellos (para no contaminar esto con lo que ya se probó en Q3C/Q4).

## Reporte

`~/yeyo/experiments/exp1bis-q6-report.md`, con:

- Tabla de las 5 corridas: qué trajo cada una, precision/recall/F1, y la
  clasificación (under/over/completa/incorrecta).
- Si predomina under-selection: es la primera señal real de que la
  composición es un problema genuino, no solo una hipótesis — el modelo
  encuentra atoms individuales bien pero no arma el conjunto completo.
- Si sale limpio (recall alto, ~3/3 en la mayoría): confirma que ni siquiera
  la composición pura rompe nada, y ahí sí valdría la pena escalar esta
  pregunta — más atoms simultáneamente aplicables (4-5), o recién ahí sumar
  mezcla de tipos (Q5) como la siguiente variable.
- Conclusión honesta y directa: ¿esta fue la ronda que finalmente encontró un
  problema real, o es la sexta hipótesis de Codex cayendo seguida?

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
