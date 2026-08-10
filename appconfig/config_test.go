package appconfig

import (
	"path/filepath"
	"testing"
)

func TestMCPConfigPathUnderAppSupportDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := MCPConfigPath()
	if err != nil {
		t.Fatalf("MCPConfigPath returned error: %v", err)
	}

	want := filepath.Join(home, "Library", "Application Support", AppName, "mcp.json")
	if got != want {
		t.Fatalf("MCPConfigPath = %q, want %q", got, want)
	}
}

func TestMemoryDBPathUnderAppSupportDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := MemoryDBPath()
	if err != nil {
		t.Fatalf("MemoryDBPath returned error: %v", err)
	}

	want := filepath.Join(home, "Library", "Application Support", AppName, "memory.db")
	if got != want {
		t.Fatalf("MemoryDBPath = %q, want %q", got, want)
	}
}
