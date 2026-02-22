package main

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/downloader"
	"github.com/techitdeveloper/data-ingestion-platform/internal/health"
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
		Str("service", "downloader").
		Logger()

	ctx := context.Background()

	logger.Info().Msg("running migrations")
	if err := db.RunMigrations(cfg.DBURL, "migrations"); err != nil {
		logger.Fatal().Err(err).Msg("migrations failed")
	}

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	sourceRepo := repository.NewSourceRepository(pool)
	fileRepo := repository.NewFileRepository(pool)
	q := queue.NewRedisQueue(cfg.RedisAddr)

	d := downloader.New(sourceRepo, fileRepo, q, cfg.DataDirectory, logger)

	// Start health check server
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	healthServer := health.NewServer(":8081", pool, redisClient, logger)
	healthServer.Start()

	logger.Info().Msg("downloader starting")

	go d.Run(ctx)

	shutdownMgr := shutdown.NewManager(logger, 30*time.Second)
	shutdownCtx := shutdownMgr.WaitForShutdown(ctx)

	logger.Info().Msg("initiating graceful shutdown")
	<-shutdownCtx.Done()

	if err := healthServer.Shutdown(context.Background()); err != nil {
		logger.Error().Err(err).Msg("health server shutdown error")
	}

	logger.Info().Msg("downloader exited cleanly")
}
