package shutdown

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

// Manager handles graceful shutdown with a timeout.
type Manager struct {
	logger          zerolog.Logger
	shutdownTimeout time.Duration
}

func NewManager(logger zerolog.Logger, timeout time.Duration) *Manager {
	return &Manager{
		logger:          logger,
		shutdownTimeout: timeout,
	}
}

// WaitForShutdown blocks until SIGINT or SIGTERM is received,
// then returns a context that will be cancelled after the shutdown timeout.
//
// Usage:
//
//	shutdownCtx := mgr.WaitForShutdown(mainCtx)
//	// Do cleanup with shutdownCtx
func (m *Manager) WaitForShutdown(ctx context.Context) context.Context {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Block until signal arrives
	sig := <-sigChan
	m.logger.Info().
		Str("signal", sig.String()).
		Msg("shutdown signal received")

	// Return a new context with shutdown timeout
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		m.shutdownTimeout,
	)

	// Cleanup — although the caller might not need this cancel function,
	// we have it here to satisfy linter expectations
	go func() {
		<-shutdownCtx.Done()
		cancel()
	}()

	return shutdownCtx
}
