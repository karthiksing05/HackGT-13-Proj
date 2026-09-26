#!/usr/bin/env bash
# ==============================================================================
# SideQuestz Backend - Production VPS Build Script
# ==============================================================================
set -euo pipefail

# Configuration defaults
TARGET_OS="${GOOS:-linux}"
TARGET_ARCH="${GOARCH:-amd64}"
OUTPUT_DIR="bin"
BINARY_NAME="sidequestz-server"
SKIP_TESTS=false
CLEAN=false

# ANSI colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}==>${NC} Starting production build for SideQuestz Backend..."

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --native)
      TARGET_OS="$(go env GOHOSTOS)"
      TARGET_ARCH="$(go env GOHOSTARCH)"
      shift
      ;;
    --arm64)
      TARGET_ARCH="arm64"
      shift
      ;;
    --amd64)
      TARGET_ARCH="amd64"
      shift
      ;;
    --skip-tests)
      SKIP_TESTS=true
      shift
      ;;
    --clean)
      CLEAN=true
      shift
      ;;
    -h|--help)
      echo "Usage: ./build.sh [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --native       Build for current host OS and architecture"
      echo "  --arm64        Target Linux arm64 (AWS Graviton / ARM VPS)"
      echo "  --amd64        Target Linux amd64 (Standard x86_64 VPS) [Default]"
      echo "  --skip-tests   Skip test suite execution before compiling"
      echo "  --clean        Remove previous build artifacts"
      echo "  -h, --help     Show this help message"
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      exit 1
      ;;
  esac
done

# Ensure Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go compiler is not installed or not in PATH.${NC}"
    exit 1
fi

echo -e "${BLUE}  Target OS:${NC}   ${TARGET_OS}"
echo -e "${BLUE}  Target Arch:${NC} ${TARGET_ARCH}"
echo -e "${BLUE}  Go Version:${NC}  $(go version)"

# Clean previous build if requested
if [ "$CLEAN" = true ]; then
  echo -e "${YELLOW}==>${NC} Cleaning previous artifacts..."
  rm -rf "${OUTPUT_DIR}"
fi

mkdir -p "${OUTPUT_DIR}"

# Download dependencies
echo -e "${BLUE}==>${NC} Downloading Go modules..."
go mod download

# Run test suite
if [ "$SKIP_TESTS" = false ]; then
  echo -e "${BLUE}==>${NC} Running test suite..."
  if ! go test -v ./...; then
    echo -e "${RED}Error: Tests failed! Aborting build.${NC}"
    exit 1
  fi
  echo -e "${GREEN}==>${NC} Tests passed successfully!"
else
  echo -e "${YELLOW}==>${NC} Skipping test suite (--skip-tests specified)"
fi

OUTPUT_PATH="${OUTPUT_DIR}/${BINARY_NAME}"
if [ "${TARGET_OS}" = "windows" ]; then
  OUTPUT_PATH="${OUTPUT_PATH}.exe"
fi

# Build static binary with production optimizations (-s -w strips symbol tables and debug info)
echo -e "${BLUE}==>${NC} Compiling static binary to ${OUTPUT_PATH}..."
CGO_ENABLED=0 GOOS="${TARGET_OS}" GOARCH="${TARGET_ARCH}" \
  go build -trimpath -ldflags="-s -w" -o "${OUTPUT_PATH}" main.go

chmod +x "${OUTPUT_PATH}"

# Print file details
FILE_SIZE=$(du -h "${OUTPUT_PATH}" | cut -f1)
echo -e "${GREEN}==> Build successful!${NC}"
echo -e "    Binary: ${OUTPUT_PATH} (${FILE_SIZE})"
echo ""
echo "To run on your VPS:"
echo "  1. Copy binary to VPS: scp ${OUTPUT_PATH} user@your-vps-ip:/opt/sidequestz/"
echo "  2. Configure environment: export PORT=8080 MONGO_URI='mongodb://127.0.0.1:27017' MONGO_DB='freetime'"
echo "  3. Execute: ./${OUTPUT_PATH}"
echo "     Or manage with systemd (see sidequestz.service)"
