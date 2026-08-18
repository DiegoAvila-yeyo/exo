// Package agenthost — scale_catalog.go builds the synthetic catalogs used by
// Experimento 1-bis Q1+Q2 (~/exo/build_prompt_YEYO_Q1Q2.md): does
// P(get correcto|inspect) degrade as the periferia index grows, with clean
// (unambiguous) distractors, once position bias is controlled for?
//
// This is deliberately additive, not a modification of the validated K2
// mechanism (atoms_decision_tool.go / atom_tool.go / checkpoint.go /
// yeyo.RenderIndex / the real ~/yeyo/atoms/periferia files). Those stay
// untouched — see checkpoint_scale.go for the parallel tool wiring that
// reuses the same *shape* (gate → index-in-inspect-response → free get) over
// a synthetic, size-parameterized catalog instead of the real one.
//
// The target atom is always the real protocolo-hulk (pulled from the yeyo
// package, real content, real name) — only the distractor pool around it is
// synthetic. The baseline condition reuses the real periferia distractors
// verbatim (10 atoms total today: target + 9 distractors — docs/vision.md
// says "9 atoms" because that described the catalog before Exp1F added 3
// "no-obvious" atoms; see build_prompt_YEYO_Q1Q2.md's note and the report's
// "Nota metodológica"), so it's the exact same catalog Exp1K/1K2 already
// validated, just one atom bigger; N=20/50/100/200 top that up with
// synthetic ones.
package agenthost

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/yeyoos/yeyo"
)

// scaleAtom is the minimal shape the scale experiment needs: enough to
// render an index line (Name+Description) and to answer "atom get" if the
// model ever asks for it (Body).
type scaleAtom struct {
	Name        string
	Description string
	Body        string
}

// targetAtomName is the single relevant atom in every corrida of this round
// — reused unchanged from Exp1/Exp1K/Exp1K2 (see build_prompt_YEYO_Q1Q2.md,
// "Diseño").
const targetAtomName = "protocolo-hulk"

// targetAtom fetches the real protocolo-hulk atom from the yeyo package —
// same name, description, and body the model would see in any other round.
// Panics on failure: this is throwaway experiment tooling, not production
// code, and a missing target atom means the environment is broken, not that
// this corrida should silently run without one.
func targetAtom() scaleAtom {
	a, ok := yeyo.Get(targetAtomName)
	if !ok {
		panic(fmt.Sprintf("agenthost: yeyo atom %q not found — is ~/yeyo/atoms/periferia intact?", targetAtomName))
	}
	return scaleAtom{Name: a.Name, Description: a.Description, Body: a.Body}
}

// baselineDistractors returns the real periferia atoms other than the
// target (9 today), in yeyo's stable (alphabetical) order — the exact
// catalog Exp1K/1K2 already validated, plus Exp1F's 3 "no-obvious"
// additions. Order doesn't matter here: buildScaleCatalog shuffles the
// whole thing.
func baselineDistractors() []scaleAtom {
	return periferiaDistractorsExcluding(targetAtomName)
}

// periferiaDistractorsExcluding returns every real periferia atom except
// the one named excludeName — the generalization baselineDistractors and
// Q3's buildQ3Catalog both need (Q3's target, worktrees-not-code-dir, is a
// different atom than Q1+Q2's protocolo-hulk; both rounds want "every real
// atom that isn't today's target" as part of the clean-distractor pool).
func periferiaDistractorsExcluding(excludeName string) []scaleAtom {
	var out []scaleAtom
	for _, a := range yeyo.Periferia() {
		if a.Name == excludeName {
			continue
		}
		out = append(out, scaleAtom{Name: a.Name, Description: a.Description, Body: a.Body})
	}
	return out
}

// domainSpec is one topic family for synthetic filler atoms: a description
// template, a body template, and a bank of short topic phrases to plug into
// both. Every synthetic atom is "type comportamiento" ("hacé X cuando Y"),
// matching the target's type, and deliberately unrelated to file size —
// see build_prompt_YEYO_Q1Q2.md, "Distractores limpios". Semantic
// near-neighbors of protocolo-hulk are explicitly out of scope for this
// round (that's Q3).
type domainSpec struct {
	key          string
	descTemplate string // %s = topic (readable)
	bodyTemplate string // %s = topic (readable)
	topics       []string
}

var syntheticDomains = []domainSpec{
	{
		key:          "testing",
		descTemplate: "Convención de testing: %s.",
		bodyTemplate: "En este proyecto, para %s: seguí el patrón ya usado en el resto de la suite — no introduzcas un estilo nuevo sin discutirlo antes en el PR.",
		topics: []string{
			"nombrar mocks con el sufijo Fake, no Mock",
			"ubicar fixtures compartidas en testdata/, nunca inline",
			"usar table-driven tests para casos con más de 3 variantes",
			"evitar sleep() en tests — usar polling con timeout",
			"nombrar subtests con t.Run y una descripción en gerundio",
			"resetear el estado global en TestMain, no en cada test",
			"no mockear el reloj del sistema salvo en tests de expiración",
			"marcar tests lentos con el build tag slow",
			"usar t.Parallel() solo si el test no toca estado compartido",
			"preferir assertions explícitas sobre helpers genéricos de diff",
			"un test de integración por endpoint público, no por handler interno",
			"snapshot tests solo para output determinístico",
			"no dejar t.Skip() sin un comentario que explique por qué",
			"testear el camino de error antes que el camino feliz",
			"nombrar el archivo de test igual al archivo bajo prueba más _test",
			"evitar goroutines sin sincronización explícita en tests",
			"usar httptest.NewServer solo para tests de cliente HTTP real",
			"no testear implementación privada, solo comportamiento público",
			"limpiar archivos temporales con t.Cleanup, no con defer suelto",
			"un solo assert lógico por test, aunque use varias líneas",
			"nombrar constantes de fixture con el prefijo test",
			"evitar aserciones sobre el orden de un mapa",
			"documentar el porqué de un test de regresión con el número de issue",
			"correr los tests de race con -race en CI, no solo local",
		},
	},
	{
		key:          "ci-deploy",
		descTemplate: "Convención de CI/deploy: %s.",
		bodyTemplate: "Regla de pipeline: %s. Documentado así para que cualquier cambio al workflow pase por el mismo criterio.",
		topics: []string{
			"todo deploy a producción requiere aprobación manual en el pipeline",
			"los jobs de CI corren en el runner self-hosted, no en el compartido",
			"el build falla si el linter reporta un solo warning nuevo",
			"las migraciones corren en un job separado, antes del deploy de código",
			"el rollback automático se dispara si el health check falla 3 veces",
			"los secretos de CI viven en el vault del pipeline, nunca en el yaml",
			"cada PR dispara un preview deploy con su propio subdominio",
			"el tag de release sigue semver, generado por el pipeline, no a mano",
			"los tests de smoke corren después del deploy, no antes",
			"el pipeline de staging usa una base de datos separada de producción",
			"un deploy fallido bloquea el siguiente hasta que se investigue",
			"el build de Docker usa multi-stage, nunca copia el árbol completo",
			"las variables de entorno de cada ambiente están versionadas aparte",
			"el pipeline corre en paralelo por servicio, no en un solo job monolítico",
			"cada release notea el changelog generado desde los commits",
			"el deploy a producción solo corre desde la rama main protegida",
			"los artifacts de build se cachean por hash de dependencias",
			"un deploy manual requiere motivo documentado en el ticket",
			"el pipeline corta si cobertura de tests baja del umbral fijado",
			"los jobs de seguridad corren antes que los de build, no después",
			"cada ambiente tiene su propio namespace de Kubernetes",
			"el canary deploy se mantiene 15 minutos antes del rollout completo",
			"el pipeline notifica a Slack solo en fallos, no en cada éxito",
			"las credenciales de deploy rotan cada 90 días automáticamente",
		},
	},
	{
		key:          "logging",
		descTemplate: "Convención de logging: %s.",
		bodyTemplate: "Sobre logging: %s. Mantiene los logs consistentes entre servicios del mismo proyecto.",
		topics: []string{
			"todo log estructurado usa JSON, nunca texto libre en producción",
			"el nivel de log por defecto es info, debug solo detrás de una flag",
			"nunca loguear el body completo de una request con datos de usuario",
			"cada log de error incluye el request ID para poder correlacionar",
			"los logs de acceso van a un sink separado de los logs de aplicación",
			"el campo timestamp siempre en UTC, nunca en hora local",
			"los logs de warning no deben repetirse más de una vez por minuto",
			"cada servicio prefija sus logs con su propio nombre corto",
			"nunca loguear stack traces completos en nivel info",
			"los logs de auditoría son inmutables, van a un almacén aparte",
			"el campo severity sigue la escala estándar (debug/info/warn/error/fatal)",
			"loguear siempre la duración de una operación que tarde más de 1s",
			"no loguear contraseñas ni tokens, ni siquiera parcialmente",
			"cada log de negocio incluye el ID de la entidad afectada",
			"los logs de background jobs incluyen el nombre del job y su intento",
			"nunca usar print() o fmt.Println para logging en código de producción",
			"el logger se inyecta por contexto, no se instancia global",
			"los logs de fatal siempre terminan el proceso después de flushear",
			"cada excepción no manejada se loguea antes de propagarse",
			"el formato de fecha en logs sigue RFC3339, no un formato custom",
			"los logs de retry incluyen el número de intento actual",
			"nunca loguear el contenido completo de un archivo subido",
			"los logs de startup confirman cada dependencia externa conectada",
			"el nivel de log se puede subir en caliente sin reiniciar el proceso",
		},
	},
	{
		key:          "dependency-management",
		descTemplate: "Convención de dependencias: %s.",
		bodyTemplate: "Manejo de dependencias: %s. Esto evita sorpresas al actualizar el árbol de paquetes.",
		topics: []string{
			"toda dependencia nueva requiere justificación en la descripción del PR",
			"las versiones se fijan exactas, nunca con rango abierto",
			"el lockfile se commitea siempre, nunca se ignora",
			"actualizar dependencias mayores va en un PR separado del feature",
			"antes de agregar una librería, chequear si ya hay una equivalente en uso",
			"las dependencias de desarrollo no se empaquetan en el build final",
			"cualquier dependencia con licencia GPL requiere aprobación legal",
			"el escaneo de vulnerabilidades corre en cada PR que toque el lockfile",
			"no vendorizar dependencias salvo que el build offline lo requiera",
			"las dependencias transitivas duplicadas se resuelven, no se ignoran",
			"actualizar una dependencia de seguridad tiene prioridad sobre features",
			"cada dependencia nueva se registra en el archivo de terceros del repo",
			"no usar forks propios de una librería sin documentar por qué",
			"las dependencias internas del monorepo se versionan igual que las externas",
			"revisar el changelog de una dependencia antes de actualizarla a mayor",
			"las dependencias no usadas se eliminan en el mismo PR que las detecta",
			"preferir la librería estándar antes que una dependencia externa chica",
			"el CI falla si el lockfile no coincide con el manifest de dependencias",
			"las dependencias de build (compiladores, herramientas) se pinnean por versión",
			"cualquier dependencia nueva pasa por el mismo review que código propio",
			"las dependencias deprecadas se reemplazan antes de la fecha de EOL",
			"no mezclar dos librerías que resuelvan el mismo problema en el mismo módulo",
			"el árbol de dependencias se audita trimestralmente, no solo ante alertas",
			"las dependencias opcionales van detrás de un build tag, no siempre activas",
		},
	},
	{
		key:          "api-design",
		descTemplate: "Convención de diseño de API: %s.",
		bodyTemplate: "Diseño de API: %s. Mantiene la superficie pública consistente entre endpoints.",
		topics: []string{
			"los endpoints de listado siempre soportan paginación por cursor",
			"los nombres de recurso en la URL van en plural, nunca en singular",
			"los campos de fecha en el body siempre en ISO 8601",
			"un cambio breaking de API requiere una nueva versión de ruta",
			"los errores de validación devuelven 422, no 400 genérico",
			"cada respuesta de error sigue el mismo esquema de código+mensaje",
			"los IDs expuestos en la API nunca son el ID autoincremental interno",
			"los endpoints de escritura son idempotentes cuando reciben un idempotency key",
			"los filtros de listado van en query params, nunca en el body",
			"la documentación OpenAPI se genera del código, nunca se escribe a mano aparte",
			"los campos opcionales en la respuesta nunca cambian de tipo entre versiones",
			"los webhooks salientes reintenta con backoff exponencial, máximo 5 veces",
			"un recurso eliminado devuelve 404, no 200 con un flag deleted",
			"los rate limits se comunican en headers estándar, no en el body",
			"los endpoints internos (no públicos) llevan el prefijo /internal",
			"las respuestas nunca incluyen campos sin documentar en el esquema",
			"cada versión de API deprecada se anuncia con 6 meses de anticipación",
			"los enums expuestos en la API son strings, nunca números mágicos",
			"un POST que crea un recurso devuelve el recurso completo, no solo el ID",
			"los headers custom del proyecto llevan el prefijo X-Proyecto-",
			"la autenticación va siempre en el header, nunca en query param",
			"los timeouts de la API se documentan por endpoint, no de forma global",
			"cada endpoint público tiene al menos un ejemplo de request/response",
			"los códigos de error propios del dominio no reusan códigos HTTP genéricos",
		},
	},
	{
		key:          "db-migrations",
		descTemplate: "Convención de migraciones de base de datos: %s.",
		bodyTemplate: "Migraciones: %s. Reduce el riesgo de romper el ambiente de producción al aplicar cambios de esquema.",
		topics: []string{
			"toda migración es reversible, con su down explícito",
			"nunca borrar una columna en la misma migración que deja de usarla",
			"las migraciones se numeran por timestamp, nunca por número secuencial a mano",
			"un cambio de tipo de columna requiere una migración de dos pasos",
			"las migraciones grandes corren fuera de horario pico, no en cualquier momento",
			"nunca modificar una migración ya aplicada en producción, se agrega una nueva",
			"los índices se crean de forma concurrente en tablas grandes",
			"cada migración se prueba contra una copia de producción antes del deploy",
			"las migraciones de datos van separadas de las migraciones de esquema",
			"un rename de columna se hace agregando la nueva y migrando datos, no en el lugar",
			"las foreign keys nuevas se agregan con NOT VALID primero, luego se validan",
			"toda migración documenta el tiempo estimado de bloqueo en la tabla",
			"nunca ejecutar una migración manual fuera del pipeline de CI/CD",
			"las migraciones de seed de datos solo corren en ambientes de desarrollo",
			"un cambio de constraint se valida contra los datos existentes antes de aplicar",
			"las migraciones fallidas dejan el estado documentado, nunca se revierten a mano",
			"cada tabla nueva incluye created_at y updated_at desde el inicio",
			"los defaults de columna se agregan antes de hacerla NOT NULL",
			"las migraciones destructivas requieren un segundo aprobador en el PR",
			"nunca usar SELECT * en una migración de datos, siempre columnas explícitas",
			"el orden de las migraciones nunca se reordena una vez mergeado a main",
			"las migraciones de índice único primero limpian duplicados existentes",
			"cada ambiente corre las migraciones pendientes automáticamente al deploy",
			"las migraciones de gran volumen se hacen en lotes, no en una sola transacción",
		},
	},
	{
		key:          "documentation",
		descTemplate: "Convención de documentación: %s.",
		bodyTemplate: "Documentación: %s. Mantiene el README y los docs internos alineados con el código real.",
		topics: []string{
			"todo endpoint público se documenta antes de mergear, no después",
			"los README de cada módulo explican el propósito, no repiten el código",
			"los comentarios explican el por qué, nunca repiten lo que ya dice el código",
			"la documentación de arquitectura se actualiza en el mismo PR que el cambio",
			"cada decisión de diseño no obvia se registra en un ADR corto",
			"los ejemplos de uso en la documentación se prueban como parte del CI",
			"el changelog se actualiza por PR, no se reconstruye al final del sprint",
			"la documentación de onboarding se revisa cada vez que cambia el setup local",
			"los diagramas de arquitectura se versionan junto al código, no aparte",
			"cada función pública exportada tiene un comentario de godoc/docstring",
			"los docs de troubleshooting incluyen el mensaje de error exacto a buscar",
			"la documentación de configuración lista cada variable de entorno usada",
			"los términos de dominio ambiguos se explican en un glosario compartido",
			"cada script de mantenimiento documenta cuándo y por qué correrlo",
			"los docs de API interna no se publican fuera del repo",
			"la guía de contribución se actualiza si cambia el flujo de PR",
			"cada breaking change se documenta en una sección propia del changelog",
			"los README de servicios listan sus dependencias externas explícitamente",
			"la documentación de deploy incluye el rollback paso a paso",
			"los ejemplos de código en docs usan datos ficticios, nunca reales",
			"cada nuevo flag de feature se documenta con su condición de remoción",
			"los docs de incident post-mortem se archivan en una carpeta dedicada",
			"la documentación pública nunca menciona nombres internos de servicios",
			"cada guía técnica indica la última fecha de revisión al pie",
		},
	},
	{
		key:          "security",
		descTemplate: "Convención de seguridad (no relacionada a tamaño de archivo): %s.",
		bodyTemplate: "Seguridad: %s. Norma independiente de cualquier convención de tamaño o estructura de archivo.",
		topics: []string{
			"toda entrada de usuario se valida en el borde, antes de llegar a la lógica de negocio",
			"los tokens de sesión expiran a las 24 horas sin actividad",
			"nunca loguear ni cachear el número completo de una tarjeta de pago",
			"las contraseñas se hashean con un algoritmo con costo ajustable, nunca MD5/SHA1 solo",
			"cada endpoint sensible requiere autenticación de dos factores",
			"los permisos se verifican en el servidor, nunca solo en el cliente",
			"las claves de cifrado rotan cada 6 meses como mínimo",
			"todo dato personal se cifra en reposo, no solo en tránsito",
			"los CORS permitidos se listan explícitamente, nunca con wildcard en producción",
			"cada acceso a un recurso protegido queda registrado en el log de auditoría",
			"las respuestas de error de autenticación no revelan si el usuario existe",
			"los uploads de archivos se escanean antes de quedar accesibles",
			"nunca confiar en el user-agent o el IP como único factor de autorización",
			"los secretos rotados invalidan las sesiones activas que los usaban",
			"cada dependencia con una CVE crítica se parchea dentro de 48 horas",
			"los links de recuperación de cuenta expiran a los 15 minutos",
			"el rate limiting por IP se aplica antes de cualquier lógica de negocio",
			"nunca exponer trazas de stack completas al cliente en producción",
			"los datos sensibles se enmascaran en cualquier entorno que no sea producción",
			"cada cambio de permisos de un usuario queda auditado con quién lo hizo",
			"las claves de API por cliente son revocables individualmente",
			"nunca reusar un mismo secreto entre ambientes de staging y producción",
			"los formularios sensibles incluyen protección CSRF explícita",
			"cada sesión activa se puede cerrar remotamente desde la cuenta del usuario",
		},
	},
}

// syntheticDistractorPool builds every synthetic filler atom once, in a
// fixed, deterministic order (domain order, then topic order). Callers take
// a prefix of this slice for a given catalog size — buildScaleCatalog does
// the shuffling, not this function. 8 domains × 24 topics = 192 atoms,
// comfortably over the 190 the largest condition (N=200) needs (200 - 1
// target - 9 real baseline distractors).
func syntheticDistractorPool() []scaleAtom {
	var out []scaleAtom
	for _, d := range syntheticDomains {
		for i, topic := range d.topics {
			name := fmt.Sprintf("%s-conv-%02d", d.key, i+1)
			out = append(out, scaleAtom{
				Name:        name,
				Description: fmt.Sprintf(d.descTemplate, capitalizeFirst(topic)),
				Body:        fmt.Sprintf(d.bodyTemplate, topic),
			})
		}
	}
	return out
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// scaleCatalog is one corrida's fully-assembled, pre-shuffled catalog: the
// rendered index text the model sees, a lookup map for "atom get", and the
// target's 1-based position within the rendered index (for the position-bias
// check — see build_prompt_YEYO_Q1Q2.md, "Medición").
type scaleCatalog struct {
	N              int
	Seed           int64
	IndexText      string
	TargetPosition int            // 1-based; position of trackedNames[0] passed to renderCatalog
	Positions      map[string]int // 1-based position of every tracked name — Q3C tracks two competing atoms at once, not just one target
	byName         map[string]scaleAtom
}

func (c scaleCatalog) get(name string) (scaleAtom, bool) {
	a, ok := c.byName[name]
	return a, ok
}

// buildScaleCatalog assembles N atoms (target + N-1 distractors: the real 8
// periferia distractors first, topped up with synthetic ones for N>9) and
// renders the index in an order shuffled by seed — both the target's
// position and the rest of the distractor order are randomized together, as
// build_prompt_YEYO_Q1Q2.md's "Precaución" requires. Panics if N is too
// small to fit the target plus the real 8 baseline distractors, or too large
// for the synthetic pool — both are programmer errors (bad experiment
// config), not runtime conditions to handle gracefully.
func buildScaleCatalog(n int, seed int64) scaleCatalog {
	if n < 1 {
		panic(fmt.Sprintf("agenthost: buildScaleCatalog: n=%d must be >= 1", n))
	}
	target := targetAtom()
	baseline := baselineDistractors()
	distractorsNeeded := n - 1

	var distractors []scaleAtom
	switch {
	case distractorsNeeded <= len(baseline):
		distractors = append(distractors, baseline[:distractorsNeeded]...)
	default:
		distractors = append(distractors, baseline...)
		extra := distractorsNeeded - len(baseline)
		pool := syntheticDistractorPool()
		if extra > len(pool) {
			panic(fmt.Sprintf("agenthost: buildScaleCatalog: n=%d needs %d synthetic distractors, pool only has %d", n, extra, len(pool)))
		}
		distractors = append(distractors, pool[:extra]...)
	}

	all := make([]scaleAtom, 0, n)
	all = append(all, target)
	all = append(all, distractors...)

	return renderCatalog(n, seed, []string{targetAtomName}, all, nil)
}

// renderCatalog shuffles `all` (by seed) and renders the index text, shared
// by buildScaleCatalog (Q1+Q2), buildQ3Catalog (Q3), buildQ3BCatalog (Q3B),
// and buildQ3CCatalog (Q3C) — all need exactly the same "shuffle position
// and order together, render name+description lines, track where specific
// atoms landed" logic, just over a different atom list.
//
// trackedNames lists every atom whose position this call needs recorded —
// Positions carries all of them; TargetPosition mirrors trackedNames[0] for
// callers (Q1+Q2/Q3/Q3B) that only ever track one atom. Q3C tracks two (the
// two atoms genuinely competing for the model's pick) at once.
//
// annotations, if non-nil, appends a suffix to a tracked atom's index line
// by name (Q3C's "[deprecated]"/"[active, supersedes: ...]" metadata
// conditions) — nil for every other round, which renders plain
// "name: description" lines exactly as before.
func renderCatalog(n int, seed int64, trackedNames []string, all []scaleAtom, annotations map[string]string) scaleCatalog {
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	byName := make(map[string]scaleAtom, len(all))
	positions := make(map[string]int, len(trackedNames))
	tracked := make(map[string]bool, len(trackedNames))
	for _, tn := range trackedNames {
		tracked[tn] = true
	}
	var b strings.Builder
	b.WriteString("Atom catalog (periferia) — call atom get <name> for the full instructions of any entry:\n\n")
	for i, a := range all {
		byName[a.Name] = a
		line := "- " + a.Name + ": " + a.Description
		if annotations != nil {
			if suffix, ok := annotations[a.Name]; ok && suffix != "" {
				line += " " + suffix
			}
		}
		b.WriteString(line + "\n")
		if tracked[a.Name] {
			positions[a.Name] = i + 1
		}
	}

	targetPos := -1
	if len(trackedNames) > 0 {
		if p, ok := positions[trackedNames[0]]; ok {
			targetPos = p
		}
	}

	return scaleCatalog{
		N:              n,
		Seed:           seed,
		IndexText:      b.String(),
		TargetPosition: targetPos,
		Positions:      positions,
		byName:         byName,
	}
}
