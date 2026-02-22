package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
)

// RevenueRepository defines DB operations for revenue records.
type RevenueRepository interface {
	// BatchInsert inserts multiple revenue rows in a single transaction.
	// All rows succeed or all fail — no partial inserts.
	BatchInsert(ctx context.Context, revenues []models.Revenue) error
}

type pgxRevenueRepo struct {
	pool *pgxpool.Pool
}

func NewRevenueRepository(pool *pgxpool.Pool) RevenueRepository {
	return &pgxRevenueRepo{pool: pool}
}

func (r *pgxRevenueRepo) BatchInsert(ctx context.Context, revenues []models.Revenue) error {
	if len(revenues) == 0 {
		return nil
	}

	// pgx.Batch lets us queue multiple statements and send them to Postgres
	// in a single network round-trip — far more efficient than individual Exec calls.
	batch := &pgx.Batch{}

	for _, rev := range revenues {
		batch.Queue(`
			INSERT INTO revenues
				(id, source_id, revenue_date, original_value, currency, usd_value, raw_payload)
			VALUES
				($1, $2, $3, $4, $5, $6, $7)
		`,
			rev.ID,
			rev.SourceID,
			rev.RevenueDate,
			rev.OriginalValue,
			rev.Currency,
			rev.USDValue,
			rev.RawPayload,
		)
	}

	// Send the entire batch in one round-trip
	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	// We must read each result — this is how pgx confirms each statement succeeded
	for i := range revenues {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch insert failed at row %d: %w", i, err)
		}
	}

	return nil
}
