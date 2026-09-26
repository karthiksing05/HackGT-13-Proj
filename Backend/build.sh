#!/usr/bin/env bash
# Builds the static server and admin binaries into bin/ (Linux/amd64 by
# default: what deploy.sh uploads). Runs the test suite first unless
# --skip-tests.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

TARGET_OS="${GOOS:-linux}"
TARGET_ARCH="${GOARCH:-amd64}"
OUTPUT_DIR="bin"
SKIP_TESTS=false
CLEAN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --native) TARGET_OS="$(go env GOHOSTOS)"; TARGET_ARCH="$(go env GOHOSTARCH)"; shift ;;
    --arm64) TARGET_ARCH="arm64"; shift ;;
    --amd64) TARGET_ARCH="amd64"; shift ;;
    --skip-tests) SKIP_TESTS=true; shift ;;
    --clean) CLEAN=true; shift ;;
    -h|--help)
      cat <<EOF
Usage: ./build.sh [--native|--arm64|--amd64] [--skip-tests] [--clean]
Builds bin/sidequestz-server (main.go) and bin/sidequestz-admin (cmd/sidequestz-admin).
EOF
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

command -v go >/dev/null || { echo "Go is not installed or not in PATH." >&2; exit 1; }
echo "==> target ${TARGET_OS}/${TARGET_ARCH} with $(go version)"

if [ "$CLEAN" = true ]; then rm -rf "$OUTPUT_DIR"; fi
mkdir -p "$OUTPUT_DIR"

go mod download
if [ "$SKIP_TESTS" = false ]; then
  echo "==> go test ./..."
  go test ./...
fi

build() {
  local name="$1" pkg="$2"
  local out="${OUTPUT_DIR}/${name}"
  [ "$TARGET_OS" = "windows" ] && out="${out}.exe"
  echo "==> building ${out}"
  CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -trimpath -ldflags="-s -w" -o "$out" "$pkg"
  chmod +x "$out"
  echo "    $(du -h "$out" | cut -f1) $out"
}
build sidequestz-server .
build sidequestz-admin ./cmd/sidequestz-admin
echo "==> build successful"
