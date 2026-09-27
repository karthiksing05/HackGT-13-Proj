#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

for service in ml Events Backend; do
  echo "Deploying $service code..."
  bash "$service/deploy.sh" "$@"
done
