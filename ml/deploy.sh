#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
source ../deploy-common.sh

ssh "$server" 'mkdir -p /opt/ml/.cache'
# Upload serving code only; leave server secrets, caches and the venv alone.
tar --exclude=__pycache__ --exclude='*.pyc' --exclude='.env*' --exclude='*.env' --exclude=gcp-sa.json --exclude='*.key' --exclude='*.pem' -czf - \
  api compatibility embedding profiles reranking tools checkpoints requirements-serve.txt \
  ml.service ml-embed-missing.service ml-embed-missing.timer \
  | ssh "$server" 'tar -xzf - -C /opt/ml'

ssh "$server" 'bash -se' <<'REMOTE'
cd /opt/ml
[[ -d .venv ]] || python3 -m venv .venv
.venv/bin/pip install -r requirements-serve.txt --extra-index-url https://download.pytorch.org/whl/cpu
HF_HOME=/opt/ml/.cache .venv/bin/python -c "from sentence_transformers import SentenceTransformer; SentenceTransformer('Qwen/Qwen3-Embedding-0.6B')"
cp ml.service ml-embed-missing.service ml-embed-missing.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable ml ml-embed-missing.timer
systemctl restart ml ml-embed-missing.timer
REMOTE
