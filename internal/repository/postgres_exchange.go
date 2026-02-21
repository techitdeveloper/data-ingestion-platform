package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
)

// ExchangeRateRepository defines DB operations for exchange rates.
type ExchangeRateRepository interface {
	GetRate(ctx context.Context, currency string, date time.Time) (*models.ExchangeRate, error)
	UpsertRate(ctx context.Context, rate *models.ExchangeRate) error
}

type pgxExchangeRateRepo struct {
	pool *pgxpool.Pool
}

func NewExchangeRateRepository(pool *pgxpool.Pool) ExchangeRateRepository {
	return &pgxExchangeRateRepo{pool: pool}
}

func (r *pgxExchangeRateRepo) GetRate(
	ctx context.Context,
	currency string,
	date time.Time,
) (*models.ExchangeRate, error) {
	var rate models.ExchangeRate
	err := r.pool.QueryRow(ctx, `
		SELECT currency, rate_to_usd, rate_date
		FROM exchange_rates
		WHERE currency = $1 AND rate_date = $2
	`, currency, date).Scan(
		&rate.Currency,
		&rate.RateToUSD,
		&rate.RateDate,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying exchange rate: %w", err)
	}
	return &rate, nil
}

func (r *pgxExchangeRateRepo) UpsertRate(ctx context.Context, rate *models.ExchangeRate) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO exchange_rates (currency, rate_to_usd, rate_date)
		VALUES ($1, $2, $3)
		ON CONFLICT (currency, rate_date)
		DO UPDATE SET rate_to_usd = EXCLUDED.rate_to_usd
	`, rate.Currency, rate.RateToUSD, rate.RateDate)
	if err != nil {
		return fmt.Errorf("upserting exchange rate: %w", err)
	}
	return nil
}
