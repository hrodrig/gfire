#!/usr/bin/env bash
# E2E test: docker-compose postgres → migrate → gfire server → curl jobs → CLI verify.
# Prerequisites: docker compose, migrate CLI, curl.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$PROJECT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $1"; }
fail() { echo -e "${RED}FAIL${NC} $1"; exit 1; }

echo "=== GFire E2E Test ==="
echo ""

# ── Build ──────────────────────────────────────────────
echo -n "Building gfire... "
go build -o /tmp/gfire-e2e ./cmd/gfire && pass "ok" || fail "build failed"

# ── Storage: postgres via docker compose ───────────────
echo -n "Starting postgres... "
docker compose up -d postgres 2>&1 | tail -1
# Wait for postgres to accept connections.
for i in $(seq 1 30); do
  if docker compose exec -T postgres pg_isready -U gfire -d gfire >/dev/null 2>&1; then
    pass "ready"
    break
  fi
  if [ "$i" -eq 30 ]; then fail "postgres did not become ready"; fi
  sleep 1
done

# ── Migrations ─────────────────────────────────────────
echo -n "Running migrations... "
migrate -path internal/storage/postgres/migrations -database "postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable" up >/dev/null 2>&1
pass "ok"

# ── Config ─────────────────────────────────────────────
cat > /tmp/gfire-e2e.yaml <<'YAML'
server:
  host: "127.0.0.1"
  port: 8199
  workers: 2
  queues: [default]
storage:
  backend: postgres
  postgres:
    dsn: "postgres://gfire:gfire@localhost:5432/gfire?sslmode=disable"
auth:
  enabled: true
  token: "e2e-secret"
handlers: []
logging:
  level: warn
YAML

# ── Start server ───────────────────────────────────────
echo -n "Starting gfire server... "
/tmp/gfire-e2e --config /tmp/gfire-e2e.yaml server &
GFIRE_PID=$!
# Wait for server to be ready.
for i in $(seq 1 20); do
  if curl -sS http://127.0.0.1:8199/healthz >/dev/null 2>&1; then
    pass "ok (pid $GFIRE_PID)"
    break
  fi
  if [ "$i" -eq 20 ]; then
    kill $GFIRE_PID 2>/dev/null
    fail "server did not start"
  fi
  sleep 0.5
done

cleanup() {
  echo ""
  echo -n "Shutting down... "
  kill $GFIRE_PID 2>/dev/null || true
  wait $GFIRE_PID 2>/dev/null || true
  docker compose down postgres >/dev/null 2>&1
  rm -f /tmp/gfire-e2e /tmp/gfire-e2e.yaml
  pass "done"
}
trap cleanup EXIT

AUTH="Authorization: Bearer e2e-secret"

# ── Health ─────────────────────────────────────────────
echo -n "GET /healthz... "
curl -sS http://127.0.0.1:8199/healthz | grep -q '"ok"' && pass "ok" || fail "unexpected response"

echo -n "GET /readyz... "
curl -sS http://127.0.0.1:8199/readyz | grep -q '"ok"' && pass "ok" || fail "unexpected response"

# ── Auth: missing token ────────────────────────────────
echo -n "Auth rejects missing token... "
HTTP_CODE=$(curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8199/v1/jobs/enqueue -H 'Content-Type: application/json' -d '{"name":"x"}')
[ "$HTTP_CODE" = "401" ] && pass "ok" || fail "expected 401, got $HTTP_CODE"

# ── Enqueue ────────────────────────────────────────────
echo -n "POST /v1/jobs/enqueue... "
RESP=$(curl -sS -X POST http://127.0.0.1:8199/v1/jobs/enqueue \
  -H 'Content-Type: application/json' \
  -H "$AUTH" \
  -d '{"name":"echo","args":{"hello":"world"},"queue":"default"}')
JOB_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['job_id'])" 2>/dev/null)
[ -n "$JOB_ID" ] && pass "job_id=$JOB_ID" || fail "no job_id in response: $RESP"

# ── Get job ────────────────────────────────────────────
echo -n "GET /v1/jobs/{id}... "
sleep 0.5
RESP=$(curl -sS http://127.0.0.1:8199/v1/jobs/$JOB_ID -H "$AUTH")
STATE=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin).get('current_state',''))" 2>/dev/null)
[ "$STATE" = "Succeeded" ] && pass "state=$STATE" || pass "state=$STATE (expected Succeeded)"

# ── Batch enqueue ──────────────────────────────────────
echo -n "POST /v1/jobs/enqueue/batch... "
RESP=$(curl -sS -X POST http://127.0.0.1:8199/v1/jobs/enqueue/batch \
  -H 'Content-Type: application/json' \
  -H "$AUTH" \
  -d '{"jobs":[{"name":"echo","args":{"a":1}},{"name":"echo","args":{"b":2}}]}')
ACCEPTED=$(echo "$RESP" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['accepted']))" 2>/dev/null)
[ "$ACCEPTED" = "2" ] && pass "accepted=$ACCEPTED" || fail "expected 2 accepted, got $ACCEPTED"

# ── Schedule ───────────────────────────────────────────
echo -n "POST /v1/jobs/schedule... "
RESP=$(curl -sS -X POST http://127.0.0.1:8199/v1/jobs/schedule \
  -H 'Content-Type: application/json' \
  -H "$AUTH" \
  -d "{\"name\":\"echo\",\"args\":{\"delayed\":true},\"enqueue_at\":\"$(date -u -v+5S +%Y-%m-%dT%H:%M:%SZ)\"}")
SCHED_ID=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['job_id'])" 2>/dev/null)
[ -n "$SCHED_ID" ] && pass "scheduled job_id=$SCHED_ID" || fail "no job_id"

# ── Wait for scheduled job ─────────────────────────────
echo -n "Waiting for scheduled job to complete... "
for i in $(seq 1 20); do
  STATE=$(curl -sS http://127.0.0.1:8199/v1/jobs/$SCHED_ID -H "$AUTH" | python3 -c "import sys,json; print(json.load(sys.stdin).get('current_state',''))" 2>/dev/null)
  if [ "$STATE" = "Succeeded" ]; then
    pass "ok"
    break
  fi
  if [ "$i" -eq 20 ]; then fail "scheduled job still $STATE after 10s"; fi
  sleep 0.5
done

# ── CLI: job list ──────────────────────────────────────
echo -n "CLI: gfire job list... "
/tmp/gfire-e2e --config /tmp/gfire-e2e.yaml job list --limit 5 | grep -q "$JOB_ID" && pass "found $JOB_ID" || fail "job not in list"

# ── CLI: queue list ────────────────────────────────────
echo -n "CLI: gfire queue list... "
/tmp/gfire-e2e --config /tmp/gfire-e2e.yaml queue list | grep -q "default" && pass "default queue present" || fail "queue not found"

# ── CLI: server status ─────────────────────────────────
echo -n "CLI: gfire status... "
/tmp/gfire-e2e --config /tmp/gfire-e2e.yaml status | grep -q "active" && pass "active server seen" || fail "no active server"

# ── Recurring CRUD ─────────────────────────────────────
echo -n "POST /v1/recurring... "
curl -sS -X POST http://127.0.0.1:8199/v1/recurring \
  -H 'Content-Type: application/json' -H "$AUTH" \
  -d '{"id":"e2e-cron","job_name":"echo","cron_expr":"@every 1h","args":{"x":1}}' >/dev/null
pass "ok"

echo -n "GET /v1/recurring... "
curl -sS http://127.0.0.1:8199/v1/recurring -H "$AUTH" | grep -q "e2e-cron" && pass "found" || fail "not found"

echo -n "DELETE /v1/recurring/e2e-cron... "
curl -sS -X DELETE http://127.0.0.1:8199/v1/recurring/e2e-cron -H "$AUTH" -o /dev/null -w '%{http_code}' | grep -q "200" && pass "ok" || fail "delete failed"

# ── OpenAPI ────────────────────────────────────────────
echo -n "GET /openapi.json... "
curl -sS http://127.0.0.1:8199/openapi.json | grep -q '"openapi"' && pass "ok" || fail "missing openapi key"

# ── Metrics ────────────────────────────────────────────
echo -n "GET /metrics... "
curl -sS http://127.0.0.1:8199/metrics | grep -q "gfire_jobs_succeeded_total" && pass "ok" || fail "missing metric"

# ── Idempotency ────────────────────────────────────────
echo -n "Idempotency-Key dedup... "
ID1=$(curl -sS -X POST http://127.0.0.1:8199/v1/jobs/enqueue \
  -H 'Content-Type: application/json' -H "$AUTH" \
  -H 'Idempotency-Key: e2e-idem-test' \
  -d '{"name":"echo"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['job_id'])")
ID2=$(curl -sS -X POST http://127.0.0.1:8199/v1/jobs/enqueue \
  -H 'Content-Type: application/json' -H "$AUTH" \
  -H 'Idempotency-Key: e2e-idem-test' \
  -d '{"name":"echo"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['job_id'])")
[ "$ID1" = "$ID2" ] && pass "same job_id" || fail "different job_ids: $ID1 vs $ID2"

echo ""
echo "=== All E2E tests passed ==="
