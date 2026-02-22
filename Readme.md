# Data Ingestion Platform

A production-grade, distributed CSV/Excel processing system built in Go. This project demonstrates real-world backend patterns including worker pools, graceful shutdown, fault tolerance, and observability.

## 📋 Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Project Structure](#project-structure)
- [Configuration](#configuration)
- [Running the Services](#running-the-services)
- [Testing](#testing)
- [Monitoring & Health Checks](#monitoring--health-checks)
- [Database Schema](#database-schema)
- [Design Decisions](#design-decisions)
- [Production Considerations](#production-considerations)
- [Troubleshooting](#troubleshooting)
- [License](#license)

---

## Overview

This system downloads CSV/Excel files from multiple sources daily, processes them in parallel, converts revenue values to USD using daily exchange rates, and stores normalized data in PostgreSQL with full transactional safety.

**Key Capabilities:**
- ✅ Distributed processing with multiple concurrent workers
- ✅ Automatic retry with exponential backoff
- ✅ Idempotent operations (safe to re-run)
- ✅ No duplicate downloads or partial inserts
- ✅ Streaming file processing (never loads full file in memory)
- ✅ Graceful shutdown with in-flight work completion
- ✅ Health checks for orchestration (Kubernetes-ready)
- ✅ Automatic recovery from crashed jobs (reconciler)

---

## Architecture

### High-Level Flow

```
┌─────────────────┐
│  sources table  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Downloader    │  Fetches files daily
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Redis Queue    │  FIFO job queue
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Processor     │  Worker pool (N goroutines)
│   (Workers)     │  Streams CSV, converts currency
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   PostgreSQL    │  Normalized revenue data
└─────────────────┘

        ┌─────────────────┐
        │  Reconciler     │  Detects stuck jobs
        └─────────────────┘

┌─────────────────┐
│  Exchanger      │  Daily exchange rates
└─────────────────┘
```

### Components

| Service | Responsibility | Port |
|---------|---------------|------|
| **Downloader** | Downloads files, validates checksums, enqueues jobs | 8081 |
| **Processor** | Consumes jobs, parses CSV, converts currency, batch inserts | 8080 |
| **Exchanger** | Fetches daily exchange rates from external API | 8082 |
| **Reconciler** | Detects and requeues stuck jobs (runs inside Processor) | - |

---

## Features

### Data Integrity
- **UNIQUE constraint** on `(source_id, file_date)` prevents duplicate downloads
- **Single DB transaction** per file ensures no partial inserts
- **Checksum validation** using SHA256 for file integrity
- **Idempotent upserts** for exchange rates (safe to re-run daily)

### Fault Tolerance
- **Exponential backoff** for network failures
- **Reconciler** automatically detects files stuck in "processing" status
- **Graceful shutdown** with 30-second drain timeout
- **Health checks** for liveness and readiness probes

### Performance
- **Worker pool** with configurable concurrency (default: 4 workers)
- **Batch inserts** (500 rows per transaction) for 500x speedup
- **Streaming CSV parsing** — never loads full file in memory
- **Connection pooling** for PostgreSQL (max 20 connections)

### Observability
- **Structured JSON logs** with zerolog
- **Request ID tracing** through the entire pipeline
- **Health endpoints** at `/health/live` and `/health/ready`
- **Processing metrics** logged (rows processed, skipped, timing)

---

## Prerequisites

- **Go** 1.21 or higher
- **PostgreSQL** 12 or higher
- **Redis** 6 or higher

---

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/techitdeveloper/data-ingestion-platform.git
cd data-ingestion-platform
```

### 2. Install dependencies

```bash
go mod download
```

### 3. Set up environment variables

Create a `.env` file in the project root:

```env
DB_URL=postgres://postgres:yourpassword@localhost:5432/data_ingestion?sslmode=disable
REDIS_ADDR=localhost:6379
WORKER_COUNT=4
DATA_DIRECTORY=./data
EXCHANGE_API_URL=https://api.exchangerate-api.com/v4/latest/USD
LOG_LEVEL=info
```

### 4. Create the database

```bash
createdb data_ingestion
```

### 5. Run migrations

Migrations run automatically on service startup, but you can also run them manually:

```bash
go run cmd/downloader/main.go
# Migrations will run and then the service will start
# Press Ctrl+C to stop after migrations complete
```

### 6. Start the services

In three separate terminals:

```bash
# Terminal 1 - Exchange Rate Updater
go run cmd/exchanger/main.go

# Terminal 2 - Downloader
go run cmd/downloader/main.go

# Terminal 3 - Processor
go run cmd/processor/main.go
```

### 7. Verify health

```bash
curl http://localhost:8080/health/ready  # Processor
curl http://localhost:8081/health/ready  # Downloader
curl http://localhost:8082/health/ready  # Exchanger
```

All should return:
```json
{"status":"ready"}
```

---

## Project Structure

```
data-ingestion-platform/
├── cmd/
│   ├── downloader/       # Downloader service binary
│   ├── processor/        # Processor service binary
│   └── exchanger/        # Exchange rate updater binary
├── internal/
│   ├── config/           # Configuration loading
│   ├── db/               # Database connection & migrations
│   ├── queue/            # Redis queue abstraction
│   ├── downloader/       # Download orchestration logic
│   ├── processor/        # Worker pool & CSV processing
│   ├── exchanger/        # Exchange rate API client
│   ├── models/           # Domain models (Source, File, Revenue)
│   ├── repository/       # Database repositories (interfaces + pgx)
│   └── health/           # Health check HTTP server
├── pkg/
│   ├── csv/              # CSV streaming parser
│   ├── shutdown/         # Graceful shutdown manager
│   └── trace/            # Request ID context utilities
├── migrations/           # SQL schema migrations
├── testdata/             # Test CSV files
├── .env                  # Environment variables (gitignored)
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

---

## Configuration

All configuration is via environment variables (loaded from `.env` in development):

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DB_URL` | PostgreSQL connection string | - | ✅ |
| `REDIS_ADDR` | Redis address | - | ✅ |
| `WORKER_COUNT` | Number of concurrent processor workers | `4` | ❌ |
| `DATA_DIRECTORY` | Local directory for downloaded files | `./data` | ❌ |
| `EXCHANGE_API_URL` | Exchange rate API endpoint | - | ❌ |
| `LOG_LEVEL` | Log level (debug, info, warn, error) | `info` | ❌ |

---

## Running the Services

### Development Mode

Run each service in a separate terminal:

```bash
go run cmd/exchanger/main.go
go run cmd/downloader/main.go
go run cmd/processor/main.go
```

### Production Mode

Build binaries:

```bash
go build -o bin/exchanger cmd/exchanger/main.go
go build -o bin/downloader cmd/downloader/main.go
go build -o bin/processor cmd/processor/main.go
```

Run binaries:

```bash
./bin/exchanger &
./bin/downloader &
./bin/processor &
```

### Docker (Optional)

TODO: Add Dockerfile and docker-compose.yml

---

## Testing

### Unit Tests

Run all unit tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

Run tests for a specific package:

```bash
go test ./internal/processor
go test ./pkg/csv
```

### Integration Tests

#### 1. Test the CSV Parser

Create a test file `testdata/test.csv`:

```csv
date,revenue,currency,product
2026-02-15,1000.00,USD,WidgetA
2026-02-15,850.00,EUR,WidgetB
2026-02-15,500.00,GBP,WidgetC
```

Test parsing:

```bash
go test ./pkg/csv -v
```

#### 2. Test End-to-End Flow

**Step 1:** Seed exchange rates

```sql
INSERT INTO exchange_rates (currency, rate_to_usd, rate_date)
VALUES
  ('USD', 1.0,    CURRENT_DATE),
  ('EUR', 1.08,   CURRENT_DATE),
  ('GBP', 1.27,   CURRENT_DATE);
```

**Step 2:** Add a test source

```sql
INSERT INTO sources (id, name, download_url, file_type, schedule_time, active)
VALUES (
    gen_random_uuid(),
    'test_source',
    'https://example.com/test.csv',
    'csv',
    '00:00:00',
    TRUE
);
```

**Step 3:** Manually insert a file record and push job

```sql
-- Insert file record
INSERT INTO files (id, source_id, file_date, local_path, status, checksum)
SELECT
    gen_random_uuid(),
    id,
    CURRENT_DATE,
    'testdata/test.csv',
    'downloaded',
    'test-checksum'
FROM sources
WHERE name = 'test_source'
LIMIT 1
RETURNING id, source_id;
```

Take note of the returned `id` and `source_id`, then push to Redis:

```bash
# Replace with actual UUIDs from above
redis-cli LPUSH jobs:files '{"file_id":"<file_id>","source_id":"<source_id>","file_path":"testdata/test.csv"}'
```

**Step 4:** Watch processor logs

You should see:
```json
{"level":"info","worker_id":0,"file_id":"...","message":"worker picked up job"}
{"level":"info","file_id":"...","message":"processing started"}
{"level":"info","total_rows":3,"skipped_rows":0,"message":"file processing stats"}
{"level":"info","file_id":"...","message":"processing completed"}
```

**Step 5:** Verify data in database

```sql
SELECT 
    currency, 
    original_value, 
    ROUND(usd_value::numeric, 2) as usd_value,
    raw_payload->>'product' as product
FROM revenues
WHERE revenue_date = CURRENT_DATE
ORDER BY created_at DESC;
```

Expected output:
```
 currency | original_value | usd_value | product
----------+----------------+-----------+---------
 GBP      |         500.00 |    635.00 | WidgetC
 EUR      |         850.00 |    918.00 | WidgetB
 USD      |        1000.00 |   1000.00 | WidgetA
```

#### 3. Test Reconciler (Stuck Job Recovery)

**Step 1:** Create a stuck file

```sql
INSERT INTO files (id, source_id, file_date, local_path, status, checksum, created_at)
SELECT
    gen_random_uuid(),
    id,
    CURRENT_DATE,
    'testdata/stuck.csv',
    'processing',
    'stuck-checksum',
    NOW() - INTERVAL '35 minutes'  -- Older than 30-minute threshold
FROM sources
WHERE name = 'test_source'
LIMIT 1;
```

**Step 2:** Wait for reconciler

Within 5 minutes, check processor logs:

```json
{"level":"warn","count":1,"message":"found stuck files, requeuing"}
{"level":"info","file_id":"...","message":"successfully requeued stuck file"}
```

**Step 3:** Verify job in Redis

```bash
redis-cli LRANGE jobs:files 0 -1
```

The stuck file should now be back in the queue.

#### 4. Test Graceful Shutdown

Start the processor, then send `SIGTERM`:

```bash
# Terminal 1
go run cmd/processor/main.go

# Terminal 2
# Find the process ID
ps aux | grep processor

# Send SIGTERM
kill -TERM <pid>
```

You should see:
```json
{"level":"info","signal":"terminated","message":"shutdown signal received"}
{"level":"info","message":"initiating graceful shutdown"}
{"level":"info","message":"all workers exited cleanly"}
{"level":"info","message":"processor exited cleanly"}
```

---

## Monitoring & Health Checks

### Health Endpoints

Each service exposes two health check endpoints:

#### Liveness Probe (`/health/live`)

Returns 200 if the process is alive. Used by Kubernetes to restart crashed pods.

```bash
curl http://localhost:8080/health/live
```

Response:
```json
{"status":"alive"}
```

#### Readiness Probe (`/health/ready`)

Returns 200 if the service is ready to accept work (DB and Redis are healthy).
Returns 503 if dependencies are unavailable.

```bash
curl http://localhost:8080/health/ready
```

Response when healthy:
```json
{"status":"ready"}
```

Response when DB is down:
```json
{"status":"not_ready","reason":"database_unavailable"}
```

### Service Ports

| Service | Health Check Port |
|---------|-------------------|
| Processor | 8080 |
| Downloader | 8081 |
| Exchanger | 8082 |

### Logs

All services use structured JSON logging with `zerolog`. Key fields:

- `level` — log level (info, warn, error)
- `service` — service name
- `worker_id` — worker number (processor only)
- `file_id` — file being processed
- `request_id` — trace ID for the entire job lifecycle
- `time` — ISO8601 timestamp

Example log entry:

```json
{
  "level": "info",
  "service": "processor",
  "worker_id": 2,
  "file_id": "a1b2c3d4-...",
  "request_id": "e5f6g7h8-...",
  "time": "2026-02-15T14:23:45Z",
  "message": "processing started"
}
```

---

## Database Schema

### `sources`

Defines which APIs to download from.

```sql
CREATE TABLE sources (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    download_url  TEXT NOT NULL,
    file_type     TEXT NOT NULL,  -- 'csv' or 'excel'
    schedule_time TIME NOT NULL,
    active        BOOLEAN DEFAULT TRUE,
    created_at    TIMESTAMP DEFAULT NOW()
);
```

### `files`

Tracks downloaded files (one per source per day).

```sql
CREATE TABLE files (
    id          UUID PRIMARY KEY,
    source_id   UUID REFERENCES sources(id),
    file_date   DATE NOT NULL,
    local_path  TEXT NOT NULL,
    status      TEXT NOT NULL,  -- 'downloaded', 'processing', 'completed', 'failed'
    checksum    TEXT,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE(source_id, file_date)  -- Prevents duplicate downloads
);
```

### `exchange_rates`

Daily exchange rates for currency conversion.

```sql
CREATE TABLE exchange_rates (
    currency    TEXT,
    rate_to_usd NUMERIC NOT NULL,  -- How many USD = 1 unit of this currency
    rate_date   DATE NOT NULL,
    PRIMARY KEY(currency, rate_date)
);
```

### `revenues`

Normalized revenue data with USD conversion.

```sql
CREATE TABLE revenues (
    id              UUID PRIMARY KEY,
    source_id       UUID,
    revenue_date    DATE,
    original_value  NUMERIC,
    currency        TEXT,
    usd_value       NUMERIC,
    raw_payload     JSONB,  -- Full original CSV row for auditing
    created_at      TIMESTAMP DEFAULT NOW()
);
```

---

## Design Decisions

### Why Three Separate Binaries?

This is a **modular monolith** pattern:
- Each binary is independently deployable
- Failure in one service doesn't crash the others
- Each can scale independently (run 10 processors, 1 downloader)
- Shared database ensures consistency
- Simpler than microservices (no service mesh, no network serialization between services)

### Why Redis Queue Instead of Direct DB Polling?

- **Decoupling** — Downloader and Processor don't need to know about each other
- **Backpressure** — Queue naturally rate-limits processing
- **Visibility** — Easy to inspect queue depth with `redis-cli`
- **Retry** — Jobs stay in queue until acknowledged

### Why Batch Inserts?

A 10,000 row CSV processed row-by-row would make 10,000 DB round-trips.
Batching 500 rows per transaction reduces this to 20 round-trips — a **500x improvement**.

### Why Stream CSV Files?

Loading a 1GB CSV fully into memory would crash the service.
Streaming via `io.Reader` means constant memory usage regardless of file size.

### Why Reconciler Instead of TTL?

Redis TTL would drop jobs if they take too long. We want jobs to **retry**, not disappear.
The reconciler detects stuck jobs and requeues them safely.

### Why Idempotent Operations?

In distributed systems, things fail and retry. Operations must be safe to run multiple times:
- Downloader: `UNIQUE` constraint prevents duplicate downloads
- Exchanger: `ON CONFLICT DO UPDATE` makes upserts safe
- Processor: Status checks prevent re-processing completed files

---

## Production Considerations

### Scaling

**Horizontal Scaling:**
- Run multiple instances of each service
- Processor scales linearly (more workers = more throughput)
- Downloader can run multiple instances (UNIQUE constraint prevents duplicates)
- Exchanger should run as a singleton (or use distributed locks)

**Database Connection Limits:**
- Each processor instance uses up to 20 DB connections
- 5 processor instances = 100 connections
- Ensure your Postgres `max_connections` is sufficient

### Monitoring

**Metrics to track:**
- Queue depth (`redis-cli LLEN jobs:files`)
- Files in each status (`SELECT status, COUNT(*) FROM files GROUP BY status`)
- Processing time per file (add to logs)
- Worker utilization (active vs idle workers)

**Alerting:**
- Queue depth growing (jobs piling up faster than processing)
- Files stuck in "processing" for hours
- Health checks failing
- Error rate increasing

### Backups

**Critical data:**
- PostgreSQL database (use `pg_dump` or streaming replication)
- Downloaded files in `DATA_DIRECTORY` (optional — can re-download)

**Redis:**
- Jobs are transient — no need to backup
- On Redis failure, reconciler will requeue stuck jobs

### Security

**Credentials:**
- Never commit `.env` to git
- Use secrets management (AWS Secrets Manager, HashiCorp Vault)
- Rotate database passwords regularly

**Network:**
- Run services in a private network
- Only expose health check ports to load balancer
- Use TLS for Postgres connections in production (`sslmode=require`)

### High Availability

**Database:**
- Run Postgres with replication (primary + read replicas)
- Use connection pooling with pgBouncer for thousands of connections

**Redis:**
- Run Redis Sentinel or Redis Cluster for HA
- Jobs can be persisted with AOF (append-only file)

**Services:**
- Deploy behind a load balancer
- Use Kubernetes `Deployment` with multiple replicas
- Configure readiness probes to stop traffic to unhealthy instances

---

## Troubleshooting

### Processor isn't picking up jobs

**Check Redis queue:**
```bash
redis-cli LLEN jobs:files
```

If queue is empty, downloader isn't publishing jobs.

**Check downloader logs:**
```bash
grep "job enqueued" downloader.log
```

**Check file status:**
```sql
SELECT status, COUNT(*) FROM files GROUP BY status;
```

If all files are "completed", there's nothing to process.

### Jobs failing with "no exchange rate"

**Check if rates exist for today:**
```sql
SELECT currency, rate_date FROM exchange_rates WHERE rate_date = CURRENT_DATE;
```

If empty, exchanger hasn't run yet or failed. Check exchanger logs.

**Manually run exchanger:**
```bash
go run cmd/exchanger/main.go
# Press Ctrl+C after "exchange rates updated successfully"
```

### "Database connection failed"

**Check Postgres is running:**
```bash
psql -h localhost -U postgres -d data_ingestion -c "SELECT 1;"
```

**Check connection string in `.env`:**
```bash
cat .env | grep DB_URL
```

Ensure password, host, port, and database name are correct.

### Files stuck in "processing" forever

This is what the reconciler fixes. Wait up to 5 minutes (reconcile interval).

**Manually check stuck files:**
```sql
SELECT id, source_id, file_date, status, created_at
FROM files
WHERE status = 'processing'
  AND created_at < NOW() - INTERVAL '30 minutes';
```

**Manually requeue:**
```bash
# Get file details from above query
redis-cli LPUSH jobs:files '{"file_id":"<id>","source_id":"<source_id>","file_path":"<local_path>"}'

# Reset status
psql -c "UPDATE files SET status = 'downloaded' WHERE id = '<id>';"
```

### High memory usage

**Check worker count:**
```bash
cat .env | grep WORKER_COUNT
```

Each worker processes one file at a time. With 10 workers and 10 large files, memory can spike.

**Solution:**
- Reduce `WORKER_COUNT` to 2-4
- Ensure CSV parser is streaming (not loading full file)
- Check for memory leaks with `go tool pprof`
