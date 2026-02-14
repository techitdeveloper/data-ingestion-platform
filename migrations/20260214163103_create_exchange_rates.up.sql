CREATE TABLE exchange_rates (
    currency TEXT,
    rate_to_usd NUMERIC NOT NULL,
    rate_date DATE NOT NULL,
    PRIMARY KEY (currency, rate_date)
);