#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

for service in Events Backend; do
  echo "Deploying $service environment..."
  bash "$service/deploy_env.sh" "$@"
done
