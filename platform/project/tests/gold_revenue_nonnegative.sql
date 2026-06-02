-- DATA-QUALITY TEST (M3 convention): must return ZERO rows. Revenue can never be
-- negative; a row here means a bad aggregation upstream.
SELECT order_date, revenue
FROM lake.gold_daily_revenue
WHERE revenue < 0;
