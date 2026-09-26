#!/bin/bash
# One-time, idempotent environment setup for ml/datagen on Raven. Run it on a login node
# (it downloads ~20 GB and installs vLLM), from a code snapshot, in a login shell:
#   mpcdf.py run raven 'cd $BASE/HackGT-13/code/<version> && SHARED_DIR=$BASE/HackGT-13/shared bash -l ml/datagen/setup_raven.sh'
# Installs vLLM into $SHARED_DIR/venv-vllm (Python 3.12, managed by uv) and downloads the
# model into $HF_HOME. Finished steps are skipped on re-runs.
set -eo pipefail
: "${SHARED_DIR:?set SHARED_DIR to the shared directory of this repo on the cluster}"
MODEL="${MODEL:-Qwen/Qwen3.5-9B}"
VLLM_VERSION="${VLLM_VERSION:-0.29.0}"
export HF_HOME="${HF_HOME:-/ptmp/$USER/hf_cache}"
export UV_CACHE_DIR="${UV_CACHE_DIR:-/ptmp/$USER/uv-cache}"   # keeps GBs of wheels off $HOME
export UV_PYTHON_INSTALL_DIR="$SHARED_DIR/uv-python"
BOOT="$SHARED_DIR/uv-boot"
VENV="$SHARED_DIR/venv-vllm"
mkdir -p "$SHARED_DIR"

if [ ! -x "$BOOT/bin/uv" ]; then
  module purge >/dev/null 2>&1 || true
  module load python-waterboa/2025.06
  python -m venv "$BOOT"
  "$BOOT/bin/pip" install --quiet uv
fi
UV="$BOOT/bin/uv"

if ! "$VENV/bin/python" -c "import vllm" >/dev/null 2>&1; then
  echo "installing vllm==$VLLM_VERSION into $VENV"
  "$UV" venv --clear --python 3.12 "$VENV"
  "$UV" pip install --python "$VENV/bin/python" "vllm==$VLLM_VERSION"
fi
"$VENV/bin/python" -c "import vllm, torch; print(f'vllm {vllm.__version__}, torch {torch.__version__} (CUDA {torch.version.cuda})')"

FETCH="from huggingface_hub import snapshot_download as s; import sys; print(s(sys.argv[1], local_files_only=sys.argv[2] == 'cached'))"
if "$VENV/bin/python" -c "$FETCH" "$MODEL" cached >/dev/null 2>&1; then
  echo "model cached: $MODEL"
else
  echo "downloading $MODEL into $HF_HOME"
  "$VENV/bin/python" -c "$FETCH" "$MODEL" download
fi
echo "setup complete"
