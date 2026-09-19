#!/usr/bin/env bash
set -euo pipefail

BASE="${SHOP_GATEWAY_URL:-http://127.0.0.1:18080}"

curl -sf "${BASE}/healthz" >/dev/null

echo "GET /users/user-1"
curl -sf "${BASE}/users/user-1"
echo

echo "POST /checkout"
curl -sf -X POST "${BASE}/checkout" \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"user-1","items":[{"sku":"sku-widget","qty":1}]}'
echo
