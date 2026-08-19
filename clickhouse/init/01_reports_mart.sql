-- Витрина отчётности для сервиса reports-api.
--
-- В отличие от предыдущей версии, Airflow больше не выполняет запросы к
-- CRM (это создавало нагрузку на OLTP при массовой выгрузке клиентов) и
-- не строит финальную витрину сам. Он агрегирует только телеметрию и
-- пишет результат в промежуточную таблицу reports.telemetry_daily_agg.
-- Данные CRM попадают в ClickHouse через CDC (Debezium -> Kafka ->
-- KafkaEngine, см. 02_crm_customers_cdc.sql), а финальная витрина
-- reports.user_report_mart_v2 собирается MaterializedView, объединяющим
-- обе таблицы (см. 03_user_report_mart_mv.sql).
CREATE DATABASE IF NOT EXISTS reports;

CREATE TABLE IF NOT EXISTS reports.telemetry_daily_agg
(
    username              String,
    report_date           Date,
    events_count          UInt32,
    avg_response_time_ms  Float32,
    avg_signal_quality    Float32,
    avg_battery_level     Float32,
    error_count           UInt32,
    updated_at            DateTime
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (username, report_date);

CREATE TABLE IF NOT EXISTS reports.user_report_mart_v2
(
    username           String,
    report_date        Date,
    full_name          String,
    region             String,
    prosthetic_model   String,
    events_count       UInt32,
    avg_response_time_ms Float32,
    avg_signal_quality Float32,
    avg_battery_level  Float32,
    error_count        UInt32,
    updated_at         DateTime
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (username, report_date);
