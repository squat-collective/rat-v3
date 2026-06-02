-- silver: cleaned + conformed — lowercase status, drop bad rows, dedupe, completed sales only.
SELECT DISTINCT
  order_id,
  customer,
  product,
  amount,
  lower(status)            AS status,
  CAST(order_date AS DATE) AS order_date
FROM {{ ref('bronze_orders') }}
WHERE order_id IS NOT NULL
  AND customer IS NOT NULL
  AND amount   IS NOT NULL
  AND lower(status) = 'completed'
