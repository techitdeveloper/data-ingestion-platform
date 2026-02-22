package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
)

// pgxSourceRepo is the real Postgres implementation of SourceRepository.
// Notice it's unexported — callers get it via NewSourceRepository().
type pgxSourceRepo struct {
	pool *pgxpool.Pool
}

// NewSourceRepository constructs a SourceRepository backed by Postgres.
// Returning the interface (not the struct) enforces the abstraction.
func NewSourceRepository(pool *pgxpool.Pool) SourceRepository {
	return &pgxSourceRepo{pool: pool}
}

func (r *pgxSourceRepo) GetActiveSources(ctx context.Context) ([]models.Source, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, download_url, file_type, schedule_time, active, created_at
		FROM sources
		WHERE active = TRUE
	`)
	if err != nil {
		return nil, fmt.Errorf("querying sources: %w", err)
	}
	defer rows.Close()

	var sources []models.Source
	for rows.Next() {
		var s models.Source
		err := rows.Scan(
			&s.ID,
			&s.Name,
			&s.DownloadURL,
			&s.FileType,
			&s.ScheduleTime,
			&s.Active,
			&s.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning source row: %w", err)
		}
		sources = append(sources, s)
	}

	// rows.Err() catches errors that happened mid-iteration —
	// always check this after iterating pgx rows.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating source rows: %w", err)
	}

	return sources, nil
}
