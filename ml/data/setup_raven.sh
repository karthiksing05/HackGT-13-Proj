#!/bin/bash
# One-time, idempotent setup for ml/data/embed.sbatch on Raven. Run it on a login node (compute
# nodes have no internet), from a code snapshot, in a login shell:
#   mpcdf.py run raven 'cd $BASE/HackGT-13/code/<version> && SHARED_DIR=$BASE/HackGT-13/shared \
#     USERS_CONFIG=users bash -l ml/data/setup_raven.sh'
# Installs an embedding env into $SHARED_DIR/venv-embed and downloads the embedding model and the
# (private) datasets into $HF_HOME, so the job can run offline. The datasets need a token: set
# HF_TOKEN, or run `HF_HOME=/ptmp/$USER/hf_cache hf auth login` once. Re-run it whenever a dataset
# on the Hub changes. Finished steps are skipped on re-runs.
set -eo pipefail
: "${SHARED_DIR:?set SHARED_DIR to the shared directory of this repo on the cluster}"
EMBED_MODEL="${EMBED_MODEL:-Qwen/Qwen3-Embedding-0.6B}"
EVENTS_REPO="${EVENTS_REPO:-karthiksing05/sidequestz-event-embedding-text}"
export HF_HOME="${HF_HOME:-/ptmp/$USER/hf_cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/ptmp/$USER/uv-cache}"
export UV_PYTHON_INSTALL_DIR="$SHARED_DIR/uv-python"
BOOT="$SHARED_DIR/uv-boot"   # shared with ml/datagen/setup_raven.sh
VENV="$SHARED_DIR/venv-embed"
mkdir -p "$SHARED_DIR"

if [ ! -x "$BOOT/bin/uv" ]; then
  module purge >/dev/null 2>&1 || true
  module load python-waterboa/2025.06
  python -m venv "$BOOT"
  "$BOOT/bin/pip" install --quiet uv
fi
UV="$BOOT/bin/uv"

if ! "$VENV/bin/python" -c "import sentence_transformers, datasets, dotenv" >/dev/null 2>&1; then
  echo "installing the embedding environment into $VENV"
  "$UV" venv --clear --python 3.12 "$VENV"
  "$UV" pip install --python "$VENV/bin/python" torch "sentence-transformers>=3.0" "transformers>=4.51" \
    "datasets>=3.0" "python-dotenv>=1.0" "numpy>=1.26"
fi
# Classifier training runs in this env too: it imports the whole `compatibility` package (which
# needs typesafe-sdk) and logs to Weights & Biases, so install everything ml/requirements.txt lists.
if ! "$VENV/bin/python" -c "import wandb, typesafe_sdk" >/dev/null 2>&1; then
  echo "installing ml/requirements.txt into $VENV"
  "$UV" pip install --python "$VENV/bin/python" -r ml/requirements.txt
fi
"$VENV/bin/python" -c "import torch, sentence_transformers as s; print(f'torch {torch.__version__} (CUDA {torch.version.cuda}), sentence-transformers {s.__version__}')"

echo "caching model $EMBED_MODEL"
"$VENV/bin/python" -c "from huggingface_hub import snapshot_download as s; import sys; print(s(sys.argv[1]))" "$EMBED_MODEL"

cd ml
cache() {  # cache <repo> [config]
  echo "caching dataset $1${2:+ (config $2)}"
  "$VENV/bin/python" -c "from data.hf_dataset import load_events; import sys; print(load_events(sys.argv[1], name=sys.argv[2] or None))" "$1" "${2:-}"
}
cache "$EVENTS_REPO"
# Users: a separate repo (USERS_REPO) and/or a config of it (USERS_CONFIG, e.g. "users"; the repo
# defaults to EVENTS_REPO, where the users config lives).
if [ -n "${USERS_REPO:-}" ] || [ -n "${USERS_CONFIG:-}" ]; then
  cache "${USERS_REPO:-$EVENTS_REPO}" "${USERS_CONFIG:-}"
fi
echo "setup complete"
