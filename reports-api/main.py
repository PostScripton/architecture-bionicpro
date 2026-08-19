import json
from datetime import date, timedelta

import boto3
import clickhouse_connect
from botocore.client import Config as BotoConfig
from botocore.exceptions import ClientError
from fastapi import Depends, FastAPI, Response

import config
from auth import Principal, require_report_access

app = FastAPI(title="reports-api")


def _clickhouse_client():
    return clickhouse_connect.get_client(
        host=config.CLICKHOUSE_HOST,
        port=config.CLICKHOUSE_HTTP_PORT,
        username=config.CLICKHOUSE_USER,
        password=config.CLICKHOUSE_PASSWORD,
        database=config.CLICKHOUSE_DB,
    )


def _s3_client():
    return boto3.client(
        "s3",
        endpoint_url=config.S3_ENDPOINT_URL,
        aws_access_key_id=config.S3_ACCESS_KEY,
        aws_secret_access_key=config.S3_SECRET_KEY,
        config=BotoConfig(s3={"addressing_style": "path"}, signature_version="s3v4"),
    )


def _expected_report_date() -> date:
    # DAG crm_telemetry_reports_etl обрабатывает завершённые сутки и
    # запускается ежедневно в 03:00 (см. Task2) - поэтому последний
    # доступный отчёт для любого пользователя всегда за вчерашний день, и
    # эту дату не нужно вычислять запросом к ClickHouse.
    return date.today() - timedelta(days=1)


def _report_key(username: str, report_date: date) -> str:
    return f"{username}/{report_date.isoformat()}.json"


def _cdn_url(key: str) -> str:
    return f"{config.CDN_BASE_URL}/{config.S3_BUCKET}/{key}"


@app.get("/healthz")
def healthz():
    return {"status": "ok"}


@app.get("/reports")
def get_report(response: Response, principal: Principal = Depends(require_report_access)):
    """Отдаёт ссылку на CDN с отчётом пользователя, минимизируя нагрузку на
    ClickHouse.

    Отчёты за дату иммутабельны (Airflow пишет витрину один раз за сутки),
    поэтому ключ объекта в S3 однозначно определяется username и датой -
    её не нужно вычислять запросом к БД. Если объект уже есть в S3, сервис
    отвечает CDN-ссылкой без единого обращения к ClickHouse. Иначе читает
    готовую строку витрины, кладёт отчёт в S3 один раз и уже потом отдаёт
    ссылку - повторные запросы этого пользователя больше не трогают OLAP.
    """
    report_date = _expected_report_date()
    key = _report_key(principal.username, report_date)
    s3 = _s3_client()

    try:
        s3.head_object(Bucket=config.S3_BUCKET, Key=key)
        return {"status": "ready", "report_url": _cdn_url(key)}
    except ClientError as exc:
        if exc.response.get("Error", {}).get("Code") not in ("404", "NoSuchKey"):
            raise

    client = _clickhouse_client()
    result = client.query(
        """
        SELECT report_date, full_name, region, prosthetic_model, events_count,
               avg_response_time_ms, avg_signal_quality, avg_battery_level,
               error_count, updated_at
        FROM reports.user_report_mart_v2 FINAL
        WHERE username = {username:String} AND report_date = {report_date:Date}
        LIMIT 1
        """,
        parameters={"username": principal.username, "report_date": report_date},
    )

    if not result.result_rows:
        # Отчёт ещё не сформирован Airflow (например, для нового пользователя
        # или до первого прогона ETL) - это не ошибка.
        response.status_code = 202
        return {"status": "pending", "message": "report is not generated yet, try again later"}

    row = dict(zip(result.column_names, result.result_rows[0]))
    report_body = {
        "status": "ready",
        "username": principal.username,
        "report_date": row["report_date"].isoformat(),
        "full_name": row["full_name"],
        "region": row["region"],
        "prosthetic_model": row["prosthetic_model"],
        "events_count": row["events_count"],
        "avg_response_time_ms": round(row["avg_response_time_ms"], 1),
        "avg_signal_quality": round(row["avg_signal_quality"], 1),
        "avg_battery_level": round(row["avg_battery_level"], 1),
        "error_count": row["error_count"],
        "updated_at": row["updated_at"].isoformat(),
    }
    s3.put_object(
        Bucket=config.S3_BUCKET,
        Key=key,
        Body=json.dumps(report_body).encode("utf-8"),
        ContentType="application/json",
    )
    return {"status": "ready", "report_url": _cdn_url(key)}
