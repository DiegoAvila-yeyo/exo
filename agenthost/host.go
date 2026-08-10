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
	"github.com/DiegoAvila-yeyo/exo/m8adapter"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/agent"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/instructions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/mcp"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/runtime"
	nbmemorytool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/layer4-knowledge-memory/localstore"
	"github.com/yeyoos/nucleo-base/layer4-knowledge-memory/memoryservice"
)

const mcpStartupTimeout = 30 * time.Second

var openMemoryStore = localstore.OpenSQLite

type Host struct {
	adapter     *m8adapter.Adapter
	registry    *nbtool.Registry
	agent       *agent.Agent
	coordinator *runtime.Coordinator
	mcpClients  []*mcp.Client
	memoryStore *localstore.SQLiteStore

	stdoutMu sync.Mutex
}

func ValidateEnv() error {
	rootPath, err := rootPathFromEnv()
	if err != nil {
		return err
	}
	systemPrompt := buildSystemPrompt(rootPath)
	_, err = buildProviderFromEnv(systemPrompt)
	return err
}

func New(ctx context.Context, manager *sessions.Manager) (*Host, error) {
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
	systemPrompt := buildSystemPrompt(rootPath)
	provider, err := buildProviderFromEnv(systemPrompt)
	if err != nil {
		return nil, err
	}

	adapter := m8adapter.New(manager)
	registry := buildToolRegistry(adapter)
	mcpClients, err := registerMCPClients(ctx, registry)
	if err != nil {
		return nil, err
	}
	memoryStore := openMemoryStoreBestEffort()
	rootAgent := agent.New(provider, systemPrompt, registry)
	coordinator := runtime.NewCoordinator(rootAgent, rootPath)
	coordinator.Budget = runtime.BudgetFromEnv()
	coordinator.Layers = instructions.FromPrompt(systemPrompt)

	return &Host{
		adapter:     adapter,
		registry:    registry,
		agent:       rootAgent,
		coordinator: coordinator,
		mcpClients:  mcpClients,
		memoryStore: memoryStore,
	}, nil
}

func (h *Host) Registry() *nbtool.Registry {
	return h.registry
}

func (h *Host) Adapter() *m8adapter.Adapter {
	return h.adapter
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

	if h.coordinator == nil {
		return fmt.Errorf("agenthost: coordinator is not configured")
	}
	_, err = h.coordinator.Run(ctx, input)
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
	return firstErr
}

func buildToolRegistry(adapter *m8adapter.Adapter) *nbtool.Registry {
	registry := nbtool.NewRegistry()
	registry.ReplaceFrom(nbtool.Default)
	registry.Register(&nbtool.TerminalOpenTool{Manager: adapter})
	registry.Register(&nbtool.TerminalReadTool{Manager: adapter})
	registry.Register(&nbtool.TerminalWriteTool{Manager: adapter})
	registry.Register(&nbtool.TerminalKillTool{Manager: adapter})
	registry.Register(&nbtool.TerminalListTool{Manager: adapter})
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

func buildSystemPrompt(rootPath string) string {
	override := strings.TrimSpace(os.Getenv("EXO_AGENT_SYSTEM_PROMPT"))
	if override != "" {
		return override
	}

	base := "You are exo's integrated coding agent. Help the user from the browser chat, prefer safe tool use, and explain failures clearly."
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
