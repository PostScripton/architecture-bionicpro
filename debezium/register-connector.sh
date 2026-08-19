#!/bin/sh
# Регистрирует (или обновляет) Debezium-коннектор CDC для таблицы
# customers CRM в Kafka Connect. В docker-compose это делает сервис
# kafka-connect-init автоматически при старте стека; скрипт нужен только
# для ручной регистрации/переприменения конфигурации, например после
# правки connector-crm-customers.json.
set -e

KAFKA_CONNECT_URL="${KAFKA_CONNECT_URL:-http://localhost:8083}"
CONNECTOR_NAME="crm-customers-connector"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Пересоздаём коннектор: удаляем прежнюю регистрацию (если была) и
# регистрируем заново с актуальным конфигом из файла.
curl -sf -X DELETE "${KAFKA_CONNECT_URL}/connectors/${CONNECTOR_NAME}" >/dev/null 2>&1 || true

curl -sf -X POST -H "Content-Type: application/json" \
  --data @"${SCRIPT_DIR}/connector-crm-customers.json" \
  "${KAFKA_CONNECT_URL}/connectors"

echo "Статус коннектора:"
curl -sf "${KAFKA_CONNECT_URL}/connectors/${CONNECTOR_NAME}/status"
