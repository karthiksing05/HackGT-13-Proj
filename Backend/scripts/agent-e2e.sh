#!/usr/bin/env bash
# Live end-to-end check of agentic checkout: real Muse, a local Events
# merchant and Stripe test mode. Runs pkg/agent/live_test.go (build tag
# `live`): a run that buys two tickets, a budget that fits one, and the
# merchant's sold_out / overcharge / price_bump scenarios. Each run must be
# finished by Muse (agent=muse), not the code fallback; the transcripts are
# printed. Costs a few dozen Muse calls; nothing leaves Stripe test mode.
#
#   Backend/scripts/agent-e2e.sh [go test -run pattern, default TestLiveMuse]
#
# Reads from the environment or the repo-root .env (never printed):
#   STRIPE_SECRET_KEY           the agent sandbox (issues tokens)
#   STRIPE_MERCHANT_SECRET_KEY  the merchant sandbox (Events charges with it)
#   STRIPE_SELLER_PROFILE       the merchant sandbox's profile_test_…
#                               (empty: looked up with the merchant key)
#   MUSE_API_KEY                Meta Model API
# Needs a local MongoDB (MONGO_TEST_URI, default mongodb://127.0.0.1:27017).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PATTERN="${1:-TestLiveMuse}"
PORT="${E2E_EVENTS_PORT:-8095}"

ENV_FILE="$ROOT/.env"
if [ -f "$ENV_FILE" ]; then
  while IFS='=' read -r key val || [ -n "$key" ]; do
    key=$(printf '%s' "$key" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    case "$key" in STRIPE_SECRET_KEY|STRIPE_MERCHANT_SECRET_KEY|STRIPE_SELLER_PROFILE|MUSE_API_KEY|MUSE_MODEL) ;; *) continue ;; esac
    val=$(printf '%s' "$val" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    val="${val#\"}"; val="${val%\"}"; val="${val#\'}"; val="${val%\'}"
    [ -z "${!key:-}" ] && export "$key=$val"
  done < "$ENV_FILE"
fi
for k in STRIPE_SECRET_KEY STRIPE_MERCHANT_SECRET_KEY MUSE_API_KEY; do
  [ -n "${!k:-}" ] || { echo "$k is not set (repo-root .env or environment)" >&2; exit 1; }
done
if [ -z "${STRIPE_SELLER_PROFILE:-}" ]; then
  STRIPE_SELLER_PROFILE=$(curl -sS https://api.stripe.com/v2/network/business_profiles/me \
    -H "Authorization: Bearer $STRIPE_MERCHANT_SECRET_KEY" -H "Stripe-Version: 2026-04-22.preview" | jq -r '.id // empty')
  [ -n "$STRIPE_SELLER_PROFILE" ] || { echo "the merchant sandbox has no Stripe profile (dashboard.stripe.com/profiles)" >&2; exit 1; }
  export STRIPE_SELLER_PROFILE
fi

WORK=$(mktemp -d)
EVENTS_PID=""
cleanup() {
  [ -n "$EVENTS_PID" ] && kill "$EVENTS_PID" 2>/dev/null || true
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

echo "==> starting Events on :$PORT (merchant sandbox, database sqz_events_e2e)"
(cd "$ROOT/Events" && go build -o "$WORK/events" .)
# Started in $WORK: Events loads ./.env, and the repo's .env is not this merchant's config.
export E2E_DEMO_KEY="e2e-$(date +%s)"
(cd "$WORK" && exec env APP_ENV=dev HTTP_ADDR=":$PORT" MERCHANT_HOST=events.sidequestz.tech MERCHANT_BASE_URL="http://localhost:$PORT" \
  PAYMENTS_MODE=sandbox MONGO_URI="${MONGO_TEST_URI:-mongodb://127.0.0.1:27017}" MONGO_DB=sqz_events_e2e DEMO_KEY="$E2E_DEMO_KEY" \
  TAP_AGENT_PUBLIC_KEY= STRIPE_SECRET_KEY="$STRIPE_MERCHANT_SECRET_KEY" STRIPE_MERCHANT_SECRET_KEY="$STRIPE_MERCHANT_SECRET_KEY" \
  "$WORK/events" > "$WORK/events.log" 2>&1) &
EVENTS_PID=$!
for _ in $(seq 40); do
  curl -sf "http://localhost:$PORT/healthz" > /dev/null && break
  sleep 0.25
done
curl -sf "http://localhost:$PORT/healthz" > /dev/null || { echo "Events didn't start:" >&2; tail -20 "$WORK/events.log" >&2; exit 1; }

echo "==> running $PATTERN (real Muse; a few minutes)"
cd "$ROOT/Backend"
E2E_MERCHANT_BASE_URL="http://localhost:$PORT" MONGO_TEST_URI="${MONGO_TEST_URI:-mongodb://127.0.0.1:27017}" \
  go test -tags live -count=1 -v -timeout 15m -run "$PATTERN" ./pkg/agent/ 2>&1 | sed -E 's/(sk|rk)_(test|live)_[A-Za-z0-9]+/\1_\2_***/g'
