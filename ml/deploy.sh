#!/usr/bin/env bash
# ==============================================================================
# SideQuests ML - VPS deploy
# ==============================================================================
#   ./deploy.sh [--skip-tests] [host] [user] [password]
#
# 1. Runs the unit tests locally (--skip-tests to bypass).
# 2. Snapshots /opt/ml to /opt/ml.prev (hard links: cheap), for rollback:
#      rm -rf /opt/ml && mv /opt/ml.prev /opt/ml && systemctl restart ml
# 3. Uploads the code. Never uploads secrets (.env, gcp-sa.json, keys), caches, venvs or the
#    training/data-generation code; those stay as they are on the server.
# 4. Permissions: directories 755, files 644, secrets 600 (the venv and the cache are left alone).
# 5. Installs requirements-serve.txt (CPU torch), prefetches the Qwen3-Embedding weights into
#    /opt/ml/.cache, installs ml.service and the ml-embed-missing timer, restarts the service.
# 6. Waits for /healthz, then /healthz?probe=1 (one real embedding); fails loudly otherwise.
#
# Server credentials (DEPLOY_HOST, DEPLOY_USER, DEPLOY_PASSWORD) come from the environment or the
# first .env found in ml/, Backend/ or the repo root; only DEPLOY_* and ML_DEPLOY_* are read.
# Secrets on the server are provisioned by hand (docs: deployment runbook):
#   /opt/ml/.env (600): HF_TOKEN, TYPESAFE_API_KEY, GOOGLE_APPLICATION_CREDENTIALS=/opt/ml/gcp-sa.json
#   /opt/ml/gcp-sa.json (600)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SKIP_TESTS=0
POSITIONAL=()
for arg in "$@"; do
    case "${arg}" in
        --skip-tests) SKIP_TESTS=1 ;;
        -h|--help) sed -n '2,25p' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) POSITIONAL+=("${arg}") ;;
    esac
done
set -- "${POSITIONAL[@]+"${POSITIONAL[@]}"}"

# Deploy credentials only: the .env files also hold API keys this script has no use for.
ENV_FILE=""
for candidate in "${SCRIPT_DIR}/.env" "${SCRIPT_DIR}/../Backend/.env" "${SCRIPT_DIR}/../.env"; do
    if [ -f "${candidate}" ]; then
        ENV_FILE="${candidate}"
        break
    fi
done
if [ -n "${ENV_FILE}" ]; then
    while IFS='=' read -r key val || [ -n "${key}" ]; do
        key=$(echo "${key}" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        case "${key}" in
            DEPLOY_*|ML_DEPLOY_*) ;;
            *) continue ;;
        esac
        val=$(echo "${val}" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        val="${val#\"}"; val="${val%\"}"; val="${val#\'}"; val="${val%\'}"
        if [ -z "${!key:-}" ]; then
            export "${key}=${val}"
        fi
    done < "${ENV_FILE}"
fi

SERVER_IP="${1:-${DEPLOY_HOST:?DEPLOY_HOST is not set (root .env or environment)}}"
SERVER_USER="${2:-${DEPLOY_USER:-root}}"
SERVER_PASS="${3:-${DEPLOY_PASSWORD:-}}"
REMOTE_DIR="${ML_DEPLOY_REMOTE_DIR:-/opt/ml}"
SERVICE_NAME="ml"
MODEL="Qwen/Qwen3-Embedding-0.6B"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# 1. Local tests
if [ "${SKIP_TESTS}" -eq 0 ]; then
    echo -e "${BLUE}==> [1/5] Running the unit tests locally...${NC}"
    PYTHON="${SCRIPT_DIR}/.venv/bin/python"
    [ -x "${PYTHON}" ] || PYTHON="$(command -v python3)"
    if ! (cd "${SCRIPT_DIR}" && "${PYTHON}" -m unittest discover -q); then
        echo -e "${RED}Unit tests failed; not deploying (use --skip-tests to override).${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}==> [1/5] Skipping the local tests (--skip-tests).${NC}"
fi

# Non-interactive SSH authentication
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
    echo -e "${YELLOW}DEPLOY_PASSWORD not set; using your SSH keys.${NC}"
fi

echo -e "${BLUE}============================================================${NC}"
echo -e "${BLUE}  SideQuests ML deploy: ${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR} (auth: ${AUTH_MODE})${NC}"
echo -e "${BLUE}============================================================${NC}"

SSH_OPTS=(-o StrictHostKeyChecking=accept-new)
run_ssh() {
    local cmd="$1"
    if [ "${AUTH_MODE}" = "sshpass" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e ssh "${SSH_OPTS[@]}" "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    elif [ "${AUTH_MODE}" = "askpass" ]; then
        ssh "${SSH_OPTS[@]}" "${SERVER_USER}@${SERVER_IP}" "${cmd}" < /dev/null
    else
        ssh "${SSH_OPTS[@]}" "${SERVER_USER}@${SERVER_IP}" "${cmd}"
    fi
}

# 2. Snapshot for rollback
echo -e "${BLUE}==> [2/5] Snapshotting ${REMOTE_DIR} to ${REMOTE_DIR}.prev...${NC}"
run_ssh "set -e; mkdir -p ${REMOTE_DIR}/.cache; rm -rf ${REMOTE_DIR}.prev; cp -al ${REMOTE_DIR} ${REMOTE_DIR}.prev"

# 3. Upload the code (secrets, caches, venvs, training data and generation code stay local)
echo -e "${BLUE}==> [3/5] Uploading ML files...${NC}"
EXCLUDES=(
    ".env" ".env.*" "*.env" "gcp-sa.json" "*.pem" "*.key"
    ".cache" ".venv" "venv" "__pycache__" "*.pyc" ".pytest_cache"
    ".git" ".idea" ".DS_Store"
    "datagen/" "data/" "*.sbatch" "wandb/" "runs/"
)
if command -v rsync >/dev/null 2>&1; then
    RSYNC_EXCLUDES=()
    for pattern in "${EXCLUDES[@]}"; do RSYNC_EXCLUDES+=("--exclude=${pattern}"); done
    # --delete removes files deleted locally; excluded paths (the server's .env, venv, cache) are kept.
    RSYNC=(rsync -az --delete "${RSYNC_EXCLUDES[@]}" -e "ssh ${SSH_OPTS[*]}" "${SCRIPT_DIR}/" "${SERVER_USER}@${SERVER_IP}:${REMOTE_DIR}/")
    if [ "${AUTH_MODE}" = "sshpass" ]; then
        SSHPASS="${SERVER_PASS}" sshpass -e "${RSYNC[@]}"
    else
        "${RSYNC[@]}"
    fi
else
    echo -e "${YELLOW}    rsync not found; streaming a tar over SSH...${NC}"
    TAR_EXCLUDES=()
    for pattern in "${EXCLUDES[@]}"; do TAR_EXCLUDES+=("--exclude=${pattern%/}"); done
    tar "${TAR_EXCLUDES[@]}" -czf - -C "${SCRIPT_DIR}" . | run_ssh "tar -xzf - -C ${REMOTE_DIR}"
fi

# 4 + 5. Permissions, dependencies, weights, units, restart, health
echo -e "${BLUE}==> [4/5] Permissions, dependencies, weights and systemd units...${NC}"
REMOTE_SCRIPT="set -euo pipefail
cd ${REMOTE_DIR}
for secret in .env gcp-sa.json; do
    if [ -f \"\${secret}\" ]; then chmod 600 \"\${secret}\"; fi
done
find . \\( -path ./.venv -o -path ./.cache \\) -prune -o -type d -exec chmod 755 {} +
find . \\( -path ./.venv -o -path ./.cache -o -name .env -o -name gcp-sa.json \\) -prune -o -type f -exec chmod 644 {} +
chmod 755 deploy.sh
if [ ! -f .env ]; then
    echo 'warning: ${REMOTE_DIR}/.env is missing: no HF_TOKEN, TYPESAFE_API_KEY or Vertex credentials (local embeddings, no Jev)'
fi
mkdir -p .cache

if [ ! -d .venv ]; then
    python3 -m venv .venv
    .venv/bin/pip install -q --upgrade pip
fi
.venv/bin/pip install -q -r requirements-serve.txt --extra-index-url https://download.pytorch.org/whl/cpu
HF_HOME=${REMOTE_DIR}/.cache HF_HUB_DISABLE_TELEMETRY=1 .venv/bin/python -c \"from sentence_transformers import SentenceTransformer as S; S('${MODEL}')\"

cp ml.service ml-embed-missing.service ml-embed-missing.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}
systemctl enable ml-embed-missing.timer
systemctl restart ${SERVICE_NAME}

echo 'waiting for /healthz...'
for i in \$(seq 1 60); do
    if curl -fsS --max-time 5 http://127.0.0.1:8000/healthz >/dev/null 2>&1; then break; fi
    if [ \"\${i}\" -eq 60 ]; then
        echo 'the ML service did not come up'; journalctl -u ${SERVICE_NAME} -n 50 --no-pager; exit 1
    fi
    sleep 5
done
echo 'probing one real embedding (/healthz?probe=1)...'
if ! curl -fsS --max-time 180 'http://127.0.0.1:8000/healthz?probe=1'; then
    echo; echo 'the embedding probe failed'; journalctl -u ${SERVICE_NAME} -n 50 --no-pager; exit 1
fi
echo
# Started only now: a timer that is due fires at once, and embed-missing needs the service up.
systemctl restart ml-embed-missing.timer
systemctl --no-pager --lines=0 status ${SERVICE_NAME}
systemctl list-timers ml-embed-missing.timer --no-pager
"
run_ssh "${REMOTE_SCRIPT}"

echo -e "${GREEN}==> [5/5] Deployed: ${SERVICE_NAME} is up on ${SERVER_IP} and embeds.${NC}"
