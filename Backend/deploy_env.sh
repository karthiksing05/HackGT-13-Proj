#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ../deploy-common.sh

ssh "$server" 'set -e
umask 077
mkdir -p /opt/backend
id -u sidequestz >/dev/null 2>&1 || useradd --system --shell /usr/sbin/nologin sidequestz
cat > /opt/backend/.env.new
chown sidequestz:sidequestz /opt/backend/.env.new
chmod 600 /opt/backend/.env.new
mv -f /opt/backend/.env.new /opt/backend/.env' < .env
