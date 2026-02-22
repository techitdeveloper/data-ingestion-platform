package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
	"github.com/techitdeveloper/data-ingestion-platform/internal/exchanger"
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
		Str("service", "exchanger").
		Logger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	rateRepo := repository.NewExchangeRateRepository(pool)

	// Build the API client — URL comes from config
	client := exchanger.NewClient(cfg.ExchangeAPIURL)

	// Build and run the updater
	updater := exchanger.NewUpdater(client, rateRepo, logger)

	logger.Info().Msg("exchanger starting")
	updater.Run(ctx)

	logger.Info().Msg("exchanger exited cleanly")
}
