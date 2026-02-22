package main

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/health"
	"github.com/techitdeveloper/data-ingestion-platform/internal/processor"
	"github.com/techitdeveloper/data-ingestion-platform/internal/queue"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
	"github.com/techitdeveloper/data-ingestion-platform/pkg/shutdown"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	level, _ := zerolog.ParseLevel(cfg.LogLevel)
	logger := zerolog.New(os.Stdout).
		Level(level).
		With().
		Timestamp().
		Str("service", "processor").
		Logger()

	// Create root context
	ctx := context.Background()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	// Wire up repositories
	fileRepo := repository.NewFileRepository(pool)
	rateRepo := repository.NewExchangeRateRepository(pool)
	revenueRepo := repository.NewRevenueRepository(pool)

	// Build processor
	proc := processor.New(fileRepo, rateRepo, revenueRepo, logger)

	// Build queue and worker pool
	q := queue.NewRedisQueue(cfg.RedisAddr)
	workerPool := processor.NewWorkerPool(proc, q, fileRepo, cfg.WorkerCount, logger)

	// Build reconciler
	reconciler := processor.NewReconciler(fileRepo, q, logger)

	// Start health check server
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	healthServer := health.NewServer(":8080", pool, redisClient, logger)
	healthServer.Start()

	logger.Info().
		Int("workers", cfg.WorkerCount).
		Msg("processor starting")

	// Start worker pool and reconciler in goroutines
	go workerPool.Run(ctx)
	go reconciler.Run(ctx)

	// Wait for shutdown signal
	shutdownMgr := shutdown.NewManager(logger, 30*time.Second)
	shutdownCtx := shutdownMgr.WaitForShutdown(ctx)

	// Graceful shutdown
	logger.Info().Msg("initiating graceful shutdown")

	// Give in-flight work time to finish
	<-shutdownCtx.Done()

	// Shutdown health server
	if err := healthServer.Shutdown(context.Background()); err != nil {
		logger.Error().Err(err).Msg("health server shutdown error")
	}

	logger.Info().Msg("processor exited cleanly")
}
