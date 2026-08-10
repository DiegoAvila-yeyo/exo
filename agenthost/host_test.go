package agenthost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/m8adapter"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	nbprovider "github.com/yeyoos/nucleo-base/layer2-runtime-rails/provider"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/layer4-knowledge-memory/localstore"
)

func TestBuildProviderFromEnvFailsFastWithClearErrorWhenUnconfigured(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)

	_, err := buildProviderFromEnv("system")
	if err == nil {
		t.Fatal("buildProviderFromEnv returned nil error, want clear configuration error")
	}
	want := "no provider configured: set ANTHROPIC_API_KEY, LITELLM_API_KEY, or OPENAI_API_KEY"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestBuildProviderFromEnvSelectsAnthropicWhenConfigured(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")

	got, err := buildProviderFromEnv("system")
	if err != nil {
		t.Fatalf("buildProviderFromEnv returned error: %v", err)
	}
	if _, ok := got.(*nbprovider.AnthropicProvider); !ok {
		t.Fatalf("provider type = %T, want *provider.AnthropicProvider", got)
	}
}

func TestBuildProviderFromEnvSelectsLiteLLMWhenConfigured(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("LITELLM_API_KEY", "test-litellm-key")

	got, err := buildProviderFromEnv("system")
	if err != nil {
		t.Fatalf("buildProviderFromEnv returned error: %v", err)
	}
	if _, ok := got.(*nbprovider.LiteLLMProvider); !ok {
		t.Fatalf("provider type = %T, want *provider.LiteLLMProvider", got)
	}
}

func TestBuildProviderFromEnvSelectsOpenAIWhenConfigured(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	got, err := buildProviderFromEnv("system")
	if err != nil {
		t.Fatalf("buildProviderFromEnv returned error: %v", err)
	}
	if _, ok := got.(*nbprovider.OpenAIProvider); !ok {
		t.Fatalf("provider type = %T, want *provider.OpenAIProvider", got)
	}
}

func TestTerminalToolsRegisteredWithAdapterBackend(t *testing.T) {
	isolateLocalMemoryService(t)
	manager := sessions.New()
	adapter := m8adapter.New(manager)

	registry := buildToolRegistry(adapter)
	assertTerminalToolManager(t, registry, "terminal_open", adapter)
	assertTerminalToolManager(t, registry, "terminal_read", adapter)
	assertTerminalToolManager(t, registry, "terminal_write", adapter)
	assertTerminalToolManager(t, registry, "terminal_kill", adapter)
	assertTerminalToolManager(t, registry, "terminal_list", adapter)
}

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_MODEL",
		"LITELLM_API_KEY",
		"LITELLM_MODEL",
		"LITELLM_BASE_URL",
		"OPENAI_API_KEY",
		"OPENAI_MODEL",
		"EXO_AGENT_MAX_TOKENS",
	} {
		t.Setenv(key, "")
	}
}

func assertTerminalToolManager(t *testing.T, registry *nbtool.Registry, name string, want *m8adapter.Adapter) {
	t.Helper()
	tool, ok := registry.Get(name)
	if !ok {
		t.Fatalf("registry missing %q", name)
	}
	switch typed := tool.(type) {
	case *nbtool.TerminalOpenTool:
		if typed.Manager != want {
			t.Fatalf("%s manager = %T, want adapter %T", name, typed.Manager, want)
		}
	case *nbtool.TerminalReadTool:
		if typed.Manager != want {
			t.Fatalf("%s manager = %T, want adapter %T", name, typed.Manager, want)
		}
	case *nbtool.TerminalWriteTool:
		if typed.Manager != want {
			t.Fatalf("%s manager = %T, want adapter %T", name, typed.Manager, want)
		}
	case *nbtool.TerminalKillTool:
		if typed.Manager != want {
			t.Fatalf("%s manager = %T, want adapter %T", name, typed.Manager, want)
		}
	case *nbtool.TerminalListTool:
		if typed.Manager != want {
			t.Fatalf("%s manager = %T, want adapter %T", name, typed.Manager, want)
		}
	default:
		t.Fatalf("tool %q type = %T, want terminal tool pointer", name, tool)
	}
}

func TestRootPathFromEnvUsesOverride(t *testing.T) {
	isolateLocalMemoryService(t)
	custom := filepath.Join(t.TempDir(), "workspace")
	t.Setenv("EXO_AGENT_ROOT_PATH", custom)

	got, err := rootPathFromEnv()
	if err != nil {
		t.Fatalf("rootPathFromEnv returned error: %v", err)
	}
	if got != custom {
		t.Fatalf("rootPath = %q, want %q", got, custom)
	}
}

func TestNewChangesProcessWorkingDirectoryToConfiguredRootPath(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	rootPath := t.TempDir()
	t.Setenv("EXO_AGENT_ROOT_PATH", rootPath)

	host, err := New(context.Background(), sessions.New())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if host == nil {
		t.Fatal("New returned nil host")
	}

	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(got) returned error: %v", err)
	}
	wantResolved, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		t.Fatalf("EvalSymlinks(rootPath) returned error: %v", err)
	}
	if gotResolved != wantResolved {
		t.Fatalf("working directory = %q (resolved %q), want %q (resolved %q)", got, gotResolved, rootPath, wantResolved)
	}
}

func TestNewSucceedsWithNoMCPConfigFile(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("EXO_AGENT_ROOT_PATH", t.TempDir())

	host, err := New(context.Background(), sessions.New())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if host == nil {
		t.Fatal("New returned nil host")
	}
}

func TestNewFailsFastOnMalformedMCPConfig(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("EXO_AGENT_ROOT_PATH", t.TempDir())

	configPath, err := appconfig.MCPConfigPath()
	if err != nil {
		t.Fatalf("MCPConfigPath returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err = New(context.Background(), sessions.New())
	if err == nil {
		t.Fatal("New returned nil error, want malformed mcp config failure")
	}
}

func TestHostCloseClosesMCPClientsWithoutError(t *testing.T) {
	isolateLocalMemoryService(t)
	host := &Host{}
	if err := host.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestNewEnablesMemoryWhenStoreOpensSuccessfully(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("EXO_AGENT_ROOT_PATH", t.TempDir())

	host, err := New(context.Background(), sessions.New())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if host.memoryStore == nil {
		t.Fatal("memoryStore = nil, want opened sqlite store")
	}
	if nbtool.LocalMemoryService == nil {
		t.Fatal("LocalMemoryService = nil, want configured service")
	}
	if !nbtool.LocalMemoryService.Enabled() {
		t.Fatal("LocalMemoryService.Enabled() = false, want true")
	}
}

func TestNewContinuesWithMemoryDisabledWhenStoreOpenFails(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("EXO_AGENT_ROOT_PATH", t.TempDir())

	originalOpenMemoryStore := openMemoryStore
	openMemoryStore = func(path string) (*localstore.SQLiteStore, error) {
		return nil, fmt.Errorf("boom opening %s", path)
	}
	t.Cleanup(func() {
		openMemoryStore = originalOpenMemoryStore
	})

	host, err := New(context.Background(), sessions.New())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if host == nil {
		t.Fatal("New returned nil host")
	}
	if host.memoryStore != nil {
		t.Fatal("memoryStore != nil, want disabled memory store")
	}
	if nbtool.LocalMemoryService != nil {
		t.Fatalf("LocalMemoryService = %#v, want nil when memory store open fails", nbtool.LocalMemoryService)
	}
}

func TestHostCloseClosesMemoryStoreWithoutError(t *testing.T) {
	isolateLocalMemoryService(t)
	clearProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("EXO_AGENT_ROOT_PATH", t.TempDir())

	host, err := New(context.Background(), sessions.New())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if host.memoryStore == nil {
		t.Fatal("memoryStore = nil, want opened sqlite store")
	}

	if err := host.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if host.memoryStore != nil {
		t.Fatal("memoryStore != nil after Close")
	}
	if nbtool.LocalMemoryService != nil {
		t.Fatalf("LocalMemoryService = %#v after Close, want nil", nbtool.LocalMemoryService)
	}
}

func isolateLocalMemoryService(t *testing.T) {
	t.Helper()
	previous := nbtool.LocalMemoryService
	nbtool.SetLocalMemoryService(nil)
	t.Cleanup(func() {
		nbtool.SetLocalMemoryService(previous)
	})
}
