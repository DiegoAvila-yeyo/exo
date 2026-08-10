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
	"sync"
	"syscall"
	"time"

	"github.com/DiegoAvila-yeyo/exo/agenthost"
	"github.com/DiegoAvila-yeyo/exo/appconfig"
	"github.com/DiegoAvila-yeyo/exo/launchdsocket"
	"github.com/DiegoAvila-yeyo/exo/lifecycle"
	"github.com/DiegoAvila-yeyo/exo/sessions"
	"github.com/DiegoAvila-yeyo/exo/sessionstore"
	"github.com/DiegoAvila-yeyo/exo/singleton"
	"github.com/DiegoAvila-yeyo/exo/termserver"
	nbtool "github.com/yeyoos/nucleo-base/layer2-runtime-rails/tool"
)

const ErrSecondInstanceExitCode = 23

var newAgentHost = agenthost.New
var validateAgentHostEnv = agenthost.ValidateEnv

type Config struct {
	LockPath        string
	SessionStoreDir string
	Port            int
	SocketName      string
	IdleTimeout     time.Duration
	GracePeriod     time.Duration
	MaxSessions     int
	InstanceID      string
}

func DefaultConfig() Config {
	lockPath, _ := appconfig.LockPath()
	sessionStoreDir, _ := appconfig.SessionStoreDir()
	return Config{
		LockPath:        lockPath,
		SessionStoreDir: sessionStoreDir,
		Port:            appconfig.DefaultPort,
		SocketName:      appconfig.SocketName,
		IdleTimeout:     appconfig.DefaultIdleTimeout,
		GracePeriod:     appconfig.DefaultGracePeriod,
		MaxSessions:     10,
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
	host, err := newAgentHost(context.Background(), manager)
	if err != nil {
		_ = lease.Release()
		return err
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
	runner := func(ctx context.Context, input string) error {
		if server == nil {
			return fmt.Errorf("termserver agent runner invoked before server initialization")
		}
		return host.Run(ctx, input, server.ChatOutputWriter())
	}

	server, err = termserver.New(config.Port, manager,
		termserver.WithActivityHook(idle.Touch),
		termserver.WithCreateGuard(idle.RejectNewSessions),
		termserver.WithAgentRunner(runner),
	)
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
