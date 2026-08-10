package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeRunner struct {
	calls      [][]string
	quietCalls [][]string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	return nil
}

func (f *fakeRunner) RunQuiet(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.quietCalls = append(f.quietCalls, call)
	return nil, nil
}

func TestRestartPromptsWhenSessionsActive(t *testing.T) {
	runner := &fakeRunner{}
	prompted := false
	command := &CLI{
		runner:       runner,
		confirm:      func(string) bool { prompted = true; return false },
		plistPath:    func() (string, error) { return "/tmp/exo.plist", nil },
		lockPath:     func() (string, error) { return "/tmp/exo.lock", nil },
		launchUID:    func() int { return 501 },
		runningProbe: func() (bool, error) { return true, nil },
		sessionCount: func(context.Context) (int, error) {
			return 2, nil
		},
	}

	err := command.Restart(context.Background())
	if !errors.Is(err, ErrRestartAborted) {
		t.Fatalf("restart error = %v, want %v", err, ErrRestartAborted)
	}
	if !prompted {
		t.Fatal("expected restart to prompt when sessions are active")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("launchctl should not have been invoked, calls = %+v", runner.calls)
	}
}

func TestRestartSkipsPromptWhenNoSessions(t *testing.T) {
	runner := &fakeRunner{}
	command := &CLI{
		runner:       runner,
		confirm:      func(string) bool { t.Fatal("restart should not prompt when no sessions are active"); return false },
		plistPath:    func() (string, error) { return "/tmp/exo.plist", nil },
		lockPath:     func() (string, error) { return "/tmp/exo.lock", nil },
		launchUID:    func() int { return 501 },
		runningProbe: func() (bool, error) { return false, nil },
	}

	if err := command.Restart(context.Background()); err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("launchctl calls = %d, want 2", len(runner.calls))
	}
}

func TestInstallUsesQuietBootoutBeforeBootstrap(t *testing.T) {
	runner := &fakeRunner{}
	tempDir := t.TempDir()
	var stdout bytes.Buffer
	capturedPath := "/opt/homebrew/bin:/Users/test/.nvm/versions/node/v22.23.1/bin"
	t.Setenv("PATH", capturedPath)
	command := &CLI{
		runner:      runner,
		stdout:      &stdout,
		executable:  func() (string, error) { return "/tmp/exo", nil },
		plistPath:   func() (string, error) { return tempDir + "/com.diegoavila.exo.plist", nil },
		envFilePath: func() (string, error) { return filepath.Join(tempDir, "agent.env"), nil },
		launchUID:   func() int { return 501 },
	}

	if err := command.Install(context.Background()); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if len(runner.quietCalls) != 1 {
		t.Fatalf("quiet calls = %d, want 1", len(runner.quietCalls))
	}
	if len(runner.calls) != 1 {
		t.Fatalf("run calls = %d, want 1", len(runner.calls))
	}
	plistData, err := os.ReadFile(filepath.Join(tempDir, "com.diegoavila.exo.plist"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Contains(plistData, []byte("<key>PATH</key>")) {
		t.Fatalf("plist missing PATH key: %s", string(plistData))
	}
	if !bytes.Contains(plistData, []byte("<string>"+capturedPath+"</string>")) {
		t.Fatalf("plist missing PATH value %q: %s", capturedPath, string(plistData))
	}
}

func TestInstallCreatesEnvTemplateWith0600Permissions(t *testing.T) {
	runner := &fakeRunner{}
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "agent.env")
	var stdout bytes.Buffer
	command := &CLI{
		runner:      runner,
		stdout:      &stdout,
		executable:  func() (string, error) { return "/tmp/exo", nil },
		plistPath:   func() (string, error) { return filepath.Join(tempDir, "com.diegoavila.exo.plist"), nil },
		envFilePath: func() (string, error) { return envPath, nil },
		launchUID:   func() int { return 501 },
	}

	if err := command.Install(context.Background()); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if perms := info.Mode().Perm(); perms != 0o600 {
		t.Fatalf("env template permissions = %o, want %o", perms, 0o600)
	}
	if got := stdout.String(); got == "" || !bytes.Contains([]byte(got), []byte(envPath)) {
		t.Fatalf("stdout = %q, want path %q", got, envPath)
	}
}

func TestInstallDoesNotOverwriteExistingEnvTemplate(t *testing.T) {
	runner := &fakeRunner{}
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "agent.env")
	original := []byte("# existing\nOPENAI_API_KEY=keep-me\n")
	if err := os.WriteFile(envPath, original, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	command := &CLI{
		runner:      runner,
		stdout:      &bytes.Buffer{},
		executable:  func() (string, error) { return "/tmp/exo", nil },
		plistPath:   func() (string, error) { return filepath.Join(tempDir, "com.diegoavila.exo.plist"), nil },
		envFilePath: func() (string, error) { return envPath, nil },
		launchUID:   func() int { return 501 },
	}

	if err := command.Install(context.Background()); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("env template content changed:\n got %q\nwant %q", got, original)
	}
}

func TestUninstallUsesQuietBootout(t *testing.T) {
	runner := &fakeRunner{}
	tempDir := t.TempDir()
	plistPath := tempDir + "/com.diegoavila.exo.plist"
	command := &CLI{
		runner:    runner,
		plistPath: func() (string, error) { return plistPath, nil },
		launchUID: func() int { return 501 },
	}

	if err := command.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if len(runner.quietCalls) != 1 {
		t.Fatalf("quiet calls = %d, want 1", len(runner.quietCalls))
	}
	if len(runner.calls) != 0 {
		t.Fatalf("run calls = %d, want 0", len(runner.calls))
	}
}
