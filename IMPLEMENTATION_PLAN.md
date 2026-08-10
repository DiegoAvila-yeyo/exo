# Plan de implementación — exo

Diseño de referencia: `tesla/DASHBOARD_TERMINAL_DESIGN.md` (10 rondas de crítica Claude↔Codex,
cerradas). Este documento traduce ese diseño ya cerrado a pasos de construcción concretos.

Repo base (dependencia): `github.com/yeyoos/nucleo-base` (agent, tool, terminal, dashboard
existentes), referenciado vía `replace` local en `go.mod` mientras `exo` es nuevo.

Alcance: **solo macOS** (v1), Shape A (`launchd` socket-activated backend, sin supervisor
separado).

## Modo de trabajo

Claude prompta cada milestone → Codex construye en `exo` → Codex genera reporte de lo hecho →
Claude revisa el código y el reporte antes de dar el milestone por cerrado.

## Milestones

- **M0 — Bootstrap**: estructura de directorios en `exo`, `go.mod` con dependencia a
  `nucleo-base`, verificación de que ambos módulos compilan juntos.
- **M1 — Core: actor-goroutine de sesión PTY** (el más crítico en corrección). Un paquete nuevo
  que implemente: un goroutine dueño por sesión, estado `owner` (`agent`|`human`) + `epoch`
  monotónico, mensajes serializados para `Write`/`Read`(subscribe)/`Resize`/`Takeover`, ring buffer
  acotado post-scrub con fanout no bloqueante (subscriber lento se desconecta, no bloquea al
  actor), scrubber de secretos aplicado en el único punto de persistencia/entrega. PTY real
  abstraída detrás de una interfaz chica para poder testear con fakes (sin terminal real en CI).
  Incluye la suite de tests determinísticos que Codex definió en la ronda 9 (write bajo epoch
  vigente, write rechazado tras takeover, subscriber cerrado en epoch bump, `ownership_lost` en
  takeover con read pendiente, orden resize/takeover/write).
- **M2 — Transporte del navegador**: endpoint WebSocket con auth vía `Sec-WebSocket-Protocol`
  token + double-submit cookie para rutas HTTP, `Origin` allow-list estricto (sin wildcard CORS),
  wire-up del actor de M1 al WS (mensajes de resize/input/output), estados de UI
  connecting/ready/reconnecting/disconnected/session lost.
- **M3 — Modelo de sesión múltiple**: varias sesiones PTY dentro de una instancia backend,
  listado/nombrado/switch en el dashboard, asociación a working directory.
- **M4 — Ciclo de vida del daemon (`launchd`)**: `LaunchAgent` plist con `Sockets`, backend recibe
  el socket vía check-in, lease/lock file de instancia única + shutdown handshake antes de
  apagarse por inactividad, subcomandos CLI `install`/`uninstall`/`status`/`restart` (con
  advertencia si `restart` va a matar sesiones activas).
- **M5 — Recuperación de crash / sesiones obsoletas**: process group + env markers por sesión,
  reconciliación al arrancar (detectar/matar huérfanos, marcar `stale_reaped`/`stale_orphaned`),
  sin reattach real de PTYs en v1.
- **M6 — UI de terminal (xterm.js)**: integración en `layer1-harness-shell/dashboard` (o su
  equivalente en `exo`), banda visual de "Agent has control" + takeover explícito, resize con
  debounce, replay en reconexión desde el ring buffer.
- **M7 — Hardening**: cerrar la lista de hallazgos menores acumulados durante M1-M6 (ver sección
  de abajo).
- **M8 — Integración con el agente de `nucleo-base`**: hasta M7, `exo` es solo la infraestructura
  de "terminal real controlada desde el navegador" — sin ninguna IA de por medio, es un tubo vacío
  esperando que alguien (un humano) le escriba comandos a mano. La meta original del proyecto
  ("programar desde un dashboard web como si fuera Claude CLI/Desktop") requiere que el **agente**
  de `nucleo-base` (`layer2-runtime-rails/agent`, con sus tools `bash`/`edit`/`writefile`/etc. en
  `layer2-runtime-rails/tool`) sea quien maneje esa terminal, no solo el humano. Piezas a resolver
  en esta fase (todavía sin diseño detallado, se decide cuando lleguemos aquí, probablemente con
  su propia mini-serie de rondas de crítica Claude↔Codex, igual que se hizo para M1-M7):
  - Cómo conecta el loop del agente de `nucleo-base` con el modelo de sesión PTY de `exo`
    (`ptyactor`/`sessions`) — lo más probable es re-vincular las tools de terminal del agente
    (`terminal_open/write/read/list/kill` en `nucleo-base`) para que operen sobre las sesiones
    reales de `exo` en vez de (o además de) `layer2-runtime-rails/terminal.Manager`, ya que el
    modelo de `exo` (actor-goroutine, epoch, resize, ring buffer) es estrictamente superior al de
    `nucleo-base` hoy (que no tiene resize y usa polling de archivo).
  - Un endpoint de chat en `exo` (análogo a `dashboard/chat.go` de `nucleo-base`) que reciba el
    mensaje del usuario, corra el agente, y streame su actividad — reusando el mismo modelo de
    auth/Origin/CSRF ya construido en `termserver`.
  - Decidir si `exo` importa y corre el agente directamente (ya tiene `nucleo-base` como
    dependencia vía `replace`), o si se integra de otra forma.
  - Flujo de aprobación humano-en-el-medio cuando el agente quiere ejecutar algo sensible, visible
    en el dashboard, coexistiendo con el modelo de ownership humano↔agente ya construido en M1-M2.

Orden de dependencia: M1 antes que todo (es la base testeable en aislamiento) → M2 (necesita M1) →
M4 y M5 pueden ir en paralelo entre sí una vez M1/M2 existen → M3 depende de M1-M2 → M6 depende de
M2 (protocolo) y M3 (modelo de sesión) para tener algo real que mostrar → M7 al final de M1-M6 →
M8 depende de tener M1-M6 (idealmente también M7) cerrados, porque conecta el agente sobre una
base de terminal ya estable y segura.

## Estado

- [x] M0 — Bootstrap
- [x] M1 — Core: actor-goroutine de sesión PTY (paquete `ptyactor`, revisado y verificado con
      `go test -race -count=5`, sin carreras detectadas — scrub actual es un stub de regex simple,
      pendiente de reemplazar por la lógica real de detección de secretos en un milestone futuro)
- [x] M2 — Transporte del navegador (paquete `termserver`: WS con auth por subprotocol
      + comparación en tiempo constante, Origin allow-list, double-submit cookie, broadcast de
      lease a todas las conexiones en takeover sin cortar el stream de observadores — revisado,
      `go test -race -count=5` limpio. Nota menor pendiente para el futuro: `broadcastLease` envía
      bajo el mutex `clientsMu`, sin non-blocking send — bajo riesgo con un solo usuario, revisar
      si se agrega más concurrencia real de clientes)
- [x] M3 — Modelo de sesión múltiple (paquetes `realpty` con PTY real vía `github.com/creack/pty`,
      `sessions` con cap/validación de workdir, `termserver` con hubs de lease por sesión —
      revisado, `go test -race -count=2` limpio. Corregido durante revisión: `Close()` ahora mata
      el grupo de proceso completo de la shell más sus grupos descendientes, no solo el PID de la
      shell — base correcta para M5)
- [x] M4 — Ciclo de vida del daemon (launchd): socket activation real vía
      `github.com/tprasadtp/go-launchd` (sin cgo, verificado como dependencia legítima), lease de
      instancia única con `flock`, idle-shutdown con ventana de gracia, plist con auto-validación
      XML, CLI `exo install/uninstall/status/restart/serve` con confirmación en restart si hay
      sesiones activas — revisado, `go test -race -count=4` limpio. Corregido durante revisión:
      se agregó cobertura de `backend.Run()` (la orquestación completa) que no tenía ningún test.
      Verificación manual real en Mac completada 2026-07-31: los 8 pasos (install, not-running,
      activación bajo demanda, CSRF, idle shutdown esperado 5.5 min reales, restart con/sin
      sesiones vivas — incluyendo burlar el CSRF con curl para probar con sesión real —, y
      uninstall) pasaron. `launchd` sí entrega el socket y `bootstrap`/`bootout` funcionan.
- [x] M5 — Recuperación de crash / sesiones obsoletas (paquete `sessionstore`: metadata persistida
      por sesión, marcadores de entorno, reconciliación al arrancar por `backend_instance_id` —
      revisado, `go test -race -count=1` limpio en 3 corridas completas independientes. Bug real
      encontrado y corregido durante revisión: `reconcileOne` marcaba `stale_orphaned` (falla
      ~4/5 veces) cuando el líder del grupo ya no existía pero el grupo seguía vivo — corregido
      con fallback por `pgid` vía `TerminateProcessGroup`, y `ProcessGroupAlive` reescrito para
      escanear la tabla completa de `ps` en vez de `ps -g`. Test unitario directo agregado para
      ese caso específico (`TestReconcileReapsAliveGroupWhenRecordedLeaderIsGone`))
- [x] M6 — UI de terminal (xterm.js): frontend embebido en `termserver` (xterm.js 5.5.0 +
      addon-fit 0.10.0, vendorizados localmente, sin CDN), lista/switch/create/close de sesiones,
      banda de "Agent has control" + takeover explícito, resize con debounce, reconexión con
      backoff. Verificado con pass manual real en el navegador de punta a punta (crear sesión,
      conectar, tomar control, escribir un comando real, ver el output real). Se encontraron y
      corrigieron 3 bugs reales durante la revisión manual (ninguno detectado por los tests de Go,
      exactamente el tipo de hueco que solo aparece con un navegador real):
      1. `GET /api/sessions` rechazaba peticiones same-origin sin header `Origin` (los navegadores
         no siempre lo mandan en GET) — se separó `ValidOrigin` (estricto, rutas mutantes/WS) de
         `ValidReadOrigin` (permite ausencia, rutas de solo lectura).
      2. El overlay de bloqueo (`.terminal-overlay`) nunca se ocultaba al usar `hidden` porque el
         CSS ya fijaba `display:flex` con más especificidad — además de cosmético, bloqueaba todos
         los clics sobre la terminal. Se agregó `.terminal-overlay[hidden] { display:none; }`.
      3. **El más grave**: `terminal.onData` mandaba los keystrokes como string por el WebSocket,
         lo cual el navegador siempre envía como frame de texto — pero el servidor solo trata los
         frames binarios como input real de terminal, y un frame de texto no-JSON hacía que
         cerrara la conexión. Ningún keystroke había llegado nunca a una shell real hasta este
         fix. Se corrigió codificando el input con `TextEncoder` antes de enviarlo.
- [x] M7 — Hardening: scrubber real con múltiples reglas (PEM keys, Authorization Bearer, AWS
      access keys, JWTs, asignaciones estilo env-var para password/secret/token/key), suite de
      tests tabla incluyendo negativos; `broadcastLease` no bloqueante (`select`/`default`,
      drop-on-full, mismo principio que el actor de M1); `install`/`uninstall` ya no imprimen el
      mensaje alarmante de `launchctl bootout` en instalación limpia (`RunQuiet` con
      `CombinedOutput`). `backend.lock` de 0 bytes tras uninstall se deja como está a propósito
      (riesgo de carrera con un holder vivo, decisión documentada). Revisado, `go test -race
      -count=2` limpio.
- [ ] M8 — Integración con el agente de `nucleo-base` (sin diseño detallado todavía)

## Criterio para hallazgos menores durante revisión

Cuando la revisión de un milestone encuentra algo de **bajo riesgo real** (no un hueco de
seguridad/corrección genuino), se anota aquí y se sigue avanzando — no se interrumpe el milestone
actual por eso. Se resuelve todo junto en **M7 — Hardening**, al final, antes de dar la v1 por
lista para uso real.

Excepción: si un hallazgo "menor" en realidad resulta ser un hueco de seguridad o de corrección
real (como el del takeover silencioso en M2), ese **no espera** — se corrige de inmediato en el
milestone donde se encontró.

### Lista de hardening pendiente (M7)

- **M1** — `scrub()` es un stub de regex simple (`api[_-]?key|token|secret`), no la lógica real de
  detección de secretos discutida en las rondas 2-3. Reemplazar antes de v1 real.
- **M2** — `broadcastLease` hace `client.leaseUpdates <- ...` (envío potencialmente bloqueante)
  mientras sostiene el mutex `clientsMu`, en vez de un non-blocking send con
  desconexión del cliente lento. Bajo riesgo con un solo usuario y pocas conexiones, pero
  inconsistente con el principio de "nunca bloquear por un consumidor lento" del resto del diseño.
- **M4** — `exo install` imprime `Boot-out failed: 5: Input/output error` en una instalación
  limpia (sin instancia previa cargada) — el error se ignora correctamente en el código, pero el
  mensaje de `launchctl` se filtra a stderr y se ve como un fallo real cuando no lo es. Suprimir o
  reemplazar por un mensaje más claro. También queda un `backend.lock` de 0 bytes tras
  `uninstall` — artefacto normal de `flock`, sin impacto funcional, opcional limpiarlo.
