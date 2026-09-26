#!/usr/bin/env bash
# ==============================================================================
# SideQuestz ML - Production VPS Deploy Script
# ==============================================================================
# 1. Loads server credentials from .env (checks ml/.env, Backend/.env, ../.env)
# 2. Copies the contents of the ml folder to /opt/ml on the remote server
# 3. chmod 755 the /opt/ml directory
# 4. Sets up / verifies python virtualenv and dependencies
# 5. Installs/updates /etc/systemd/system/ml.service and restarts ml
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Find and load .env from ml/.env, Backend/.env, or repo root
ENV_FILE=""
if [ -f "${SCRIPT_DIR}/.env" ]; then
    ENV_FILE="${SCRIPT_DIR}/.env"
elif [ -f "${SCRIPT_DIR}/../Backend/.env" ]; then
    ENV_FILE="${SCRIPT_DIR}/../Backend/.env"
elif [ -f "${SCRIPT_DIR}/../.env" ]; then
    ENV_FILE="${SCRIPT_DIR}/../.env"
fi

if [ -n "${ENV_FILE}" ]; then
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
    done < "${ENV_FILE}"
fi

SERVER_IP="${1:-${DEPLOY_HOST:-45.32.223.40}}"
SERVER_USER="${2:-${DEPLOY_USER:-root}}"
SERVER_PASS="${3:-${DEPLOY_PASSWORD:-}}"
REMOTE_DIR="${ML_DEPLOY_REMOTE_DIR:-/opt/ml}"
SERVICE_NAME="ml"

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
echo -e "${BLUE}  SideQuestz ML Deploy: ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}${NC}"
echo -e "${BLUE}============================================================${NC}"

# Remote command execution helper
run_ssh() {
    local cmd="$1"
    if command -v sshpass >/dev/null 2>&1 && [ -n "${SERVER_PASS}" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    else
        ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    fi
}

# 1. Prepare remote directory
echo -e "${BLUE}==> [1/3] Preparing remote directory ${REMOTE_DIR}...${NC}"
run_ssh "mkdir -p ${REMOTE_DIR}"

# 2. Upload ml folder contents
echo -e "${BLUE}==> [2/3] Uploading ML files to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}...${NC}"

EXCLUDES=(
    "--exclude=__pycache__"
    "--exclude=*.pyc"
    "--exclude=.pytest_cache"
    "--exclude=.venv"
    "--exclude=venv"
    "--exclude=.git"
    "--exclude=.idea"
    "--exclude=.DS_Store"
    "--exclude=runs"
)

if command -v rsync >/dev/null 2>&1; then
    echo -e "${GREEN}    Using rsync for delta transfer...${NC}"
    if command -v sshpass >/dev/null 2>&1 && [ -n "${SERVER_PASS}" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e rsync -avz \
            "${EXCLUDES[@]}" \
            -e "ssh -o StrictHostKeyChecking=accept-new" \
            "${SCRIPT_DIR}/" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/"
    else
        rsync -avz \
            "${EXCLUDES[@]}" \
            -e "ssh -o StrictHostKeyChecking=accept-new" \
            "${SCRIPT_DIR}/" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/"
    fi
else
    echo -e "${YELLOW}    rsync not found; bundling files with tar over SSH...${NC}"
    if command -v sshpass >/dev/null 2>&1 && [ -n "${SERVER_PASS}" ]; then
        tar --exclude="__pycache__" --exclude="*.pyc" --exclude=".pytest_cache" \
            --exclude=".venv" --exclude="venv" --exclude=".git" --exclude="runs" \
            -czf - -C "${SCRIPT_DIR}" . | \
            SSHPASS="${SERVER_PASS}" sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "tar -xzf - -C ${REMOTE_DIR}"
    else
        tar --exclude="__pycache__" --exclude="*.pyc" --exclude=".pytest_cache" \
            --exclude=".venv" --exclude="venv" --exclude=".git" --exclude="runs" \
            -czf - -C "${SCRIPT_DIR}" . | \
            ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "tar -xzf - -C ${REMOTE_DIR}"
    fi
fi

# 3. chmod 755 the folder, set up python environment, install service, and systemctl restart ml
echo -e "${BLUE}==> [3/3] Setting chmod 755, setting up environment, and restarting ${SERVICE_NAME}...${NC}"

REMOTE_SCRIPT="set -e
# Set permissions
chmod -R 755 ${REMOTE_DIR}

# Ensure Python venv and requirements
if [ ! -d \"${REMOTE_DIR}/.venv\" ]; then
    echo 'Creating python venv at ${REMOTE_DIR}/.venv...'
    python3 -m venv ${REMOTE_DIR}/.venv
    ${REMOTE_DIR}/.venv/bin/pip install --upgrade pip
fi
if [ -f \"${REMOTE_DIR}/requirements.txt\" ]; then
    echo 'Installing / verifying python requirements...'
    ${REMOTE_DIR}/.venv/bin/pip install -q -r ${REMOTE_DIR}/requirements.txt
fi

# Install systemd service unit if present
if [ -f \"${REMOTE_DIR}/ml.service\" ]; then
    cp \"${REMOTE_DIR}/ml.service\" /etc/systemd/system/ml.service
    systemctl daemon-reload
    systemctl enable ml
fi

# Restart service and check status
systemctl restart ${SERVICE_NAME}
sleep 1
systemctl status ${SERVICE_NAME} --no-pager
"

run_ssh "${REMOTE_SCRIPT}"

echo ""
echo -e "${GREEN}============================================================${NC}"
echo -e "${GREEN}  ✓ Deployment complete! ${SERVICE_NAME} restarted on ${SERVER_IP}. 🚀${NC}"
echo -e "${GREEN}============================================================${NC}"
