package exchanger

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
)

// Updater fetches exchange rates from an external API and stores them in the DB.
type Updater struct {
	client   *Client
	rateRepo repository.ExchangeRateRepository
	logger   zerolog.Logger
}

func NewUpdater(
	client *Client,
	rateRepo repository.ExchangeRateRepository,
	logger zerolog.Logger,
) *Updater {
	return &Updater{
		client:   client,
		rateRepo: rateRepo,
		logger:   logger,
	}
}

// Run is the main scheduler loop — runs immediately on start, then once per day.
// This is idempotent: running it multiple times on the same day just updates
// the same DB records (ON CONFLICT DO UPDATE).
func (u *Updater) Run(ctx context.Context) {
	u.logger.Info().Msg("exchange rate updater started")

	// Run immediately on startup to ensure rates are fresh
	u.updateRates(ctx)

	// Then schedule daily at midnight UTC (adjust as needed)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			u.updateRates(ctx)
		case <-ctx.Done():
			u.logger.Info().Msg("updater shutting down")
			return
		}
	}
}

// updateRates fetches the latest rates and stores them in the DB.
func (u *Updater) updateRates(ctx context.Context) {
	u.logger.Info().Msg("fetching exchange rates")

	apiResp, err := u.client.FetchRates(ctx)
	if err != nil {
		u.logger.Error().Err(err).Msg("failed to fetch exchange rates")
		return
	}

	// Parse the date from the API response
	rateDate, err := time.Parse("2006-01-02", apiResp.Date)
	if err != nil {
		u.logger.Error().
			Err(err).
			Str("api_date", apiResp.Date).
			Msg("failed to parse date from API")
		return
	}

	u.logger.Info().
		Str("base", apiResp.Base).
		Str("date", apiResp.Date).
		Int("rate_count", len(apiResp.Rates)).
		Msg("received rates from API")

	// The API gives us rates where "1 USD = X units of currency".
	// Our DB stores "rate_to_usd" meaning "1 unit of currency = Y USD".
	// So we need to invert most rates. BUT for USD itself, rate_to_usd = 1.0.

	var upsertCount int
	for currency, apiRate := range apiResp.Rates {
		// apiRate is "1 USD = apiRate units of this currency"
		// We want "1 unit of this currency = (1/apiRate) USD"
		var rateToUSD float64
		if apiRate == 0 {
			u.logger.Warn().
				Str("currency", currency).
				Msg("skipping currency with zero rate")
			continue
		}
		rateToUSD = 1.0 / apiRate

		rate := &models.ExchangeRate{
			Currency:  currency,
			RateToUSD: rateToUSD,
			RateDate:  rateDate,
		}

		if err := u.rateRepo.UpsertRate(ctx, rate); err != nil {
			u.logger.Error().
				Err(err).
				Str("currency", currency).
				Msg("failed to upsert rate")
			continue
		}
		upsertCount++
	}

	// Also upsert USD itself with rate 1.0 (1 USD = 1 USD)
	usdRate := &models.ExchangeRate{
		Currency:  "USD",
		RateToUSD: 1.0,
		RateDate:  rateDate,
	}
	if err := u.rateRepo.UpsertRate(ctx, usdRate); err != nil {
		u.logger.Error().Err(err).Msg("failed to upsert USD rate")
	} else {
		upsertCount++
	}

	u.logger.Info().
		Int("currencies_updated", upsertCount).
		Msg("exchange rates updated successfully")
}
