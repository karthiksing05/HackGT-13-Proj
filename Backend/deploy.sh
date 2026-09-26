#!/usr/bin/env bash
# ==============================================================================
# SideQuestz Backend - Production VPS Build & Deploy Script
# ==============================================================================
# 1. Loads server credentials from .env
# 2. Runs build.sh
# 3. SSH copies binary to the server into /opt/backend
# 4. chmod 755 the binary
# 5. systemctl restart sidequestz
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load environment configuration from .env if present (handles CRLF & LF)
if [ -f "${SCRIPT_DIR}/.env" ]; then
    while IFS='=' read -r key val || [ -n "$key" ]; do
        key=$(echo "${key}" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        val=$(echo "${val}" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        if [ -n "${key}" ] && [[ ! "${key}" =~ ^# ]]; then
            val="${val#\"}"
            val="${val%\"}"
            val="${val#\'}"
            val="${val%\'}"
            export "${key}=${val}"
        fi
    done < "${SCRIPT_DIR}/.env"
fi

SERVER_IP="${1:-${DEPLOY_HOST:-45.32.223.40}}"
SERVER_USER="${2:-${DEPLOY_USER:-root}}"
SERVER_PASS="${3:-${DEPLOY_PASSWORD:-}}"
REMOTE_DIR="${DEPLOY_REMOTE_DIR:-/opt/backend}"
SERVICE_NAME="${DEPLOY_SERVICE:-sidequestz}"
BINARY_NAME="sidequestz-server"
LOCAL_BINARY="bin/${BINARY_NAME}"

# ANSI colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

if [ -z "${SERVER_PASS}" ]; then
    echo -e "${YELLOW}Warning: DEPLOY_PASSWORD not found in .env or environment.${NC}"
fi

echo -e "${BLUE}============================================================${NC}"
echo -e "${BLUE}  SideQuestz Deploy: ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}${NC}"
echo -e "${BLUE}============================================================${NC}"

# 1. Run build.sh
echo -e "${BLUE}==> [1/3] Running build.sh...${NC}"
if [ -f "${SCRIPT_DIR}/build.sh" ]; then
    bash "${SCRIPT_DIR}/build.sh" --amd64
else
    echo -e "${RED}Error: build.sh not found.${NC}"
    exit 1
fi

if [ ! -f "${LOCAL_BINARY}" ]; then
    echo -e "${RED}Error: ${LOCAL_BINARY} was not created by build.sh.${NC}"
    exit 1
fi

echo -e "${GREEN}    ✓ Build succeeded: ${LOCAL_BINARY}${NC}"

# 2. Upload to server via SSH into /opt/backend
echo -e "${BLUE}==> [2/3] Copying binary via SSH to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/${BINARY_NAME}...${NC}"

if command -v sshpass >/dev/null 2>&1 && [ -n "${SERVER_PASS}" ]; then
    export SSHPASS="${SERVER_PASS}"
    sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "mkdir -p ${REMOTE_DIR}"
    sshpass -e scp -o StrictHostKeyChecking=accept-new "${LOCAL_BINARY}" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/${BINARY_NAME}"
    echo -e "${BLUE}==> [3/3] Setting chmod 755 and restarting ${SERVICE_NAME}...${NC}"
    sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "chmod 755 ${REMOTE_DIR}/${BINARY_NAME} && systemctl restart ${SERVICE_NAME} && sleep 1 && systemctl status ${SERVICE_NAME} --no-pager"
else
    if [ -n "${SERVER_PASS}" ]; then
        echo -e "${YELLOW}Server password (from .env): ${SERVER_PASS}${NC}"
    fi
    ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "mkdir -p ${REMOTE_DIR}"
    scp -o StrictHostKeyChecking=accept-new "${LOCAL_BINARY}" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/${BINARY_NAME}"
    echo -e "${BLUE}==> [3/3] Setting chmod 755 and restarting ${SERVICE_NAME}...${NC}"
    ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "chmod 755 ${REMOTE_DIR}/${BINARY_NAME} && systemctl restart ${SERVICE_NAME} && sleep 1 && systemctl status ${SERVICE_NAME} --no-pager"
fi

echo ""
echo -e "${GREEN}============================================================${NC}"
echo -e "${GREEN}  ✓ Deployment complete! ${SERVICE_NAME} restarted on ${SERVER_IP}. 🚀${NC}"
echo -e "${GREEN}============================================================${NC}"
