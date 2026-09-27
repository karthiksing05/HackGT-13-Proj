#!/usr/bin/env bash
# Build the server for Linux/amd64. Tests are a separate `make test` step.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
packages=(.)
names=(sidequestz-server)
for arg in "$@"; do
  case "$arg" in
    --native) GOOS="$(go env GOHOSTOS)"; GOARCH="$(go env GOHOSTARCH)" ;;
    --admin) packages=(. ./cmd/sidequestz-admin); names=(sidequestz-server sidequestz-admin) ;;
    -h|--help) echo "Usage: ./build.sh [--native] [--admin] (or set GOOS/GOARCH)"; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

export GOOS GOARCH CGO_ENABLED=0
mkdir -p bin
for i in "${!packages[@]}"; do
  output="bin/${names[$i]}"
  [[ "$GOOS" != windows ]] || output+=.exe
  go build -trimpath -ldflags='-s -w' -o "$output" "${packages[$i]}"
  echo "Built $output ($GOOS/$GOARCH)"
done
