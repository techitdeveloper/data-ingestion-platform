package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
)

// SourceRepository defines all DB operations related to sources.
// Any struct that implements these methods satisfies this interface.
type SourceRepository interface {
	GetActiveSources(ctx context.Context) ([]models.Source, error)
}

// FileRepository defines all DB operations related to files.
type FileRepository interface {
	// CreateFile inserts a new file record. Returns ErrDuplicateFile if
	// the source already has a file for that date (UNIQUE constraint).
	CreateFile(ctx context.Context, file *models.File) error

	// GetFileBySourceAndDate checks if a file already exists for that day.
	GetFileBySourceAndDate(ctx context.Context, sourceID uuid.UUID, date time.Time) (*models.File, error)

	// UpdateFileStatus changes the status column of a file record.
	UpdateFileStatus(ctx context.Context, fileID uuid.UUID, status models.FileStatus) error
}
