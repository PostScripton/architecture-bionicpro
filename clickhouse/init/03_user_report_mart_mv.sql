-- Финальная витрина отчётности собирается MaterializedView: на каждую
-- новую строку телеметрии (её пишет Airflow в telemetry_daily_agg)
-- MV подтягивает актуальные данные клиента из reports.crm_customers
-- (наполняется CDC, см. 02_crm_customers_cdc.sql) и кладёт готовую
-- строку в reports.user_report_mart_v2, которую читает reports-api.
-- Отчёт строится только для активных клиентов с протезом - как и в
-- прежней версии на Airflow (WHERE prosthetic_active = TRUE).
CREATE MATERIALIZED VIEW IF NOT EXISTS reports.user_report_mart_mv
TO reports.user_report_mart_v2
AS
SELECT
    t.username              AS username,
    t.report_date           AS report_date,
    c.full_name              AS full_name,
    c.region                 AS region,
    ifNull(c.prosthetic_model, '') AS prosthetic_model,
    t.events_count           AS events_count,
    t.avg_response_time_ms   AS avg_response_time_ms,
    t.avg_signal_quality     AS avg_signal_quality,
    t.avg_battery_level      AS avg_battery_level,
    t.error_count            AS error_count,
    t.updated_at             AS updated_at
FROM reports.telemetry_daily_agg AS t
INNER JOIN
(
    SELECT username, full_name, region, prosthetic_model
    FROM reports.crm_customers FINAL
    WHERE is_deleted = 0 AND prosthetic_active = 1
) AS c
ON t.username = c.username;
