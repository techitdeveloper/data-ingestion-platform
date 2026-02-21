package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/processor"
	"github.com/techitdeveloper/data-ingestion-platform/internal/queue"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	// Wire up all repositories
	fileRepo := repository.NewFileRepository(pool)
	rateRepo := repository.NewExchangeRateRepository(pool)
	revenueRepo := repository.NewRevenueRepository(pool)

	// Build the processor (stateless — safe to share across workers)
	proc := processor.New(fileRepo, rateRepo, revenueRepo, logger)

	// Build and run the worker pool
	q := queue.NewRedisQueue(cfg.RedisAddr)
	workerPool := processor.NewWorkerPool(proc, q, fileRepo, cfg.WorkerCount, logger)

	logger.Info().
		Int("workers", cfg.WorkerCount).
		Msg("processor starting")

	workerPool.Run(ctx)

	logger.Info().Msg("processor exited cleanly")
}
