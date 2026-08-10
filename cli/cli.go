package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/launchagent"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/DiegoAvila-yeyo/exo/singleton"
)

var ErrRestartAborted = errors.New("cli: restart aborted by user")

type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	RunQuiet(ctx context.Context, name string, args ...string) ([]byte, error)
}

type CLI struct {
	runner       Runner
	httpClient   *http.Client
	stdout       io.Writer
	confirm      func(string) bool
	executable   func() (string, error)
	plistPath    func() (string, error)
	lockPath     func() (string, error)
	envFilePath  func() (string, error)
	launchUID    func() int
	sessionCount func(context.Context) (int, error)
	runningProbe func() (bool, error)
}

type Status struct {
	Installed    bool
	Running      bool
	SessionCount int
}

func New() *CLI {
	return &CLI{
		runner:     execRunner{},
		httpClient: &http.Client{},
		stdout:     os.Stdout,
		confirm: func(prompt string) bool {
			fmt.Fprint(os.Stdout, prompt)
			var answer string
			_, _ = fmt.Fscanln(os.Stdin, &answer)
			answer = strings.TrimSpace(strings.ToLower(answer))
			return answer == "y" || answer == "yes"
		},
		executable: func() (string, error) {
			path, err := os.Executable()
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(path)
		},
		plistPath:   appconfig.LaunchAgentPath,
		lockPath:    appconfig.LockPath,
		envFilePath: appconfig.EnvFilePath,
		launchUID:   os.Getuid,
	}
}

func (c *CLI) Install(ctx context.Context) error {
	plistPath, err := c.plistPath()
	if err != nil {
		return err
	}
	binaryPath, err := c.executable()
	if err != nil {
		return err
	}
	payload, err := launchagent.RenderPlist(launchagent.Config{
		Label:       appconfig.Label,
		ProgramPath: binaryPath,
		SocketName:  appconfig.SocketName,
		Port:        appconfig.DefaultPort,
		// PATH is captured at install time; if the user's shell PATH changes later,
		// they need to run exo install again for launchd to pick it up.
		EnvironmentVariables: launchEnvironmentVariables(),
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, payload, 0o644); err != nil {
		return err
	}
	envFilePath, err := c.envFilePath()
	if err != nil {
		return err
	}
	if err := ensureEnvTemplate(envFilePath); err != nil {
		return err
	}
	fmt.Fprintf(c.output(), "Agent environment template: %s\n", envFilePath)
	_, _ = c.runner.RunQuiet(ctx, "launchctl", launchagent.BootoutArgs(c.launchUID(), plistPath)...)
	return c.runner.Run(ctx, "launchctl", launchagent.BootstrapArgs(c.launchUID(), plistPath)...)
}

func (c *CLI) Uninstall(ctx context.Context) error {
	plistPath, err := c.plistPath()
	if err != nil {
		return err
	}
	_, _ = c.runner.RunQuiet(ctx, "launchctl", launchagent.BootoutArgs(c.launchUID(), plistPath)...)
	if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (c *CLI) Status(ctx context.Context) (Status, error) {
	plistPath, err := c.plistPath()
	if err != nil {
		return Status{}, err
	}
	_, statErr := os.Stat(plistPath)
	installed := statErr == nil

	running, err := c.runningStatus()
	if err != nil {
		return Status{}, err
	}
	status := Status{Installed: installed, Running: running}
	if running {
		count, err := c.sessionCountFn()(ctx)
		if err != nil {
			return Status{}, err
		}
		status.SessionCount = count
	}
	return status, nil
}

func (c *CLI) Restart(ctx context.Context) error {
	plistPath, err := c.plistPath()
	if err != nil {
		return err
	}
	running, err := c.runningStatus()
	if err != nil {
		return err
	}
	count := 0
	if running {
		count, err = c.sessionCountFn()(ctx)
		if err != nil {
			return err
		}
	}
	if count > 0 {
		prompt := fmt.Sprintf("This will terminate %d active terminal session(s). Session reattach after restart is not supported in v1. Continue? [y/N] ", count)
		if !c.confirm(prompt) {
			return ErrRestartAborted
		}
	}
	if err := c.runner.Run(ctx, "launchctl", launchagent.BootoutArgs(c.launchUID(), plistPath)...); err != nil {
		return err
	}
	return c.runner.Run(ctx, "launchctl", launchagent.BootstrapArgs(c.launchUID(), plistPath)...)
}

func (c *CLI) sessionCountFn() func(context.Context) (int, error) {
	if c.sessionCount != nil {
		return c.sessionCount
	}
	return c.querySessionCount
}

func (c *CLI) querySessionCount(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appconfig.BaseURL()+"/api/sessions", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", appconfig.DefaultPort))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status querying sessions: %d", resp.StatusCode)
	}
	var sessionsList []sessions.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&sessionsList); err != nil {
		return 0, err
	}
	return len(sessionsList), nil
}

func (c *CLI) runningStatus() (bool, error) {
	if c.runningProbe != nil {
		return c.runningProbe()
	}
	lockPath, err := c.lockPath()
	if err != nil {
		return false, err
	}
	lease, err := singleton.Acquire(lockPath)
	switch {
	case err == nil:
		_ = lease.Release()
		return false, nil
	case errors.Is(err, singleton.ErrLeaseHeld):
		return true, nil
	default:
		return false, err
	}
}

func (c *CLI) output() io.Writer {
	if c.stdout != nil {
		return c.stdout
	}
	return os.Stdout
}

const envTemplate = `# exo agent configuration - uncomment and fill in what you need.
# At least one provider key is required.
# ANTHROPIC_API_KEY=
# ANTHROPIC_MODEL=
# LITELLM_API_KEY=
# LITELLM_BASE_URL=
# LITELLM_MODEL=
# OPENAI_API_KEY=
# OPENAI_MODEL=
# EXO_AGENT_ROOT_PATH=
# EXO_AGENT_MAX_TOKENS=
# EXO_AGENT_SYSTEM_PROMPT=
`

func ensureEnvTemplate(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(envTemplate), 0o600)
}

func launchEnvironmentVariables() map[string]string {
	path := os.Getenv("PATH")
	if path == "" {
		return nil
	}
	return map[string]string{"PATH": path}
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (execRunner) RunQuiet(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}
