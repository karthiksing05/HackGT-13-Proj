#!/bin/bash -l
# training.md end to end on one Raven node: embeddings -> triples -> baseline lambda sweep ->
# phase 1 (variant comparison, 3 seeds) -> selection -> phase 2 (tuning) -> final test (once).
# Everything logs to Weights & Biases (project sidequestz-compatibility, one group per job).
# Needs ml/data/setup_raven.sh (with USERS_CONFIG=users) to have cached the models and data, and a
# W&B key in $SHARED_DIR/secrets/wandb_api_key (chmod 600). From the repo root:
#   mpcdf.py submit raven ml/compatibility/classifier/train.sbatch
#   resume: ... -- --work <earlier run dir>   (finished stages and runs are skipped)
#SBATCH --job-name=compat-train
#SBATCH --time=02:00:00
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --gres=gpu:a100:4
#SBATCH --cpus-per-task=72
#SBATCH --mem=0

set -eo pipefail
WORK="$RUN_DIR"
while [ $# -gt 0 ]; do
  case "$1" in
    --work) WORK="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
REPO="${REPO:-karthiksing05/sidequestz-event-embedding-text}"
EMB="${EMB:-$SHARED_DIR/embeddings/Qwen--Qwen3-Embedding-0.6B}"
PY="$SHARED_DIR/venv-embed/bin/python"
export HF_HOME="/ptmp/$USER/hf_cache" HF_HUB_OFFLINE=1 HF_DATASETS_OFFLINE=1
export XDG_CACHE_HOME="$SHARED_DIR/cache/xdg" PYTHONUNBUFFERED=1 TOKENIZERS_PARALLELISM=false
export WANDB_PROJECT=sidequestz-compatibility WANDB_ENTITY=karthiksing05-Independent WANDB_DIR="$WORK"
export WANDB_RUN_GROUP="${WANDB_RUN_GROUP:-recipe-$(basename "$WORK")}"
[ -f "$SHARED_DIR/secrets/wandb_api_key" ] || { echo "missing $SHARED_DIR/secrets/wandb_api_key" >&2; exit 1; }
WANDB_API_KEY="$(cat "$SHARED_DIR/secrets/wandb_api_key")"; export WANDB_API_KEY
mkdir -p "$WORK"

NGPU=$(nvidia-smi -L | wc -l)
echo "job $SLURM_JOB_ID on $(hostname): $NGPU GPU(s), work dir $WORK"
echo "W&B: https://wandb.ai/$WANDB_ENTITY/$WANDB_PROJECT (group $WANDB_RUN_GROUP)"
cd ml
EXP=("$PY" -m compatibility.classifier.experiment --out "$WORK" --group "$WANDB_RUN_GROUP")

phase() {  # phase <n>: one worker per GPU, runs split round-robin
  for i in $(seq 0 $((NGPU - 1))); do
    CUDA_VISIBLE_DEVICES=$i "${EXP[@]}" train --phase "$1" --worker-index "$i" --num-workers "$NGPU" \
      > "$WORK/phase$1-worker$i.log" 2>&1 &
  done
  local status=0
  for pid in $(jobs -p); do wait "$pid" || status=1; done
  grep -h -E "p$1-.*: *$|Traceback|Error" "$WORK"/phase$1-worker*.log | tail -n 20 || true
  return $status
}

echo "[$(date +%T)] embeddings (ml/data/embed.py; finished splits are skipped)"
CUDA_VISIBLE_DEVICES=0 "$PY" -m data.embed --out "$EMB" --users-repo "$REPO" --users-config users \
  > "$WORK/embed.log" 2>&1 || { tail -n 30 "$WORK/embed.log"; exit 1; }
[ -f "$WORK/data.pt" ] || { echo "[$(date +%T)] prepare"; CUDA_VISIBLE_DEVICES=0 "${EXP[@]}" prepare --emb "$EMB"; }
[ -f "$WORK/baseline.json" ] || { echo "[$(date +%T)] baseline"; CUDA_VISIBLE_DEVICES=0 "${EXP[@]}" baseline; }
echo "[$(date +%T)] phase 1"; phase 1
[ -f "$WORK/phase1.json" ] || { echo "[$(date +%T)] select phase 1"; "${EXP[@]}" select; }
echo "[$(date +%T)] phase 2"; phase 2
echo "[$(date +%T)] final"; CUDA_VISIBLE_DEVICES=0 "${EXP[@]}" final
cat "$WORK/final/comparison.txt"
