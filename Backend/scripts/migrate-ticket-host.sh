#!/usr/bin/env bash
# Points the Saltlight catalog's ticketed events at the sandbox ticket merchant:
# rewrites url, ticketUrl and sources[].url from https://saltlight.example/ to
# https://events.sidequestz.tech/ on every freetime.demo_activities document
# whose ticketUrl is on the old host. Places without tickets keep
# saltlight.example. Embeddings are untouched (URLs aren't in embeddingText).
#
#   Backend/scripts/migrate-ticket-host.sh [--apply] [--container NAME | --uri URI] [--db NAME]
#
#   (default)         the production server over SSH, like pull-demo-catalog.sh
#   --container NAME  a local Docker container running MongoDB instead
#   --uri URI         a local mongosh against URI instead
#   --db NAME         database (default: freetime)
#   --apply           write the change; without it, only count what would change
#
# Idempotent: a second run finds nothing to change. DEPLOY_HOST, DEPLOY_USER
# and DEPLOY_PASSWORD come from the environment or the repo-root .env (never
# printed).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

OLD="https://saltlight.example/"
NEW="https://events.sidequestz.tech/"
CONTAINER=""
URI=""
DB="freetime"
APPLY=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply) APPLY=true; shift ;;
    --container) CONTAINER="$2"; URI=""; shift 2 ;;
    --uri) URI="$2"; CONTAINER=""; shift 2 ;;
    --db) DB="$2"; shift 2 ;;
    -h|--help) sed -n '2,18p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

read -r -d '' JS <<EOF || true
const c = db.getSiblingDB('${DB}').demo_activities;
const filter = { ticketUrl: { \$regex: '^${OLD//./\\\\.}' } };
const n = c.countDocuments(filter);
print(n + ' ticketed activities on ${OLD}');
if (${APPLY} && n > 0) {
  const swap = (f) => ({ \$replaceOne: { input: f, find: '${OLD}', replacement: '${NEW}' } });
  const r = c.updateMany(filter, [{ \$set: {
    ticketUrl: swap('\$ticketUrl'),
    url: { \$cond: [{ \$eq: [{ \$type: '\$url' }, 'string'] }, swap('\$url'), '\$url'] },
    sources: { \$cond: [{ \$isArray: '\$sources' }, { \$map: { input: '\$sources', as: 's',
      in: { \$cond: [{ \$eq: [{ \$type: '\$\$s.url' }, 'string'] },
        { \$mergeObjects: ['\$\$s', { url: { \$replaceOne: { input: '\$\$s.url', find: '${OLD}', replacement: '${NEW}' } } }] },
        '\$\$s'] } } }, '\$sources'] },
  } }]);
  print('updated ' + r.modifiedCount);
}
print(c.countDocuments({ ticketUrl: { \$regex: '^${NEW//./\\\\.}' } }) + ' ticketed activities on ${NEW}');
EOF

if [ -n "$CONTAINER" ]; then
  docker exec "$CONTAINER" mongosh --quiet --eval "$JS"
elif [ -n "$URI" ]; then
  mongosh --quiet "$URI" --eval "$JS"
else
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
  ASKPASS_FILE=""
  cleanup() { [ -n "$ASKPASS_FILE" ] && rm -f "$ASKPASS_FILE" 2>/dev/null || true; }
  trap cleanup EXIT INT TERM
  if [ -n "${DEPLOY_PASSWORD:-}" ]; then
    ASKPASS_FILE=$(mktemp); chmod 700 "$ASKPASS_FILE"
    printf '#!/usr/bin/env bash\necho "${DEPLOY_PASS_INTERNAL}"\n' > "$ASKPASS_FILE"
    export DEPLOY_PASS_INTERNAL="$DEPLOY_PASSWORD" SSH_ASKPASS="$ASKPASS_FILE" SSH_ASKPASS_REQUIRE="force" DISPLAY="${DISPLAY:-:0}"
  fi
  echo "==> ${SERVER_USER}@${DEPLOY_HOST} ${DB}.demo_activities ($($APPLY && echo apply || echo dry run))"
  printf '%s' "$JS" | ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "${SERVER_USER}@${DEPLOY_HOST}" "mongosh --quiet --file /dev/stdin"
fi
$APPLY || echo "    dry run: pass --apply to write"
