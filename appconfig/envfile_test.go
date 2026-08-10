package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsUnsetVars(t *testing.T) {
	t.Setenv("EXO_TEST_ALPHA", "")
	t.Setenv("EXO_TEST_BETA", "")

	path := writeEnvFile(t, "EXO_TEST_ALPHA=one\nEXO_TEST_BETA=two\n")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}
	if got := os.Getenv("EXO_TEST_ALPHA"); got != "one" {
		t.Fatalf("EXO_TEST_ALPHA = %q, want %q", got, "one")
	}
	if got := os.Getenv("EXO_TEST_BETA"); got != "two" {
		t.Fatalf("EXO_TEST_BETA = %q, want %q", got, "two")
	}
}

func TestLoadEnvFileDoesNotOverrideAmbientEnv(t *testing.T) {
	t.Setenv("EXO_TEST_ALPHA", "ambient")

	path := writeEnvFile(t, "EXO_TEST_ALPHA=file\n")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}
	if got := os.Getenv("EXO_TEST_ALPHA"); got != "ambient" {
		t.Fatalf("EXO_TEST_ALPHA = %q, want %q", got, "ambient")
	}
}

func TestLoadEnvFileMissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.env")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error for missing file: %v", err)
	}
}

func TestLoadEnvFileSkipsMalformedLines(t *testing.T) {
	t.Setenv("EXO_TEST_ALPHA", "")

	path := writeEnvFile(t, "MALFORMED_LINE\nEXO_TEST_ALPHA=loaded\n")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile returned error: %v", err)
	}
	if got := os.Getenv("EXO_TEST_ALPHA"); got != "loaded" {
		t.Fatalf("EXO_TEST_ALPHA = %q, want %q", got, "loaded")
	}
}

func writeEnvFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}
