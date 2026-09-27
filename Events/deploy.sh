#!/usr/bin/env bash
# Builds and deploys the events merchant server to the VPS:
#   1. ./build.sh (the linux/amd64 events-server; run `go test ./...` first)
#   2. uploads binary to /opt/events as events-server.new and swaps it in,
#      keeping events-server.prev for rollback
#   3. syncs events.service, restarts the unit, checks it is active (healthz)
#
# The server's /opt/events/.env is uploaded during deployment.
# Connection settings come from DEPLOY_HOST, DEPLOY_USER,
# DEPLOY_PASSWORD, DEPLOY_EVENTS_REMOTE_DIR (default /opt/events), DEPLOY_EVENTS_SERVICE (default events),
# read from the environment or ../.env / .env.
# Usage: ./deploy.sh [host] [user]
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

POSITIONAL=()
for arg in "$@"; do
  POSITIONAL+=("$arg")
done
SERVER_IP="${POSITIONAL[0]:-${DEPLOY_HOST:-}}"
SERVER_USER="${POSITIONAL[1]:-${DEPLOY_USER:-root}}"
SERVER_PASS="${DEPLOY_PASSWORD:-}"
REMOTE_DIR="${DEPLOY_EVENTS_REMOTE_DIR:-${DEPLOY_REMOTE_DIR_EVENTS:-/opt/events}}"
SERVICE_NAME="${DEPLOY_EVENTS_SERVICE:-events}"
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

echo "==> deploy events merchant to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR} (auth: ${AUTH_MODE})"

echo "==> [1/4] build"
bash "${SCRIPT_DIR}/build.sh" --amd64
[ -s "${SCRIPT_DIR}/bin/events-server" ] || { echo "bin/events-server missing" >&2; exit 1; }

echo "==> [2/4] pre-flight"
run_ssh "mkdir -p ${REMOTE_DIR}"

echo "==> [3/4] upload binary, unit, and env"
run_scp "${SCRIPT_DIR}/bin/events-server" "${REMOTE_DIR}/events-server.new"
run_scp "${SCRIPT_DIR}/.env" "${REMOTE_DIR}/.env"
run_scp "${SCRIPT_DIR}/events.service" "/etc/systemd/system/${SERVICE_NAME}.service.new"

echo "==> [4/4] swap, restart, verify"
REMOTE_CMDS="set -e
cd ${REMOTE_DIR}
chmod 755 events-server.new
if [ -f events-server ]; then mv -f events-server events-server.prev; fi
mv -f events-server.new events-server
mv -f /etc/systemd/system/${SERVICE_NAME}.service.new /etc/systemd/system/${SERVICE_NAME}.service
id -u sidequestz >/dev/null 2>&1 || useradd --system --home ${REMOTE_DIR} --shell /usr/sbin/nologin sidequestz
chown -R sidequestz:sidequestz ${REMOTE_DIR}
chmod 600 ${REMOTE_DIR}/.env
sed -i 's|MERCHANT_BASE_URL=.*|MERCHANT_BASE_URL=https://events.sidequestz.tech|' ${REMOTE_DIR}/.env
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}.service >/dev/null 2>&1 || true
systemctl restart ${SERVICE_NAME}
sleep 2
systemctl is-active --quiet ${SERVICE_NAME} || { systemctl status ${SERVICE_NAME} --no-pager | tail -20; exit 1; }
curl -fsS http://127.0.0.1:8085/healthz
echo
"
run_ssh "$REMOTE_CMDS"
echo "==> deployed events merchant; rollback: mv -f ${REMOTE_DIR}/events-server.prev ${REMOTE_DIR}/events-server && systemctl restart ${SERVICE_NAME}"
