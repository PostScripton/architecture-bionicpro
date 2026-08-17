-- Телеметрия: сырые события от чипа протеза (миосигналы/актуаторы), которые
-- поступают в реальном времени. username соответствует username в Keycloak
-- и используется Airflow для объединения с данными CRM.
CREATE TABLE IF NOT EXISTS device_events (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    device_id VARCHAR(64) NOT NULL,
    event_ts TIMESTAMPTZ NOT NULL,
    response_time_ms INTEGER NOT NULL,
    signal_quality SMALLINT NOT NULL,
    battery_level SMALLINT NOT NULL,
    is_error BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_device_events_username_ts ON device_events (username, event_ts);

-- Демонстрационные события за вчерашний и позавчерашний день, чтобы у Airflow
-- сразу был обработанный период для построения витрины.
INSERT INTO device_events (username, device_id, event_ts, response_time_ms, signal_quality, battery_level, is_error)
SELECT
    u.username,
    u.device_id,
    now() - (interval '1 day') * d - (interval '1 hour') * h,
    (60 + (random() * 60))::int,
    (70 + (random() * 30))::int,
    (30 + (random() * 70))::int,
    random() < 0.03
FROM (VALUES
    ('prothetic1', 'dev-prothetic1-01'),
    ('prothetic2', 'dev-prothetic2-01'),
    ('prothetic3', 'dev-prothetic3-01'),
    ('john.doe', 'dev-johndoe-01'),
    ('alex.johnson', 'dev-alexjohnson-01')
) AS u(username, device_id)
CROSS JOIN generate_series(1, 2) AS d
CROSS JOIN generate_series(0, 23, 4) AS h;
