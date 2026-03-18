#!/bin/bash
# End-to-end test suite for the Notification System
# Requires: docker-compose up -d (system must be running)
# Usage: ./scripts/e2e_test.sh

set -e

BASE="${BASE_URL:-http://localhost:8080}"
PASS=0
FAIL=0

check() {
  local name="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "  PASS: $name"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $name (expected $expected, got $actual)"
    FAIL=$((FAIL+1))
  fi
}

echo "=========================================="
echo "  NOTIFICATION SYSTEM - E2E TEST SUITE"
echo "=========================================="
echo "  Target: $BASE"
echo ""

# --- Health ---
echo "--- Health Check ---"
HEALTH=$(curl -s $BASE/health)
STATUS=$(echo "$HEALTH" | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
check "health status is healthy" "healthy" "$STATUS"

# --- Create SMS ---
echo "--- Create SMS ---"
RESULT=$(curl -s -w "\n%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"recipient":"+905551234567","channel":"sms","content":"E2E test","priority":"high"}')
HTTP=$(echo "$RESULT" | tail -1)
check "create SMS returns 201" "201" "$HTTP"
SMS_ID=$(echo "$RESULT" | head -1 | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

sleep 2
SENT_STATUS=$(curl -s $BASE/api/v1/notifications/$SMS_ID | python3 -c "import sys,json; print(json.load(sys.stdin)['status'])")
check "SMS delivered (status=sent)" "sent" "$SENT_STATUS"

# --- Create Email ---
echo "--- Create Email ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d '{"recipient":"test@example.com","channel":"email","content":"E2E email","subject":"Test"}')
check "create email returns 201" "201" "$HTTP"

# --- Validation errors ---
echo "--- Validation ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" -d '{"channel":"sms","content":"No recipient"}')
check "missing recipient returns 400" "400" "$HTTP"

HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" -d '{"recipient":"+90555","channel":"fax","content":"Bad channel"}')
check "invalid channel returns 400" "400" "$HTTP"

LONG=$(python3 -c "print('x'*161)")
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" -d "{\"recipient\":\"+90555\",\"channel\":\"sms\",\"content\":\"$LONG\"}")
check "SMS >160 chars returns 400" "400" "$HTTP"

HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" -d '{"recipient":"a@b.com","channel":"email","content":"No subject"}')
check "email without subject returns 400" "400" "$HTTP"

# --- Idempotency ---
echo "--- Idempotency ---"
KEY="e2e-idemp-$(date +%s)"
curl -s -o /dev/null -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d "{\"recipient\":\"+90555\",\"channel\":\"sms\",\"content\":\"First\",\"idempotency_key\":\"$KEY\"}"
sleep 1
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d "{\"recipient\":\"+90555\",\"channel\":\"sms\",\"content\":\"Dup\",\"idempotency_key\":\"$KEY\"}")
check "duplicate idempotency key returns 409" "409" "$HTTP"

# --- Not found ---
echo "--- Not Found ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/v1/notifications/00000000-0000-0000-0000-000000000000)
check "get unknown ID returns 404" "404" "$HTTP"

# --- Batch ---
echo "--- Batch Create ---"
RESULT=$(curl -s -w "\n%{http_code}" -X POST $BASE/api/v1/notifications/batch \
  -H "Content-Type: application/json" \
  -d '{"notifications":[{"recipient":"+90551","channel":"sms","content":"B1"},{"recipient":"+90552","channel":"sms","content":"B2"}]}')
HTTP=$(echo "$RESULT" | tail -1)
COUNT=$(echo "$RESULT" | head -1 | python3 -c "import sys,json; print(json.load(sys.stdin)['count'])")
check "batch returns 201" "201" "$HTTP"
check "batch count is 2" "2" "$COUNT"

# --- List + pagination ---
echo "--- List & Pagination ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/v1/notifications?status=sent&limit=3")
check "list with filters returns 200" "200" "$HTTP"

PAGE=$(curl -s "$BASE/api/v1/notifications?limit=2")
HAS_MORE=$(echo "$PAGE" | python3 -c "import sys,json; print(json.load(sys.stdin)['pagination']['has_more'])")
check "pagination has_more is True" "True" "$HAS_MORE"

# --- Metrics ---
echo "--- Metrics ---"
METRICS=$(curl -s $BASE/metrics)
HAS_ALL=$(echo "$METRICS" | python3 -c "import sys,json; d=json.load(sys.stdin); print('queues' in d and 'latency' in d and 'circuit_breakers' in d and 'notifications' in d)")
check "metrics has all sections" "True" "$HAS_ALL"

# --- Swagger ---
echo "--- Swagger ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" $BASE/swagger/index.html)
check "swagger UI accessible" "200" "$HTTP"

# --- Content-Type ---
echo "--- Content-Type Enforcement ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" -X POST $BASE/api/v1/notifications \
  -H "Content-Type: text/plain" -d '{}')
check "wrong Content-Type returns 415" "415" "$HTTP"

# --- Template ---
echo "--- Template System ---"
TMPL_NAME="e2e_tmpl_$(date +%s)"
TMPL=$(curl -s -X POST $BASE/api/v1/templates \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$TMPL_NAME\",\"channel\":\"sms\",\"content\":\"Hi {{name}}, code {{code}}\"}")
TMPL_ID=$(echo "$TMPL" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
NOTIF=$(curl -s -X POST $BASE/api/v1/notifications \
  -H "Content-Type: application/json" \
  -d "{\"recipient\":\"+90555\",\"channel\":\"sms\",\"template_id\":\"$TMPL_ID\",\"variables\":{\"name\":\"Test\",\"code\":\"9999\"}}")
CONTENT=$(echo "$NOTIF" | python3 -c "import sys,json; print(json.load(sys.stdin)['content'])")
check "template renders correctly" "Hi Test, code 9999" "$CONTENT"

# --- WebSocket ---
echo "--- WebSocket ---"
HTTP=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/v1/ws)
check "websocket endpoint exists (426)" "426" "$HTTP"

# --- Results ---
echo ""
echo "=========================================="
TOTAL=$((PASS+FAIL))
echo "  RESULTS: $PASS/$TOTAL passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
  exit 1
fi
