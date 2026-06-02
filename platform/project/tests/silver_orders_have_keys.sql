-- DATA-QUALITY TEST (M3 convention): must return ZERO rows — any row returned is a
-- failure. Asserts that every silver order carries its key fields.
SELECT order_id, customer, amount
FROM lake.silver_orders
WHERE order_id IS NULL OR customer IS NULL OR amount IS NULL;
