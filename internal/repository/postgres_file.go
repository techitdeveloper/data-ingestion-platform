package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
)

type pgxFileRepo struct {
	pool *pgxpool.Pool
}

func NewFileRepository(pool *pgxpool.Pool) FileRepository {
	return &pgxFileRepo{pool: pool}
}

func (r *pgxFileRepo) CreateFile(ctx context.Context, file *models.File) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO files (id, source_id, file_date, local_path, status, checksum)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		file.ID,
		file.SourceID,
		file.FileDate,
		file.LocalPath,
		file.Status,
		file.Checksum,
	)
	if err != nil {
		// Check if Postgres returned a unique violation (code 23505).
		// This is how you detect our UNIQUE(source_id, file_date) constraint.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateFile
		}
		return fmt.Errorf("inserting file: %w", err)
	}
	return nil
}

func (r *pgxFileRepo) GetFileBySourceAndDate(
	ctx context.Context,
	sourceID uuid.UUID,
	date time.Time,
) (*models.File, error) {
	var f models.File
	err := r.pool.QueryRow(ctx, `
		SELECT id, source_id, file_date, local_path, status, checksum, created_at
		FROM files
		WHERE source_id = $1 AND file_date = $2
	`, sourceID, date).Scan(
		&f.ID,
		&f.SourceID,
		&f.FileDate,
		&f.LocalPath,
		&f.Status,
		&f.Checksum,
		&f.CreatedAt,
	)
	if err != nil {
		// pgx returns pgx.ErrNoRows when nothing is found —
		// we translate that into our own ErrNotFound sentinel.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("querying file: %w", err)
	}
	return &f, nil
}

func (r *pgxFileRepo) UpdateFileStatus(
	ctx context.Context,
	fileID uuid.UUID,
	status models.FileStatus,
) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE files SET status = $1 WHERE id = $2`,
		status, fileID,
	)
	if err != nil {
		return fmt.Errorf("updating file status: %w", err)
	}
	return nil
}
