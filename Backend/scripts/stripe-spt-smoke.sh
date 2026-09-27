#!/usr/bin/env bash
# Proves the Stripe test-mode Shared Payment Token (SPT) path agentic checkout
# depends on, and prints the error codes the decline mapping needs.
#
#   Backend/scripts/stripe-spt-smoke.sh
#
# Reads from the environment or the repo-root .env (never printed):
#   STRIPE_SECRET_KEY           the agent account (SideQuestz): issues SPTs. sk_test_ only.
#   STRIPE_MERCHANT_SECRET_KEY  the merchant account (SideQuestz Events): confirms
#                               PaymentIntents. Defaults to STRIPE_SECRET_KEY (one account).
#   STRIPE_SELLER_PROFILE       the merchant account's Stripe profile (profile_…).
#
# Steps: 1 issue an SPT, 2 the merchant charges it, 3 a charge above max_amount,
# 4 reusing a spent SPT, 5 an SPT that has expired. Nothing leaves test mode.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ENV_FILE="${SCRIPT_DIR}/../../.env"
if [ -f "$ENV_FILE" ]; then
  while IFS='=' read -r key val || [ -n "$key" ]; do
    key=$(printf '%s' "$key" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    case "$key" in STRIPE_SECRET_KEY|STRIPE_MERCHANT_SECRET_KEY|STRIPE_SELLER_PROFILE) ;; *) continue ;; esac
    val=$(printf '%s' "$val" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    val="${val#\"}"; val="${val%\"}"; val="${val#\'}"; val="${val%\'}"
    [ -z "${!key:-}" ] && export "$key=$val"
  done < "$ENV_FILE"
fi
AGENT_KEY="${STRIPE_SECRET_KEY:?STRIPE_SECRET_KEY is not set (repo-root .env or environment)}"
MERCHANT_KEY="${STRIPE_MERCHANT_SECRET_KEY:-$AGENT_KEY}"
PROFILE="${STRIPE_SELLER_PROFILE:?STRIPE_SELLER_PROFILE is not set (the merchant account's profile_…)}"
for k in "$AGENT_KEY" "$MERCHANT_KEY"; do
  case "$k" in sk_test_*|rk_test_*) ;; *) echo "refusing: Stripe keys must be test keys (sk_test_/rk_test_)" >&2; exit 1 ;; esac
done

API=https://api.stripe.com/v1
PREVIEW="Stripe-Version: 2026-04-22.preview"
RETURN_URL="https://api.sidequestz.tech/checkout/stripe/return"

# stripe KEY METHOD PATH [curl args…]: prints the JSON body; never fails the script.
stripe() {
  local key="$1" method="$2" path="$3"; shift 3
  curl -sS -X "$method" "$API$path" -u "$key:" -H "$PREVIEW" "$@" || true
}
err() { jq -r '.error | if . then "\(.type // "") / \(.code // "") / \(.decline_code // "") / \(.message // "")" else "none" end'; }

issue() { # issue MAX_CENTS EXPIRES_AT → issued-token JSON
  stripe "$AGENT_KEY" POST /shared_payment/issued_tokens \
    -d "payment_method=${PM}" \
    -d "seller_details[network_business_profile]=${PROFILE}" \
    -d "usage_limits[currency]=usd" \
    -d "usage_limits[max_amount]=$1" \
    -d "usage_limits[expires_at]=$2" \
    --data-urlencode "return_url=${RETURN_URL}"
}
charge() { # charge SPT AMOUNT → PaymentIntent JSON (merchant account)
  stripe "$MERCHANT_KEY" POST /payment_intents \
    -d "amount=$2" -d currency=usd -d confirm=true \
    -d "payment_method_data[shared_payment_granted_token]=$1" \
    -d "metadata[source]=stripe-spt-smoke"
}

now=$(date +%s); soon=$((now + 600))

echo "==> 1. issue an SPT (max \$10.00) from pm_card_visa"
PM=pm_card_visa
out=$(issue 1000 "$soon")
if [ "$(echo "$out" | jq -r '.id // empty')" = "" ]; then
  echo "    pm_card_visa rejected: $(echo "$out" | err)"
  echo "    creating a PaymentMethod from tok_visa instead"
  PM=$(stripe "$AGENT_KEY" POST /payment_methods -d type=card -d "card[token]=tok_visa" | jq -r '.id // empty')
  [ -n "$PM" ] || { echo "    could not create a PaymentMethod" >&2; exit 1; }
  out=$(issue 1000 "$soon")
fi
SPT=$(echo "$out" | jq -r '.id // empty')
[ -n "$SPT" ] || { echo "    FAILED: $(echo "$out" | err)"; exit 1; }
echo "    ok: $SPT status=$(echo "$out" | jq -r .status) payment_method=$PM"

echo "==> 2. merchant charges it (\$10.00)"
pi=$(charge "$SPT" 1000)
echo "    status=$(echo "$pi" | jq -r '.status // "error"') id=$(echo "$pi" | jq -r '.id // "-"') error=$(echo "$pi" | err)"
echo "    granted token: $(stripe "$MERCHANT_KEY" GET "/shared_payment/granted_tokens/$SPT" | jq -c '{status: (.deactivated_reason // "active"), usage_limits, card: (.payment_method_details.card // .payment_method_preview.card // null) | if . then {brand, last4} else null end}')"

echo "==> 3. charge above max_amount (SPT max \$10.00, charge \$12.50)"
over=$(issue 1000 "$soon" | jq -r '.id // empty')
echo "    error=$(charge "$over" 1250 | err)"

echo "==> 4. reuse the SPT spent in step 2"
echo "    error=$(charge "$SPT" 1000 | err)"

echo "==> 5. an SPT whose expiry has passed"
exp=$(issue 1000 $((now + 5)))
if [ "$(echo "$exp" | jq -r '.id // empty')" = "" ]; then
  echo "    issue refused: $(echo "$exp" | err)"
else
  sleep 8
  echo "    error=$(charge "$(echo "$exp" | jq -r .id)" 1000 | err)"
fi
echo "==> done: copy the codes above into docs/AGENTIC_CHECKOUT.md (Stripe error codes)"
