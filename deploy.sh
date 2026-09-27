#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

for service in ml Events Backend; do
  echo "Deploying $service..."
  bash "$service/deploy.sh" "$@"
done
