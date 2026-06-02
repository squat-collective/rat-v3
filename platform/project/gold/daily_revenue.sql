-- gold: a business-ready mart — revenue per day from completed sales.
CREATE OR REPLACE TABLE lake.gold_daily_revenue AS
SELECT order_date,
       count(*)              AS orders,
       round(sum(amount), 2) AS revenue
FROM lake.silver_orders
GROUP BY order_date
ORDER BY order_date;
