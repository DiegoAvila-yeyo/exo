# Ideas — backlog

Ideas de producto que surgieron en conversación pero no son parte del roadmap formal de ningún
milestone ni de `CANVAS_STATUS.md`. Sin fecha de cuándo se retoman — se documentan aquí para no
perderlas, no para comprometerse a construirlas pronto.

## Mascota de `exo`

Surgió el 2026-08-21, en la misma sesión donde se le puso a cada respuesta del chat un avatar
placeholder: un círculo azul con "ojitos" (`chat-avatar` en `termserver/assets/app.css`/`app.js`),
inspirado en el estilo de Kimi. La idea del usuario: crear una mascota real para `exo`, generando la
imagen con **Veo 3** y, para la animación, generando "un assets" (sin especificar formato).

**Sin decidir todavía — abierto antes de retomarlo:**
- **Para qué es exactamente**: ¿reemplaza/evoluciona el avatar placeholder del chat, o es branding
  más amplio de `exo` (loading states, landing, marketing) independiente de dónde vive hoy ese
  círculo azul?
- **Veo 3 es un modelo de video, no de imagen estática** — puede sacar frames de un video generado,
  pero es un flujo distinto a pedirle una imagen directamente a un modelo de imagen (ej.
  Imagen/Nano Banana). Sin decidir: ¿generar video con Veo 3 y extraer el frame base de ahí, o
  generar la imagen estática con otro modelo primero y usar video solo para la animación?
- **Formato del asset de animación**: spritesheet (frames sueltos, animado con CSS — mismo patrón
  que los puntitos de "pensando" ya construidos), o video/Lottie en loop.
- **Estilo/personalidad**: tipo de criatura, paleta (¿mantener el azul ya usado en el chat?),
  formal vs. juguetón — sin definir.
- **Generación real fuera de esta sesión**: este entorno no tiene acceso directo a generación de
  imagen/video — el rol de Claude aquí sería diseñar el concepto, escribir los prompts, y luego
  integrar el asset final en la UI: la generación en sí correría en otra herramienta.
