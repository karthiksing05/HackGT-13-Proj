#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ../deploy-common.sh

ssh "$server" 'set -e
umask 077
mkdir -p /opt/events
id -u sidequestz >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin sidequestz
cat > /opt/events/.env.new
chown sidequestz:sidequestz /opt/events/.env.new
chmod 600 /opt/events/.env.new
mv -f /opt/events/.env.new /opt/events/.env
systemctl restart events' < .env
