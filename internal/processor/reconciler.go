package processor

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
	"github.com/techitdeveloper/data-ingestion-platform/internal/queue"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
)

const (
	// Files stuck in "processing" for longer than this are considered crashed
	stuckThreshold = 30 * time.Minute
	// How often to check for stuck files
	reconcileInterval = 5 * time.Minute
)

// Reconciler finds stuck jobs and requeues them.
// It runs in its own goroutine alongside the worker pool.
type Reconciler struct {
	fileRepo repository.FileRepository
	queue    queue.Queue
	logger   zerolog.Logger
}

func NewReconciler(
	fileRepo repository.FileRepository,
	q queue.Queue,
	logger zerolog.Logger,
) *Reconciler {
	return &Reconciler{
		fileRepo: fileRepo,
		queue:    q,
		logger:   logger,
	}
}

// Run starts the reconciliation loop. It runs immediately on start,
// then every reconcileInterval.
func (r *Reconciler) Run(ctx context.Context) {
	r.logger.Info().Msg("reconciler started")

	// Run immediately
	r.reconcile(ctx)

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.reconcile(ctx)
		case <-ctx.Done():
			r.logger.Info().Msg("reconciler shutting down")
			return
		}
	}
}

// reconcile finds stuck files and requeues them.
func (r *Reconciler) reconcile(ctx context.Context) {
	r.logger.Debug().Msg("checking for stuck files")

	stuckFiles, err := r.findStuckFiles(ctx)
	if err != nil {
		r.logger.Error().Err(err).Msg("failed to find stuck files")
		return
	}

	if len(stuckFiles) == 0 {
		r.logger.Debug().Msg("no stuck files found")
		return
	}

	r.logger.Warn().
		Int("count", len(stuckFiles)).
		Msg("found stuck files, requeuing")

	for _, file := range stuckFiles {
		if err := r.requeueFile(ctx, file); err != nil {
			r.logger.Error().
				Err(err).
				Str("file_id", file.ID.String()).
				Msg("failed to requeue stuck file")
		}
	}
}

// findStuckFiles queries for files that have been "processing" for too long.
func (r *Reconciler) findStuckFiles(ctx context.Context) ([]models.File, error) {
	return r.fileRepo.GetStuckFiles(ctx, stuckThreshold)
}

// requeueFile resets a stuck file to "downloaded" and pushes a new job.
func (r *Reconciler) requeueFile(ctx context.Context, file models.File) error {
	// Reset status
	if err := r.fileRepo.UpdateFileStatus(ctx, file.ID, models.FileStatusDownloaded); err != nil {
		return fmt.Errorf("resetting file status: %w", err)
	}

	// Re-enqueue
	job := queue.Job{
		FileID:   file.ID.String(),
		SourceID: file.SourceID.String(),
		FilePath: file.LocalPath,
	}
	if err := r.queue.Publish(ctx, job); err != nil {
		return fmt.Errorf("publishing requeue job: %w", err)
	}

	r.logger.Info().
		Str("file_id", file.ID.String()).
		Msg("successfully requeued stuck file")

	return nil
}
