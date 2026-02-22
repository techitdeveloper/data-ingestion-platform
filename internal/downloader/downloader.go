package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
	"github.com/techitdeveloper/data-ingestion-platform/internal/queue"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
)

// Downloader orchestrates: check → download → store → register → enqueue.
type Downloader struct {
	sourceRepo    repository.SourceRepository
	fileRepo      repository.FileRepository
	queue         queue.Queue
	dataDirectory string
	httpClient    *http.Client
	logger        zerolog.Logger
}

func New(
	sourceRepo repository.SourceRepository,
	fileRepo repository.FileRepository,
	q queue.Queue,
	dataDir string,
	logger zerolog.Logger,
) *Downloader {
	return &Downloader{
		sourceRepo:    sourceRepo,
		fileRepo:      fileRepo,
		queue:         q,
		dataDirectory: dataDir,
		// Always set a timeout on HTTP clients — never use http.DefaultClient
		// in production because it has no timeout and can leak goroutines.
		httpClient: &http.Client{Timeout: 60 * time.Second},
		logger:     logger,
	}
}

// Run is the main scheduler loop. It ticks every minute and checks
// whether any source is due for download.
func (d *Downloader) Run(ctx context.Context) {
	d.logger.Info().Msg("downloader scheduler started")

	// Run once immediately on startup, then every minute
	d.checkAndDownloadAll(ctx)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.checkAndDownloadAll(ctx)
		case <-ctx.Done():
			d.logger.Info().Msg("downloader shutting down")
			return
		}
	}
}

// checkAndDownloadAll loads all active sources and attempts to download
// each one that hasn't been downloaded today yet.
func (d *Downloader) checkAndDownloadAll(ctx context.Context) {
	sources, err := d.sourceRepo.GetActiveSources(ctx)
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to fetch sources")
		return
	}

	for _, source := range sources {
		// Each source is processed independently — one failure
		// doesn't block the others.
		if err := d.processSource(ctx, source); err != nil {
			d.logger.Error().
				Err(err).
				Str("source_id", source.ID.String()).
				Str("source_name", source.Name).
				Msg("failed to process source")
		}
	}
}

// processSource handles the full lifecycle for a single source.
func (d *Downloader) processSource(ctx context.Context, source models.Source) error {
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Idempotency check — has this source already been downloaded today?
	existing, err := d.fileRepo.GetFileBySourceAndDate(ctx, source.ID, today)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("checking existing file: %w", err)
	}
	if existing != nil {
		d.logger.Debug().
			Str("source", source.Name).
			Msg("already downloaded today, skipping")
		return nil
	}

	d.logger.Info().
		Str("source", source.Name).
		Str("url", source.DownloadURL).
		Msg("starting download")

	// Build the local file path: /data/<source_name>/<date>.<ext>
	localPath, err := d.buildLocalPath(source, today)
	if err != nil {
		return fmt.Errorf("building local path: %w", err)
	}

	// Download with retry + exponential backoff
	checksum, err := d.downloadWithRetry(ctx, source.DownloadURL, localPath)
	if err != nil {
		return fmt.Errorf("downloading file: %w", err)
	}

	// Register the file in DB
	file := &models.File{
		ID:        uuid.New(),
		SourceID:  source.ID,
		FileDate:  today,
		LocalPath: localPath,
		Status:    models.FileStatusDownloaded,
		Checksum:  checksum,
	}

	if err := d.fileRepo.CreateFile(ctx, file); err != nil {
		if errors.Is(err, repository.ErrDuplicateFile) {
			// Another downloader instance raced us — this is fine, not an error.
			d.logger.Warn().
				Str("source", source.Name).
				Msg("race condition: file already registered, skipping")
			return nil
		}
		return fmt.Errorf("registering file in db: %w", err)
	}

	// Push job to Redis queue for the processor to pick up
	job := queue.Job{
		FileID:   file.ID.String(),
		SourceID: source.ID.String(),
		FilePath: localPath,
	}
	if err := d.queue.Publish(ctx, job); err != nil {
		// DB record exists but job not queued — the reconciler will fix this.
		// We log it but don't fail the whole operation.
		d.logger.Error().
			Err(err).
			Str("file_id", file.ID.String()).
			Msg("file registered but failed to enqueue — reconciler will retry")
		return nil
	}

	d.logger.Info().
		Str("source", source.Name).
		Str("file_id", file.ID.String()).
		Str("checksum", checksum).
		Msg("download complete and job enqueued")

	return nil
}

// downloadWithRetry downloads a file to disk using exponential backoff.
// It streams the response body directly to disk — never loads it fully in memory.
func (d *Downloader) downloadWithRetry(ctx context.Context, url, localPath string) (string, error) {
	var checksum string

	// backoff.WithContext respects context cancellation — if the service
	// shuts down mid-retry, the backoff stops cleanly.
	operation := func() error {
		sum, err := d.downloadFile(ctx, url, localPath)
		if err != nil {
			return err // backoff will retry on any error
		}
		checksum = sum
		return nil
	}

	b := backoff.WithContext(
		backoff.NewExponentialBackOff(),
		ctx,
	)

	if err := backoff.Retry(operation, b); err != nil {
		return "", fmt.Errorf("all retries exhausted: %w", err)
	}

	return checksum, nil
}

// downloadFile performs a single HTTP GET, writes to disk, and returns SHA256.
func (d *Downloader) downloadFile(ctx context.Context, url, localPath string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Create the file on disk
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("creating local file: %w", err)
	}
	defer f.Close()

	// io.TeeReader streams the body to disk AND through the hasher simultaneously.
	// This is the key Go streaming pattern — we compute the checksum while
	// writing, making a single pass over the data without loading it all in memory.
	hasher := sha256.New()
	tee := io.TeeReader(resp.Body, hasher)

	if _, err := io.Copy(f, tee); err != nil {
		return "", fmt.Errorf("writing file to disk: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	return checksum, nil
}

// buildLocalPath constructs: /data/<source_name>/<YYYY-MM-DD>.<ext>
func (d *Downloader) buildLocalPath(source models.Source, date time.Time) (string, error) {
	ext := source.FileType
	dir := filepath.Join(d.dataDirectory, source.Name)
	filename := fmt.Sprintf("%s.%s", date.Format("2006-01-02"), ext)
	return filepath.Join(dir, filename), nil
}
