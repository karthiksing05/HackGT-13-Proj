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
#   STRIPE_SELLER_PROFILE       the merchant account's TEST-mode Stripe profile (profile_test_…);
#                               empty = looked up and printed.
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
PROFILE="${STRIPE_SELLER_PROFILE:-}"
for k in "$AGENT_KEY" "$MERCHANT_KEY"; do
  case "$k" in sk_test_*|rk_test_*) ;; *) echo "refusing: Stripe keys must be test keys (sk_test_/rk_test_)" >&2; exit 1 ;; esac
done

API=https://api.stripe.com/v1
PREVIEW="Stripe-Version: 2026-04-22.preview"

# The seller profile must be the merchant account's TEST-mode profile
# (profile_test_…). Empty: look it up with the merchant key.
if [ -z "$PROFILE" ]; then
  PROFILE=$(curl -sS https://api.stripe.com/v2/network/business_profiles/me -H "Authorization: Bearer $MERCHANT_KEY" -H "$PREVIEW" | jq -r '.id // empty')
  [ -n "$PROFILE" ] || { echo "no Stripe profile on the merchant account: create one at https://dashboard.stripe.com/profiles (in test mode / the sandbox)" >&2; exit 1; }
  echo "==> seller profile (test mode): $PROFILE  (put this in STRIPE_SELLER_PROFILE)"
fi
case "$PROFILE" in
  profile_test_*) ;;
  *) echo "STRIPE_SELLER_PROFILE is a live-mode profile; test keys need the test one (profile_test_…). Unset it and this script prints it." >&2; exit 1 ;;
esac

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
    -d "usage_limits[expires_at]=$2"
}
charge() { # charge SPT AMOUNT → PaymentIntent JSON (merchant account)
  stripe "$MERCHANT_KEY" POST /payment_intents \
    -d "amount=$2" -d currency=usd -d confirm=true \
    -d "payment_method_data[shared_payment_granted_token]=$1" \
    -d "expand[]=payment_method" \
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
if [ -z "$SPT" ]; then
  echo "    FAILED: $(echo "$out" | err)"
  case "$out" in *"same as the counterparty"*)
    echo "    The agent and the merchant must be different Stripe accounts: make a second sandbox for SideQuestz Events" >&2
    echo "    and set STRIPE_MERCHANT_SECRET_KEY to its key (STRIPE_SELLER_PROFILE empty, so its profile is looked up)." >&2 ;;
  esac
  exit 1
fi
echo "    ok: $SPT status=$(echo "$out" | jq -r .status) payment_method=$PM"

echo "==> 2. merchant charges it (\$10.00)"
pi=$(charge "$SPT" 1000)
echo "    status=$(echo "$pi" | jq -r '.status // "error"') id=$(echo "$pi" | jq -r '.id // "-"') error=$(echo "$pi" | err)"
echo "    card on the PaymentIntent (what Events shows): $(echo "$pi" | jq -c '.payment_method.card // null | if . then {brand, last4} else null end')"
echo "    granted token: $(stripe "$MERCHANT_KEY" GET "/shared_payment/granted_tokens/$SPT" | jq -c '{status: (.deactivated_reason // "active"), usage_limits}')"

echo "==> 3. charge above max_amount (SPT max \$10.00, charge \$12.50)"
over=$(issue 1000 "$soon" | jq -r '.id // empty')
echo "    error=$(charge "$over" 1250 | err)"

echo "==> 4. reuse the SPT spent in step 2"
echo "    error=$(charge "$SPT" 1000 | err)"

echo "==> 5. an SPT whose expiry has passed (waits ~25s)"
exp=$(issue 1000 $(( $(date +%s) + 20 )))
if [ "$(echo "$exp" | jq -r '.id // empty')" = "" ]; then
  echo "    issue refused: $(echo "$exp" | err)"
else
  sleep 25
  echo "    error=$(charge "$(echo "$exp" | jq -r .id)" 1000 | err)"
fi
echo "==> done: copy the codes above into docs/AGENTIC_CHECKOUT.md (Stripe error codes)"
