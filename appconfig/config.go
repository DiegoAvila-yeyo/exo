package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	AppName     = "exo"
	Label       = "com.diegoavila.exo"
	SocketName  = "listener"
	DefaultPort = 45873
)

var (
	DefaultIdleTimeout = 5 * time.Minute
	DefaultGracePeriod = 2 * time.Second
)

func BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", DefaultPort)
}

func AppSupportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", AppName), nil
}

func LockPath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "backend.lock"), nil
}

func SessionStoreDir() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

func EnvFilePath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent.env"), nil
}

func MCPConfigPath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcp.json"), nil
}

func MemoryDBPath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "memory.db"), nil
}

func LaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}
