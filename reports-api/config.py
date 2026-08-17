import os


def _env(key: str, default: str) -> str:
    return os.environ.get(key, default)


KEYCLOAK_INTERNAL_URL = _env("KEYCLOAK_INTERNAL_URL", "http://keycloak:8080")
# Keycloak ставит в claim "iss" тот hostname, с которого браузер начал
# authorization code flow (KEYCLOAK_PUBLIC_URL в bionicpro-auth), а не тот,
# что используется для server-to-server обмена кода на токен - поэтому issuer
# для проверки токена и URL для получения JWKS настраиваются раздельно.
KEYCLOAK_PUBLIC_URL = _env("KEYCLOAK_PUBLIC_URL", "http://localhost:8080")
KEYCLOAK_REALM = _env("KEYCLOAK_REALM", "reports-realm")
REQUIRED_AUDIENCE = _env("REQUIRED_AUDIENCE", "reports-api")
REQUIRED_ROLE = _env("REQUIRED_ROLE", "prothetic_user")

CLICKHOUSE_HOST = _env("CLICKHOUSE_HOST", "clickhouse")
CLICKHOUSE_HTTP_PORT = int(_env("CLICKHOUSE_HTTP_PORT", "8123"))
CLICKHOUSE_USER = _env("CLICKHOUSE_USER", "default")
CLICKHOUSE_PASSWORD = _env("CLICKHOUSE_PASSWORD", "")
CLICKHOUSE_DB = _env("CLICKHOUSE_DB", "reports")

# S3-совместимое хранилище (Minio) - готовые отчёты сохраняются сюда, чтобы
# не пересчитывать их из ClickHouse при каждом запросе.
S3_ENDPOINT_URL = _env("S3_ENDPOINT_URL", "http://minio:9000")
S3_ACCESS_KEY = _env("S3_ACCESS_KEY", "minioadmin")
S3_SECRET_KEY = _env("S3_SECRET_KEY", "minioadmin")
S3_BUCKET = _env("S3_BUCKET", "reports")
# Публичный адрес Nginx, который кеширует и раздаёт объекты бакета как CDN.
CDN_BASE_URL = _env("CDN_BASE_URL", "http://localhost:8082")

LISTEN_PORT = int(_env("LISTEN_PORT", "8001"))

ISSUER = f"{KEYCLOAK_PUBLIC_URL.rstrip('/')}/realms/{KEYCLOAK_REALM}"
JWKS_URL = f"{KEYCLOAK_INTERNAL_URL.rstrip('/')}/realms/{KEYCLOAK_REALM}/protocol/openid-connect/certs"
