-- Витрина отчётности для сервиса reports-api.
-- Наполняется исключительно Airflow DAG'ом (crm_telemetry_reports ETL),
-- reports-api только читает уже готовые агрегаты - без вычислений в реальном времени.
CREATE DATABASE IF NOT EXISTS reports;

CREATE TABLE IF NOT EXISTS reports.user_report_mart
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
