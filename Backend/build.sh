#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/sidequestz-server .
