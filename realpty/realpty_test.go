package realpty

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTerminalRunsRealShellAndClosesCleanly(t *testing.T) {
	workdir := t.TempDir()
	term, err := New(workdir)
	if err != nil {
		t.Fatalf("new terminal failed: %v", err)
	}

	pid := term.PID()
	t.Cleanup(func() {
		_ = term.Close()
	})

	if _, err := term.Write([]byte("echo hello-from-pty\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	output := readUntilContains(t, term, "hello-from-pty")
	if !strings.Contains(output, "hello-from-pty") {
		t.Fatalf("output = %q, want substring %q", output, "hello-from-pty")
	}

	if err := term.Resize(100, 30); err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	if term.PGID() != pid {
		t.Fatalf("pgid = %d, want shell pid %d for dedicated process group", term.PGID(), pid)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return !processExists(pid)
	})
}

func TestTerminalCloseKillsBackgroundChildProcess(t *testing.T) {
	workdir := t.TempDir()
	term, err := New(workdir)
	if err != nil {
		t.Fatalf("new terminal failed: %v", err)
	}
	t.Cleanup(func() {
		_ = term.Close()
	})

	if _, err := term.Write([]byte("sleep 100 & echo CHILD:$!\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	output := readUntilMatch(t, term, regexp.MustCompile(`CHILD:(\d+)`))
	childPID := parseChildPID(t, output)
	if !processExists(childPID) {
		t.Fatalf("expected child pid %d to be running before close", childPID)
	}

	if err := term.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return !processRunning(childPID)
	})
}

func TestWithCommandRunsLogicalCommandAndRecordsIt(t *testing.T) {
	workdir := t.TempDir()
	command := `printf 'hello-from-command\n'`
	term, err := New(workdir, WithCommand(command))
	if err != nil {
		t.Fatalf("new terminal with command failed: %v", err)
	}
	t.Cleanup(func() {
		_ = term.Close()
	})

	if got := term.Command(); got != command {
		t.Fatalf("Command() = %q, want %q", got, command)
	}

	output := readUntilContains(t, term, "hello-from-command")
	if !strings.Contains(output, "hello-from-command") {
		t.Fatalf("output = %q, want substring %q", output, "hello-from-command")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		select {
		case <-term.Done():
			return true
		default:
			return false
		}
	})
}

func readUntilContains(t *testing.T, term *Terminal, want string) string {
	t.Helper()
	return readUntilMatch(t, term, regexp.MustCompile(regexp.QuoteMeta(want)))
}

func readUntilMatch(t *testing.T, term *Terminal, pattern *regexp.Regexp) string {
	t.Helper()
	chunks := make(chan string, 32)
	errs := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}()

	deadline := time.After(5 * time.Second)
	var builder strings.Builder
	for {
		select {
		case chunk := <-chunks:
			builder.WriteString(chunk)
			if pattern.MatchString(builder.String()) {
				return builder.String()
			}
		case err := <-errs:
			if errors.Is(err, os.ErrClosed) {
				t.Fatalf("terminal closed before reading %q", pattern.String())
			}
			t.Fatalf("read failed before finding %q: %v", pattern.String(), err)
		case <-deadline:
			t.Fatalf("timed out waiting for %q in output %q", pattern.String(), builder.String())
		}
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(timeout)

	for {
		if condition() {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("timed out waiting for condition")
		}
	}
}

func processExists(pid int) bool {
	return processRunning(pid)
}

func processRunning(pid int) bool {
	cmd := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		stat := strings.TrimSpace(line)
		if stat == "" {
			continue
		}
		if !strings.HasPrefix(stat, "Z") {
			return true
		}
	}
	return false
}

func parseChildPID(t *testing.T, output string) int {
	t.Helper()
	matches := regexp.MustCompile(`CHILD:(\d+)`).FindStringSubmatch(output)
	if len(matches) != 2 {
		t.Fatalf("child pid not found in output %q", output)
	}
	pid, err := strconv.Atoi(matches[1])
	if err != nil {
		t.Fatalf("invalid child pid %q: %v", matches[1], err)
	}
	return pid
}
