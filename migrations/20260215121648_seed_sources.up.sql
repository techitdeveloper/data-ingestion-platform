INSERT INTO sources (id, name, download_url, file_type, schedule_time, active)
VALUES (
    gen_random_uuid(),
    'sample_sales',
    'https://people.sc.fsu.edu/~jburkardt/data/csv/addresses.csv',
    'csv',
    '00:00:00',
    TRUE
);