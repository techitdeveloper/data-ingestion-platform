INSERT INTO exchange_rates (currency, rate_to_usd, rate_date)
VALUES
  ('USD', 1.0,    CURRENT_DATE),
  ('EUR', 1.08,   CURRENT_DATE),
  ('GBP', 1.27,   CURRENT_DATE),
  ('INR', 0.012,  CURRENT_DATE),
  ('JPY', 0.0067, CURRENT_DATE)
ON CONFLICT (currency, rate_date) DO NOTHING;