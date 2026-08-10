package realpty

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	defaultCols = 80
	defaultRows = 24
)

// Terminal is a real PTY-backed shell process implementing ptyactor.PTY.
type Terminal struct {
	cmd       *exec.Cmd
	ptmx      *os.File
	waitCh    chan error
	doneCh    chan struct{}
	closeOnce sync.Once
	waitMu    sync.RWMutex
	waitErr   error
	command   string
	pid       int
	pgid      int
	startTime string
}

type Option func(*options)

type options struct {
	env     map[string]string
	command string
}

// New starts a shell attached to a real PTY in the provided working directory.
func New(workdir string, opts ...Option) (*Terminal, error) {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}
	if strings.TrimSpace(cfg.command) != "" {
		return startCommand(workdir, cfg)
	}
	var lastErr error
	for _, shell := range resolveShellCandidates() {
		term, err := startShell(shell, workdir, cfg)
		if err == nil {
			return term, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("realpty: no usable shell found")
	}
	return nil, lastErr
}

func (t *Terminal) Write(p []byte) (int, error) {
	return t.ptmx.Write(p)
}

func (t *Terminal) Read(p []byte) (int, error) {
	return t.ptmx.Read(p)
}

func (t *Terminal) Resize(cols, rows int) error {
	return pty.Setsize(t.ptmx, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (t *Terminal) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		if t.cmd.Process == nil {
			_ = t.ptmx.Close()
			return
		}
		_ = t.ptmx.Close()
		closeErr = TerminateProcessTree(t.pid, t.pgid)
		if closeErr != nil {
			return
		}
		err := <-t.waitCh
		closeErr = normalizeWaitErr(err)
	})
	return closeErr
}

// Command returns the shell command launched for this PTY.
func (t *Terminal) Command() string {
	return t.command
}

// PID returns the child process id.
func (t *Terminal) PID() int {
	return t.pid
}

// PGID returns the process group id for the shell session.
func (t *Terminal) PGID() int {
	return t.pgid
}

// StartTime returns the shell process start time string as reported by ps lstart.
func (t *Terminal) StartTime() string {
	return t.startTime
}

// Done closes once the child process exits.
func (t *Terminal) Done() <-chan struct{} {
	return t.doneCh
}

func (t *Terminal) waitError() error {
	t.waitMu.RLock()
	defer t.waitMu.RUnlock()
	return t.waitErr
}

func resolveShellCandidates() []string {
	seen := make(map[string]bool)
	candidates := make([]string, 0, 4)
	add := func(shell string) {
		if shell == "" || seen[shell] {
			return
		}
		info, err := os.Stat(shell)
		if err != nil || info.IsDir() {
			return
		}
		seen[shell] = true
		candidates = append(candidates, shell)
	}

	add(os.Getenv("SHELL"))
	add("/bin/zsh")
	add("/bin/bash")
	add("/bin/sh")
	return candidates
}

func normalizeWaitErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func (t *Terminal) descendantProcessGroups() []int {
	groups := map[int]struct{}{t.pgid: {}}
	descendants, err := DescendantProcessGroups(t.pid)
	if err != nil {
		return []int{t.pgid}
	}
	for _, pgid := range descendants {
		if pgid > 0 {
			groups[pgid] = struct{}{}
		}
	}
	out := make([]int, 0, len(groups))
	for pgid := range groups {
		out = append(out, pgid)
	}
	return out
}

func startShell(shell, workdir string, cfg options) (*Terminal, error) {
	cmd := exec.Command(shell, "-i")
	return startCommandWithExec(cmd, workdir, cfg, shell)
}

func startCommand(workdir string, cfg options) (*Terminal, error) {
	cmd := exec.Command("/bin/sh", "-lc", cfg.command)
	return startCommandWithExec(cmd, workdir, cfg, cfg.command)
}

func startCommandWithExec(cmd *exec.Cmd, workdir string, cfg options, logicalCommand string) (*Terminal, error) {
	cmd.Dir = filepath.Clean(workdir)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	for key, value := range cfg.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: defaultCols,
		Rows: defaultRows,
	})
	if err != nil {
		return nil, err
	}

	term := &Terminal{
		cmd:     cmd,
		ptmx:    ptmx,
		waitCh:  make(chan error, 1),
		doneCh:  make(chan struct{}),
		command: logicalCommand,
		pid:     cmd.Process.Pid,
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		return nil, err
	}
	term.pgid = pgid
	snapshot, err := ProcessSnapshot(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		return nil, err
	}
	term.startTime = snapshot.StartTime

	go func() {
		err := cmd.Wait()
		term.waitMu.Lock()
		term.waitErr = err
		term.waitMu.Unlock()
		term.waitCh <- err
		close(term.doneCh)
	}()

	return term, nil
}

func WithEnv(env map[string]string) Option {
	return func(cfg *options) {
		if cfg.env == nil {
			cfg.env = make(map[string]string, len(env))
		}
		for key, value := range env {
			cfg.env[key] = value
		}
	}
}

func WithCommand(command string) Option {
	return func(cfg *options) {
		cfg.command = strings.TrimSpace(command)
	}
}

func signalProcessGroups(pgids []int, signal syscall.Signal) {
	for _, pgid := range pgids {
		if pgid <= 0 {
			continue
		}
		err := syscall.Kill(-pgid, signal)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			continue
		}
	}
}

func TerminateProcessGroup(pgid int) error {
	if pgid <= 0 {
		return errors.New("realpty: invalid process group")
	}
	signalProcessGroups([]int{pgid}, syscall.SIGTERM)
	if waitForProcessGroupsExit([]int{pgid}, 750*time.Millisecond) {
		return nil
	}
	signalProcessGroups([]int{pgid}, syscall.SIGKILL)
	if waitForProcessGroupsExit([]int{pgid}, 750*time.Millisecond) {
		return nil
	}
	return errors.New("realpty: timed out waiting for process group to exit")
}

type Snapshot struct {
	PID       int
	PGID      int
	StartTime string
}

func ProcessSnapshot(pid int) (Snapshot, error) {
	cmd := exec.Command("/bin/ps", "-o", "pid=,pgid=,lstart=", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return Snapshot{}, err
	}
	line := strings.TrimSpace(string(output))
	if line == "" {
		return Snapshot{}, errors.New("realpty: process not found")
	}
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return Snapshot{}, errors.New("realpty: unexpected ps output")
	}
	gotPID, err1 := strconv.Atoi(fields[0])
	gotPGID, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil {
		return Snapshot{}, errors.New("realpty: failed to parse ps output")
	}
	return Snapshot{
		PID:       gotPID,
		PGID:      gotPGID,
		StartTime: strings.Join(fields[2:], " "),
	}, nil
}

func ProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	cmd := exec.Command("/bin/ps", "-axo", "pid=,pgid=,stat=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		gotPGID, err := strconv.Atoi(fields[1])
		if err != nil || gotPGID != pgid {
			continue
		}
		if !strings.HasPrefix(fields[2], "Z") {
			return true
		}
	}
	return false
}

func DescendantProcessGroups(rootPID int) ([]int, error) {
	cmd := exec.Command("/bin/ps", "-axo", "pid=,ppid=,pgid=")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	type procInfo struct {
		pid  int
		ppid int
		pgid int
	}
	procs := make(map[int]procInfo)
	children := make(map[int][]int)

	for _, line := range bytes.Split(output, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		if len(fields) != 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		procs[pid] = procInfo{pid: pid, ppid: ppid, pgid: pgid}
		children[ppid] = append(children[ppid], pid)
	}

	var out []int
	stack := append([]int(nil), children[rootPID]...)
	seen := make(map[int]bool)
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if proc, ok := procs[pid]; ok {
			out = append(out, proc.pgid)
		}
		stack = append(stack, children[pid]...)
	}
	return out, nil
}

func TerminateProcessTree(rootPID, rootPGID int) error {
	processGroups := map[int]struct{}{rootPGID: {}}
	descendants, err := DescendantProcessGroups(rootPID)
	if err == nil {
		for _, pgid := range descendants {
			if pgid > 0 {
				processGroups[pgid] = struct{}{}
			}
		}
	}

	groups := make([]int, 0, len(processGroups))
	for pgid := range processGroups {
		groups = append(groups, pgid)
	}

	signalProcessGroups(groups, syscall.SIGTERM)
	if waitForProcessGroupsExit(groups, 750*time.Millisecond) {
		return nil
	}
	signalProcessGroups(groups, syscall.SIGKILL)
	if waitForProcessGroupsExit(groups, 750*time.Millisecond) {
		return nil
	}
	return errors.New("realpty: timed out waiting for process groups to exit")
}

func waitForProcessGroupsExit(pgids []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allGone := true
		for _, pgid := range pgids {
			if pgid > 0 && ProcessGroupAlive(pgid) {
				allGone = false
				break
			}
		}
		if allGone {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
