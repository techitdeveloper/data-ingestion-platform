package main

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/exchanger"
	"github.com/techitdeveloper/data-ingestion-platform/internal/health"
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
		Str("service", "exchanger").
		Logger()

	ctx := context.Background()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	rateRepo := repository.NewExchangeRateRepository(pool)
	client := exchanger.NewClient(cfg.ExchangeAPIURL)
	updater := exchanger.NewUpdater(client, rateRepo, logger)

	// Start health check server
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	healthServer := health.NewServer(":8082", pool, redisClient, logger)
	healthServer.Start()

	logger.Info().Msg("exchanger starting")

	go updater.Run(ctx)

	shutdownMgr := shutdown.NewManager(logger, 30*time.Second)
	shutdownCtx := shutdownMgr.WaitForShutdown(ctx)

	logger.Info().Msg("initiating graceful shutdown")
	<-shutdownCtx.Done()

	if err := healthServer.Shutdown(context.Background()); err != nil {
		logger.Error().Err(err).Msg("health server shutdown error")
	}

	logger.Info().Msg("exchanger exited cleanly")
}
