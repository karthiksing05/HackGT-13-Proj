#!/bin/bash -l
# Build the `users` config: synthetic users -> candidate events per user -> LLM compatibility ratings.
# Needs ml/datagen/setup_raven.sh (vLLM env) and ml/data/setup_raven.sh (embedding env + cached
# events dataset) to have run once on a login node. From the repo root:
#   full run:   mpcdf.py submit raven ml/datagen/users.sbatch -- --users 10000
#   quick test: mpcdf.py submit raven ml/datagen/users.sbatch -p gpudev --gres=gpu:a100:1 \
#                 --cpus-per-task=18 --mem=120000 --time=00:15:00 -- --users 40 --limit-events 3000
#   resume:     ... -- --users 10000 --work <earlier run dir>   (finished stages are skipped)
# Output (in the run dir): users.jsonl, tasks-<i>.jsonl, ratings-<i>.jsonl, selection.json, stats.
#SBATCH --job-name=users-datagen
#SBATCH --time=02:00:00
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --gres=gpu:a100:4
#SBATCH --cpus-per-task=72
#SBATCH --mem=0

set -eo pipefail
USERS=10000 LIMIT_EVENTS="" WORK="$RUN_DIR"
while [ $# -gt 0 ]; do
  case "$1" in
    --users) USERS="$2"; shift 2 ;;
    --limit-events) LIMIT_EVENTS="--limit-events $2"; shift 2 ;;
    --work) WORK="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
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
echo "job $SLURM_JOB_ID on $(hostname): $NGPU GPU(s), $USERS users, work dir $WORK"

run_workers() {  # run_workers <label> <command...>; the command gets --worker-index/-i placeholders expanded
  local label="$1"; shift
  for i in $(seq 0 $((NGPU - 1))); do
    CUDA_VISIBLE_DEVICES=$i "${@//@I@/$i}" > "$WORK/$label-$i.log" 2>&1 &
  done
  local status=0
  for pid in $(jobs -p); do wait "$pid" || status=1; done
  for i in $(seq 0 $((NGPU - 1))); do echo "--- $label worker $i"; tail -n 2 "$WORK/$label-$i.log"; done
  return $status
}

# 1. users (5% extra so duplicates can be dropped)
if [ ! -f "$WORK/stage1.done" ]; then
  echo "[$(date +%T)] stage 1: generating users"
  run_workers users "$VLLM_PY" ml/datagen/generate_users.py --model "$MODEL" --total $((USERS * 105 / 100)) \
    --num-workers "$NGPU" --worker-index @I@ --out "$WORK/users-raw-@I@.jsonl" --stats "$WORK/users-stats-@I@.json"
  touch "$WORK/stage1.done"
fi

# 2. persona split, event pools, candidate events (frozen embedding model, one GPU)
if [ ! -f "$WORK/selection.json" ]; then
  echo "[$(date +%T)] stage 2: selecting candidates"
  CUDA_VISIBLE_DEVICES=0 "$EMBED_PY" ml/datagen/select_candidates.py --users-glob "$WORK/users-raw-*.jsonl" \
    --out "$WORK" --users "$USERS" --shards "$NGPU" $LIMIT_EVENTS > "$WORK/select.log" 2>&1 \
    || { tail -n 30 "$WORK/select.log"; exit 1; }
  tail -n 3 "$WORK/select.log"
fi

# 3. ratings
if [ ! -f "$WORK/stage3.done" ]; then
  echo "[$(date +%T)] stage 3: rating candidates"
  run_workers judge "$VLLM_PY" ml/datagen/judge.py --model "$MODEL" --tasks "$WORK/tasks-@I@.jsonl" \
    --out "$WORK/ratings-@I@.jsonl" --stats "$WORK/judge-stats-@I@.json"
  touch "$WORK/stage3.done"
fi
echo "[$(date +%T)] done: $(cat "$WORK"/users.jsonl | wc -l) users, $(cat "$WORK"/ratings-*.jsonl | wc -l) ratings"
