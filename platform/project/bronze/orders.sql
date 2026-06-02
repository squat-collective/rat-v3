-- bronze: raw orders, ingested AS-IS from the landing zone (types inferred, nothing
-- cleaned). The ${LANDING} placeholder is substituted by the runner with the platform's
-- landing/ path. This is the only model that reads a raw file; everything downstream
-- reads lake tables.
CREATE OR REPLACE TABLE lake.bronze_orders AS
SELECT * FROM read_csv_auto('${LANDING}/orders.csv', header = true);
