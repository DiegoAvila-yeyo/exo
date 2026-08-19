package agenthost

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/canvasstore"
	"github.com/DiegoAvila-yeyo/exo/m8adapter"
	"github.com/DiegoAvila-yeyo/exo/planningstore"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/DiegoAvila-yeyo/exo/yeyotelemetry"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/agent"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/instructions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/mcp"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/runtime"
	nbmemorytool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/layer4-knowledge-memory/localstore"
	"github.com/yeyoos/nucleo-base/layer4-knowledge-memory/memoryservice"
	"github.com/yeyoos/nucleo-base/shared/api"
	"github.com/yeyoos/yeyo"
)

const mcpStartupTimeout = 30 * time.Second

var openMemoryStore = localstore.OpenSQLite

type Host struct {
	adapter          *m8adapter.Adapter
	registry         *nbtool.Registry
	agent            *agent.Agent
	coordinator      *runtime.Coordinator
	mcpClients       []*mcp.Client
	memoryStore      *localstore.SQLiteStore
	originalRootPath string // what SetRootPath("") resets back to
	currentRootPath  string // last path SetRootPath actually moved into

	planningStore *planningstore.Store // nil when Planning isn't configured (see New)
	planningCtx   *planningContext     // shared with the planning_* tools; see planning_context.go
	navigateCell  *navigateCell        // shared with the planning navigation tools; see planning_navigate.go

	canvasStore *canvasstore.Store // nil when Canvas isn't configured (see New), same fixed-registry/gated-at-Execute pattern as planningStore
	canvasCell  *canvasCell        // shared with the canvas_* tools; see canvas_context.go

	// gateEnabled and gateOnlySnapshot exist only when EXO_YEYO_GATE is set
	// (see yeyoGateEnabled). gateOnlySnapshot is an immutable copy holding
	// only atomsDecisionTool — Run resets `registry` (== agent.Tools, same
	// pointer) back to this snapshot's contents at the start of every turn,
	// so the gate fires once per turn, not once per session. It must stay a
	// separate object from `registry`: atomsDecisionTool.Execute mutates
	// `registry` in place via ReplaceFrom (that's how Phase 2 opens), so
	// `registry` itself can't double as the reset source once a turn has
	// run.
	gateEnabled      bool
	gateOnlySnapshot *nbtool.Registry

	// telemetry and gateTurn exist only when gateEnabled and the telemetry
	// DB opened successfully (openTelemetryStoreBestEffort) — nil either
	// way means every recordGate* call in gate_telemetry.go is a no-op, per
	// the same fail-isolated pattern openMemoryStoreBestEffort already
	// uses for the memory store. gateTurn is reset at the start of every
	// turn (beginGateTurn) so telemetry events always carry the current
	// turn's session_id/turn_id, never a stale one from the turn before.
	telemetry *yeyotelemetry.Store
	gateTurn  *gateTurnState

	stdoutMu sync.Mutex
}

func ValidateEnv() error {
	rootPath, err := rootPathFromEnv()
	if err != nil {
		return err
	}
	systemPrompt := buildSystemPrompt(rootPath, "")
	_, err = buildProviderFromEnv(systemPrompt)
	return err
}

// New builds a Host. planningStore may be nil (Planning not configured for
// this deployment, e.g. no PlanningStoreDir) — the three planning_* tools
// are still registered in that case, but every call fails clearly with
// "planning is not configured" rather than the tools being conditionally
// absent (see buildToolRegistry and build_prompt_PLANNING_ROUND2.md's "Tool
// availability" decision: fixed registry, gated at Execute).
func New(ctx context.Context, manager *sessions.Manager, planningStore *planningstore.Store, canvasStore *canvasstore.Store) (*Host, error) {
	if manager == nil {
		return nil, fmt.Errorf("agenthost: sessions manager is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rootPath, err := rootPathFromEnv()
	if err != nil {
		return nil, err
	}
	// Nucleo's write_file and bash tools resolve relative paths against the
	// process working directory, not runtime.Coordinator.rootPath. Changing the
	// cwd here is what makes EXO_AGENT_ROOT_PATH actually scope agent file I/O.
	if err := os.Chdir(rootPath); err != nil {
		return nil, fmt.Errorf("agenthost: change working directory to %q: %w", rootPath, err)
	}
	systemPrompt := buildSystemPrompt(rootPath, "")
	provider, err := buildProviderFromEnv(systemPrompt)
	if err != nil {
		return nil, err
	}

	adapter := m8adapter.New(manager)
	planningCtx := &planningContext{}
	navigateCell := newNavigateCell()
	canvasCell := newCanvasCell()
	gateEnabled := yeyoGateEnabled()
	registry := buildToolRegistry(adapter, planningStore, planningCtx, navigateCell, canvasStore, canvasCell, !gateEnabled)
	var mcpClients []*mcp.Client
	// exp1g Experimento L: MCP servers (GitHub, Jira) register their own
	// tools directly onto registry, bypassing buildToolRegistry's judge-only
	// gate — skip that step entirely in this mode so the model truly has
	// nothing beyond the two bootstrap tools.
	if strings.TrimSpace(os.Getenv("EXO_YEYO_JUDGE_ONLY")) == "" {
		mcpClients, err = registerMCPClients(ctx, registry)
		if err != nil {
			return nil, err
		}
	}

	// h is constructed early (before registry/agent/coordinator are ready)
	// so wrapRegistryWithGate can close over its recordGate* methods —
	// those closures aren't actually called until a real turn runs, long
	// after every field below is filled in, so a partially-built *Host at
	// this point is safe to hand out. See gate_telemetry.go's doc comment.
	h := &Host{
		adapter:          adapter,
		originalRootPath: rootPath,
		currentRootPath:  rootPath,
		planningStore:    planningStore,
		planningCtx:      planningCtx,
		navigateCell:     navigateCell,
		canvasStore:      canvasStore,
		canvasCell:       canvasCell,
		gateEnabled:      gateEnabled,
	}

	var bootstrapReg *nbtool.Registry
	if gateEnabled {
		// Captured from the pre-gate registry, before it's reassigned to
		// the gate-only live registry below — see buildBootstrapRegistry's
		// doc-comment for why Coordinator needs this separately.
		bootstrapReg = buildBootstrapRegistry(registry)
		h.telemetry = openTelemetryStoreBestEffort()
		registry, h.gateOnlySnapshot = wrapRegistryWithGate(h, registry)
	}

	memoryStore := openMemoryStoreBestEffort()
	rootAgent := agent.New(provider, systemPrompt, registry)
	coordinator := runtime.NewCoordinator(rootAgent, rootPath)
	coordinator.Budget = runtime.BudgetFromEnv()
	coordinator.Layers = instructions.FromPrompt(systemPrompt)
	if gateEnabled {
		coordinator.BootstrapTools = bootstrapReg
	}

	h.registry = registry
	h.agent = rootAgent
	h.coordinator = coordinator
	h.mcpClients = mcpClients
	h.memoryStore = memoryStore
	return h, nil
}

// wrapRegistryWithGate takes the registry buildToolRegistry produced
// (already built with includeAtomTool=false when the gate is on — see
// New) and turns it into the K2 three-registry shape the validated
// mechanism needs: withoutAtom (full = base, no atom tool — the "skip"
// destination), withAtom (base + atomTool — the "inspect" destination),
// and a live registry holding only atomsDecisionTool, which is what New's
// caller should use as `registry`/agent.Tools from here on.
//
// Returns the live registry and an immutable snapshot of its
// gate-only starting contents, for Run to reset back to every turn — see
// Host.gateOnlySnapshot's doc-comment for why a separate snapshot is
// needed instead of reusing live itself.
//
// h is used only to wire the telemetry hooks (onDecision, and the
// atomGetTelemetryTool wrapper in place of a bare atomTool{}) — it does
// not need to be a fully-built Host yet, see New's comment on why handing
// out h this early is safe.
func wrapRegistryWithGate(h *Host, base *nbtool.Registry) (live, snapshot *nbtool.Registry) {
	withoutAtomReg := nbtool.NewRegistry()
	withoutAtomReg.ReplaceFrom(base)

	withAtomReg := nbtool.NewRegistry()
	withAtomReg.ReplaceFrom(base)
	withAtomReg.Register(atomGetTelemetryTool{inner: atomTool{}, host: h})

	live = nbtool.NewRegistry()
	live.Register(atomsDecisionTool{
		registry:       live,
		withAtom:       withAtomReg,
		withoutAtom:    withoutAtomReg,
		indexOnInspect: true,
		onDecision:     h.recordGateDecision,
	})

	snapshot = nbtool.NewRegistry()
	snapshot.ReplaceFrom(live)
	return live, snapshot
}

// bootstrapToolNames lists the fixed-name tools runtime.Coordinator.Run's
// own pre-turn bookkeeping calls directly, before the model ever sees a
// turn: "bash" (ensurePreflight), "tasks_file"/"progress" (ensureTask).
// "delegate_research" (autoInvestigate) isn't listed here because its name
// is dynamic — see buildBootstrapRegistry.
var bootstrapToolNames = []string{"bash", "tasks_file", "progress"}

// buildBootstrapRegistry pulls bootstrapToolNames plus every
// dynamically-named "delegate_<subagent>" tool (autoInvestigate's
// delegate_research among them, if a "research" subagent is registered) out
// of base, for runtime.Coordinator.BootstrapTools. Must be called on the
// pre-gate registry (before wrapRegistryWithGate reduces it to just
// atoms_decision) — see the doc-comment on runtime.Coordinator.
// BootstrapTools (nucleo-base) for why this exists: without it, gating
// Agent.Tools down to the gate tool alone makes Coordinator's own
// bookkeeping fail before the model gets a turn, since execTool has no
// other registry to call through.
func buildBootstrapRegistry(base *nbtool.Registry) *nbtool.Registry {
	out := nbtool.NewRegistry()
	for _, name := range bootstrapToolNames {
		if t, ok := base.Get(name); ok {
			out.Register(t)
		}
	}
	for _, def := range base.Definitions() {
		if strings.HasPrefix(def.Name, "delegate_") {
			if t, ok := base.Get(def.Name); ok {
				out.Register(t)
			}
		}
	}
	return out
}

func (h *Host) Registry() *nbtool.Registry {
	return h.registry
}

func (h *Host) Adapter() *m8adapter.Adapter {
	return h.adapter
}

// Messages returns the agent's current in-memory conversation history.
// Callers that support multiple chat sessions save this after each turn and
// restore it via SetMessages before the next turn, so the shared agent/tool
// registry can be reused across sessions without cross-talk.
func (h *Host) Messages() []api.Message {
	if h.agent == nil {
		return nil
	}
	return h.agent.Messages()
}

// SetMessages swaps in the conversation history for the session about to run.
// Only safe to call between turns — callers must hold the same lock that
// serializes calls to Run (e.g. Server.agentMu).
func (h *Host) SetMessages(messages []api.Message) {
	if h.agent == nil {
		return
	}
	h.agent.SetMessages(messages)
}

// SetRootPath moves the agent into path for the turn about to run: it chdirs
// the process (the same mechanism New uses at startup — bash/file tools
// resolve relative paths against process cwd, not Coordinator.RootPath) and
// rebuilds the system prompt so the project's own rules (AGENTS.md,
// .harness/instructions/*.md) get read fresh and injected.
//
// An empty path means "no project selected" — it resets back to the root
// exo started with (EXO_AGENT_ROOT_PATH / $HOME), rather than leaving the
// agent sitting in whatever project the previous turn happened to be in.
// Repeated calls with the same effective path are cheap no-ops (skips the
// chdir + instructions re-read) so turns that don't change project pay
// nothing extra.
//
// Only safe to call between turns — same locking requirement as SetMessages,
// since os.Chdir affects the whole process and every goroutine's relative
// paths, not just the caller's.
func (h *Host) SetRootPath(path string) error {
	target := path
	if target == "" {
		target = h.originalRootPath
	}
	clean := filepath.Clean(target)
	if clean == h.currentRootPath {
		return nil
	}
	if err := os.Chdir(clean); err != nil {
		return fmt.Errorf("agenthost: change working directory to %q: %w", clean, err)
	}
	systemPrompt := buildSystemPrompt(clean, "")
	if h.agent != nil {
		h.agent.System = systemPrompt
	}
	if h.coordinator != nil {
		h.coordinator.RootPath = clean
		h.coordinator.Layers = instructions.FromPrompt(systemPrompt)
	}
	h.currentRootPath = clean
	return nil
}

// SetPlanningContext scopes the turn about to run to a Planning/Board — or
// clears it when both are empty — for the planning_* tools
// (planning_tools.go) to read during Execute. Same per-turn contract as
// SetRootPath: only safe to call between turns, under the same lock that
// serializes calls to Run (verified, not assumed — see planningContext's
// doc comment in planning_context.go). Callers are expected to have already
// validated planningID/boardID as an atomic pair (both set or both empty);
// this method does not re-validate that, it just stores what it's given.
func (h *Host) SetPlanningContext(planningID, boardID string) {
	h.planningCtx.set(planningID, boardID)
}

func (h *Host) Run(ctx context.Context, input string, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}

	h.stdoutMu.Lock()
	defer h.stdoutMu.Unlock()

	restore, err := redirectStdout(output)
	if err != nil {
		return err
	}
	defer restore()

	// Telemetry bookkeeping for this turn, ahead of the gate reset below —
	// see gate_telemetry.go. turnID/sessionID/messageIndexBefore are all
	// captured before coordinator.Run so the eventual turn_result event
	// reflects exactly what this turn started with, not anything a tool
	// mutated mid-turn (h.Messages() in particular changes as soon as the
	// agent appends its own messages during the run).
	turnID := h.beginGateTurn(ctx)
	sessionID := sessionIDFromContext(ctx)
	messageIndexBefore := len(h.Messages())

	// Reset to gate-only before every turn, not just the first — the gate
	// (EXO_YEYO_GATE) is meant to fire per turn, since what's relevant to
	// yeyo's periferia can change from one message to the next in a long
	// session. Without this, atomsDecisionTool.Execute's in-place
	// ReplaceFrom (how "inspect"/"skip" opens Phase 2) would open the
	// normal tools once on turn 1 and leave them open for the rest of the
	// session — the gate would only ever ask once, not per turn.
	if h.gateEnabled && h.gateOnlySnapshot != nil && h.registry != nil {
		h.registry.ReplaceFrom(h.gateOnlySnapshot)
	}

	if h.coordinator == nil {
		return fmt.Errorf("agenthost: coordinator is not configured")
	}
	_, err = h.coordinator.Run(ctx, input)

	// Technical outcome only — never a judgment about whether the gate's
	// decision was right, see recordGateTurnResult's doc comment.
	result := "completed"
	switch {
	case ctx.Err() != nil:
		result = "cancelled"
	case err != nil:
		result = "error"
	}
	h.recordGateTurnResult(sessionID, turnID, result, messageIndexBefore)

	return err
}

func (h *Host) Close() error {
	var firstErr error
	for _, client := range h.mcpClients {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	nbmemorytool.SetLocalMemoryService(nil)
	if h.memoryStore != nil {
		if err := h.memoryStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		h.memoryStore = nil
	}
	if h.telemetry != nil {
		if err := h.telemetry.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		h.telemetry = nil
	}
	return firstErr
}

// yeyoGateEnabled reports whether this session should use the validated
// atoms_decision gate (K2 shape: mandatory inspect|skip, index delivered
// inside the "inspect" response) instead of the older always-inject
// behavior (yeyo.RenderIndex() forced into the system prompt every
// session, atomTool always available, no decision point — the Exp1D
// approach that 11 rounds of navigation experiments showed the model
// mostly ignores). Off by default: this changes real session behavior, so
// it's opt-in until verified end-to-end on real usage. See
// docs/experiments-roadmap.md, "Fase operacional", for the design this
// implements.
func yeyoGateEnabled() bool {
	return strings.TrimSpace(os.Getenv("EXO_YEYO_GATE")) != ""
}

func buildToolRegistry(adapter *m8adapter.Adapter, planningStore *planningstore.Store, planningCtx *planningContext, navigateCell *navigateCell, canvasStore *canvasstore.Store, canvasCell *canvasCell, includeAtomTool bool) *nbtool.Registry {
	// exp1g Experimento L: judgment-only mode. When set, the model gets no
	// tool that lets it act on the task or consult the atom catalog — not
	// even atom itself. "tasks_file"/"progress" are kept registered only
	// because runtime.Coordinator.ensureTask calls them internally before
	// the model's first turn even starts (bootstrap plumbing, not something
	// the model chooses to use to solve the task) — dropping them makes
	// every turn error out before the model says anything. Never set
	// outside this experiment's harness.
	if strings.TrimSpace(os.Getenv("EXO_YEYO_JUDGE_ONLY")) != "" {
		return nbtool.Default.Subset("tasks_file", "progress")
	}

	registry := nbtool.NewRegistry()
	registry.ReplaceFrom(nbtool.Default)
	registry.Register(&nbtool.TerminalOpenTool{Manager: adapter})
	registry.Register(&nbtool.TerminalReadTool{Manager: adapter})
	registry.Register(&nbtool.TerminalWriteTool{Manager: adapter})
	registry.Register(&nbtool.TerminalKillTool{Manager: adapter})
	registry.Register(&nbtool.TerminalListTool{Manager: adapter})
	if includeAtomTool {
		registry.Register(atomTool{})
	}
	registry.Register(secretValueTool{})

	// Round 2: always registered (fixed registry, per
	// build_prompt_PLANNING_ROUND2.md's "Tool availability" decision), gated
	// at Execute time via planningToolBase — a model in the Home chat can
	// see these in its tool list, but calling one fails clearly rather than
	// acting on the wrong (or no) Planning Board.
	planningBase := planningToolBase{store: planningStore, ctx: planningCtx}
	registry.Register(planningListBoardTool{planningBase})
	registry.Register(planningCreateBoardTool{planningBase})
	registry.Register(planningCreateNoteTool{planningBase})

	// Round 3: navigation tools. Unlike the three above, these need no
	// existing Planning/Board context — their job is to establish one — so
	// they're gated only on the store being configured, never on
	// planningCtx. See planning_navigate_tools.go and
	// build_prompt_PLANNING_ROUND3.md.
	navigateBase := navigateToolBase{store: planningStore, cell: navigateCell}
	registry.Register(planningOpenTool{navigateBase})
	registry.Register(planningCreateBoardAndOpenTool{navigateBase})
	registry.Register(planningCreatePlanningAndOpenTool{navigateBase})

	// Canvas: same fixed-registry, gated-at-Execute pattern as the planning
	// tools above — canvasStore may be nil (Canvas not configured for this
	// deployment), in which case every canvas_* call fails clearly via
	// canvasToolBase.currentCanvas rather than the tools being conditionally
	// absent. See canvas_tools.go and canvas_context.go.
	canvasBase := canvasToolBase{store: canvasStore, cell: canvasCell}
	registry.Register(canvasListDraftsTool{canvasBase})
	registry.Register(canvasCreateDraftTool{canvasBase})
	registry.Register(canvasMaterializeDraftTool{canvasBase})
	return registry
}

func registerMCPClients(ctx context.Context, registry *nbtool.Registry) ([]*mcp.Client, error) {
	configPath, err := appconfig.MCPConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := mcp.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	registerCtx, cancel := context.WithTimeout(ctx, mcpStartupTimeout)
	defer cancel()

	return mcp.Register(registerCtx, cfg, registry, logMCPProgress), nil
}

// openTelemetryStoreBestEffort mirrors openMemoryStoreBestEffort's
// isolation pattern: a failure to open the yeyo gate telemetry DB must
// never take the gate itself down with it — the whole point of
// instrumenting a mechanism already validated end-to-end is to observe it,
// not to add a new way for it to break. Only called when gateEnabled.
func openTelemetryStoreBestEffort() *yeyotelemetry.Store {
	path, err := appconfig.YeyoTelemetryDBPath()
	if err != nil {
		log.Printf("agenthost: yeyo telemetry disabled: resolve db path: %v", err)
		return nil
	}
	store, err := yeyotelemetry.Open(path)
	if err != nil {
		log.Printf("agenthost: yeyo telemetry disabled: open sqlite store %q: %v", path, err)
		return nil
	}
	return store
}

func openMemoryStoreBestEffort() *localstore.SQLiteStore {
	path, err := appconfig.MemoryDBPath()
	if err != nil {
		log.Printf("agenthost: memory disabled: resolve memory db path: %v", err)
		nbmemorytool.SetLocalMemoryService(nil)
		return nil
	}

	store, err := openMemoryStore(path)
	if err != nil {
		log.Printf("agenthost: memory disabled: open sqlite store %q: %v", path, err)
		nbmemorytool.SetLocalMemoryService(nil)
		return nil
	}

	nbmemorytool.SetLocalMemoryService(memoryservice.New(store))
	return store
}

func logMCPProgress(server string, status mcp.ProgressStatus, total int) {
	switch status {
	case mcp.ProgressBegin:
		log.Printf("agenthost: MCP registration starting (%d servers)", total)
	case mcp.ProgressConnecting:
		log.Printf("agenthost: MCP connecting to %q", server)
	case mcp.ProgressConnected:
		log.Printf("agenthost: MCP connected to %q", server)
	case mcp.ProgressFailed:
		log.Printf("agenthost: MCP failed for %q", server)
	case mcp.ProgressDone:
		log.Printf("agenthost: MCP registration finished")
	}
}

// buildSystemPrompt builds the base system prompt for rootPath. canvasBlock
// is dynamicCentro's per-turn output (canvas_centro.go) — the active
// Canvas objects' current content, injected right after the static
// yeyo.RenderCentro() block. Pass "" when there is no canvas context to
// inject (e.g. Canvas not configured, or between-turn rebuilds that don't
// go through RefreshCanvasCentro).
func buildSystemPrompt(rootPath, canvasBlock string) string {
	override := strings.TrimSpace(os.Getenv("EXO_AGENT_SYSTEM_PROMPT"))
	if override != "" {
		return override
	}

	base := "You are exo's integrated coding agent. Help the user from the browser chat, prefer safe tool use, and explain failures clearly."
	base = base + "\n\n" + yeyo.RenderCentro()
	if canvasBlock != "" {
		base = base + canvasBlock
	}
	if !yeyoGateEnabled() {
		// exp1d: force the periferia index into every turn's system prompt so
		// the model no longer has to decide whether to go look for the
		// catalog — it's just there. atom get <name> is still how it fetches
		// a full body. Superseded by the atoms_decision gate when
		// EXO_YEYO_GATE is set — see yeyoGateEnabled's doc-comment. With the
		// gate on, the index travels inside the gate's own "inspect"
		// response instead (atoms_decision_tool.go's indexOnInspect), so
		// injecting it here too would just duplicate it.
		base = base + "\n\n" + yeyo.RenderIndex()
	}

	loaded := instructions.Render(instructions.Load(rootPath))
	if loaded == "" {
		return base
	}
	return base + "\n\n" + loaded
}

func rootPathFromEnv() (string, error) {
	if override := strings.TrimSpace(os.Getenv("EXO_AGENT_ROOT_PATH")); override != "" {
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve agent root path: %w", err)
	}
	return home, nil
}
