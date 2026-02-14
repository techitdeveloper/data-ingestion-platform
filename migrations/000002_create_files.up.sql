CREATE TABLE files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID REFERENCES sources(id),
    file_date DATE NOT NULL,
    local_path TEXT NOT NULL,
    status TEXT NOT NULL,
    checksum TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(source_id, file_date)
);