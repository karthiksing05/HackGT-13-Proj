#!/usr/bin/env bash
# Runs the showcase seed (Backend/cmd/seed) against the live database through an SSH tunnel, then
# closes the tunnel. A dry run unless you pass --apply; --remove (with --apply) takes it back out.
# Other flags go to the seed as they are (--world atlanta|saltlight|all, --demo-email ADDRESS,
# --history @handle,… for the past of real accounts).
#
# The connection comes from DEPLOY_HOST / DEPLOY_USER / DEPLOY_PASSWORD in the root .env (the same
# settings the deploy scripts use). The tunnel uses local ports 27018 (MongoDB) and 8001 (the ML
# service), so it can never reach a local MongoDB on 27017 by mistake.
#
# Usage: Backend/scripts/seed-live.sh [--apply] [--remove] [--world atlanta|saltlight|all]
#        Backend/scripts/seed-live.sh --history @handle[=outdoors|nightlife|arts],… [--apply] [--remove]
set -euo pipefail
cd "$(dirname "$0")/.."

# deploy-common.sh reads $1/$2 as host/user: hide the seed's flags from it.
args=("$@")
set --
source ../deploy-common.sh
set -- ${args[@]+"${args[@]}"}

mongo_port=27018
ml_port=8001
seed=./bin/seed
# Rebuild whenever Go is here, so bin/seed is never older than cmd/seed.
if command -v go >/dev/null; then
  go build -o "$seed" ./cmd/seed
elif [[ ! -x "$seed" ]]; then
  echo "bin/seed is missing and Go isn't installed: build it with 'go build -o bin/seed ./cmd/seed'." >&2
  exit 1
fi
if nc -z 127.0.0.1 "$mongo_port" 2>/dev/null; then
  echo "Port $mongo_port is already in use (an old tunnel?). Close it and try again." >&2
  exit 1
fi

echo "==> tunnel to $server (MongoDB → 127.0.0.1:$mongo_port, ML → 127.0.0.1:$ml_port)"
ssh -N -o ExitOnForwardFailure=yes -o StrictHostKeyChecking=accept-new \
  -L "$mongo_port:127.0.0.1:27017" -L "$ml_port:127.0.0.1:8000" "$server" &
tunnel=$!
trap 'kill "$tunnel" 2>/dev/null || true; rm -f "${askpass:-}"' EXIT

for _ in $(seq 1 30); do
  nc -z 127.0.0.1 "$mongo_port" 2>/dev/null && break
  kill -0 "$tunnel" 2>/dev/null || { echo "The tunnel to $server closed (check DEPLOY_* in .env)." >&2; exit 1; }
  sleep 1
done
nc -z 127.0.0.1 "$mongo_port" 2>/dev/null || { echo "The tunnel to $server didn't open in 30 s." >&2; exit 1; }

MONGO_URI="mongodb://127.0.0.1:$mongo_port" MONGO_DB="freetime" ML_SERVICE_URL="http://127.0.0.1:$ml_port" \
  "$seed" ${args[@]+"${args[@]}"}
