#!/bin/bash -l
# Generate the synthetic event dataset with vLLM, one worker process per GPU of one Raven node.
# Needs ml/datagen/setup_raven.sh to have run once on a login node. From the repo root:
#   full run:   mpcdf.py submit raven ml/datagen/generate.sbatch -- --total 104000
#   quick test: mpcdf.py submit raven ml/datagen/generate.sbatch -p gpudev --gres=gpu:a100:1 \
#                 --cpus-per-task=18 --mem=120000 --time=00:15:00 -- --total 300
# Workers write $RUN_DIR/shard-<i>.jsonl, stats-<i>.json and worker-<i>.log.
#SBATCH --job-name=event-datagen
#SBATCH --time=02:00:00
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --gres=gpu:a100:4
#SBATCH --cpus-per-task=72
#SBATCH --mem=0

set -eo pipefail
MODEL="${MODEL:-Qwen/Qwen3.5-9B}"
PY="$SHARED_DIR/venv-vllm/bin/python"
export HF_HOME="/ptmp/$USER/hf_cache" HF_HUB_OFFLINE=1   # weights come from the cache only
export TRITON_CACHE_DIR="$SHARED_DIR/cache/triton" VLLM_CACHE_ROOT="$SHARED_DIR/cache/vllm"
export XDG_CACHE_HOME="$SHARED_DIR/cache/xdg" PYTHONUNBUFFERED=1 TOKENIZERS_PARALLELISM=false
# FlashInfer's sampler JIT-compiles CUDA code and needs nvcc, which Raven only offers via
# modules; vLLM's built-in PyTorch sampler needs no compiler and costs little here.
export VLLM_USE_FLASHINFER_SAMPLER=0

if ! "$PY" -c "import vllm" 2>/dev/null; then
  echo "no vLLM environment in $SHARED_DIR/venv-vllm: run ml/datagen/setup_raven.sh on a login node first" >&2
  exit 1
fi

NGPU=$(nvidia-smi -L | wc -l)
echo "job $SLURM_JOB_ID on $(hostname): $NGPU GPU(s), model $MODEL, outputs in $RUN_DIR"
for i in $(seq 0 $((NGPU - 1))); do
  CUDA_VISIBLE_DEVICES=$i "$PY" ml/datagen/generate.py --model "$MODEL" \
    --worker-index "$i" --num-workers "$NGPU" \
    --out "$RUN_DIR/shard-$i.jsonl" --stats "$RUN_DIR/stats-$i.json" "$@" \
    > "$RUN_DIR/worker-$i.log" 2>&1 &
done

status=0
for pid in $(jobs -p); do wait "$pid" || status=1; done
for i in $(seq 0 $((NGPU - 1))); do echo "--- worker $i"; tail -n 4 "$RUN_DIR/worker-$i.log"; done
echo "rows written: $(cat "$RUN_DIR"/shard-*.jsonl 2>/dev/null | wc -l)"
exit $status
