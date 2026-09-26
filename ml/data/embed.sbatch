#!/bin/bash -l
# Embed every split of the event (and user) datasets with a frozen text-embedding model, for
# classifier training. Needs ml/data/setup_raven.sh to have run once on a login node. From the
# repo root:
#   mpcdf.py submit raven ml/data/embed.sbatch -- --users-repo <owner/users-repo>
#   quick test: mpcdf.py submit raven ml/data/embed.sbatch -p gpudev --time=00:15:00 -- --limit 500 --out <dir>
# Extra arguments go to `python -m data.embed` (see its --help). Output defaults to
# $SHARED_DIR/embeddings/<model>; finished splits are skipped, so resubmitting resumes.
#SBATCH --job-name=embed
#SBATCH --time=01:00:00
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --gres=gpu:a100:1
#SBATCH --cpus-per-task=18
#SBATCH --mem=120000

set -eo pipefail
EMBED_MODEL="${EMBED_MODEL:-Qwen/Qwen3-Embedding-0.6B}"
OUT="${OUT:-$SHARED_DIR/embeddings/${EMBED_MODEL//\//--}}"
PY="$SHARED_DIR/venv-embed/bin/python"
export HF_HOME="/ptmp/$USER/hf_cache" HF_HUB_OFFLINE=1 HF_DATASETS_OFFLINE=1   # all from the cache
export XDG_CACHE_HOME="$SHARED_DIR/cache/xdg" PYTHONUNBUFFERED=1 TOKENIZERS_PARALLELISM=false

if ! "$PY" -c "import sentence_transformers" 2>/dev/null; then
  echo "no embedding environment in $SHARED_DIR/venv-embed: run ml/data/setup_raven.sh on a login node first" >&2
  exit 1
fi

echo "job $SLURM_JOB_ID on $(hostname): model $EMBED_MODEL, output in $OUT"
nvidia-smi -L
cd ml
"$PY" -m data.embed --model "$EMBED_MODEL" --out "$OUT" "$@"
