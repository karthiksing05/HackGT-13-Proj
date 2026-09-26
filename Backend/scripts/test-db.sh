#!/usr/bin/env bash
# Runs the whole Go suite against the local MongoDB container with the race
# detector on. CI=1 turns "no server" into a failure instead of a skip.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

CONTAINER="${MONGO_CONTAINER:-sq-mongo}"
export MONGO_TEST_URI="${MONGO_TEST_URI:-mongodb://127.0.0.1:27017}"

if command -v docker >/dev/null 2>&1; then
  if ! docker ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER"; then
      echo "starting $CONTAINER"
      docker start "$CONTAINER" >/dev/null
      sleep 2
    else
      echo "MongoDB container '$CONTAINER' not found; create it once with:" >&2
      echo "  docker run -d --name $CONTAINER -p 27017:27017 mongo:7" >&2
      exit 1
    fi
  fi
fi

CI=1 go test -race -count=1 "$@" ./...
