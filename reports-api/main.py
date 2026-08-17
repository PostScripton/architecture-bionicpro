import clickhouse_connect
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


@app.get("/healthz")
def healthz():
    return {"status": "ok"}


@app.get("/reports")
def get_report(response: Response, principal: Principal = Depends(require_report_access)):
    """Возвращает уже готовый отчёт пользователя из витрины ClickHouse.

    Доступ ограничен личностью из проверенного токена (см. auth.py) - в
    запросе нет параметра "чей отчёт", поэтому получить чужой отчёт
    невозможно. Данные не пересчитываются на лету: витрину наполняет только
    Airflow, здесь - однократный SELECT по ключу.
    """
    client = _clickhouse_client()
    result = client.query(
        """
        SELECT report_date, full_name, region, prosthetic_model, events_count,
               avg_response_time_ms, avg_signal_quality, avg_battery_level,
               error_count, updated_at
        FROM reports.user_report_mart FINAL
        WHERE username = {username:String}
        ORDER BY report_date DESC
        LIMIT 1
        """,
        parameters={"username": principal.username},
    )

    if not result.result_rows:
        # Отчёт ещё не сформирован Airflow (например, для нового пользователя
        # или до первого прогона ETL за текущие сутки) - это не ошибка.
        response.status_code = 202
        return {"status": "pending", "message": "report is not generated yet, try again later"}

    row = dict(zip(result.column_names, result.result_rows[0]))
    return {
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
