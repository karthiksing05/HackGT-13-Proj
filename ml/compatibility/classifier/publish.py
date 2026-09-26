"""Publish a finished experiment (experiment.py output) as a private Hugging Face model repo.

    python -m compatibility.classifier.publish RUN_DIR --repo-id USER/NAME [--wandb-url URL] [--push]

Uploads the final checkpoint, every run's checkpoint and validation metrics, the selection files and a
model card (README.md) generated from them. Without --push only the card is written (RUN_DIR/README.md).
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any

SHOW = ("ndcg@5", "ndcg@10", "precision@5", "precision@10", "recall@5", "recall@10", "spearman", "mae", "rmse")


def fmt(v: Any) -> str:
    return "-" if v is None else f"{v:.4f}" if isinstance(v, float) else str(v)


def table(rows: dict[str, dict[str, Any]], cols: tuple[str, ...]) -> str:
    head = "| | " + " | ".join(cols) + " |\n|---|" + "---|" * len(cols)
    return head + "\n" + "\n".join(f"| {name} | " + " | ".join(fmt(r.get(c)) for c in cols) + " |"
                                   for name, r in rows.items())


def card(run_dir: Path, repo_id: str, dataset: str, wandb_url: str | None) -> str:
    res = json.loads((run_dir / "final" / "results.json").read_text())
    phase1 = json.loads((run_dir / "phase1.json").read_text())
    base = json.loads((run_dir / "baseline.json").read_text())
    data = json.loads((run_dir / "data_summary.json").read_text())["splits"]
    cfg, tuned = res["config"], res["tuned_baseline_lambda"]
    test = {"**this model**": res["test"]["classifier"],
            f"cosine baseline (λ = {cfg['negative_weight']:g}, the model's)": res["test"]["cosine_baseline"],
            f"cosine baseline (λ = {tuned:g}, tuned on validation)": res["test"][f"cosine_baseline_lambda{tuned:g}"]}
    p1 = {name: {k: f"{row[f'{k}_mean']:.4f} ± {row[f'{k}_std']:.4f}" for k in ("ndcg@10", "recall@10", "spearman")}
          for name, row in sorted(phase1["table"].items(), key=lambda kv: -kv[1]["ndcg@10_mean"])}
    runs = {name: {"run": name, **v} for name, v in res["candidates_validation"].items()}
    p2 = dict(sorted(runs.items(), key=lambda kv: -kv[1].get("ndcg@10", 0)))
    lam = {f"λ = {float(k):g}": v for k, v in base["validation"].items()}
    arch = f"`{cfg['architecture']}`" + (" with the residual cosine prior" if cfg["use_residual_baseline"] else "")
    diag = res["test"]["classifier"]
    diag_line = (f"On test, alpha is {diag['alpha']:.3f} and the correction-to-baseline spread ratio is "
                 f"{diag['correction_to_baseline_std_ratio']:.2f}; well below 1 means the network refines the "
                 f"cosine prior, well above 1 means it overrides it.") if cfg["use_residual_baseline"] else ""
    wandb = f"Training curves, per-epoch validation metrics and every run: [W&B]({wandb_url}).\n" if wandb_url else ""
    return f"""---
license: apache-2.0
library_name: pytorch
tags:
- recommendation
- event-recommendation
- compatibility
- embeddings
datasets:
- {dataset}
base_model: Qwen/Qwen3-Embedding-0.6B
---

# SideQuests compatibility classifier

This model scores how well an event suits a user, the ranking model behind SideQuests' event
recommendations. Its inputs are frozen `Qwen/Qwen3-Embedding-0.6B` embeddings of three texts, all in
the app's eight-section format: the user's likes (`positive_text`), the user's dislikes
(`negative_text`), and the event (`embedding_text`). It was trained following the repo's
`ml/training.md` on the synthetic [`{dataset}`](https://huggingface.co/datasets/{dataset}) data (`users`
config: LLM-rated user–event pairs).

{wandb}
**Chosen model:** `{res['chosen_run']}`: {arch}, {cfg['loss']} loss, lr {cfg['learning_rate']:g}, weight decay
{cfg['weight_decay']:g}, batch {cfg['batch_size']}, best epoch {res['best_epoch']} (selection: {cfg.get('selection_metric', 'val_loss')}).

## Test results (evaluated once, after selection on validation)

{table(test, SHOW)}

Rankings are per user, over each test user's 20 candidate events. Test users and test events never
appear in training. Relevant means label ≥ 0.5 (a judge rating of 2 or 3 out of 3). {diag_line}

## How it was selected

1. **Cosine baseline, λ sweep (validation).** Score: `cos(pos, event) - λ·cos(neg, event)`.

{table(lam, ("ndcg@10", "recall@10", "spearman"))}

2. **Phase 1, the recipe's comparison (validation, mean ± std over seeds 0–2).** Variants ranked by
   NDCG@10, then Recall@10, then Spearman; the winner was `{phase1['winner']}`.

{table(p1, ("ndcg@10", "recall@10", "spearman"))}

3. **Phase 2, tuning the winner (validation, seed 0).** One change at a time. The best validation run
   becomes the final model. For scale: across seeds, the phase-1 winner's NDCG@10 varied by
   ±{phase1['table'][phase1['winner']]['ndcg@10_std']:.4f}, so smaller differences are noise.

{table(p2, ("ndcg@10", "recall@10", "spearman", "mae"))}

## Data

| Split | Users | Pairs | Events | Label mean | Relevant share |
|---|---|---|---|---|---|
""" + "\n".join(f"| {s} | {d['users']:,} | {d['pairs']:,} | {d['events']:,} | {d['label_mean']:.3f} | "
                f"{d['relevant_share']:.1%} |" for s, d in data.items()) + f"""

## Usage

The model code lives in the SideQuests repo (`ml/compatibility/classifier`).

```python
import torch
from huggingface_hub import hf_hub_download
from sentence_transformers import SentenceTransformer
from compatibility.classifier import load_checkpoint

encoder = SentenceTransformer("Qwen/Qwen3-Embedding-0.6B")
model, ckpt = load_checkpoint(hf_hub_download("{repo_id}", "final/best.pt"))

def embed(text):  # an empty text (no dislikes) is a zero vector, as in training
    if not text.strip():
        return torch.zeros(1, 1024)
    return torch.tensor(encoder.encode([text], normalize_embeddings=True))

score = model(embed(positive_text), embed(negative_text), embed(event_embedding_text))  # higher = better fit
```

## Files

- `final/best.pt`: the chosen checkpoint (`model_state`, `config`, `epoch`, `val_loss`, `history`,
  `optimizer_state`), loadable with `load_checkpoint`.
- `final/results.json` and `final/comparison.txt`: validation and test metrics.
- `runs/<run>/best.pt` and `runs/<run>/val_metrics.json`: every trained run, for comparison.
- `baseline.json`, `phase1.json`, `data_summary.json`: the selection record.

## Limitations

- **LLM labels.** The training labels are one LLM's judgments on synthetic users and events, not real
  interactions. Expect a gap on real traffic, and retrain on logged outcomes when they exist.
- **Tied encoder.** Scores are only meaningful for `Qwen/Qwen3-Embedding-0.6B` embeddings (L2-normalized,
  no prompt) of texts in the eight-section format.
- **Candidate ranker.** The model re-ranks candidates. It was trained on retrieved, hard-negative and
  random candidates per user, not on the full catalog.
"""


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("run_dir", type=Path)
    parser.add_argument("--repo-id", required=True)
    parser.add_argument("--dataset", default="karthiksing05/sidequestz-event-embedding-text")
    parser.add_argument("--wandb-url", default=None)
    parser.add_argument("--push", action="store_true")
    args = parser.parse_args()

    text = card(args.run_dir, args.repo_id, args.dataset, args.wandb_url)
    (args.run_dir / "README.md").write_text(text)
    print(f"card written to {args.run_dir / 'README.md'}")
    if args.push:
        from huggingface_hub import HfApi

        api = HfApi()
        api.create_repo(args.repo_id, repo_type="model", private=True, exist_ok=True)
        api.upload_folder(repo_id=args.repo_id, folder_path=str(args.run_dir), commit_message="Add trained compatibility classifiers",
                          allow_patterns=["README.md", "final/*", "runs/*/best.pt", "runs/*/val_metrics.json",
                                          "baseline.json", "phase1.json", "data_summary.json"])
        print(f"pushed to https://huggingface.co/{args.repo_id} (private)")


if __name__ == "__main__":
    main()
