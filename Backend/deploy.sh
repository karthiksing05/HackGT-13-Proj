#!/usr/bin/env bash
# Builds and deploys the backend to the VPS:
#   1. ./build.sh --amd64 (server + admin, tests included)
#   2. uploads both binaries to /opt/backend as *.new and swaps them in,
#      keeping sidequestz-server.prev / sidequestz-admin.prev for rollback
#   3. syncs backend.service, restarts the unit, checks it is active
#   --seed additionally runs "sidequestz-admin ensure-indexes" on the server.
#
# The server's /opt/backend/.env is never uploaded or touched (pre-flight
# checks it exists). Connection settings come from DEPLOY_HOST, DEPLOY_USER,
# DEPLOY_PASSWORD (optional; sshpass or SSH_ASKPASS), DEPLOY_REMOTE_DIR,
# DEPLOY_SERVICE, read from the environment or the repo-root .env / Backend/.env.
# Usage: ./deploy.sh [host] [user] [--seed]
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load DEPLOY_* only (never echoed) from the first env file found.
load_deploy_vars() {
  local file="$1"
  [ -f "$file" ] || return 0
  while IFS='=' read -r key val || [ -n "$key" ]; do
    key=$(printf '%s' "$key" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    case "$key" in DEPLOY_*) ;; *) continue ;; esac
    val=$(printf '%s' "$val" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    val="${val#\"}"; val="${val%\"}"; val="${val#\'}"; val="${val%\'}"
    [ -z "${!key:-}" ] && export "$key=$val"
  done < "$file"
}
load_deploy_vars "${SCRIPT_DIR}/../.env"
load_deploy_vars "${SCRIPT_DIR}/.env"

SEED=false
POSITIONAL=()
for arg in "$@"; do
  case "$arg" in
    --seed) SEED=true ;;
    *) POSITIONAL+=("$arg") ;;
  esac
done
SERVER_IP="${POSITIONAL[0]:-${DEPLOY_HOST:-}}"
SERVER_USER="${POSITIONAL[1]:-${DEPLOY_USER:-root}}"
SERVER_PASS="${DEPLOY_PASSWORD:-}"
REMOTE_DIR="${DEPLOY_REMOTE_DIR:-/opt/backend}"
SERVICE_NAME="${DEPLOY_SERVICE:-backend}"
[ -n "$SERVER_IP" ] || { echo "DEPLOY_HOST (or the host argument) is required" >&2; exit 1; }

# Non-interactive SSH: sshpass when available, else SSH_ASKPASS.
AUTH_MODE="none"; ASKPASS_FILE=""
cleanup() { [ -n "$ASKPASS_FILE" ] && rm -f "$ASKPASS_FILE" 2>/dev/null || true; }
trap cleanup EXIT INT TERM
if [ -n "$SERVER_PASS" ]; then
  if command -v sshpass >/dev/null 2>&1; then
    AUTH_MODE="sshpass"
  else
    AUTH_MODE="askpass"
    ASKPASS_FILE=$(mktemp); chmod 700 "$ASKPASS_FILE"
    printf '#!/usr/bin/env bash\necho "${DEPLOY_PASS_INTERNAL}"\n' > "$ASKPASS_FILE"
    export DEPLOY_PASS_INTERNAL="$SERVER_PASS" SSH_ASKPASS="$ASKPASS_FILE" SSH_ASKPASS_REQUIRE="force" DISPLAY="${DISPLAY:-:0}"
  fi
fi
run_ssh() {
  case "$AUTH_MODE" in
    sshpass) SSHPASS="$SERVER_PASS" sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "$1" ;;
    askpass) ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "$1" < /dev/null ;;
    *) ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "$1" ;;
  esac
}
run_scp() {
  case "$AUTH_MODE" in
    sshpass) SSHPASS="$SERVER_PASS" sshpass -e scp -o StrictHostKeyChecking=accept-new "$1" "${SERVER_USER}@${SERVER_IP}:$2" ;;
    askpass) scp -o StrictHostKeyChecking=accept-new "$1" "${SERVER_USER}@${SERVER_IP}:$2" < /dev/null ;;
    *) scp -o StrictHostKeyChecking=accept-new "$1" "${SERVER_USER}@${SERVER_IP}:$2" ;;
  esac
}

echo "==> deploy to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR} (auth: ${AUTH_MODE}, seed: ${SEED})"

echo "==> [1/4] build"
bash "${SCRIPT_DIR}/build.sh" --amd64
for bin in sidequestz-server sidequestz-admin; do
  [ -s "${SCRIPT_DIR}/bin/${bin}" ] || { echo "bin/${bin} missing" >&2; exit 1; }
done

echo "==> [2/4] pre-flight"
run_ssh "mkdir -p ${REMOTE_DIR} && test -s ${REMOTE_DIR}/.env" \
  || { echo "${REMOTE_DIR}/.env is missing or empty on the server; create it first (see docs/design/app-docs-deploy.md §3.2). Nothing was uploaded." >&2; exit 1; }

echo "==> [3/4] upload binaries and unit"
for bin in sidequestz-server sidequestz-admin; do
  run_scp "${SCRIPT_DIR}/bin/${bin}" "${REMOTE_DIR}/${bin}.new"
done
run_scp "${SCRIPT_DIR}/backend.service" "/etc/systemd/system/${SERVICE_NAME}.service.new"

echo "==> [4/4] swap, restart, verify"
REMOTE_CMDS="set -e
cd ${REMOTE_DIR}
for bin in sidequestz-server sidequestz-admin; do
  chmod 755 \${bin}.new
  if [ -f \${bin} ]; then mv -f \${bin} \${bin}.prev; fi
  mv -f \${bin}.new \${bin}
done
mv -f /etc/systemd/system/${SERVICE_NAME}.service.new /etc/systemd/system/${SERVICE_NAME}.service
id -u sidequestz >/dev/null 2>&1 || useradd --system --home ${REMOTE_DIR} --shell /usr/sbin/nologin sidequestz
chown sidequestz:sidequestz ${REMOTE_DIR}/.env
chmod 600 ${REMOTE_DIR}/.env
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}.service >/dev/null 2>&1 || true
systemctl restart ${SERVICE_NAME}
sleep 2
systemctl is-active --quiet ${SERVICE_NAME} || { systemctl status ${SERVICE_NAME} --no-pager | tail -20; exit 1; }
curl -fsS http://127.0.0.1:8080/healthz
echo
"
if [ "$SEED" = true ]; then
  REMOTE_CMDS+="${REMOTE_DIR}/sidequestz-admin --env-file ${REMOTE_DIR}/.env ensure-indexes
"
fi
run_ssh "$REMOTE_CMDS"
echo "==> deployed; rollback: mv -f ${REMOTE_DIR}/sidequestz-server.prev ${REMOTE_DIR}/sidequestz-server && systemctl restart ${SERVICE_NAME}"
