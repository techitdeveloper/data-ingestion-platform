package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all environment-driven configuration for the application.
// Every service binary will load this once at startup and pass it around.
type Config struct {
	DBURL          string
	RedisAddr      string
	WorkerCount    int
	DataDirectory  string
	ExchangeAPIURL string
	LogLevel       string
}

// Load reads the .env file (if present) then reads environment variables.
// Environment variables always win over .env values — this is intentional
// so that in production you set real env vars and .env is ignored.
func Load() (*Config, error) {
	// godotenv.Load is a no-op if .env doesn't exist — safe to always call
	_ = godotenv.Load()

	workerCount, err := strconv.Atoi(getEnv("WORKER_COUNT", "4"))
	if err != nil {
		return nil, fmt.Errorf("invalid WORKER_COUNT: %w", err)
	}

	cfg := &Config{
		DBURL:          requireEnv("DB_URL"),
		RedisAddr:      requireEnv("REDIS_ADDR"),
		WorkerCount:    workerCount,
		DataDirectory:  getEnv("DATA_DIRECTORY", "./data"),
		ExchangeAPIURL: getEnv("EXCHANGE_API_URL", ""),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
	}

	return cfg, nil
}

// getEnv returns the env var value or a fallback default.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// requireEnv panics early if a critical env var is missing.
// Better to crash at startup with a clear message than fail mysteriously later.
func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		panic(fmt.Sprintf("required environment variable %q is not set", key))
	}
	return val
}
