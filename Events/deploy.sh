#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ../deploy-env.sh

bash build.sh
ssh "$server" 'mkdir -p /opt/events'
scp bin/events-server "$server:/opt/events/events-server.new"
scp .env "$server:/opt/events/.env"
scp events.service "$server:/etc/systemd/system/events.service"
ssh "$server" 'bash -se' <<'REMOTE'
cd /opt/events
chmod 755 events-server.new
mv -f events-server.new events-server
id -u sidequestz >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin sidequestz
chown sidequestz:sidequestz .env
chmod 600 .env
sed -i 's|MERCHANT_BASE_URL=.*|MERCHANT_BASE_URL=https://events.sidequestz.tech|' .env
systemctl daemon-reload
systemctl enable events
systemctl restart events
REMOTE
