# Задание 4. Повышение оперативности и стабильности работы CRM

## Архитектурное решение

<img src="/images/tasks/Task4/crm_cdc_pipeline.svg" alt="CDC-пайплайн CRM в витрину отчётности"/>

Массовая выгрузка клиентов CRM для витрины отчётности выполнялась прямым `SELECT` из Airflow к `crm_db` и нагружала транзакционную БД, замедляя её OLTP-запросы. Поток выгрузки отделён от потока транзакций: данные CRM теперь попадают в ClickHouse через Change Data Capture, без единого дополнительного запроса к таблице `customers`.

## Компоненты CDC

- [`crm/init.sql`](../crm/init.sql) - `crm_db` включён в режим логической репликации (`wal_level=logical` в [`docker-compose.yaml`](../docker-compose.yaml)), таблица `customers` переведена на `REPLICA IDENTITY FULL`. Без этого DELETE-события логической репликации несли бы только первичный ключ, и удалённую строку было бы нечем сматчить по `username` в ClickHouse.
- [`debezium/connector-crm-customers.json`](../debezium/connector-crm-customers.json) - конфигурация Debezium PostgreSQL Connector: читает WAL `crm_db` (plugin `pgoutput`), публикует изменения таблицы `customers` в топик `crm.public.customers`. Трансформация `ExtractNewRecordState` разворачивает Debezium-envelope в плоский JSON с текущим состоянием строки и служебными полями `__deleted`, `__op`, `__ts_ms`.
- [`docker-compose.yaml`](../docker-compose.yaml) - сервисы `kafka` (Apache Kafka в режиме KRaft, без Zookeeper), `kafka-connect` (Kafka Connect с предустановленным Debezium-коннектором) и разовый `kafka-connect-init`, который регистрирует коннектор через REST API Kafka Connect после старта воркера.
- [`debezium/register-connector.sh`](../debezium/register-connector.sh) - скрипт для ручной (пере)регистрации коннектора, если конфигурацию нужно применить без пересоздания стека.

## Приём в ClickHouse и витрина

- [`clickhouse/init/02_crm_customers_cdc.sql`](../clickhouse/init/02_crm_customers_cdc.sql) - `reports.crm_customers_queue` (движок `Kafka`) читает топик `crm.public.customers`; `MaterializedView crm_customers_mv` переносит данные в `reports.crm_customers` (`ReplacingMergeTree`) - зеркало клиентов CRM в ClickHouse. Версией для дедупликации служит `__ts_ms` (время события Debezium), а не бизнес-поле `updated_at`: при `REPLICA IDENTITY DEFAULT` DELETE-событие сохраняло бы старое значение `updated_at`, и insert с delete одного `username` были бы неотличимы по версии.
- [`clickhouse/init/01_reports_mart.sql`](../clickhouse/init/01_reports_mart.sql) - `reports.telemetry_daily_agg`, промежуточная таблица агрегатов телеметрии, и `reports.user_report_mart_v2` - финальная витрина.
- [`clickhouse/init/03_user_report_mart_mv.sql`](../clickhouse/init/03_user_report_mart_mv.sql) - `MaterializedView user_report_mart_mv`: на каждую новую строку в `telemetry_daily_agg` подтягивает актуальные данные клиента из `reports.crm_customers` (`is_deleted = 0 AND prosthetic_active = 1`) и кладёт готовую строку в `reports.user_report_mart_v2`.
- [`airflow/dags/reports_etl_dag.py`](../airflow/dags/reports_etl_dag.py) - DAG `crm_telemetry_reports_etl` больше не запрашивает CRM: задача `extract_crm` удалена, остались только `extract_telemetry` (агрегация телеметрии за завершённые сутки) и `load_telemetry_agg` (загрузка агрегатов в `reports.telemetry_daily_agg`).
- [`reports-api/main.py`](../reports-api/main.py) - `SELECT` переведён с `reports.user_report_mart` на `reports.user_report_mart_v2`.

## Проверка

Стек поднят через `docker-compose up` (`crm_db`, `telemetry_db`, `clickhouse`, `kafka`, `kafka-connect`, `kafka-connect-init`, `airflow-init`):

- Коннектор `crm-customers-connector` переходит в состояние `RUNNING` (`GET /connectors/crm-customers-connector/status` через Kafka Connect REST API), снапшот всех 8 строк `customers` доходит до `reports.crm_customers` в ClickHouse.
- `UPDATE customers SET region = ... WHERE username = 'prothetic1'` в `crm_db` без дополнительных действий отражается в `reports.crm_customers FINAL` - новое значение `region` появляется в течение нескольких секунд.
- `DELETE FROM customers WHERE username = 'prothetic3'` помечает соответствующую строку `is_deleted = 1` в `reports.crm_customers FINAL`; такой клиент перестаёт попадать в `JOIN` витрины и в саму витрину.
- `airflow dags test crm_telemetry_reports_etl <дата>` агрегирует телеметрию из `telemetry_db`, пишет строки в `reports.telemetry_daily_agg`, `MaterializedView` строит по ним `reports.user_report_mart_v2` с актуальными на момент вставки данными CRM (включая обновлённый `region` и без удалённого клиента) - без единого `SELECT` к `crm_db` со стороны Airflow.

## Как проверить самостоятельно

```bash
docker compose up -d crm_db telemetry_db clickhouse kafka kafka-connect kafka-connect-init
```

Дождитесь, пока коннектор зарегистрируется и перейдёт в `RUNNING` (Kafka Connect REST - http://localhost:8083):

```bash
curl -s http://localhost:8083/connectors/crm-customers-connector/status
```

Проверьте, что снапшот дошёл до ClickHouse (ClickHouse HTTP - http://localhost:8123):

```bash
curl -s "http://localhost:8123/?query=SELECT+username,region,is_deleted+FROM+reports.crm_customers+FINAL+FORMAT+PrettyCompact"
```

Проверьте, что изменения в CRM доходят до витрины без прямых запросов Airflow к `crm_db` (порт `crm_db` наружу - `5434`, пользователь/БД `crm_user`/`crm`, пароль `crm_password`):

```bash
psql -h localhost -p 5434 -U crm_user -d crm -c "UPDATE customers SET region = 'Novosibirsk' WHERE username = 'prothetic1';"
sleep 5
curl -s "http://localhost:8123/?query=SELECT+region+FROM+reports.crm_customers+FINAL+WHERE+username='prothetic1'+FORMAT+PrettyCompact"
```

Новое значение `region` должно появиться в ClickHouse за несколько секунд без каких-либо действий со стороны Airflow. Аналогично можно проверить `DELETE FROM customers WHERE username = 'prothetic3';` - строка должна получить `is_deleted = 1` и пропасть из `reports.user_report_mart_v2` после следующего прогона Airflow (см. [Задание 2](../Task2) для команды запуска DAG).
