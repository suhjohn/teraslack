#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_DIR="$ROOT_DIR/server"
BASE_ENV_FILE="${INTEGRATION_TEST_ENV_FILE:-$ROOT_DIR/.env.integration_test}"
STATE_DIR="$ROOT_DIR/tmp/integration_test"

if [[ ! -f "$BASE_ENV_FILE" ]]; then
  echo "integration_test: missing env file $BASE_ENV_FILE" >&2
  exit 1
fi

mkdir -p "$STATE_DIR"

run_id="it$(date +%Y%m%d%H%M%S)$(python3 - <<'PY'
import secrets
print(secrets.token_hex(3))
PY
)"
compose_project="teraslack-${run_id}"
s3_prefix="integration-test/${run_id}"
tp_prefix="integration_test_${run_id}"
state_env="$STATE_DIR/${run_id}.env"

pick_free_port() {
  python3 - <<'PY'
import socket

while True:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    if port not in (5432, 8080):
        print(port)
        break
PY
}

db_port="$(pick_free_port)"
api_port="$(pick_free_port)"
while [[ "$api_port" == "$db_port" ]]; do
  api_port="$(pick_free_port)"
done

sanitize_env_file() {
  local input_file="$1"
  local output_file="$2"

  : >"$output_file"
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      ""|\#*)
        printf '%s\n' "$line" >>"$output_file"
        ;;
      *=*)
        local key="${line%%=*}"
        local value="${line#*=}"
        key="$(printf '%s' "$key" | tr -d '[:space:]')"
        value="$(printf '%s' "$value" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
        printf '%s=%s\n' "$key" "$value" >>"$output_file"
        ;;
    esac
  done <"$input_file"
}

has_key() {
  local file="$1"
  local key="$2"
  grep -Eq "^${key}=" "$file"
}

sanitize_env_file "$BASE_ENV_FILE" "$state_env"

if ! has_key "$state_env" "S3_ACCESS_KEY" && has_key "$state_env" "AWS_ACCESS_KEY_ID"; then
  printf 'S3_ACCESS_KEY=%s\n' "$(grep '^AWS_ACCESS_KEY_ID=' "$state_env" | head -n1 | cut -d= -f2-)" >>"$state_env"
fi
if ! has_key "$state_env" "S3_SECRET_KEY" && has_key "$state_env" "AWS_SECRET_ACCESS_KEY"; then
  printf 'S3_SECRET_KEY=%s\n' "$(grep '^AWS_SECRET_ACCESS_KEY=' "$state_env" | head -n1 | cut -d= -f2-)" >>"$state_env"
fi

cat >>"$state_env" <<EOF
DB_PORT=$db_port
API_PORT=$api_port
COMPOSE_PROJECT_NAME=$compose_project
S3_KEY_PREFIX=$s3_prefix
WEBHOOK_QUEUE_S3_KEY=$s3_prefix/queues/webhooks/queue.json
INDEX_QUEUE_S3_KEY=$s3_prefix/queues/index/queue.json
TURBOPUFFER_NS_PREFIX=$tp_prefix
EOF

set -a
source "$state_env"
set +a

host_base_url="http://localhost:${API_PORT}"
host_database_url="postgres://slackbackend:slackbackend@localhost:${DB_PORT}/slackbackend?sslmode=disable"
test_exit=0
cleanup_ran=0

compose() {
  docker compose --env-file "$state_env" -p "$COMPOSE_PROJECT_NAME" "$@"
}

cleanup() {
  if [[ "$cleanup_ran" -eq 1 ]]; then
    return
  fi
  cleanup_ran=1

  set +e
  echo
  echo "integration_test: tearing down compose stack"
  (cd "$ROOT_DIR" && compose down -v --remove-orphans)

  echo "integration_test: cleaning remote test data"
  (
    cd "$ROOT_DIR"
    S3_BUCKET="${S3_BUCKET:-}" \
    S3_REGION="${S3_REGION:-}" \
    S3_ENDPOINT="${S3_ENDPOINT:-}" \
    S3_ACCESS_KEY="${S3_ACCESS_KEY:-}" \
    S3_SECRET_KEY="${S3_SECRET_KEY:-}" \
    AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}" \
    AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}" \
    S3_KEY_PREFIX="${S3_KEY_PREFIX:-}" \
    TURBOPUFFER_API_KEY="${TURBOPUFFER_API_KEY:-}" \
    TURBOPUFFER_NS_PREFIX="${TURBOPUFFER_NS_PREFIX:-}" \
    go -C "$SERVER_DIR" run ./cmd/integration-cleanup
  )

  rm -f "$state_env"
}

trap cleanup EXIT
trap 'exit "$test_exit"' INT TERM

echo "integration_test: env file $BASE_ENV_FILE"
echo "integration_test: compose project $compose_project"
echo "integration_test: db port $DB_PORT"
echo "integration_test: api port $API_PORT"
echo "integration_test: s3 prefix $S3_KEY_PREFIX"
echo "integration_test: turbopuffer prefix $TURBOPUFFER_NS_PREFIX"

(cd "$ROOT_DIR" && compose up --build -d)

echo "integration_test: waiting for API health"
for _ in $(seq 1 120); do
  if curl -fsS "$host_base_url/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl -fsS "$host_base_url/healthz" >/dev/null 2>&1; then
  echo "integration_test: API did not become healthy" >&2
  (cd "$ROOT_DIR" && compose ps) >&2 || true
  exit 1
fi

echo "integration_test: running compose-backed e2e flows"
if (
  cd "$ROOT_DIR"
  TERASLACK_E2E=1 \
  DATABASE_URL="$host_database_url" \
  TERASLACK_E2E_BASE_URL="$host_base_url" \
  go -C "$SERVER_DIR" test ./internal/e2e/... -run 'TestComposeE2E_(AgentSessionFlow|CodexPeerChat|ExternalEventsPaginationAndFiltering|WebhookExternalEventDelivery)$' -count=1 -v
); then
  test_exit=0
else
  test_exit=$?
fi

echo
if [[ "$test_exit" -eq 0 ]]; then
  echo "integration_test: all tests passed"
else
  echo "integration_test: tests failed with exit code $test_exit"
  echo "integration_test: compose service status"
  (cd "$ROOT_DIR" && compose ps) || true
fi
exit "$test_exit"
