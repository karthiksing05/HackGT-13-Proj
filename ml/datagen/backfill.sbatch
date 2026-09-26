#!/bin/bash -l
# Step 2 of ml/datagen/mongo_backfill.py: write the missing activity texts with Qwen3.5-9B (one vLLM
# worker per GPU), then embed every text with Qwen3-Embedding-0.6B. Needs ml/datagen/setup_raven.sh
# and ml/data/setup_raven.sh to have run once. --in is the export directory copied to Raven.
# From the repo root:
#   full run:   mpcdf.py submit raven ml/datagen/backfill.sbatch -- --in <dir>
#   quick test: mpcdf.py submit raven ml/datagen/backfill.sbatch -p gpudev --gres=gpu:a100:1 \
#                 --cpus-per-task=18 --mem=120000 --time=00:15:00 -- --in <dir> --limit 200
#   resume:     ... -- --in <dir> --work <earlier run dir>   (written texts are kept)
# Output (in the run dir): texts-<i>.jsonl, failed-<i>.jsonl, text-stats-<i>.json, embeddings/.
#SBATCH --job-name=activity-backfill
#SBATCH --time=01:00:00
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --gres=gpu:a100:4
#SBATCH --cpus-per-task=72
#SBATCH --mem=0

set -eo pipefail
IN="" LIMIT="" WORK="$RUN_DIR"
while [ $# -gt 0 ]; do
  case "$1" in
    --in) IN="$2"; shift 2 ;;
    --limit) LIMIT="--limit $2"; shift 2 ;;
    --work) WORK="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[ -f "$IN/texts_todo.jsonl" ] && [ -f "$IN/embed_todo.jsonl" ] || { echo "--in must hold texts_todo.jsonl and embed_todo.jsonl" >&2; exit 2; }
MODEL="${MODEL:-Qwen/Qwen3.5-9B}"
VLLM_PY="$SHARED_DIR/venv-vllm/bin/python"
EMBED_PY="$SHARED_DIR/venv-embed/bin/python"
export HF_HOME="/ptmp/$USER/hf_cache" HF_HUB_OFFLINE=1 HF_DATASETS_OFFLINE=1   # everything from the cache
export TRITON_CACHE_DIR="$SHARED_DIR/cache/triton" VLLM_CACHE_ROOT="$SHARED_DIR/cache/vllm"
export XDG_CACHE_HOME="$SHARED_DIR/cache/xdg" PYTHONUNBUFFERED=1 TOKENIZERS_PARALLELISM=false
export VLLM_USE_FLASHINFER_SAMPLER=0   # FlashInfer's sampler JIT-compiles CUDA and needs nvcc
mkdir -p "$WORK"

"$VLLM_PY" -c "import vllm" 2>/dev/null || { echo "missing $SHARED_DIR/venv-vllm: run ml/datagen/setup_raven.sh" >&2; exit 1; }
"$EMBED_PY" -c "import sentence_transformers" 2>/dev/null || { echo "missing $SHARED_DIR/venv-embed: run ml/data/setup_raven.sh" >&2; exit 1; }

NGPU=$(nvidia-smi -L | wc -l)
echo "job $SLURM_JOB_ID on $(hostname): $NGPU GPU(s), $(wc -l < "$IN/texts_todo.jsonl") texts to write, $(wc -l < "$IN/embed_todo.jsonl") existing texts to embed, work dir $WORK"

run_workers() {  # run_workers <label> <command...>; @I@ in the command becomes the worker index
  local label="$1"; shift
  for i in $(seq 0 $((NGPU - 1))); do
    CUDA_VISIBLE_DEVICES=$i "${@//@I@/$i}" > "$WORK/$label-$i.log" 2>&1 &
  done
  local status=0
  for pid in $(jobs -p); do wait "$pid" || status=1; done
  for i in $(seq 0 $((NGPU - 1))); do echo "--- $label worker $i"; tail -n 2 "$WORK/$label-$i.log"; done
  return $status
}

# 1. missing texts
echo "[$(date +%T)] stage 1: writing texts"
run_workers texts "$VLLM_PY" ml/datagen/activity_texts.py --model "$MODEL" --todo "$IN/texts_todo.jsonl" \
  --num-workers "$NGPU" --worker-index @I@ --out "$WORK/texts-@I@.jsonl" --failed "$WORK/failed-@I@.jsonl" \
  --stats "$WORK/text-stats-@I@.json" $LIMIT

# 2. embeddings for every text, existing and new (one GPU is plenty)
echo "[$(date +%T)] stage 2: embedding"
CUDA_VISIBLE_DEVICES=0 "$EMBED_PY" ml/datagen/activity_embeddings.py --todo "$IN/embed_todo.jsonl" \
  --texts "$WORK"/texts-*.jsonl --out "$WORK/embeddings" $LIMIT > "$WORK/embed.log" 2>&1 \
  || { tail -n 30 "$WORK/embed.log"; exit 1; }
echo "[$(date +%T)] done: $(cat "$WORK"/texts-*.jsonl | wc -l) texts written, $(cat "$WORK"/failed-*.jsonl | wc -l) failed," \
  "$(grep -o '"count": [0-9]*' "$WORK/embeddings/meta.json") vectors"
