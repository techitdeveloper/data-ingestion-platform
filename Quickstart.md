# Quick Start Guide

Get the data ingestion platform running in under 5 minutes.

## Prerequisites

- Go 1.21+
- PostgreSQL running on `localhost:5432`
- Redis running on `localhost:6379`

## Steps

### 1. Clone and Install

```bash
git clone https://github.com/techitdeveloper/data-ingestion-platform.git
cd data-ingestion-platform
go mod download
```

### 2. Configure

Create `.env` in the project root:

```bash
cat > .env << 'EOF'
DB_URL=postgres://postgres:yourpassword@localhost:5432/data_ingestion?sslmode=disable
REDIS_ADDR=localhost:6379
WORKER_COUNT=4
DATA_DIRECTORY=./data
EXCHANGE_API_URL=https://api.exchangerate-api.com/v4/latest/USD
LOG_LEVEL=info
EOF
```

Replace `yourpassword` with your actual Postgres password.

### 3. Create Database

```bash
createdb data_ingestion
```

### 4. Run Services

Open three terminals:

**Terminal 1 - Exchanger:**
```bash
go run cmd/exchanger/main.go
```
Wait for: `"exchange rates updated successfully"`

**Terminal 2 - Downloader:**
```bash
go run cmd/downloader/main.go
```
Wait for: `"downloader ready — waiting for signal"`

**Terminal 3 - Processor:**
```bash
go run cmd/processor/main.go
```
Wait for: `"processor starting"`

### 5. Verify

Check health:
```bash
curl http://localhost:8080/health/ready
curl http://localhost:8081/health/ready
curl http://localhost:8082/health/ready
```

All should return `{"status":"ready"}`.

### 6. Seed Test Data

```bash
make seed
```

Or manually:

```sql
psql data_ingestion -c "
INSERT INTO exchange_rates (currency, rate_to_usd, rate_date)
VALUES
  ('USD', 1.0,    CURRENT_DATE),
  ('EUR', 1.08,   CURRENT_DATE),
  ('GBP', 1.27,   CURRENT_DATE);
"
```

### 7. Add a Test Source

Create `testdata/sample.csv`:

```csv
date,revenue,currency,product
2026-02-15,1000.00,USD,WidgetA
2026-02-15,850.00,EUR,WidgetB
2026-02-15,500.00,GBP,WidgetC
```

Insert source:

```sql
psql data_ingestion -c "
INSERT INTO sources (id, name, download_url, file_type, schedule_time, active)
VALUES (
    gen_random_uuid(),
    'test_source',
    'file:///path/to/testdata/sample.csv',
    'csv',
    '00:00:00',
    TRUE
);
"
```

### 8. Manually Trigger Processing

Insert file record:

```sql
psql data_ingestion -c "
INSERT INTO files (id, source_id, file_date, local_path, status, checksum)
SELECT
    gen_random_uuid(),
    id,
    CURRENT_DATE,
    'testdata/sample.csv',
    'downloaded',
    'test-checksum'
FROM sources
WHERE name = 'test_source'
LIMIT 1
RETURNING id, source_id;
"
```

Push job to Redis (replace UUIDs with values from above):

```bash
redis-cli LPUSH jobs:files '{"file_id":"<file_id>","source_id":"<source_id>","file_path":"testdata/sample.csv"}'
```

### 9. Verify Results

Check processor logs — you should see:

```json
{"level":"info","worker_id":0,"file_id":"...","message":"worker picked up job"}
{"level":"info","file_id":"...","message":"processing started"}
{"level":"info","total_rows":3,"message":"file processing stats"}
{"level":"info","file_id":"...","message":"processing completed"}
```

Query database:

```sql
psql data_ingestion -c "
SELECT 
    currency, 
    original_value, 
    ROUND(usd_value::numeric, 2) as usd_value,
    raw_payload->>'product' as product
FROM revenues
WHERE revenue_date = CURRENT_DATE;
"
```

Expected output:

```
 currency | original_value | usd_value | product
----------+----------------+-----------+---------
 GBP      |         500.00 |    635.00 | WidgetC
 EUR      |         850.00 |    918.00 | WidgetB
 USD      |        1000.00 |   1000.00 | WidgetA
```

## Success! 🎉

You now have:
- ✅ All three services running
- ✅ Health checks responding
- ✅ Exchange rates loaded
- ✅ Test data processed
- ✅ Revenues in database with USD conversion

## Next Steps

- Read the full [README.md](README.md) for architecture details
- Check [CONTRIBUTING.md](CONTRIBUTING.md) if you want to contribute
- Run tests: `make test`
- Explore the codebase starting with `cmd/processor/main.go`

## Common Issues

**"Database connection failed"**
- Ensure Postgres is running: `psql -U postgres -c "SELECT 1;"`
- Check your DB_URL in `.env`

**"Redis ping failed"**
- Ensure Redis is running: `redis-cli ping`
- Should return `PONG`

**"No exchange rate for currency"**
- Run exchanger service first
- Or manually seed rates: `make seed`

**Jobs not processing**
- Check Redis queue: `redis-cli LLEN jobs:files`
- Check file status: `psql data_ingestion -c "SELECT status, COUNT(*) FROM files GROUP BY status;"`
- Check processor logs for errors

## Stop Services

Hit `Ctrl+C` in each terminal. Services will shut down gracefully:

```json
{"level":"info","signal":"interrupt","message":"shutdown signal received"}
{"level":"info","message":"initiating graceful shutdown"}
{"level":"info","message":"<service> exited cleanly"}
```