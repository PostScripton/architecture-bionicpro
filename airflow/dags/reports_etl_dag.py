"""ETL для витрины отчётности BionicPRO.

Объединяет данные о клиентах из CRM (PostgreSQL) с агрегированной
телеметрией протезов (PostgreSQL) за завершённые сутки и загружает
готовую витрину в ClickHouse (reports.user_report_mart), откуда её
без дополнительных вычислений читает reports-api.

Соединения с источниками и приёмником настраиваются через переменные
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


def _crm_conn():
    return psycopg2.connect(
        host=os.environ["CRM_DB_HOST"],
        port=os.environ.get("CRM_DB_PORT", "5432"),
        dbname=os.environ["CRM_DB_NAME"],
        user=os.environ["CRM_DB_USER"],
        password=os.environ["CRM_DB_PASSWORD"],
    )


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


def extract_crm(**context):
    """Активные клиенты CRM с протезом - только у них может быть отчёт."""
    with _crm_conn() as conn, conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor) as cur:
        cur.execute(
            """
            SELECT username, full_name, region, prosthetic_model
            FROM customers
            WHERE prosthetic_active = TRUE
            """
        )
        customers = cur.fetchall()
    context["ti"].xcom_push(key="customers", value=[dict(row) for row in customers])


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


def build_and_load_mart(**context):
    """Объединяет CRM и телеметрию по username и загружает готовые строки витрины в ClickHouse."""
    ti = context["ti"]
    report_date = datetime.strptime(context["ds"], "%Y-%m-%d").date()
    customers = {c["username"]: c for c in ti.xcom_pull(key="customers", task_ids="extract_crm")}
    telemetry = {t["username"]: t for t in ti.xcom_pull(key="telemetry", task_ids="extract_telemetry")}

    rows = []
    now = datetime.utcnow()
    for username, customer in customers.items():
        agg = telemetry.get(username)
        if agg is None or not agg["events_count"]:
            # За обрабатываемые сутки телеметрии не было - отчёт за этот день не строим,
            # у пользователя останется последний ранее сформированный отчёт (если был).
            continue
        rows.append((
            username,
            report_date,
            customer["full_name"],
            customer["region"],
            customer["prosthetic_model"] or "",
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
        "reports.user_report_mart",
        rows,
        column_names=[
            "username", "report_date", "full_name", "region", "prosthetic_model",
            "events_count", "avg_response_time_ms", "avg_signal_quality",
            "avg_battery_level", "error_count", "updated_at",
        ],
    )


default_args = {
    "owner": "bionicpro-data",
    "retries": 2,
    "retry_delay": timedelta(minutes=5),
}

with DAG(
    dag_id="crm_telemetry_reports_etl",
    description="CRM + телеметрия -> витрина отчётности в ClickHouse",
    default_args=default_args,
    schedule_interval="0 3 * * *",
    start_date=datetime(2025, 1, 1),
    catchup=False,
    max_active_runs=1,
    tags=["reports", "clickhouse"],
) as dag:
    extract_crm_task = PythonOperator(
        task_id="extract_crm",
        python_callable=extract_crm,
    )

    extract_telemetry_task = PythonOperator(
        task_id="extract_telemetry",
        python_callable=extract_and_aggregate_telemetry,
    )

    load_mart_task = PythonOperator(
        task_id="build_and_load_mart",
        python_callable=build_and_load_mart,
    )

    [extract_crm_task, extract_telemetry_task] >> load_mart_task
