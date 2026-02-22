package processor

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/rs/zerolog"
	"github.com/techitdeveloper/data-ingestion-platform/internal/queue"
	"github.com/techitdeveloper/data-ingestion-platform/internal/repository"
	"github.com/techitdeveloper/data-ingestion-platform/pkg/trace"
)

// WorkerPool manages N goroutines that each consume jobs from Redis
// and process them concurrently.
//
// Design:
//
//	Redis → [Consume goroutine] → jobChan → [Worker 1]
//	                                      → [Worker 2]
//	                                      → [Worker N]
//
// The consume goroutine feeds jobs into a buffered channel.
// Each worker goroutine reads from that channel independently.
// sync.WaitGroup lets us wait for all workers to finish before exiting.
type WorkerPool struct {
	processor   *Processor
	queue       queue.Queue
	fileRepo    repository.FileRepository
	workerCount int
	logger      zerolog.Logger
}

func NewWorkerPool(
	processor *Processor,
	q queue.Queue,
	fileRepo repository.FileRepository,
	workerCount int,
	logger zerolog.Logger,
) *WorkerPool {
	return &WorkerPool{
		processor:   processor,
		queue:       q,
		fileRepo:    fileRepo,
		workerCount: workerCount,
		logger:      logger,
	}
}

// Run starts N workers and one consumer goroutine, then blocks until
// ctx is cancelled. On shutdown it waits for all in-flight jobs to finish.
func (wp *WorkerPool) Run(ctx context.Context) {
	// Buffered channel — holds up to workerCount jobs before blocking the consumer.
	// This gives workers a small buffer so they don't sit idle waiting for the
	// next Redis pop.
	jobChan := make(chan Job, wp.workerCount)

	var wg sync.WaitGroup

	// Start N worker goroutines.
	// Each one loops: read from jobChan → process → repeat.
	for i := range wp.workerCount {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			wp.runWorker(ctx, workerID, jobChan)
		}(i)
	}

	// Start the consumer goroutine — pulls jobs from Redis and feeds jobChan.
	// When ctx is cancelled, this goroutine returns and closes jobChan.
	// Closing jobChan signals all workers to exit their range loops.
	go wp.consumeJobs(ctx, jobChan)

	// Block here until all workers have finished their current jobs.
	// This is graceful shutdown — we never kill a job mid-processing.
	wg.Wait()
	wp.logger.Info().Msg("all workers exited cleanly")
}

// consumeJobs reads from Redis and forwards to the job channel.
// When ctx is cancelled it closes the channel, which drains the workers.
func (wp *WorkerPool) consumeJobs(ctx context.Context, jobChan chan<- Job) {
	defer close(jobChan) // closing this drains all workers cleanly

	for {
		// Consume is a blocking call — it waits until a job is available.
		// It returns immediately if ctx is cancelled (BRPOP respects context).
		queueJob, err := wp.queue.Consume(ctx)
		if err != nil {
			// Context cancelled = normal shutdown, not an error
			if ctx.Err() != nil {
				wp.logger.Info().Msg("consumer shutting down")
				return
			}
			wp.logger.Error().Err(err).Msg("error consuming from queue, retrying")
			continue
		}

		job := Job{
			FileID:   queueJob.FileID,
			SourceID: queueJob.SourceID,
			FilePath: queueJob.FilePath,
		}

		// Send job to workers. If all workers are busy, this blocks here
		// until one becomes free — natural backpressure.
		select {
		case jobChan <- job:
			// job dispatched
		case <-ctx.Done():
			wp.logger.Info().Msg("consumer context done, dropping job")
			return
		}
	}
}

// runWorker is the logic each individual worker goroutine runs.
// It processes jobs until jobChan is closed.
func (wp *WorkerPool) runWorker(ctx context.Context, workerID int, jobChan <-chan Job) {
	log := wp.logger.With().Int("worker_id", workerID).Logger()
	log.Info().Msg("worker started")

	// range over channel — this loop exits automatically when
	// consumeJobs closes jobChan. Clean and idiomatic.
	for job := range jobChan {
		jobCtx := trace.WithRequestID(ctx)
		requestID := trace.GetRequestID(jobCtx)
		log.Info().
			Str("file_id", job.FileID).
			Msg("worker picked up job")

		if err := wp.processor.Process(ctx, job); err != nil {
			log.Error().
				Err(err).
				Str("file_id", job.FileID).
				Str("request_id", requestID).
				Msg("job processing failed")
			// Job stays as "failed" in DB — reconciler or manual retry handles it
		}
	}

	log.Info().Msg("worker exited")
}

// unmarshalJob is a helper used if you extend this to raw JSON payloads.
func unmarshalJob(data string) (Job, error) {
	var j Job
	err := json.Unmarshal([]byte(data), &j)
	return j, err
}
