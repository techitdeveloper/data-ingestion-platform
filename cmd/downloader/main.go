package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/config"
	"github.com/techitdeveloper/data-ingestion-platform/internal/db"
)

func main() {
	// 1. Load config — fail fast if misconfigured
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	// 2. Set up structured logger
	level, _ := zerolog.ParseLevel(cfg.LogLevel)
	logger := zerolog.New(os.Stdout).
		Level(level).
		With().
		Timestamp().
		Str("service", "downloader").
		Logger()

	// 3. Root context — cancelled on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 4. Run migrations
	logger.Info().Msg("running migrations")
	if err := db.RunMigrations(cfg.DBURL, "migrations"); err != nil {
		logger.Fatal().Err(err).Msg("migrations failed")
	}

	// 5. Connect to DB
	logger.Info().Msg("connecting to database")
	pool, err := db.NewPool(ctx, cfg.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	logger.Info().Msg("downloader ready — waiting for signal")

	// 6. Block until shutdown signal
	<-ctx.Done()
	logger.Info().Msg("shutdown signal received, exiting cleanly")
}
