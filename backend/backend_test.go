package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DiegoAvila-yeyo/exo/realpty"
	"github.com/DiegoAvila-yeyo/exo/sessionstore"
	"github.com/DiegoAvila-yeyo/exo/singleton"
	"github.com/gorilla/websocket"
)

func TestRunIdleShutdownReleasesLease(t *testing.T) {
	setBackendProviderEnv(t)
	port := freePort(t)
	lockPath := filepath.Join(t.TempDir(), "backend.lock")
	cfg := Config{
		LockPath:        lockPath,
		SessionStoreDir: filepath.Join(t.TempDir(), "sessions"),
		ChatStoreDir:    filepath.Join(t.TempDir(), "chats"),
		Port:            port,
		SocketName:      "listener",
		IdleTimeout:     100 * time.Millisecond,
		GracePeriod:     100 * time.Millisecond,
		MaxSessions:     2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, cfg)
	}()

	waitForHTTPReady(t, port)
	getSessions(t, port)

	err := waitForRunResult(t, result, 3*time.Second)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		lease, err := singleton.Acquire(lockPath)
		if err != nil {
			return false
		}
		_ = lease.Release()
		return true
	})
}

func TestRunHoldsSingleInstanceLease(t *testing.T) {
	setBackendProviderEnv(t)
	port := freePort(t)
	lockPath := filepath.Join(t.TempDir(), "backend.lock")
	cfg := Config{
		LockPath:        lockPath,
		SessionStoreDir: filepath.Join(t.TempDir(), "sessions"),
		ChatStoreDir:    filepath.Join(t.TempDir(), "chats"),
		Port:            port,
		SocketName:      "listener",
		IdleTimeout:     2 * time.Second,
		GracePeriod:     100 * time.Millisecond,
		MaxSessions:     2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, cfg)
	}()

	waitForHTTPReady(t, port)

	lease, err := singleton.Acquire(lockPath)
	if !errors.Is(err, singleton.ErrLeaseHeld) {
		if lease != nil {
			_ = lease.Release()
		}
		t.Fatalf("second lease acquire error = %v, want %v", err, singleton.ErrLeaseHeld)
	}

	cancel()
	err = waitForRunResult(t, result, 3*time.Second)
	if err != nil {
		t.Fatalf("run returned error after cancel: %v", err)
	}
}

func TestRunContextCancelStopsServerAndReleasesLease(t *testing.T) {
	setBackendProviderEnv(t)
	port := freePort(t)
	lockPath := filepath.Join(t.TempDir(), "backend.lock")
	cfg := Config{
		LockPath:        lockPath,
		SessionStoreDir: filepath.Join(t.TempDir(), "sessions"),
		ChatStoreDir:    filepath.Join(t.TempDir(), "chats"),
		Port:            port,
		SocketName:      "listener",
		IdleTimeout:     5 * time.Second,
		GracePeriod:     100 * time.Millisecond,
		MaxSessions:     2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, cfg)
	}()

	waitForHTTPReady(t, port)
	cancel()

	err := waitForRunResult(t, result, 3*time.Second)
	if err != nil {
		t.Fatalf("run returned error after context cancel: %v", err)
	}

	lease, err := singleton.Acquire(lockPath)
	if err != nil {
		t.Fatalf("expected lease release after context cancel: %v", err)
	}
	_ = lease.Release()

	waitForCondition(t, 2*time.Second, func() bool {
		_, err := getSessionsRaw(port)
		return err != nil
	})
}

func TestRunReconcilesCrashedInstanceSessions(t *testing.T) {
	setBackendProviderEnv(t)
	if os.Getenv("EXO_BACKEND_HELPER") == "1" {
		runBackendHelperProcess()
		return
	}

	port := freePort(t)
	lockPath := filepath.Join(t.TempDir(), "backend.lock")
	storeDir := filepath.Join(t.TempDir(), "sessions")

	helper := startBackendHelper(t, port, lockPath, storeDir, "instance-first")
	defer func() {
		_ = helper.Process.Kill()
		_, _ = helper.Process.Wait()
	}()

	waitForHTTPReady(t, port)
	sessionID, token := createSessionViaHTTP(t, port, t.TempDir())
	holdShellAliveAcrossCrash(t, port, token, sessionID)
	metadata := loadSessionFile(t, storeDir, sessionID)
	if metadata.ProcessGroupID == 0 || metadata.ShellPID == 0 {
		t.Fatalf("metadata missing process identity: %+v", metadata)
	}

	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper failed: %v", err)
	}
	_, _ = helper.Process.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, Config{
			LockPath:        lockPath,
			SessionStoreDir: storeDir,
			ChatStoreDir:    filepath.Join(t.TempDir(), "chats"),
			Port:            port,
			SocketName:      "listener",
			IdleTimeout:     5 * time.Second,
			GracePeriod:     100 * time.Millisecond,
			MaxSessions:     2,
			InstanceID:      "instance-second",
		})
	}()

	waitForHTTPReady(t, port)
	waitForCondition(t, 3*time.Second, func() bool {
		return !realpty.ProcessGroupAlive(metadata.ProcessGroupID)
	})

	updated := loadSessionFile(t, storeDir, sessionID)
	if updated.Status != sessionstore.StatusStaleReaped {
		t.Fatalf("updated status = %q, want %q", updated.Status, sessionstore.StatusStaleReaped)
	}

	cancel()
	err := waitForRunResult(t, result, 3*time.Second)
	if err != nil {
		t.Fatalf("second backend run returned error: %v", err)
	}
}

func TestBackendRunFailsFastOnProviderConstructionError(t *testing.T) {
	clearBackendProviderEnv(t)
	lockPath := filepath.Join(t.TempDir(), "backend.lock")
	sessionStoreDir := filepath.Join(t.TempDir(), "sessions")

	err := Run(context.Background(), Config{
		LockPath:        lockPath,
		SessionStoreDir: sessionStoreDir,
		Port:            freePort(t),
		SocketName:      "listener",
		IdleTimeout:     5 * time.Second,
		GracePeriod:     100 * time.Millisecond,
		MaxSessions:     2,
	})
	if err == nil {
		t.Fatal("Run returned nil error, want fail-fast provider configuration error")
	}
	want := "no provider configured: set ANTHROPIC_API_KEY, LITELLM_API_KEY, or OPENAI_API_KEY"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if _, statErr := os.Stat(lockPath); !os.IsNotExist(statErr) {
		t.Fatalf("lock path should not be created on provider fail-fast, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(sessionStoreDir); !os.IsNotExist(statErr) {
		t.Fatalf("session store dir should not be created on provider fail-fast, stat err = %v", statErr)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port failed: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHTTPReady(t *testing.T, port int) {
	t.Helper()
	waitForCondition(t, 3*time.Second, func() bool {
		resp, err := getSessionsRaw(port)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func getSessions(t *testing.T, port int) {
	t.Helper()
	resp, err := getSessionsRaw(port)
	if err != nil {
		t.Fatalf("get sessions failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get sessions status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func getSessionsRaw(port int) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/sessions", port), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{Timeout: 200 * time.Millisecond}
	return client.Do(req)
}

func waitForRunResult(t *testing.T, result <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		t.Fatal("timed out waiting for backend.Run to return")
		return nil
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func setBackendProviderEnv(t *testing.T) {
	t.Helper()
	clearBackendProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	t.Setenv("OPENAI_MODEL", "gpt-5-codex")
}

func clearBackendProviderEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_MODEL",
		"LITELLM_API_KEY",
		"LITELLM_MODEL",
		"LITELLM_BASE_URL",
		"OPENAI_API_KEY",
		"OPENAI_MODEL",
		"EXO_AGENT_MAX_TOKENS",
		"EXO_AGENT_SYSTEM_PROMPT",
		"EXO_AGENT_ROOT_PATH",
	} {
		t.Setenv(key, "")
	}
}

func startBackendHelper(t *testing.T, port int, lockPath, storeDir, instanceID string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "TestRunReconcilesCrashedInstanceSessions")
	cmd.Env = append(os.Environ(),
		"EXO_BACKEND_HELPER=1",
		fmt.Sprintf("EXO_HELPER_PORT=%d", port),
		"EXO_HELPER_LOCK="+lockPath,
		"EXO_HELPER_STORE="+storeDir,
		"EXO_HELPER_CHATS="+filepath.Join(t.TempDir(), "chats"),
		"EXO_HELPER_INSTANCE="+instanceID,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process failed: %v", err)
	}
	return cmd
}

func runBackendHelperProcess() {
	port, _ := strconv.Atoi(os.Getenv("EXO_HELPER_PORT"))
	err := Run(context.Background(), Config{
		LockPath:        os.Getenv("EXO_HELPER_LOCK"),
		SessionStoreDir: os.Getenv("EXO_HELPER_STORE"),
		ChatStoreDir:    os.Getenv("EXO_HELPER_CHATS"),
		Port:            port,
		SocketName:      "listener",
		IdleTimeout:     time.Hour,
		GracePeriod:     100 * time.Millisecond,
		MaxSessions:     2,
		InstanceID:      os.Getenv("EXO_HELPER_INSTANCE"),
	})
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func createSessionViaHTTP(t *testing.T, port int, workdir string) (string, string) {
	t.Helper()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar failed: %v", err)
	}
	client.Jar = jar

	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("bootstrap GET failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read bootstrap body failed: %v", err)
	}
	htmlBody := string(body)
	csrf := parseMeta(t, htmlBody, "nucleo-csrf")
	token := parseMeta(t, htmlBody, "nucleo-token")

	payload := fmt.Sprintf(`{"workdir":%q,"name":"helper"}`, workdir)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/sessions?csrf_token="+url.QueryEscape(csrf), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build create request failed: %v", err)
	}
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", port))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("create session request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var info struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode session create response failed: %v", err)
	}
	return info.ID, token
}

func parseMeta(t *testing.T, htmlBody, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`<meta name="` + regexp.QuoteMeta(name) + `" content="([^"]+)">`)
	matches := pattern.FindStringSubmatch(htmlBody)
	if len(matches) != 2 {
		t.Fatalf("meta %q not found", name)
	}
	return matches[1]
}

func holdShellAliveAcrossCrash(t *testing.T, port int, token, sessionID string) {
	t.Helper()
	header := http.Header{"Origin": []string{fmt.Sprintf("http://127.0.0.1:%d", port)}}
	dialer := websocket.Dialer{Subprotocols: []string{"nucleo-term." + token}}
	conn, _, err := dialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/api/terminal/%s/stream", port, sessionID), header)
	if err != nil {
		t.Fatalf("dial terminal websocket failed: %v", err)
	}
	defer conn.Close()

	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read terminal ready failed: %v", err)
	}
	command := "echo CRASH_GUARD_READY; trap '' HUP; exec /bin/sh -c 'trap \"\" HUP; while :; do sleep 100; done'\n"
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(command)); err != nil {
		t.Fatalf("write crash guard command failed: %v", err)
	}
	waitForCondition(t, 3*time.Second, func() bool {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			t.Fatalf("set read deadline failed: %v", err)
		}
		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					return false
				}
				t.Fatalf("read crash guard output failed: %v", err)
			}
			if messageType == websocket.BinaryMessage && strings.Contains(string(payload), "CRASH_GUARD_READY") {
				return true
			}
		}
	})
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("reset read deadline failed: %v", err)
	}
}

func loadSessionFile(t *testing.T, storeDir, sessionID string) sessionstore.SessionMetadata {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(storeDir, sessionID+".json"))
	if err != nil {
		t.Fatalf("read session metadata failed: %v", err)
	}
	var metadata sessionstore.SessionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("unmarshal session metadata failed: %v", err)
	}
	return metadata
}
