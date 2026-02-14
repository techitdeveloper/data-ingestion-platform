CREATE TABLE revenues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID,
    revenue_date DATE,
    original_value NUMERIC,
    currency TEXT,
    usd_value NUMERIC,
    raw_payload JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);