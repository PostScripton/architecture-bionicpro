-- Приём CDC-событий по таблице customers CRM через KafkaEngine.
--
-- Debezium читает изменения crm_db через логическую репликацию
-- (без запросов к таблице) и публикует их в топик crm.public.customers.
-- Коннектор настроен с transform ExtractNewRecordState (unwrap), поэтому
-- в топике лежат уже "плоские" JSON-записи с текущим состоянием строки и
-- служебными полями __deleted и __ts_ms, а не полный envelope Debezium.
-- Debezium с JsonConverter отдаёт логический тип Date как число дней от
-- эпохи (io.debezium.time.Date), а не строкой - поэтому contract_signed_at
-- здесь UInt32 и приводится к Date уже в MV. prosthetic_active - JSON
-- boolean (true/false), для него используется тип Bool (алиас UInt8).
CREATE TABLE IF NOT EXISTS reports.crm_customers_queue
(
    id                  Int32,
    username            String,
    full_name           String,
    region              String,
    prosthetic_model    Nullable(String),
    prosthetic_active   Bool,
    contract_signed_at  UInt32,
    -- Debezium отдаёт ZonedTimestamp строкой с микросекундами
    -- ("...686315Z"), DateTime64(3) не парсит такой формат напрямую,
    -- поэтому строка приводится к DateTime64 уже в MV.
    updated_at          String,
    __deleted           String,
    __ts_ms             UInt64
)
ENGINE = Kafka
SETTINGS
    kafka_broker_list = 'kafka:9092',
    kafka_topic_list = 'crm.public.customers',
    kafka_group_name = 'clickhouse_crm_customers',
    kafka_format = 'JSONEachRow',
    kafka_num_consumers = 1,
    kafka_skip_broken_messages = 5;

-- Зеркало customers в ClickHouse: ReplacingMergeTree хранит только
-- последнее состояние строки на каждый username. Версией для дедупликации
-- служит не бизнес-поле updated_at, а __ts_ms (время события в Debezium) -
-- при REPLICA IDENTITY DEFAULT DELETE-событие переносит updated_at из
-- удалённой строки без изменений, и по updated_at insert и delete одного
-- и того же username были бы неотличимы по "свежести". __ts_ms всегда
-- строго возрастает по мере поступления событий и однозначно определяет
-- порядок insert/update/delete для одного username.
CREATE TABLE IF NOT EXISTS reports.crm_customers
(
    id                  Int32,
    username            String,
    full_name           String,
    region              String,
    prosthetic_model    Nullable(String),
    prosthetic_active   UInt8,
    contract_signed_at  Date,
    updated_at          DateTime64(3),
    version             UInt64,
    is_deleted          UInt8
)
ENGINE = ReplacingMergeTree(version, is_deleted)
ORDER BY username;

CREATE MATERIALIZED VIEW IF NOT EXISTS reports.crm_customers_mv
TO reports.crm_customers
AS
SELECT
    id,
    username,
    full_name,
    region,
    prosthetic_model,
    prosthetic_active,
    toDate(contract_signed_at) AS contract_signed_at,
    parseDateTime64BestEffort(updated_at, 3) AS updated_at,
    __ts_ms AS version,
    if(__deleted = 'true', 1, 0) AS is_deleted
FROM reports.crm_customers_queue;
