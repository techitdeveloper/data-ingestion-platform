-- Insert a file record pointing to your local test file
INSERT INTO files (id, source_id, file_date, local_path, status, checksum)
SELECT
    gen_random_uuid(),
    id,
    CURRENT_DATE,
    'testdata/sample_revenue.csv',
    'downloaded',
    'test-checksum'
FROM sources
WHERE name = 'sample_revenue'
LIMIT 1;