package agenthost

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/m8adapter"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/agent"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/instructions"
	nbprovider "github.com/yeyoos/nucleo-base/layer2-runtime-rails/provider"
	"github.com/yeyoos/nucleo-base/layer2-runtime-rails/runtime"
	"github.com/yeyoos/nucleo-base/shared/api"
)

// usageTrackingMockProvider wraps nbprovider.MockProvider with a TotalUsage
// method — runtime.Coordinator.Run only computes TurnResult.TokenDelta when
// its provider satisfies an internal `interface{ TotalUsage() api.Usage }`
// (coordinator.go's tokenCounter), which the bare MockProvider doesn't
// implement. This is a minimal, test-only stand-in for what
// provider.AnthropicProvider/OpenAIProvider already do for real.
type usageTrackingMockProvider struct {
	*nbprovider.MockProvider
	mu    sync.Mutex
	total api.Usage
}

func (p *usageTrackingMockProvider) Send(ctx context.Context, system string, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	resp, err := p.MockProvider.Send(ctx, system, messages, tools)
	if err == nil {
		p.mu.Lock()
		p.total = p.total.Add(resp.Usage)
		p.mu.Unlock()
	}
	return resp, err
}

func (p *usageTrackingMockProvider) TotalUsage() api.Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}

// TestHostRunCapturesTurnResultTokenDelta is the test build_prompt_SESSION_
// RECALL.md's piece 3/5 ask for: replaces the discarded `_,` at the old
// host.go:404 (`_, err = h.coordinator.Run(...)`) — assert TokenDelta
// actually reaches LastTurnUsage after Run returns.
func TestHostRunCapturesTurnResultTokenDelta(t *testing.T) {
	isolateLocalMemoryService(t)

	// tasks_file/progress (ensureTask, part of coordinator.Run's bootstrap
	// bookkeeping) resolve relative paths against the process cwd, not
	// against Coordinator.RootPath — same reason New()/SetRootPath os.Chdir
	// (see host.go). Mirrors TestNewChangesProcessWorkingDirectoryTo
	// ConfiguredRootPath's chdir-and-restore pattern.
	// The process cwd at this point may already be a since-deleted t.TempDir
	// left behind by an earlier test in this package that also os.Chdir's
	// without restoring synchronously — restoring into a directory that no
	// longer exists would itself fail, so fall back to a directory
	// guaranteed to exist rather than asserting on the original.
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		restoreTo := originalWD
		if _, statErr := os.Stat(originalWD); statErr != nil {
			restoreTo = os.TempDir()
		}
		if err := os.Chdir(restoreTo); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	rootPath := t.TempDir()
	if err := os.Chdir(rootPath); err != nil {
		t.Fatalf("Chdir(%q) failed: %v", rootPath, err)
	}
	mockProvider := &usageTrackingMockProvider{
		MockProvider: nbprovider.NewMockProvider(api.Response{
			StopReason: api.StopEndTurn,
			Content:    []api.Block{{Type: api.BlockText, Text: "hola, todo listo."}},
			Usage:      api.Usage{InputTokens: 120, OutputTokens: 30},
		}),
	}

	manager := sessions.New()
	adapter := m8adapter.New(manager)
	canvasCell := newCanvasCell()
	registry := buildToolRegistry(adapter, nil, &planningContext{}, newNavigateCell(), nil, canvasCell, nil, true)

	systemPrompt := "you are a test agent"
	rootAgent := agent.New(mockProvider, systemPrompt, registry)
	coordinator := runtime.NewCoordinator(rootAgent, rootPath)
	coordinator.Layers = instructions.FromPrompt(systemPrompt)

	h := &Host{
		registry:            registry,
		agent:               rootAgent,
		coordinator:         coordinator,
		originalRootPath:    rootPath,
		currentRootPath:     rootPath,
		canvasCell:          canvasCell,
		provider:            mockProvider,
		modelID:             mockProvider.Model(),
		contextWindowTokens: 200_000,
	}

	if err := h.Run(context.Background(), "hola", io.Discard); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	usage := h.LastTurnUsage()
	if usage.LastTurnTokens != 150 {
		t.Fatalf("LastTurnUsage().LastTurnTokens = %d, want 150 (120 input + 30 output)", usage.LastTurnTokens)
	}
	if usage.ContextWindowTokens != 200_000 {
		t.Fatalf("LastTurnUsage().ContextWindowTokens = %d, want 200000", usage.ContextWindowTokens)
	}
}
