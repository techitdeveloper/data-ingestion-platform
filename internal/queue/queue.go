package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const jobQueueKey = "jobs:files"

// Job is the payload pushed onto the Redis queue after a successful download.
// The processor will deserialize this to know what file to process.
type Job struct {
	FileID   string `json:"file_id"`
	SourceID string `json:"source_id"`
	FilePath string `json:"file_path"`
}

// Queue defines the publishing and consuming contract.
type Queue interface {
	Publish(ctx context.Context, job Job) error
	Consume(ctx context.Context) (*Job, error)
}

type redisQueue struct {
	client *redis.Client
}

// NewRedisQueue constructs a Queue backed by Redis.
func NewRedisQueue(addr string) Queue {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &redisQueue{client: client}
}

// Publish serializes the job to JSON and pushes it to the LEFT of the list.
// The processor uses BRPOP (blocking right pop) — LPUSH+BRPOP = FIFO queue.
func (q *redisQueue) Publish(ctx context.Context, job Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshaling job: %w", err)
	}

	if err := q.client.LPush(ctx, jobQueueKey, payload).Err(); err != nil {
		return fmt.Errorf("pushing job to redis: %w", err)
	}
	return nil
}

// Consume blocks until a job is available, then deserializes and returns it.
// The timeout of 0 means block forever — the context controls cancellation.
func (q *redisQueue) Consume(ctx context.Context) (*Job, error) {
	// BRPOP returns [key, value] — we only care about value at index 1
	result, err := q.client.BRPop(ctx, 0, jobQueueKey).Result()
	if err != nil {
		return nil, fmt.Errorf("popping job from redis: %w", err)
	}

	var job Job
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return nil, fmt.Errorf("unmarshaling job: %w", err)
	}
	return &job, nil
}
