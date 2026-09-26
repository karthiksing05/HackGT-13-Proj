#!/usr/bin/env bash
# Copies the Saltlight demo catalog from the production MongoDB into a local
# MongoDB. The server's freetime.demo_activities is the catalog of record: 100
# activities, each with its embeddingText and 1024-d Qwen3-Embedding-0.6B
# vector. dataingestion/demo/saltlight_harbor.json is only the generator's raw
# output and has neither, so don't import it for anything that ranks.
#
#   Backend/scripts/pull-demo-catalog.sh [--container NAME | --uri URI] [--db NAME]
#
#   --container NAME  restore through a Docker container running MongoDB (default: sq-mongo)
#   --uri URI         restore with a local mongorestore to URI instead
#   --db NAME         target database (default: freetime); its demo_activities is replaced
#
# DEPLOY_HOST, DEPLOY_USER and DEPLOY_PASSWORD come from the environment or the
# repo-root .env (never printed). The server needs mongodump (it has it).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

CONTAINER="sq-mongo"
URI=""
DB="freetime"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2"; URI=""; shift 2 ;;
    --uri) URI="$2"; CONTAINER=""; shift 2 ;;
    --db) DB="$2"; shift 2 ;;
    -h|--help) sed -n '2,17p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

# DEPLOY_* only, from the repo-root .env, unless already set.
ENV_FILE="${SCRIPT_DIR}/../../.env"
if [ -f "$ENV_FILE" ]; then
  while IFS='=' read -r key val || [ -n "$key" ]; do
    key=$(printf '%s' "$key" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    case "$key" in DEPLOY_HOST|DEPLOY_USER|DEPLOY_PASSWORD) ;; *) continue ;; esac
    val=$(printf '%s' "$val" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    val="${val#\"}"; val="${val%\"}"; val="${val#\'}"; val="${val%\'}"
    [ -z "${!key:-}" ] && export "$key=$val"
  done < "$ENV_FILE"
fi
[ -n "${DEPLOY_HOST:-}" ] || { echo "DEPLOY_HOST is not set (repo-root .env or environment)" >&2; exit 1; }
SERVER_USER="${DEPLOY_USER:-root}"

# Non-interactive SSH with the password when there is one.
ASKPASS_FILE=""
cleanup() { [ -n "$ASKPASS_FILE" ] && rm -f "$ASKPASS_FILE" 2>/dev/null || true; }
trap cleanup EXIT INT TERM
if [ -n "${DEPLOY_PASSWORD:-}" ]; then
  ASKPASS_FILE=$(mktemp); chmod 700 "$ASKPASS_FILE"
  printf '#!/usr/bin/env bash\necho "${DEPLOY_PASS_INTERNAL}"\n' > "$ASKPASS_FILE"
  export DEPLOY_PASS_INTERNAL="$DEPLOY_PASSWORD" SSH_ASKPASS="$ASKPASS_FILE" SSH_ASKPASS_REQUIRE="force" DISPLAY="${DISPLAY:-:0}"
fi

restore() {
  local ns=(--nsInclude "freetime.demo_activities" --nsFrom "freetime.demo_activities" --nsTo "${DB}.demo_activities")
  if [ -n "$CONTAINER" ]; then
    docker exec -i "$CONTAINER" mongorestore --quiet --archive --gzip --drop "${ns[@]}"
  else
    mongorestore --quiet --uri "$URI" --archive --gzip --drop "${ns[@]}"
  fi
}

count() {
  local js="const c = db.getSiblingDB('${DB}').demo_activities; print(c.countDocuments() + ' activities, ' + c.countDocuments({'embedding.1023': {\$exists: true}}) + ' with 1024-d vectors, ' + c.countDocuments({embeddingText: {\$type: 'string', \$ne: ''}}) + ' with embedding texts')"
  if [ -n "$CONTAINER" ]; then
    docker exec "$CONTAINER" mongosh --quiet --eval "$js"
  else
    mongosh --quiet "$URI" --eval "$js"
  fi
}

echo "==> copying freetime.demo_activities from ${SERVER_USER}@${DEPLOY_HOST} to ${CONTAINER:+container ${CONTAINER}}${URI:+${URI}} as ${DB}.demo_activities"
ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "${SERVER_USER}@${DEPLOY_HOST}" \
  "mongodump --quiet --db freetime --collection demo_activities --archive --gzip" < /dev/null | restore
echo "==> ${DB}.demo_activities: $(count)"
echo "    indexes: run 'sidequestz-admin ensure-indexes' (or start the API) against that database to add the catalog indexes"
