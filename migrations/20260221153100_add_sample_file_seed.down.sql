DELETE FROM files
WHERE local_path = 'testdata/sample_revenue.csv'
  AND checksum = 'test-checksum';