package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/downloader"
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
		Str("service", "downloader").
		Logger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info().Msg("running migrations")
	if err := db.RunMigrations(cfg.DBURL, "migrations"); err != nil {
		logger.Fatal().Err(err).Msg("migrations failed")
	}

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	// Wire up dependencies
	sourceRepo := repository.NewSourceRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	q := queue.NewRedisQueue(cfg.RedisAddr)

	// Create and run the downloader
	d := downloader.New(sourceRepo, fileRepo, q, cfg.DataDirectory, logger)
	d.Run(ctx)

	logger.Info().Msg("downloader exited cleanly")
}
