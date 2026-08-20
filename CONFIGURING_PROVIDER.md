# Configurando el proveedor de LLM (LiteLLM / Anthropic / OpenAI)

`exo` necesita un proveedor de LLM configurado para que el chat tenga IA de verdad detrás. Esto
cubre cómo se configura, en qué orden se resuelve si hay más de uno, y los dos problemas más
comunes al configurarlo.

## Dónde vive la configuración

**`~/Library/Application Support/exo/agent.env`** — no una variable de shell, no un `.env` en este
repo. Se crea automáticamente (vacío/plantilla) la primera vez que corres `exo install`. Es un
archivo `KEY=value` simple, una variable por línea, sin comillas.

```bash
# ejemplo de contenido — LITELLM_API_KEY es un placeholder, poné la tuya real
LITELLM_API_KEY=sk-litellm-...
LITELLM_BASE_URL=http://localhost:4000
```

**Nunca commitear este archivo ni pegar su contenido real en un doc/commit** — vive fuera del repo
a propósito, en `~/Library/Application Support`, para que nunca termine en git.

## Los tres proveedores soportados

`agenthost/provider.go`'s `buildProviderFromEnv` decide cuál usar mirando qué variable de entorno
está seteada, en este orden de prioridad — **la primera que encuentra gana**, sin importar qué más
haya configurado:

1. `ANTHROPIC_API_KEY` — usa Anthropic directo. Modelo opcional vía `ANTHROPIC_MODEL` (default
   `claude-sonnet-4-6`).
2. `LITELLM_API_KEY` — usa un gateway LiteLLM. Requiere también `LITELLM_BASE_URL` (default
   `http://localhost:4000` si no se setea). Modelo opcional vía `LITELLM_MODEL` (default `primary`
   — tiene que ser un nombre que exista en el catálogo de tu gateway, ver abajo cómo consultarlo).
3. `OPENAI_API_KEY` — usa OpenAI directo. Modelo opcional vía `OPENAI_MODEL` (default
   `gpt-5-codex`).

Si ninguna está seteada, `exo` falla al arrancar el agente con
`"no provider configured: set ANTHROPIC_API_KEY, LITELLM_API_KEY, or OPENAI_API_KEY"`.

**Culpable típico #1**: si tenés `ANTHROPIC_API_KEY` seteada en tu shell o en cualquier proceso
padre, gana sobre LiteLLM sin avisar — `agent.env` nunca sobreescribe una variable que ya esté
seteada (`appconfig.LoadEnvFile` hace `os.Setenv` solo si `os.Getenv(key)` está vacío). Revisá con
`echo $ANTHROPIC_API_KEY` antes de asumir que el problema es LiteLLM.

## Culpable típico #2: hay que reiniciar después de editar

`agent.env` solo se lee **una vez, al arrancar el proceso** (`backend.Run` →
`appconfig.LoadEnvFile`, ver `backend/backend.go`). Editar el archivo con el servidor ya corriendo
no tiene efecto hasta:

```bash
exo restart
```

## Verificar que el gateway LiteLLM responde antes de culpar a `exo`

```bash
curl -s http://localhost:4000/model/info -H "Authorization: Bearer $LITELLM_API_KEY" | head -c 500
```

Si eso no devuelve JSON con una lista de modelos, el problema está en el gateway/la key, no en la
configuración de `exo`. El nombre que pongas en `LITELLM_MODEL` (o el default `primary`) tiene que
aparecer como `model_name` en esa respuesta.

## Checklist rápido si el chat no arranca

1. `cat ~/Library/Application\ Support/exo/agent.env` — ¿está la key que esperás, sin espacios ni
   comillas de más?
2. `echo $ANTHROPIC_API_KEY` en la shell donde arrancás `exo` — ¿vacío? (si no, esa gana)
3. `curl .../model/info` de arriba — ¿responde con el modelo que esperás?
4. `exo restart` — ¿lo corriste después de editar `agent.env`?
5. `exo status` — ¿`running=true`?
