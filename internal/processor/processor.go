package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/models"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
	csvparser "github.com/techitdeveloper/data-ingestion-platform/pkg/csv"
	"github.com/techitdeveloper/data-ingestion-platform/pkg/trace"
)

const (
	batchSize       = 500
	requiredDateCol = "date"
	requiredRevCol  = "revenue"
	requiredCurrCol = "currency"
)

var requiredColumns = []string{requiredDateCol, requiredRevCol, requiredCurrCol}

// Job is what the worker receives — already deserialized from Redis.
type Job struct {
	FileID   string
	SourceID string
	FilePath string
}

// Processor handles the full processing lifecycle for a single file.
// One Processor instance is shared across all workers — it is stateless,
// so this is safe. All state lives in the DB.
type Processor struct {
	fileRepo    repository.FileRepository
	rateRepo    repository.ExchangeRateRepository
	revenueRepo repository.RevenueRepository
	logger      zerolog.Logger
}

func New(
	fileRepo repository.FileRepository,
	rateRepo repository.ExchangeRateRepository,
	revenueRepo repository.RevenueRepository,
	logger zerolog.Logger,
) *Processor {
	return &Processor{
		fileRepo:    fileRepo,
		rateRepo:    rateRepo,
		revenueRepo: revenueRepo,
		logger:      logger,
	}
}

// Process is the entry point for a single job.
// It is safe to call concurrently from multiple goroutines.
func (p *Processor) Process(ctx context.Context, job Job) error {
	fileID, err := uuid.Parse(job.FileID)
	if err != nil {
		return fmt.Errorf("invalid file_id: %w", err)
	}
	sourceID, err := uuid.Parse(job.SourceID)
	if err != nil {
		return fmt.Errorf("invalid source_id: %w", err)
	}

	requestID := trace.GetRequestID(ctx)

	log := p.logger.With().
		Str("file_id", job.FileID).
		Str("source_id", job.SourceID).
		Str("request_id", requestID).
		Logger()

	log.Info().Msg("processing started")

	// Mark as processing — if we crash after this point, the reconciler
	// (Phase 6) will detect this file is stuck and requeue it.
	if err := p.fileRepo.UpdateFileStatus(ctx, fileID, models.FileStatusProcessing); err != nil {
		return fmt.Errorf("marking file as processing: %w", err)
	}

	// Do the actual work — if this fails, mark file as failed
	if err := p.processFile(ctx, fileID, sourceID, job.FilePath, log); err != nil {
		// Best-effort status update — we log if it fails but return the
		// original error so the worker knows the job failed
		if statusErr := p.fileRepo.UpdateFileStatus(ctx, fileID, models.FileStatusFailed); statusErr != nil {
			log.Error().Err(statusErr).Msg("failed to mark file as failed after processing error")
		}
		return fmt.Errorf("processing file: %w", err)
	}

	if err := p.fileRepo.UpdateFileStatus(ctx, fileID, models.FileStatusCompleted); err != nil {
		// File was processed successfully but we couldn't mark it complete.
		// Log it — the reconciler handles this case.
		log.Error().Err(err).Msg("processed successfully but failed to mark completed")
	}

	log.Info().Msg("processing completed")
	return nil
}

// processFile streams the CSV, converts currencies, and batch-inserts revenues.
func (p *Processor) processFile(
	ctx context.Context,
	fileID uuid.UUID,
	sourceID uuid.UUID,
	filePath string,
	log zerolog.Logger,
) error {
	parser := csvparser.NewParser(filePath)
	rowChan, errChan := parser.Stream(requiredColumns)

	var (
		batch       []models.Revenue
		totalRows   int
		skippedRows int
	)

	for row := range rowChan {
		// Check context on every row — if shutdown signal arrives mid-file,
		// we stop cleanly rather than continuing to process.
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled mid-file: %w", ctx.Err())
		}

		revenue, err := p.buildRevenue(ctx, row, sourceID)
		if err != nil {
			// Log and skip bad rows — one bad row shouldn't kill the whole file
			log.Warn().
				Err(err).
				Int("line", row.LineNumber).
				Msg("skipping row")
			skippedRows++
			continue
		}

		batch = append(batch, *revenue)
		totalRows++

		// Flush batch when it reaches batchSize
		if len(batch) >= batchSize {
			if err := p.revenueRepo.BatchInsert(ctx, batch); err != nil {
				return fmt.Errorf("batch insert at row %d: %w", totalRows, err)
			}
			log.Debug().
				Int("rows_flushed", len(batch)).
				Int("total_so_far", totalRows).
				Msg("batch flushed")
			// Reset slice but reuse the underlying array memory
			batch = batch[:0]
		}
	}

	// Check if the streaming goroutine exited with an error
	if err := <-errChan; err != nil {
		return fmt.Errorf("csv streaming error: %w", err)
	}

	// Flush any remaining rows that didn't fill a full batch
	if len(batch) > 0 {
		if err := p.revenueRepo.BatchInsert(ctx, batch); err != nil {
			return fmt.Errorf("final batch insert: %w", err)
		}
	}

	log.Info().
		Int("total_rows", totalRows).
		Int("skipped_rows", skippedRows).
		Msg("file processing stats")

	return nil
}

// buildRevenue parses a single CSV row into a Revenue model.
// It fetches the exchange rate and converts the value to USD.
func (p *Processor) buildRevenue(
	ctx context.Context,
	row csvparser.Row,
	sourceID uuid.UUID,
) (*models.Revenue, error) {
	// Parse date
	dateStr := row.Headers[requiredDateCol]
	revenueDate, err := parseDate(dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q: %w", dateStr, err)
	}

	// Parse revenue amount
	revenueStr := row.Headers[requiredRevCol]
	originalValue, err := strconv.ParseFloat(revenueStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid revenue %q: %w", revenueStr, err)
	}

	// Get currency
	currency := row.Headers[requiredCurrCol]
	if currency == "" {
		return nil, fmt.Errorf("empty currency")
	}

	// Fetch exchange rate for this currency on the revenue date
	rate, err := p.rateRepo.GetRate(ctx, currency, revenueDate)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("no exchange rate for currency %q on %s", currency, dateStr)
		}
		return nil, fmt.Errorf("fetching exchange rate: %w", err)
	}

	// Convert to USD: if rate is "how many USD per 1 unit of currency"
	usdValue := originalValue * rate.RateToUSD

	// Serialize the entire row as JSONB — preserves all columns including
	// ones we don't explicitly use, for auditing and debugging
	rawPayload, err := json.Marshal(row.Headers)
	if err != nil {
		return nil, fmt.Errorf("marshaling raw payload: %w", err)
	}

	return &models.Revenue{
		ID:            uuid.New(),
		SourceID:      sourceID,
		RevenueDate:   revenueDate,
		OriginalValue: originalValue,
		Currency:      currency,
		USDValue:      usdValue,
		RawPayload:    rawPayload,
	}, nil
}

// parseDate tries multiple common date formats — real world files are inconsistent.
func parseDate(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"02/01/2006",
		"01/02/2006",
		"2006/01/02",
		"02-01-2006",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date format: %q", s)
}
