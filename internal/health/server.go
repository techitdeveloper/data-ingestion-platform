package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Server provides HTTP health check endpoints.
type Server struct {
	addr        string
	dbPool      *pgxpool.Pool
	redisClient *redis.Client
	logger      zerolog.Logger
	server      *http.Server
}

func NewServer(
	addr string,
	dbPool *pgxpool.Pool,
	redisClient *redis.Client,
	logger zerolog.Logger,
) *Server {
	return &Server{
		addr:        addr,
		dbPool:      dbPool,
		redisClient: redisClient,
		logger:      logger,
	}
}

// Start begins serving health check endpoints on the configured address.
// It runs in a goroutine so it doesn't block the caller.
func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", s.liveness)
	mux.HandleFunc("/health/ready", s.readiness)

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	go func() {
		s.logger.Info().Str("addr", s.addr).Msg("health check server starting")
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("health server error")
		}
	}()
}

// Shutdown gracefully stops the health check server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down health server")
	return s.server.Shutdown(ctx)
}

// liveness reports if the process is alive.
// This should return 200 as long as the process is running.
// Used by Kubernetes to restart crashed pods.
func (s *Server) liveness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "alive",
	})
}

// readiness reports if the service is ready to accept work.
// This checks dependencies (DB, Redis). If they're down, return 503.
// Used by load balancers to stop sending traffic to unhealthy instances.
func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Check DB
	if err := s.dbPool.Ping(ctx); err != nil {
		s.logger.Error().Err(err).Msg("db ping failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "not_ready",
			"reason": "database_unavailable",
		})
		return
	}

	// Check Redis
	if err := s.redisClient.Ping(ctx).Err(); err != nil {
		s.logger.Error().Err(err).Msg("redis ping failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "not_ready",
			"reason": "redis_unavailable",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	})
}
