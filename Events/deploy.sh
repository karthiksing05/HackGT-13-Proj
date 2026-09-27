#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load DEPLOY_* variables from .env
if [ -f "${SCRIPT_DIR}/.env" ]; then
    export $(grep '^DEPLOY_' "${SCRIPT_DIR}/.env" | xargs)
fi

SERVER_IP="${DEPLOY_HOST}"
SERVER_USER="${DEPLOY_USER}"
REMOTE_DIR="/opt/events"
SERVICE_NAME="events"

# Set up askpass to feed the password without requiring sshpass
ASKPASS_FILE=$(mktemp)
printf '#!/usr/bin/env bash\nprintf "%%s\\n" "%s"\n' "${DEPLOY_PASSWORD}" > "$ASKPASS_FILE"
chmod 700 "$ASKPASS_FILE"
export SSH_ASKPASS="$ASKPASS_FILE" SSH_ASKPASS_REQUIRE="force" DISPLAY="${DISPLAY:-:0}"
trap 'rm -f "$ASKPASS_FILE"' EXIT INT TERM

SSH_CMD="ssh -o StrictHostKeyChecking=accept-new ${SERVER_USER}@${SERVER_IP}"
SCP_CMD="scp -o StrictHostKeyChecking=accept-new"

echo "Deploying to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}..."

echo "1. Creating remote directory..."
$SSH_CMD "mkdir -p ${REMOTE_DIR}" < /dev/null

echo "2. Uploading binary, service file, and .env.example..."
$SCP_CMD "${SCRIPT_DIR}/bin/events-server" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/events-server.new" < /dev/null
$SCP_CMD "${SCRIPT_DIR}/events.service" "${SERVER_USER}@${SERVER_IP}:/tmp/events.service.new" < /dev/null
$SCP_CMD "${SCRIPT_DIR}/.env.example" "${SERVER_USER}@${SERVER_IP}:/tmp/.env.example" < /dev/null

echo "3. Updating permissions and restarting service..."
$SSH_CMD "
cd ${REMOTE_DIR}
chmod 755 events-server.new
if [ -f events-server ]; then mv -f events-server events-server.prev; fi
mv -f events-server.new events-server

if [ ! -f .env ]; then mv /tmp/.env.example .env; fi
rm -f /tmp/.env.example

# Create user if not exists
id -u sidequestz >/dev/null 2>&1 || useradd --system --home ${REMOTE_DIR} --shell /usr/sbin/nologin sidequestz
chown sidequestz:sidequestz ${REMOTE_DIR}
if [ -f ${REMOTE_DIR}/.env ]; then chown sidequestz:sidequestz ${REMOTE_DIR}/.env; fi

# Install service
sudo mv -f /tmp/events.service.new /etc/systemd/system/${SERVICE_NAME}.service
sudo systemctl daemon-reload
sudo systemctl enable ${SERVICE_NAME}.service >/dev/null 2>&1 || true
sudo systemctl restart ${SERVICE_NAME}
" < /dev/null

echo "Deployment complete!"
