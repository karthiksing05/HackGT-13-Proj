#!/usr/bin/env bash
# ==============================================================================
# SideQuestz Backend - Production VPS Build & Deploy Script
# ==============================================================================
# 1. Loads server credentials from .env
# 2. Runs build.sh to compile static Linux/amd64 binary
# 3. SSH copies binary (and backend.service unit) to the server
# 4. chmod 755 the binary
# 5. Restarts systemd service and verifies active status
#
# Fully compatible with Debian WSL, native Linux, and macOS.
# Automatically handles password authentication non-interactively via sshpass
# or OpenSSH SSH_ASKPASS without blocking.
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ANSI colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

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
LOCAL_BINARY="${SCRIPT_DIR}/bin/${BINARY_NAME}"

# Setup non-interactive SSH authentication
AUTH_MODE="none"
ASKPASS_FILE=""

cleanup() {
    if [ -n "${ASKPASS_FILE}" ] && [ -f "${ASKPASS_FILE}" ]; then
        rm -f "${ASKPASS_FILE}" 2>/dev/null || true
    fi
}
trap cleanup EXIT INT TERM

if [ -n "${SERVER_PASS}" ]; then
    if command -v sshpass >/dev/null 2>&1; then
        AUTH_MODE="sshpass"
    else
        AUTH_MODE="askpass"
        ASKPASS_FILE=$(mktemp)
        chmod 700 "${ASKPASS_FILE}"
        cat << 'EOF' > "${ASKPASS_FILE}"
#!/usr/bin/env bash
echo "${DEPLOY_PASS_INTERNAL}"
EOF
        export DEPLOY_PASS_INTERNAL="${SERVER_PASS}"
        export SSH_ASKPASS="${ASKPASS_FILE}"
        export SSH_ASKPASS_REQUIRE="force"
        export DISPLAY="${DISPLAY:-:0}"
    fi
else
    echo -e "${YELLOW}Warning: DEPLOY_PASSWORD not found in .env or environment; relying on SSH keys or interactive auth.${NC}"
fi

# SSH & SCP helper runners
run_ssh() {
    local cmd="$1"
    if [ "${AUTH_MODE}" = "sshpass" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    elif [ "${AUTH_MODE}" = "askpass" ]; then
        DISPLAY="${DISPLAY:-:0}" SSH_ASKPASS="${ASKPASS_FILE}" SSH_ASKPASS_REQUIRE="force" \
            ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "${cmd}" < /dev/null
    else
        ssh -o StrictHostKeyChecking=accept-new "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    fi
}

run_scp() {
    local src="$1"
    local dst="$2"
    if [ "${AUTH_MODE}" = "sshpass" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e scp -o StrictHostKeyChecking=accept-new "${src}" "${SERVER_USER}@${SERVER_IP}:${dst}"
    elif [ "${AUTH_MODE}" = "askpass" ]; then
        DISPLAY="${DISPLAY:-:0}" SSH_ASKPASS="${ASKPASS_FILE}" SSH_ASKPASS_REQUIRE="force" \
            scp -o StrictHostKeyChecking=accept-new "${src}" "${SERVER_USER}@${SERVER_IP}:${dst}" < /dev/null
    else
        scp -o StrictHostKeyChecking=accept-new "${src}" "${SERVER_USER}@${SERVER_IP}:${dst}"
    fi
}

echo -e "${BLUE}============================================================${NC}"
echo -e "${BLUE}  SideQuestz Deploy: ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}${NC}"
echo -e "${BLUE}  Auth Mode: ${AUTH_MODE}${NC}"
echo -e "${BLUE}============================================================${NC}"

# 1. Run build.sh
echo -e "${BLUE}==> [1/3] Building Go backend for Linux amd64...${NC}"
if [ -f "${SCRIPT_DIR}/build.sh" ]; then
    bash "${SCRIPT_DIR}/build.sh" --amd64
else
    echo -e "${RED}Error: build.sh not found in ${SCRIPT_DIR}.${NC}"
    exit 1
fi

if [ ! -f "${LOCAL_BINARY}" ]; then
    echo -e "${RED}Error: ${LOCAL_BINARY} was not created by build.sh.${NC}"
    exit 1
fi

echo -e "${GREEN}    ✓ Build verified: ${LOCAL_BINARY}${NC}"

# 2. Upload binary & systemd service
echo -e "${BLUE}==> [2/3] Uploading binary to ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/${BINARY_NAME}...${NC}"
run_ssh "mkdir -p ${REMOTE_DIR}"

# Upload to temporary file first to avoid 'Text file busy' (ETXTBSY) if service is running
run_scp "${LOCAL_BINARY}" "${REMOTE_DIR}/${BINARY_NAME}.new"
run_ssh "chmod 755 ${REMOTE_DIR}/${BINARY_NAME}.new && mv -f ${REMOTE_DIR}/${BINARY_NAME}.new ${REMOTE_DIR}/${BINARY_NAME}"

if [ -f "${SCRIPT_DIR}/backend.service" ]; then
    echo -e "${BLUE}    Syncing backend.service systemd unit...${NC}"
    run_scp "${SCRIPT_DIR}/backend.service" "/etc/systemd/system/backend.service"
fi

# 3. chmod 755, configure systemd, and restart service
echo -e "${BLUE}==> [3/3] Configuring systemd and restarting service...${NC}"
REMOTE_CMDS="set -e
chmod 755 ${REMOTE_DIR}/${BINARY_NAME}

# If backend.service exists, ensure sidequestz.service aliases to it and enable it
if [ -f /etc/systemd/system/backend.service ]; then
    ln -sf /etc/systemd/system/backend.service /etc/systemd/system/sidequestz.service
    systemctl daemon-reload
    systemctl enable backend.service >/dev/null 2>&1 || true
fi

# Restart whichever service is active (sidequestz or backend)
if systemctl list-unit-files | grep -q 'sidequestz.service'; then
    systemctl restart sidequestz
    TARGET_SVC='sidequestz'
else
    systemctl restart backend
    TARGET_SVC='backend'
fi

sleep 1
systemctl status \"\${TARGET_SVC}\" --no-pager
"

run_ssh "${REMOTE_CMDS}"

echo ""
echo -e "${GREEN}============================================================${NC}"
echo -e "${GREEN}  ✓ Deployment complete! Service restarted on ${SERVER_IP}. 🚀${NC}"
echo -e "${GREEN}============================================================${NC}"
