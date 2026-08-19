# Задание 2. Разработка сервиса отчётов

## Архитектурное решение

<img src="/images/tasks/Task2/reports_service_architecture.svg" alt="Архитектура сервиса отчётов BionicPRO"/>

Источники данных - CRM (PostgreSQL, `crm/`) и телеметрия протезов (PostgreSQL, `telemetry/`). Airflow объединяет их по расписанию и строит готовую витрину в ClickHouse. Бэкенд-сервис `reports-api` только читает эту витрину по запросу пользователя, без вычислений в реальном времени. `bionicpro-auth` проксирует запрос и подставляет access-токен, полученный ранее по PKCE flow (см. Задание 1).

## Витрина отчётности (Airflow -> ClickHouse)

Код ETL вынесен в отдельную папку [`airflow/`](../airflow):

- `airflow/dags/reports_etl_dag.py` - DAG `crm_telemetry_reports_etl` с задачами `extract_telemetry` (агрегация событий протеза за обрабатываемые сутки: количество событий, среднее время отклика, среднее качество сигнала, средний заряд батареи, число ошибок) и `load_telemetry_agg` (загрузка агрегатов в ClickHouse).
- Соединения с источниками и ClickHouse настроены через переменные окружения контейнеров, а не через Airflow Connections UI - DAG работает сразу после `docker-compose up`, без ручной настройки.
- Расписание - `0 3 * * *` (ежедневно в 03:00), `catchup=False`. Обрабатываются данные за завершённые сутки (`{{ ds }}`), поэтому пользователь не может получить отчёт за период, который ещё не прошёл через ETL - витрина просто не содержит такой даты, и `reports-api` в этом случае возвращает статус "отчёт не готов".
- Схема витрины - [`clickhouse/init/01_reports_mart.sql`](../clickhouse/init/01_reports_mart.sql), таблица `reports.user_report_mart_v2` (`ReplacingMergeTree`, `ORDER BY (username, report_date)`) - подобрана под быстрый поиск по конкретному пользователю.

В [Задании 4](../Task4) данные CRM в этой схеме заменены на приём через CDC (Debezium -> Kafka -> ClickHouse), чтобы массовая выгрузка клиентов не нагружала транзакционную БД CRM - Airflow здесь описывает только агрегацию телеметрии.

## Сервис reports-api

Новый бэкенд-сервис [`reports-api/`](../reports-api) (Python, FastAPI):

- `GET /reports` - возвращает последний обработанный отчёт по пользователю из токена. Если Airflow ещё не успел обработать данные за текущий период, возвращается `202` со статусом `pending`, а не ошибка.
- Доступ к эндпоинту требует валидный access-токен Keycloak: проверяются подпись (JWKS), `issuer`, `audience = reports-api` и наличие роли `prothetic_user` в `realm_access.roles`. Без токена - `401`, с ролью `user` без протеза - `403`.
- Личность пользователя (`preferred_username`) берётся исключительно из проверенного токена, в запросе нет параметра "чей отчёт" - получить чужой отчёт технически невозможно.
- Аудитория токена обеспечена protocol-mapper'ом `reports-api-audience` на клиенте `reports-frontend` в `keycloak/realm-export.json` - `bionicpro-auth` получает access-токен с `aud: reports-api` и передаёт его дальше в заголовке `Authorization: Bearer`.
- `reports-api` не выполняет никаких вычислений над телеметрией - только `SELECT` из готовой витрины ClickHouse.

`bionicpro-auth` (`REPORTS_API_URL=http://reports-api:8001`) проксирует `/reports` без изменений.

## Фронтенд

В `frontend/src/components/ReportPage.tsx` кнопка "Download Report" теперь не просто проверяет статус ответа, а отображает полученный отчёт: пользователя, регион, модель протеза, дату отчёта, количество событий, среднее время отклика, качество сигнала, заряд батареи и число ошибок. Отдельно обрабатываются случаи "отчёт ещё не готов" (202) и "нет доступа" (403).

## Проверка

Стек поднят через `docker-compose up` с нуля (включая чистый импорт realm в Keycloak):

- `crm_db` и `telemetry_db` наполняются сид-данными при первом старте, `clickhouse` создаёт БД и таблицу витрины по `clickhouse/init/`.
- DAG `crm_telemetry_reports_etl` распознаётся Airflow без ошибок импорта, ручной и плановый запуски завершаются `success`, витрина в ClickHouse наполняется агрегатами по каждому пользователю с протезом.
- `GET /reports` с валидным токеном `prothetic_user` возвращает `200` и отчёт по своему username; без токена - `401`; с ролью `user` (нет протеза) - `403`; для периода, ещё не обработанного Airflow, - `202` вместо ошибки.
- Полный цикл в браузере: вход через PKCE (Keycloak) -> `bionicpro-auth` устанавливает сессию -> кнопка "Download Report" -> `bionicpro-auth` проксирует запрос в `reports-api` с access-токеном -> ClickHouse -> отчёт отображается в UI.
- Обнаруженная и исправленная в процессе проверки деталь: `iss` в access-токене после реального authorization code flow соответствует публичному URL Keycloak (тому, с которого браузер начал вход), а не внутреннему адресу, используемому для обмена кода на токен - поэтому в `reports-api` issuer для проверки токена и адрес для получения JWKS настроены раздельно (`KEYCLOAK_PUBLIC_URL` и `KEYCLOAK_INTERNAL_URL`).

## Как проверить самостоятельно

```bash
docker compose up -d clickhouse crm_db telemetry_db airflow-init airflow-webserver airflow-scheduler bionicpro-auth reports-api frontend
```

Сервисы: Airflow UI - http://localhost:8081 (логин `admin` / пароль `admin`), reports-api напрямую - http://localhost:8001, ClickHouse HTTP - http://localhost:8123.

Запустить DAG вручную, не дожидаясь расписания 03:00 (дата - вчера относительно текущей, т.к. DAG обрабатывает только завершённые сутки):

```bash
docker compose exec airflow-webserver airflow dags test crm_telemetry_reports_etl $(date -v-1d +%F)
```

Проверить, что витрина наполнилась (пользователи с активным протезом - `prothetic1`, `prothetic2`, `prothetic3`, `john.doe`, `alex.johnson`, см. [`crm/init.sql`](../crm/init.sql)):

```bash
curl -s "http://localhost:8123/?query=SELECT+username,report_date,events_count+FROM+reports.user_report_mart_v2+FORMAT+PrettyCompact"
```

Проверить API:

1. Без cookie: `curl -i http://localhost:8000/reports` -> `401`.
2. Войдите на http://localhost:3000 под `prothetic1` / `prothetic123` (пройдите настройку OTP при первом входе, см. [Задание 1](../Task1)), нажмите "Download Report" - отчёт должен отрисоваться в UI. Значение cookie `bionicpro_session` можно взять из DevTools -> Application -> Cookies и повторить запрос из терминала: `curl -i --cookie "bionicpro_session=<значение>" http://localhost:8000/reports`.
3. Войдите под `user1` / `password123` (роль `user`, без `prothetic_user`) и нажмите "Download Report" - должен вернуться `403`.
4. Если DAG ещё не запускали или отчёта за сегодняшнюю дату ещё нет - `reports-api` должен вернуть `202` со статусом `pending`, а не ошибку.
