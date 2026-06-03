-- bronze: raw orders, ingested as-is from the landing zone.
SELECT * FROM read_csv_auto('{{ env_var('RAT_LANDING') }}/orders.csv', header = true)
