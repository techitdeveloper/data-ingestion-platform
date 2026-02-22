# Architecture Deep Dive

This document explains the system's architecture, design patterns, and key decisions.

## Table of Contents

- [System Overview](#system-overview)
- [Component Design](#component-design)
- [Data Flow](#data-flow)
- [Concurrency Model](#concurrency-model)
- [Fault Tolerance](#fault-tolerance)
- [Performance Optimizations](#performance-optimizations)
- [Security Considerations](#security-considerations)

---

## System Overview

### Architecture Style: Distributed Modular Monolith

This isn't a microservices architecture (no network calls between services), and it isn't a traditional monolith (services can be deployed independently). It's a **modular monolith** where:

- **Single codebase** with clear module boundaries
- **Multiple independent binaries** that can be deployed separately
- **Shared database** as the source of truth
- **Message queue** (Redis) for async communication
- **Stateless services** that can scale horizontally

### Why This Pattern?

| Concern | Microservices | Monolith | Modular Monolith |
|---------|--------------|----------|------------------|
| Deployment independence | ✅ | ❌ | ✅ |
| Data consistency | ❌ (eventual) | ✅ (ACID) | ✅ (ACID) |
| Network latency | ❌ (high) | ✅ (none) | ✅ (none) |
| Operational complexity | ❌ (high) | ✅ (low) | ✅ (moderate) |
| Scalability | ✅ | ❌ | ✅ (per-service) |

We get the best of both worlds: independent deployment and scaling, but with shared-database simplicity.

---

## Component Design

### 1. Downloader Service

**Responsibility:** Orchestrates file downloads from external sources.

**Key Patterns:**
- **Scheduler pattern** — runs on a ticker, checks if work is due
- **Idempotency** — uses DB constraint `UNIQUE(source_id, file_date)` to prevent duplicates
- **Retry with backoff** — network failures trigger exponential backoff
- **Streaming I/O** — uses `io.TeeReader` to compute checksum while writing to disk

**Code Flow:**
```
main() 
  → Run()
    → checkAndDownloadAll()  [every 1 min]
      → processSource()  [for each source]
        → GetFileBySourceAndDate()  [idempotency check]
        → downloadWithRetry()
          → downloadFile()  [HTTP GET + SHA256]
        → CreateFile()  [DB insert]
        → queue.Publish()  [push to Redis]
```

**Concurrency:** None within a single instance (processes sources sequentially). Can run multiple instances — DB constraint prevents duplicates.

**State:** Stateless — all state in DB.

---

### 2. Processor Service

**Responsibility:** Consumes jobs from Redis, parses CSVs, converts currencies, batch-inserts to DB.

**Key Patterns:**
- **Worker pool** — N goroutines consuming from a shared channel
- **Batch processing** — accumulates rows and inserts in groups of 500
- **Streaming parser** — CSV rows streamed via channel, never loads full file
- **Graceful shutdown** — waits for in-flight jobs to finish
- **Request ID tracing** — propagates trace ID through context

**Code Flow:**
```
main()
  → NewWorkerPool()
    → Run()
      → consumeJobs()  [1 goroutine]
        → queue.Consume()  [blocking pop from Redis]
        → jobChan <- job  [dispatch to workers]
      → runWorker() [N goroutines]
        → Process()
          → processFile()
            → CSV parser streams rows
            → buildRevenue()  [fetch exchange rate, convert to USD]
            → BatchInsert()  [every 500 rows]
```

**Concurrency Model:**

```
                    ┌─────────────┐
                    │   Redis     │
                    └──────┬──────┘
                           │ BRPOP
                    ┌──────▼──────┐
                    │  Consumer   │  [1 goroutine]
                    │  Goroutine  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │   jobChan   │  [buffered channel, size = worker count]
                    └──┬───┬───┬──┘
                       │   │   │
            ┌──────────┘   │   └──────────┐
            │              │              │
       ┌────▼────┐    ┌────▼────┐   ┌────▼────┐
       │ Worker  │    │ Worker  │   │ Worker  │
       │    1    │    │    2    │   │    N    │
       └────┬────┘    └────┬────┘   └────┬────┘
            │              │              │
            └──────────────┼──────────────┘
                           │
                    ┌──────▼──────┐
                    │  Postgres   │
                    └─────────────┘
```

**Shutdown Sequence:**
1. Context cancelled (signal received)
2. Consumer stops pulling from Redis
3. Consumer closes `jobChan`
4. Workers finish current jobs, exit when channel closes
5. `WaitGroup` releases
6. Health server shuts down
7. Process exits

**State:** Stateless — all state in DB. Jobs tracked in Redis queue and DB `files.status`.

---

### 3. Exchanger Service

**Responsibility:** Fetches daily exchange rates from external API and stores in DB.

**Key Patterns:**
- **Idempotent upsert** — `ON CONFLICT DO UPDATE` makes it safe to run multiple times
- **HTTP client with timeout** — prevents hanging requests
- **Daily scheduler** — runs immediately on start, then every 24 hours
- **Rate inversion** — API gives "USD per X", we store "X per USD"

**Code Flow:**
```
main()
  → NewUpdater()
    → Run()
      → updateRates()  [immediately, then daily]
        → client.FetchRates()  [HTTP GET]
        → UpsertRate()  [for each currency]
```

**Concurrency:** None needed — single daily task.

**State:** Stateless — all state in DB.

---

### 4. Reconciler (part of Processor)

**Responsibility:** Detects jobs that started processing but never completed (crashed worker).

**Key Patterns:**
- **Background reconciliation** — runs independently in its own goroutine
- **Time-based detection** — anything in "processing" > 30 minutes is stuck
- **Automatic retry** — resets status and re-enqueues

**Code Flow:**
```
main()
  → NewReconciler()
    → Run()
      → reconcile()  [every 5 min]
        → GetStuckFiles()  [status = processing, created_at < NOW - 30 min]
        → requeueFile()  [for each stuck file]
          → UpdateFileStatus(downloaded)
          → queue.Publish()
```

**Why Needed?**

Without reconciler:
```
Worker starts processing → Worker crashes → File stuck forever as "processing"
```

With reconciler:
```
Worker crashes → 30 min passes → Reconciler detects stuck file → Requeue → Different worker picks it up
```

---

## Data Flow

### End-to-End Journey of a Revenue Record

```
1. Exchanger fetches rates
   ├─ HTTP GET https://api.exchangerate-api.com/v4/latest/USD
   ├─ Invert rates (1/X)
   └─ INSERT INTO exchange_rates ... ON CONFLICT DO UPDATE

2. Source exists in DB
   └─ INSERT INTO sources (manually seeded or via admin API)

3. Downloader wakes up (every 1 min)
   ├─ SELECT * FROM sources WHERE active = TRUE
   ├─ For each source:
   │   ├─ Check: SELECT * FROM files WHERE source_id = ? AND file_date = TODAY
   │   ├─ If not exists:
   │   │   ├─ HTTP GET download_url
   │   │   ├─ Compute SHA256 checksum
   │   │   ├─ INSERT INTO files (status = 'downloaded')
   │   │   └─ LPUSH jobs:files {file_id, source_id, file_path}

4. Processor worker consumes job
   ├─ BRPOP jobs:files (blocks until job available)
   ├─ UPDATE files SET status = 'processing'
   ├─ Open CSV file
   ├─ Stream rows via channel
   ├─ For each row:
   │   ├─ Parse date, revenue, currency
   │   ├─ SELECT rate_to_usd FROM exchange_rates WHERE currency = ? AND rate_date = ?
   │   ├─ Calculate: usd_value = original_value * rate_to_usd
   │   ├─ Accumulate in batch (up to 500 rows)
   │   └─ When batch full: INSERT INTO revenues (id, source_id, ...) VALUES (batch)
   ├─ Flush remaining rows
   └─ UPDATE files SET status = 'completed'

5. Data is now queryable
   └─ SELECT * FROM revenues WHERE revenue_date = '2026-02-15'
```

---

## Concurrency Model

### Goroutines in the System

| Service | Goroutine Count | Purpose |
|---------|----------------|---------|
| Exchanger | 2 | Main loop + HTTP server |
| Downloader | 2 | Main loop + HTTP server |
| Processor | N+3 | N workers + consumer + reconciler + HTTP server |

### Channel Usage Patterns

#### Pattern 1: Generator (CSV Parser)

```go
// Producer goroutine
go func() {
    defer close(rowChan)
    for {
        row := readCSVRow()
        rowChan <- row
    }
}()

// Consumer (main goroutine)
for row := range rowChan {
    process(row)
}
```

**Why:** Decouples file reading from business logic. Producer can be slow (disk I/O) without blocking consumer.

#### Pattern 2: Worker Pool (Processor)

```go
jobChan := make(chan Job, workerCount)

// N workers
for i := 0; i < workerCount; i++ {
    go worker(jobChan)
}

// 1 producer
go func() {
    for {
        job := queue.Consume()
        jobChan <- job
    }
}()
```

**Why:** 
- Buffered channel provides small buffer so workers don't sit idle
- Closing channel cleanly signals all workers to exit
- Natural load balancing — free worker picks up next job

#### Pattern 3: Fan-Out (Downloader)

```go
for _, source := range sources {
    go processSource(source)  // Each source processed in parallel
}
```

**Why:** Sources are independent — parallelizing speeds up total download time.

**Note:** We don't actually use this pattern in the current implementation (sources are processed sequentially), but it's a natural extension point.

---

## Fault Tolerance

### Failure Modes and Mitigations

| Failure | Impact Without Mitigation | How We Handle It |
|---------|---------------------------|------------------|
| Worker crash mid-file | File stuck "processing" forever | Reconciler detects and requeues |
| DB connection lost | All writes fail | Connection pool retries, health check fails |
| Redis connection lost | Jobs can't be published/consumed | Health check fails, queue consumer retries |
| Network timeout on download | Download fails | Exponential backoff, retry up to 5 times |
| Corrupt CSV row | Entire file fails | Skip row, log warning, continue processing |
| Missing exchange rate | All rows fail | Fail job, mark file as "failed" |
| Duplicate download | Same file downloaded twice | DB constraint rejects duplicate, log race condition |
| Partial insert | Some rows inserted, then crash | Single transaction — all or nothing |

### Idempotency Guarantees

**Downloader:**
```sql
-- Running twice on same day won't duplicate
INSERT INTO files (source_id, file_date, ...)
-- Raises error due to UNIQUE(source_id, file_date)
```

**Exchanger:**
```sql
-- Running multiple times just updates rates
INSERT INTO exchange_rates (currency, rate_date, rate_to_usd)
VALUES (...)
ON CONFLICT (currency, rate_date)
DO UPDATE SET rate_to_usd = EXCLUDED.rate_to_usd
```

**Processor:**
```go
// Check before processing
existing, err := fileRepo.GetFileBySourceAndDate(ctx, sourceID, date)
if existing != nil && existing.Status == "completed" {
    return nil  // Already done
}
```

---

## Performance Optimizations

### 1. Batch Inserts

**Problem:** Individual INSERT per row = N network round-trips for N rows.

**Solution:** Accumulate 500 rows, send in one batch.

```go
batch := &pgx.Batch{}
for _, row := range rows {
    batch.Queue("INSERT INTO revenues (...) VALUES (...)", row...)
}
pool.SendBatch(ctx, batch)
```

**Impact:** 500x reduction in round-trips. 10,000 row file goes from 10,000 queries to 20 queries.

### 2. Streaming CSV Parsing

**Problem:** `ioutil.ReadFile()` loads entire file into memory.

**Solution:** Stream via `encoding/csv.Reader` over `os.File`.

```go
f, _ := os.Open(path)
reader := csv.NewReader(f)
for {
    record, err := reader.Read()
    // Process one row at a time
}
```

**Impact:** Constant memory usage regardless of file size.

### 3. Connection Pooling

**Problem:** Opening a new DB connection per query is slow (TCP handshake, auth, etc.).

**Solution:** `pgxpool` maintains a pool of open connections.

```go
pool, err := pgxpool.New(ctx, dbURL)
// Reuses connections across all workers
```

**Impact:** ~10ms saved per query (no connection overhead).

### 4. Buffered Channels

**Problem:** Unbuffered channel causes worker to block until next job arrives.

**Solution:** Buffer = worker count. Workers can grab next job immediately.

```go
jobChan := make(chan Job, workerCount)
```

**Impact:** Eliminates idle time when queue has multiple jobs.

---

## Security Considerations

### Current State (Development)

- Database password in `.env` (not committed to git)
- No authentication on health endpoints (anyone can hit them)
- Redis has no password
- Downloaded files stored locally (no encryption at rest)
- HTTP API calls without TLS verification

### Production Hardening Recommendations

1. **Secrets Management**
   - Use AWS Secrets Manager, HashiCorp Vault, or similar
   - Rotate credentials regularly
   - Never log secrets

2. **Network Security**
   - Run services in a private VPC/network
   - Only expose health checks to load balancer
   - Use TLS for all DB connections (`sslmode=require`)
   - Add Redis password (`AUTH` command)

3. **Authentication & Authorization**
   - Add API keys or OAuth for health endpoints
   - Implement RBAC if building admin UI
   - Log all access attempts

4. **Input Validation**
   - Validate CSV structure before processing
   - Sanitize file paths (prevent directory traversal)
   - Limit file size (prevent disk exhaustion)
   - Rate-limit external API calls

5. **Data Protection**
   - Encrypt sensitive data in DB (PII, financial data)
   - Encrypt files at rest (use encrypted volumes)
   - Implement data retention policies
   - Add audit logging

6. **Denial of Service Prevention**
   - Add rate limiting on download endpoints
   - Limit concurrent workers (prevent resource exhaustion)
   - Set max file size (prevent memory exhaustion)
   - Implement circuit breakers for external APIs

---

## Scaling Considerations

### Vertical Scaling (Per Instance)

| Component | Bottleneck | How to Scale |
|-----------|-----------|--------------|
| Processor | CPU (parsing) | Increase worker count |
| Downloader | Network I/O | Multiple instances |
| Exchanger | API rate limit | Singleton, cache results |
| Database | Connections | Increase `max_connections` |
| Redis | Memory | Increase RAM, use persistent storage |

### Horizontal Scaling (Multiple Instances)

**Processor:**
- ✅ Safe to run N instances
- Each instance runs `workerCount` workers
- Total workers = `N * workerCount`
- Redis queue naturally load-balances

**Downloader:**
- ✅ Safe to run N instances
- DB constraint prevents duplicate downloads
- Race condition is harmless (one wins, others get error)

**Exchanger:**
- ⚠️ Should run as singleton (or use distributed lock)
- Multiple instances will make redundant API calls
- Idempotent upsert makes it safe, just wasteful

**Database:**
- Use read replicas for analytics queries
- Keep writes on primary
- Consider sharding by `source_id` if TB-scale

**Redis:**
- Single instance is usually sufficient
- For HA, use Redis Sentinel or Cluster
- Jobs are ephemeral, can tolerate loss

### Performance Targets

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Download latency | < 30s per file | Log timestamp diff |
| Processing throughput | > 10,000 rows/sec | Rows / processing time |
| Queue depth | < 100 jobs | `redis-cli LLEN` |
| Health check response | < 100ms | Load balancer metrics |
| DB connection usage | < 80% of max | `pg_stat_activity` |

---

## Future Extensions

### 1. Metrics & Monitoring

Add Prometheus metrics:
```go
var (
    filesDownloaded = prometheus.NewCounter(...)
    processingDuration = prometheus.NewHistogram(...)
    queueDepth = prometheus.NewGauge(...)
)
```

### 2. Distributed Tracing

Add OpenTelemetry:
```go
ctx, span := tracer.Start(ctx, "ProcessFile")
defer span.End()
```

Trace a file from download → queue → processing → insert.

### 3. Dead Letter Queue

For jobs that fail repeatedly (e.g., corrupt CSV):
```go
if retryCount > 5 {
    publishToDLQ(job)
}
```

### 4. Dynamic Source Management

Add REST API:
```
POST /sources      - Add new source
PUT /sources/:id   - Update source
DELETE /sources/:id - Deactivate source
```

### 5. Multi-Tenancy

Add `tenant_id` to all tables, isolate data per customer.

---

## Conclusion

This architecture demonstrates production Go patterns at scale:

- ✅ Worker pools for concurrent processing
- ✅ Graceful shutdown with context
- ✅ Idempotent operations
- ✅ Fault tolerance with reconciliation
- ✅ Health checks for orchestration
- ✅ Structured logging with tracing
- ✅ Performance via batching and streaming

The modular monolith pattern gives us deployment flexibility without microservices complexity, while maintaining ACID guarantees and simple operations.