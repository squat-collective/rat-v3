-- silver: cleaned + conformed orders.
--   * lowercase the status (the raw data mixes COMPLETED/completed/Completed),
--   * drop rows missing a key field (customer 1005, amount 1002),
--   * dedupe (order 1003 lands twice),
--   * keep only real completed sales (no PENDING/CANCELLED).
CREATE OR REPLACE TABLE lake.silver_orders AS
SELECT DISTINCT
  order_id,
  customer,
  product,
  amount,
  lower(status)          AS status,
  CAST(order_date AS DATE) AS order_date
FROM lake.bronze_orders
WHERE order_id IS NOT NULL
  AND customer IS NOT NULL
  AND amount   IS NOT NULL
  AND lower(status) = 'completed';
