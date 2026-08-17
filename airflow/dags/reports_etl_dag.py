"""ETL телеметрии для витрины отчётности BionicPRO.

Раньше DAG также запрашивал справочник клиентов из CRM (PostgreSQL) и
объединял его с телеметрией перед загрузкой в ClickHouse. Массовая
выгрузка всех клиентов из CRM при каждом запуске нагружала транзакционную
БД CRM и замедляла её OLTP-запросы (см. Задание 4).

Теперь Airflow отвечает только за агрегацию телеметрии протезов за
завершённые сутки и загрузку результата в промежуточную таблицу
reports.telemetry_daily_agg. Данные CRM попадают в ClickHouse отдельным
потоком через CDC (Debezium -> Kafka -> KafkaEngine, см.
clickhouse/init/02_crm_customers_cdc.sql), а финальную витрину
reports.user_report_mart_v2 собирает MaterializedView в ClickHouse
(clickhouse/init/03_user_report_mart_mv.sql), объединяя обе таблицы -
без единого дополнительного запроса к CRM.

Соединения с источником и приёмником настраиваются через переменные
окружения контейнеров airflow-scheduler/airflow-webserver, а не через
Airflow Connections UI, чтобы DAG работал сразу после docker-compose up
без ручной настройки.
"""
import os
from datetime import datetime, timedelta

import clickhouse_connect
import psycopg2
import psycopg2.extras
from airflow import DAG
from airflow.operators.python import PythonOperator


def _telemetry_conn():
    return psycopg2.connect(
        host=os.environ["TELEMETRY_DB_HOST"],
        port=os.environ.get("TELEMETRY_DB_PORT", "5432"),
        dbname=os.environ["TELEMETRY_DB_NAME"],
        user=os.environ["TELEMETRY_DB_USER"],
        password=os.environ["TELEMETRY_DB_PASSWORD"],
    )


def _clickhouse_client():
    return clickhouse_connect.get_client(
        host=os.environ["CLICKHOUSE_HOST"],
        port=int(os.environ.get("CLICKHOUSE_HTTP_PORT", "8123")),
        username=os.environ.get("CLICKHOUSE_USER", "default"),
        password=os.environ.get("CLICKHOUSE_PASSWORD", ""),
        database=os.environ.get("CLICKHOUSE_DB", "reports"),
    )


def extract_and_aggregate_telemetry(**context):
    """Агрегирует события протеза по пользователям за обрабатываемые сутки (ds)."""
    report_date = context["ds"]
    with _telemetry_conn() as conn, conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute(
            """
            SELECT
                username,
                count(*) AS events_count,
                avg(response_time_ms) AS avg_response_time_ms,
                avg(signal_quality) AS avg_signal_quality,
                avg(battery_level) AS avg_battery_level,
                sum(CASE WHEN is_error THEN 1 ELSE 0 END) AS error_count
            FROM device_events
            WHERE event_ts >= %(start)s AND event_ts < %(end)s
            GROUP BY username
            """,
            {"start": report_date, "end": (datetime.strptime(report_date, "%Y-%m-%d") + timedelta(days=1)).date()},
        )
        aggregates = cur.fetchall()
    context["ti"].xcom_push(key="telemetry", value=[dict(row) for row in aggregates])


def load_telemetry_agg(**context):
    """Загружает агрегаты телеметрии в промежуточную таблицу ClickHouse.

    Дальше строки в финальную витрину добавляет MaterializedView
    reports.user_report_mart_mv, подтягивая данные клиента из
    reports.crm_customers (наполняется CDC) - без запроса к CRM отсюда.
    """
    ti = context["ti"]
    report_date = datetime.strptime(context["ds"], "%Y-%m-%d").date()
    telemetry = ti.xcom_pull(key="telemetry", task_ids="extract_telemetry")

    rows = []
    now = datetime.utcnow()
    for agg in telemetry:
        if not agg["events_count"]:
            continue
        rows.append((
            agg["username"],
            report_date,
            int(agg["events_count"]),
            float(agg["avg_response_time_ms"]),
            float(agg["avg_signal_quality"]),
            float(agg["avg_battery_level"]),
            int(agg["error_count"]),
            now,
        ))

    if not rows:
        return

    client = _clickhouse_client()
    client.insert(
        "reports.telemetry_daily_agg",
        rows,
        column_names=[
            "username", "report_date", "events_count", "avg_response_time_ms",
            "avg_signal_quality", "avg_battery_level", "error_count", "updated_at",
        ],
    )


default_args = {
    "owner": "bionicpro-data",
    "retries": 2,
    "retry_delay": timedelta(minutes=5),
}

with DAG(
    dag_id="crm_telemetry_reports_etl",
    description="Телеметрия -> reports.telemetry_daily_agg (ClickHouse); CRM попадает в витрину через CDC",
    default_args=default_args,
    schedule_interval="0 3 * * *",
    start_date=datetime(2025, 1, 1),
    catchup=False,
    max_active_runs=1,
    tags=["reports", "clickhouse"],
) as dag:
    extract_telemetry_task = PythonOperator(
        task_id="extract_telemetry",
        python_callable=extract_and_aggregate_telemetry,
    )

    load_telemetry_agg_task = PythonOperator(
        task_id="load_telemetry_agg",
        python_callable=load_telemetry_agg,
    )

    extract_telemetry_task >> load_telemetry_agg_task
