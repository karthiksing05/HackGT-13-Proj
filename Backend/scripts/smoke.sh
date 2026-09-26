#!/usr/bin/env bash
# Smoke test against a running server: healthz → signup → me → refresh → logout.
# The area agents extend it (preferences → /plans/generate → /plans/route →
# /itineraries → list → /calendar/days → free-now post → /forum/posts/mine)
# as their endpoints land. Needs curl and jq. Never prints tokens.
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
JQ="${JQ:-jq}"
TZ_HEADER="X-Time-Zone: America/New_York"
command -v curl >/dev/null || { echo "curl required" >&2; exit 1; }
command -v "$JQ" >/dev/null || { echo "jq required (set JQ=/path/to/jq)" >&2; exit 1; }

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "ok   $*"; }

# call METHOD PATH [JSON] [TOKEN] → prints "STATUS\nBODY"
call() {
  local method="$1" path="$2" body="${3:-}" token="${4:-}"
  local args=(-s -o /tmp/sq_smoke_body -w '%{http_code}' -X "$method" -H "$TZ_HEADER" -H "Content-Type: application/json")
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(--data "$body")
  curl "${args[@]}" "$BASE_URL$path"
  echo
  cat /tmp/sq_smoke_body
}

status=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/healthz")
[ "$status" = "200" ] || fail "healthz returned $status"
pass "healthz"

EMAIL="smoke.$(date +%s).$RANDOM@example.test"
PASSWORD="smoke-$(date +%s)9"
SIGNUP=$($JQ -cn --arg e "$EMAIL" --arg p "$PASSWORD" '{name:"Smoke Test", email:$e, password:$p, date_of_birth:"2004-05-02"}')
out=$(call POST /auth/signup "$SIGNUP")
status=${out%%$'\n'*}; body=${out#*$'\n'}
[ "$status" = "201" ] || fail "signup returned $status: $body"
ACCESS=$(printf '%s' "$body" | $JQ -r .tokens.access_token)
REFRESH=$(printf '%s' "$body" | $JQ -r .tokens.refresh_token)
USER_ID=$(printf '%s' "$body" | $JQ -r .user.id)
[ -n "$ACCESS" ] && [ "$ACCESS" != "null" ] || fail "signup returned no access token"
pass "signup ($EMAIL)"

out=$(call GET /me "" "$ACCESS")
status=${out%%$'\n'*}; body=${out#*$'\n'}
[ "$status" = "200" ] || fail "me returned $status: $body"
[ "$(printf '%s' "$body" | $JQ -r .id)" = "$USER_ID" ] || fail "me returned another user"
pass "me"

out=$(call POST /auth/refresh "$($JQ -cn --arg r "$REFRESH" '{refresh_token:$r}')")
status=${out%%$'\n'*}; body=${out#*$'\n'}
[ "$status" = "200" ] || fail "refresh returned $status: $body"
NEW_REFRESH=$(printf '%s' "$body" | $JQ -r .refresh_token)
[ -n "$NEW_REFRESH" ] && [ "$NEW_REFRESH" != "$REFRESH" ] || fail "refresh token was not rotated"
pass "refresh (rotated)"

out=$(call POST /auth/logout "$($JQ -cn --arg r "$NEW_REFRESH" '{refresh_token:$r}')" "$ACCESS")
status=${out%%$'\n'*}
[ "$status" = "204" ] || fail "logout returned $status"
out=$(call POST /auth/refresh "$($JQ -cn --arg r "$NEW_REFRESH" '{refresh_token:$r}')")
status=${out%%$'\n'*}
[ "$status" = "401" ] || fail "refresh after logout returned $status"
pass "logout (session revoked)"

rm -f /tmp/sq_smoke_body
echo "smoke: all steps passed against $BASE_URL"
