package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/canvasstore"
	"github.com/DiegoAvila-yeyo/exo/chatstore"
	"github.com/DiegoAvila-yeyo/exo/launchdsocket"
	"github.com/DiegoAvila-yeyo/exo/lifecycle"
	"github.com/DiegoAvila-yeyo/exo/planningstore"
	"github.com/DiegoAvila-yeyo/exo/sessionrecall"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/DiegoAvila-yeyo/exo/sessionstore"
	"github.com/DiegoAvila-yeyo/exo/singleton"
	"github.com/DiegoAvila-yeyo/exo/termserver"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
	"github.com/yeyoos/nucleo-base/shared/api"
)

const ErrSecondInstanceExitCode = 23

var newAgentHost = agenthost.New
var validateAgentHostEnv = agenthost.ValidateEnv

type Config struct {
	LockPath              string
	SessionStoreDir       string
	ChatStoreDir          string
	PlanningStoreDir      string
	CanvasStoreDir        string
	SessionRecallStoreDir string
	Port                  int
	SocketName            string
	IdleTimeout           time.Duration
	GracePeriod           time.Duration
	MaxSessions           int
	InstanceID            string
}

func DefaultConfig() Config {
	lockPath, _ := appconfig.LockPath()
	sessionStoreDir, _ := appconfig.SessionStoreDir()
	chatStoreDir, _ := appconfig.ChatStoreDir()
	planningStoreDir, _ := appconfig.PlanningStoreDir()
	canvasStoreDir, _ := appconfig.CanvasStoreDir()
	sessionRecallStoreDir, _ := appconfig.SessionRecallStoreDir()
	return Config{
		LockPath:              lockPath,
		SessionStoreDir:       sessionStoreDir,
		ChatStoreDir:          chatStoreDir,
		PlanningStoreDir:      planningStoreDir,
		CanvasStoreDir:        canvasStoreDir,
		SessionRecallStoreDir: sessionRecallStoreDir,
		Port:                  appconfig.DefaultPort,
		SocketName:            appconfig.SocketName,
		IdleTimeout:           appconfig.DefaultIdleTimeout,
		GracePeriod:           appconfig.DefaultGracePeriod,
		MaxSessions:           10,
	}
}

func Run(ctx context.Context, config Config) error {
	envFilePath, err := appconfig.EnvFilePath()
	if err != nil {
		return err
	}
	if err := appconfig.LoadEnvFile(envFilePath); err != nil {
		return fmt.Errorf("load agent env file %q: %w", envFilePath, err)
	}

	if err := validateAgentHostEnv(); err != nil {
		log.Printf("backend: agent host configuration failed before startup: %v", err)
		return err
	}

	instanceID := config.InstanceID
	if instanceID == "" {
		instanceID, err = newInstanceID()
		if err != nil {
			return err
		}
	}
	lease, err := singleton.Acquire(config.LockPath)
	if err != nil {
		return err
	}

	store, err := sessionstore.New(config.SessionStoreDir)
	if err != nil {
		_ = lease.Release()
		return err
	}
	if _, err := sessionstore.Reconcile(store, instanceID); err != nil {
		_ = lease.Release()
		return err
	}

	manager := sessions.New(
		sessions.WithMaxSessions(config.MaxSessions),
		sessions.WithSessionStore(store),
		sessions.WithBackendInstanceID(instanceID),
	)

	// Planning is optional the same way the project-root scanner below is:
	// callers that don't set a directory (older Config literals, most
	// existing tests) get a server with no /api/plannings capability and no
	// planning_* agent tools, instead of a hard failure. Built before
	// newAgentHost so the same *planningstore.Store instance can be handed
	// to both the agent's tools and termserver's HTTP API — one store, two
	// consumers, never constructed twice.
	var planningStore *planningstore.Store
	if config.PlanningStoreDir != "" {
		var psErr error
		planningStore, psErr = planningstore.New(config.PlanningStoreDir)
		if psErr != nil {
			_ = lease.Release()
			return psErr
		}
	}

	// Canvas follows the exact same optional-store, built-before-newAgentHost
	// pattern as Planning above — one *canvasstore.Store instance shared
	// between the agent's canvas_* tools and termserver's HTTP API.
	var canvasStore *canvasstore.Store
	if config.CanvasStoreDir != "" {
		var csErr error
		canvasStore, csErr = canvasstore.New(config.CanvasStoreDir)
		if csErr != nil {
			_ = lease.Release()
			return csErr
		}
	}

	// Session recall ("Sesiones") follows the same optional-store,
	// built-before-newAgentHost pattern as Planning/Canvas above — one
	// *sessionrecall.Store shared between the agent's session_recall tool
	// and termserver's close-session HTTP endpoint.
	var sessionRecallStore *sessionrecall.Store
	if config.SessionRecallStoreDir != "" {
		var srErr error
		sessionRecallStore, srErr = sessionrecall.New(config.SessionRecallStoreDir)
		if srErr != nil {
			_ = lease.Release()
			return srErr
		}
	}

	host, err := newAgentHost(context.Background(), manager, planningStore, canvasStore, sessionRecallStore)
	if err != nil {
		_ = lease.Release()
		return err
	}

	chatStore, err := chatstore.New(config.ChatStoreDir)
	if err != nil {
		_ = lease.Release()
		return err
	}

	var planningStoreOpt termserver.Option
	if planningStore != nil {
		planningStoreOpt = termserver.WithPlanningStore(planningStore)
	}
	var canvasStoreOpt termserver.Option
	if canvasStore != nil {
		canvasStoreOpt = termserver.WithCanvasStore(canvasStore)
	}
	var sessionRecallOpts []termserver.Option
	if sessionRecallStore != nil {
		sessionRecallOpts = append(sessionRecallOpts,
			termserver.WithSessionRecallStore(sessionRecallStore),
			termserver.WithSessionSummarizer(host.SummarizeSession),
		)
	}

	shutdownCh := make(chan error, 1)
	var httpServer *http.Server
	var idle *lifecycle.IdleMonitor
	var cleanupOnce sync.Once
	var cleanupErr error

	cleanup := func() error {
		cleanupOnce.Do(func() {
			recordErr := func(err error) {
				if err != nil && cleanupErr == nil {
					cleanupErr = err
				}
			}
			if idle != nil {
				idle.Close()
			}
			if host != nil {
				recordErr(host.Close())
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if httpServer != nil {
				recordErr(httpServer.Shutdown(shutdownCtx))
			}
			recordErr(lease.Release())
		})
		return cleanupErr
	}

	idle = lifecycle.New(lifecycle.Config{
		IdleTimeout: config.IdleTimeout,
		GracePeriod: config.GracePeriod,
		OnShutdown: func() {
			shutdownCh <- cleanup()
		},
	})

	var server *termserver.Server
	runner := func(ctx context.Context, input string, history []api.Message, projectPath string, planningID string, boardID string, canvasObjectID string) ([]api.Message, *termserver.NavigateAction, *termserver.CanvasSuggestion, *termserver.TurnUsage, error) {
		if server == nil {
			return nil, nil, nil, nil, fmt.Errorf("termserver agent runner invoked before server initialization")
		}
		// Swap in this session's history and move the agent into whatever
		// project it picked (rereading that project's own rules) before the
		// turn, then hand back whatever the agent accumulated afterward.
		// Safe without extra locking: termserver only ever calls the runner
		// while holding its own agentMu, so turns never overlap — and
		// SetRootPath's os.Chdir is process-wide, which is exactly why that
		// guarantee matters here. SetPlanningContext/BeginTurn rely on the
		// same guarantee (verified via termserver/chat.go's handleChat,
		// which holds agentMu for the full duration of one turn's host.Run
		// call — see Round 2's build prompt for why that check mattered
		// before making this mutable Host state instead of threading it
		// per-call).
		if err := host.SetRootPath(projectPath); err != nil {
			return nil, nil, nil, nil, err
		}
		host.SetMessages(history)
		host.SetPlanningContext(planningID, boardID)
		host.BeginTurn(input, canvasObjectID)
		// Must run after BeginTurn (canvasCell.projectID is only current once
		// BeginTurn has read it off currentRootPath) and every turn,
		// unconditionally — see Host.RefreshCanvasCentro's doc comment for
		// why this can't piggyback on SetRootPath's change-guarded rebuild.
		host.RefreshCanvasCentro()

		// Canvas-suggestion detection (Canvas build spec's "contextual
		// button" channel): snapshot the project's draft object IDs before
		// the turn runs, diff against after — a turn that leaves exactly one
		// new draft is unambiguous enough to surface a materialize button
		// for; zero or more than one is not (never guess which one), so no
		// suggestion is made either way. This is a coarse but honest proxy
		// for "the agent's own reply signals this draft is well-defined" —
		// canvas_create_draft is only ever called once that threshold has
		// been crossed (see its tool description in agenthost/canvas_tools.go).
		var beforeDraftIDs map[string]bool
		if canvasStore != nil && projectPath != "" {
			if before, err := canvasStore.Load(projectPath); err == nil {
				beforeDraftIDs = make(map[string]bool, len(before.Objects))
				for _, obj := range before.Objects {
					if obj.Phase == canvasstore.PhaseDraft {
						beforeDraftIDs[obj.ObjectID] = true
					}
				}
			}
		}

		err := host.Run(ctx, input, server.ChatOutputWriter())
		// Read back whatever a navigation tool committed regardless of err
		// — see AgentRunner's doc comment (termserver/chat.go) and
		// Host.TakeNavigateAction's (agenthost/planning_navigate.go) for
		// why this must not be skipped just because Run itself failed.
		var nav *termserver.NavigateAction
		if a := host.TakeNavigateAction(); a != nil {
			nav = &termserver.NavigateAction{
				PlanningID:   a.PlanningID,
				PlanningName: a.PlanningName,
				BoardID:      a.BoardID,
				BoardName:    a.BoardName,
			}
		}

		var canvasSuggestion *termserver.CanvasSuggestion
		if beforeDraftIDs != nil {
			if after, loadErr := canvasStore.Load(projectPath); loadErr == nil {
				var newDrafts []canvasstore.CanvasObject
				for _, obj := range after.Objects {
					if obj.Phase == canvasstore.PhaseDraft && !beforeDraftIDs[obj.ObjectID] {
						newDrafts = append(newDrafts, obj)
					}
				}
				if len(newDrafts) == 1 {
					canvasSuggestion = &termserver.CanvasSuggestion{
						ObjectID: newDrafts[0].ObjectID,
						Name:     newDrafts[0].Name,
					}
				}
			}
		}

		// Piece 3 (build_prompt_SESSION_RECALL.md): this turn's token-window
		// reading, read back the same post-Run way as TakeNavigateAction —
		// unconditional (not gated on err) since Host always resolves a
		// context window size and TokenDelta is meaningful even on a turn
		// that errored partway through.
		usage := host.LastTurnUsage()
		turnUsage := &termserver.TurnUsage{
			LastTurnTokens:      usage.LastTurnTokens,
			ContextWindowTokens: usage.ContextWindowTokens,
			ModelID:             usage.ModelID,
		}

		return host.Messages(), nav, canvasSuggestion, turnUsage, err
	}

	termserverOpts := []termserver.Option{
		termserver.WithActivityHook(idle.Touch),
		termserver.WithCreateGuard(idle.RejectNewSessions),
		termserver.WithAgentRunner(runner),
		termserver.WithChatStore(chatStore),
	}
	if planningStoreOpt != nil {
		termserverOpts = append(termserverOpts, planningStoreOpt)
	}
	if canvasStoreOpt != nil {
		termserverOpts = append(termserverOpts, canvasStoreOpt)
	}
	if len(sessionRecallOpts) > 0 {
		termserverOpts = append(termserverOpts, sessionRecallOpts...)
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		termserverOpts = append(termserverOpts, termserver.WithProjectRoot(home))
	}

	server, err = termserver.New(config.Port, manager, termserverOpts...)
	if err != nil {
		_ = cleanup()
		return err
	}
	nbtool.SetGlobalApproveFunc(server.RequestApproval)
	httpServer = &http.Server{Handler: server}

	listener, err := activatedOrFallbackListener(config.SocketName, config.Port)
	if err != nil {
		_ = cleanup()
		return err
	}

	idle.Start()
	serveErrCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
			return
		}
		serveErrCh <- nil
	}()

	select {
	case <-ctx.Done():
		return cleanup()
	case err := <-shutdownCh:
		return err
	case err := <-serveErrCh:
		if err != nil {
			_ = cleanup()
		}
		return err
	}
}

func activatedOrFallbackListener(socketName string, port int) (net.Listener, error) {
	listeners, err := launchdsocket.ActivateNamedSocket(socketName)
	if err == nil && len(listeners) > 0 {
		return listeners[0], nil
	}
	if err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, syscall.ENOENT) {
		return nil, err
	}
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

func newInstanceID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
