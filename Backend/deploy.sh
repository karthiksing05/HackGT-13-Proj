#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ../deploy-common.sh

bash build.sh
ssh "$server" 'mkdir -p /opt/backend'
scp bin/sidequestz-server "$server:/opt/backend/sidequestz-server.new"
scp backend.service "$server:/etc/systemd/system/sidequestz.service"
ssh "$server" 'bash -se' <<'REMOTE'
cd /opt/backend
chmod 755 sidequestz-server.new
mv -f sidequestz-server.new sidequestz-server
id -u sidequestz >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin sidequestz
systemctl daemon-reload
systemctl enable sidequestz
systemctl restart sidequestz
REMOTE
