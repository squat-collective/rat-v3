-- gold: a business-ready mart — daily revenue from completed sales.
SELECT order_date,
       count(*)              AS orders,
       round(sum(amount), 2) AS revenue
FROM {{ ref('silver_orders') }}
GROUP BY order_date
ORDER BY order_date
