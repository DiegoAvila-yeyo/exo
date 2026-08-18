# Build prompt — Experimento 4/Q4: precedencia estructural bajo señal léxica adversaria

## Contexto

Sesión de Claude Code nueva, sin memoria de la investigación previa. Leé, en
orden: `~/yeyo/docs/vision.md` completo (especialmente "Topología semántica",
"Tres métricas de corrección", y "Metadata de autoridad"),
`~/yeyo/docs/experiments-roadmap.md`, `~/yeyo/docs/codex_consult_escala_
catalogo.md` completo (la cuarta respuesta de Codex tiene el diseño exacto de
esta ronda), y `~/yeyo/experiments/exp1bis-q3c-report.md` (el resultado que
motiva esto: sin metadata de autoridad explícita, dos atoms en conflicto real
son inestables incluso sin sesgo léxico — con `status`/`supersedes`, 3/3
correctas).

## Por qué esta ronda ya no es sobre autoridad — es sobre especificidad y excepción

Q3C ya demostró que la autoridad (`status`/`supersedes`) necesita
representación explícita. Esta ronda asume eso resuelto — **los 3 atoms de
esta ronda son todos `status: active`**, para aislar la pregunta que sigue
sin contestar: ¿el modelo resuelve bien la precedencia entre una regla
general, una especialización, y una excepción, incluso cuando la señal léxica
de la tarea empuja hacia la regla equivocada?

## Los tres atoms (contenido exacto — no improvises otros)

**G — general** (`file-size-general`):
- `status: active`, `scope: source-file`
- Redacción neutral: "Cualquier archivo de código que crezca demasiado en
  tamaño debería dividirse según su responsabilidad."
- Redacción alineada léxicamente: "Si un HTTP handler generado supera las 300
  líneas, dividilo por responsabilidad."

**S — específico** (`http-handler-transport-split`):
- `status: active`, `scope: http-handler`, `specializes: file-size-general`
- Redacción neutral: "El código que traduce entre el mundo HTTP y la lógica
  de negocio debe mantenerse separado cuando crece."
- Redacción alineada léxicamente: "Los HTTP handlers generados que superan
  300 líneas deben separar transporte de dominio."

**E — excepción** (`generated-handler-exempt`):
- `status: active`, `scope: generated-http-handler`, `exception_of:
  http-handler-transport-split`
- Redacción neutral: "El código producido automáticamente por herramientas no
  debe reorganizarse manualmente, incluso si excede los límites normales de
  tamaño."
- Redacción alineada léxicamente: "HTTP handlers generados que superan 300
  líneas están exentos de modularización."

**Tarea de prueba, fija en las 4 condiciones**: "Este HTTP handler generado
automáticamente por una herramienta ya superó las 300 líneas — ¿debería
dividirlo?" — **la respuesta correcta es E (no dividirlo, está exento)**.

## Las 4 condiciones (Q4-A a Q4-D)

Mismo catálogo base N=50 (los 3 atoms + distractores limpios de rondas
anteriores) en las 4 — lo único que cambia es qué redacción (neutral o
alineada) usa cada uno de los 3:

- **Q4-A**: E alineada, G y S neutrales. Señal léxica favorece a la respuesta
  correcta.
- **Q4-B**: los 3 neutrales. Sin ventaja léxica de ningún lado — razonamiento
  estructural puro.
- **Q4-C — la condición crítica**: G alineada, S y E neutrales. La señal
  léxica empuja hacia la regla general (equivocada), la correcta (E) no tiene
  ninguna ventaja de superficie. Esta condición es, a la vez, el control de
  "metadata en conflicto real con la redacción, no redundante" que pidió
  Codex — no hace falta una quinta condición aparte.
- **Q4-D**: S alineada, G y E neutrales. La señal léxica empuja hacia la
  específica (también equivocada) — prueba si la excepción sigue ganando
  aunque la específica tenga más superposición de superficie.

3 repeticiones por condición (12 corridas), randomizando posición y orden de
todo el catálogo en cada corrida, misma disciplina de siempre.

## Medición — las tres métricas de `vision.md`, más ranking y explicación

Por cada corrida, registrá:

1. **Corrección funcional** — ¿la respuesta final dice correctamente que no
   hace falta dividir el handler (por ser generado)?
2. **Corrección canónica** — ¿identificó específicamente `E` (o citó su
   contenido/relación), no solo llegó a la conclusión correcta por otro
   camino?
3. **Corrección de procedencia** — si el resultado funcional fue correcto,
   ¿fue porque consultó `E`, o llegó ahí sin haberlo consultado realmente
   (por ejemplo, aplicando conocimiento general sobre código generado sin
   pasar por el atom)?
4. **Ranking top-5** antes del primer `get` (misma instrumentación de rondas
   anteriores).
5. **¿Verbaliza la relación de precedencia correctamente?** — por ejemplo,
   menciona que `E` es una excepción de `S`, que a su vez especializa a `G` —
   o simplemente actúa sin explicar por qué.

## Validación previa

Ping simple a `exo`, confirmá que responde bien. Verificá por código que cada
condición tiene exactamente la redacción especificada arriba para cada atom
(alineada o neutral, según corresponda), y que las relaciones
`specializes`/`exception_of` están presentes en las 4 condiciones sin
cambios.

## Reporte

`~/yeyo/experiments/exp4-q4-report.md`, con:

- Tabla de las 12 corridas, las 3 métricas de corrección + ranking, agrupadas
  por condición.
- **Foco especial en Q4-C**: si la respuesta correcta (E) gana pese a que la
  regla general (G) tiene toda la ventaja léxica, es evidencia fuerte de que
  la metadata estructural domina sobre la superficie — el cierre real del
  tema del sesgo léxico que empezó en Q3B. Si G gana en Q4-C, es la primera
  señal de que la topología semántica sola no alcanza contra una señal léxica
  fuerte en contra.
- Comparación Q4-A vs. Q4-B vs. Q4-C vs. Q4-D — ¿hay degradación gradual a
  medida que la señal léxica se aleja de la respuesta correcta, o es más
  binario (funciona salvo en el peor caso)?
- Conclusión: ¿la representación `specializes`/`exception_of` alcanza tal
  cual, o hace falta ajustar el schema? ¿Esto ya justifica considerar cerrada
  la pregunta de topología semántica, o falta una ronda más (ej. con más de 3
  atoms relacionados, cadenas de excepciones, etc.)?

Commits en `~/yeyo`, mismo formato de rondas anteriores. `~/exo` sigue sin
commitear.
