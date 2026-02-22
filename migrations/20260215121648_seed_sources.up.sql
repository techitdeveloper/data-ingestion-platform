INSERT INTO sources (id, name, download_url, file_type, schedule_time, active)
VALUES (
    gen_random_uuid(),
    'sample_revenue',
    'https://raw.githubusercontent.com/techitdeveloper/data-ingestion-platform/main/testdata/sample_revenue.csv',
    'csv',
    '00:00:00',
    TRUE
)
ON CONFLICT DO NOTHING;