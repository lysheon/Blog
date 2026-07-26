#!/bin/sh
# Runs the Playwright real-API E2E: built SPA + live Go API/Worker + MySQL/Redis.
#
# Locally the MySQL/Redis test instances come from deploy/compose.integration.yaml.
# CI provides them as job services and sets E2E_SKIP_DEPS=1 (and an empty
# E2E_REDIS_PASSWORD, because the CI Redis runs without one).
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
API_PORT=${E2E_API_PORT:-18091}
MYSQL_PORT=${E2E_MYSQL_PORT:-33306}
REDIS_PORT=${E2E_REDIS_PORT:-36379}
REDIS_PASSWORD=${E2E_REDIS_PASSWORD-integration-only-password}
COMPOSE_FILE="$ROOT_DIR/deploy/compose.integration.yaml"

API_PID=""
WORKER_PID=""
STARTED_DEPS=0
API_LOG=${TMPDIR:-/tmp}/real-api-e2e-api.log
WORKER_LOG=${TMPDIR:-/tmp}/real-api-e2e-worker.log

cleanup() {
  status=$?
  [ -n "$API_PID" ] && kill "$API_PID" 2>/dev/null || true
  [ -n "$WORKER_PID" ] && kill "$WORKER_PID" 2>/dev/null || true
  wait 2>/dev/null || true
  if [ "$status" -ne 0 ]; then
    echo "== API log tail ==" >&2
    tail -20 "$API_LOG" >&2 2>/dev/null || true
    echo "== Worker log tail ==" >&2
    tail -20 "$WORKER_LOG" >&2 2>/dev/null || true
  fi
  if [ "$STARTED_DEPS" = "1" ]; then
    docker compose -f "$COMPOSE_FILE" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

if [ "${E2E_SKIP_DEPS:-0}" != "1" ]; then
  echo "== starting disposable MySQL and Redis =="
  INTEGRATION_MYSQL_PORT="$MYSQL_PORT" INTEGRATION_REDIS_PORT="$REDIS_PORT" \
    docker compose -f "$COMPOSE_FILE" up -d mysql-integration redis-integration --wait
  STARTED_DEPS=1
fi

echo "== building backend binaries =="
cd "$ROOT_DIR/backend"
go build -o "$ROOT_DIR/bin/api" ./cmd/api
go build -o "$ROOT_DIR/bin/worker" ./cmd/worker
go build -o "$ROOT_DIR/bin/migrate" ./cmd/migrate

# Disposable runtime configuration: AI stays disabled so the ask page exercises
# the stable ai_not_enabled contract end to end.
APP_NAME=blog
APP_ENV=dev
MYSQL_DSN="blog_test:integration-only-password@tcp(127.0.0.1:${MYSQL_PORT})/blog_integration?charset=utf8mb4&parseTime=true&loc=UTC"
REDIS_ADDR="127.0.0.1:${REDIS_PORT}"
REDIS_KEY_PREFIX='blog:e2e:'
JWT_SECRET='real-api-test-only-jwt-secret-32-bytes-min'
AUTH_COOKIE_SECURE=false
HTTP_TRUSTED_PROXIES=127.0.0.0/8
RATE_REGISTER_PER_MINUTE=100
RATE_LOGIN_PER_MINUTE=100
RATE_REFRESH_PER_MINUTE=100
RATE_COMMENT_PER_MINUTE=100
JOBS_POLL_INTERVAL=1s
LOG_LEVEL=warn
LOG_FORMAT=text
export APP_NAME APP_ENV MYSQL_DSN REDIS_ADDR REDIS_PASSWORD REDIS_KEY_PREFIX \
  JWT_SECRET AUTH_COOKIE_SECURE HTTP_TRUSTED_PROXIES \
  RATE_REGISTER_PER_MINUTE RATE_LOGIN_PER_MINUTE RATE_REFRESH_PER_MINUTE RATE_COMMENT_PER_MINUTE \
  JOBS_POLL_INTERVAL LOG_LEVEL LOG_FORMAT

echo "== migrating =="
APP_SERVICE_MODE=api "$ROOT_DIR/bin/migrate" up

echo "== starting API and Worker =="
APP_SERVICE_MODE=api HTTP_ADDR=":${API_PORT}" "$ROOT_DIR/bin/api" >"$API_LOG" 2>&1 &
API_PID=$!
APP_SERVICE_MODE=worker METRICS_ADDR=':19092' "$ROOT_DIR/bin/worker" >"$WORKER_LOG" 2>&1 &
WORKER_PID=$!

ready=0
attempt=0
while [ "$attempt" -lt 40 ]; do
  if curl -fsS "http://127.0.0.1:${API_PORT}/health/ready" >/dev/null 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$ready" != "1" ]; then
  echo "API did not become ready on port ${API_PORT}" >&2
  exit 1
fi

echo "== building SPA and running the real-API suite =="
cd "$ROOT_DIR/frontend"
npm run build
E2E_REAL_API=1 VITE_DEV_API_TARGET="http://127.0.0.1:${API_PORT}" npx playwright test
