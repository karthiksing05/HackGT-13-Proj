"""Run training.md end to end on the synthetic users data, logging everything to Weights & Biases.

Stages (each reads what the previous ones wrote under --out):

    prepare   load embeddings (ml/data/embed.py output) + users config, build the persona-split
              triples, save them as data.pt
    baseline  cosine baseline on validation for a grid of lambdas; the best lambda is kept for a
              fair baseline comparison (training.md: "Tune lambda on validation for the baseline")
    train     train the runs of one phase (split across workers / GPUs), each to its own checkpoint,
              then evaluate on validation
    select    rank a phase's runs on validation: NDCG@10, then Recall@10, then Spearman
    final     pick the overall best run on validation, evaluate it on test exactly once, write the
              report and the final checkpoint

Phase 1 is training.md's comparison procedure: the residual, late_fusion and flat variants plus the
residual with MSE loss, each with seeds 0, 1, 2 so differences can be judged against seed noise.
Phase 2 tunes the phase-1 winner (learning rate, weight decay, batch size, dropout, width, the
tuned lambda, patience, and selecting epochs by validation NDCG@10: training.md's open question).

    python -m compatibility.classifier.experiment prepare --emb EMB --out OUT
    python -m compatibility.classifier.experiment train --out OUT --phase 1 --worker-index 0 --num-workers 4
"""

from __future__ import annotations

import argparse
import json
import logging
import math
import os
import shutil
import statistics
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import torch

from . import metrics
from .config import MODEL_VARIANTS, ClassifierConfig
from .data import TripleSet
from .evaluate import BASELINE, evaluate, format_comparison, score_dataset
from .train import TrainingCallback, TrainingHistory, load_checkpoint, resolve_device, train

logger = logging.getLogger("experiment")

LAMBDAS = (0.0, 0.25, 0.5, 0.75, 1.0, 1.25, 1.5, 2.0)
RANK_KEYS = ("ndcg@10", "recall@10", "spearman")  # training.md's selection order
SEEDS = (0, 1, 2)
EMBEDDING_MODEL = "Qwen/Qwen3-Embedding-0.6B"
BATCH_LOG_EVERY = 25


# --------------------------------------------------------------------------- plans


@dataclass
class RunSpec:
    name: str
    variant: str
    overrides: dict[str, Any] = field(default_factory=dict)
    tags: list[str] = field(default_factory=list)

    def config(self, embedding_dim: int) -> ClassifierConfig:
        return ClassifierConfig(embedding_dim=embedding_dim).with_variant(self.variant).replace(**self.overrides)


def phase1_plan() -> list[RunSpec]:
    runs = []
    for seed in SEEDS:
        for variant, loss in (("residual", "huber"), ("late_fusion", "huber"), ("flat", "huber"), ("residual", "mse")):
            suffix = "" if loss == "huber" else f"-{loss}"
            runs.append(RunSpec(f"p1-{variant}{suffix}-s{seed}", variant, {"loss": loss, "seed": seed},
                                ["phase1", variant, loss]))
    return runs


def phase2_plan(winner: RunSpec, tuned_lambda: float) -> list[RunSpec]:
    """Single changes around the phase-1 winner (seed 0), so each effect is readable on its own."""
    base = {k: v for k, v in winner.overrides.items() if k != "seed"}
    late = MODEL_VARIANTS[winner.variant]["architecture"] == "late_fusion"
    changes: dict[str, dict[str, Any]] = {
        "lr3e-4": {"learning_rate": 3e-4},
        "lr3e-3": {"learning_rate": 3e-3},
        "wd1e-3": {"weight_decay": 1e-3},
        "bs512": {"batch_size": 512},
        "patience10": {"early_stopping_patience": 10},
        "select-ndcg": {"selection_metric": "ndcg@10"},
    }
    if late:
        changes["drop0.1"] = {"embedding_branch_dropout": 0.1}
        changes["drop0.35"] = {"embedding_branch_dropout": 0.35}
        changes["wide"] = {"embedding_branch_hidden_dims": (512,), "embedding_branch_output_dim": 128,
                           "final_head_hidden_dims": (64,)}
    else:
        changes["drop0.1"] = {"dropout": (0.1, 0.05)}
        changes["drop0.35"] = {"dropout": (0.35, 0.2)}
        changes["wide"] = {"hidden_dims": (1024, 256), "dropout": (0.2, 0.1)}
    if winner.variant == "residual" and not math.isclose(tuned_lambda, 1.0):
        changes[f"lambda{tuned_lambda:g}"] = {"negative_weight": tuned_lambda}
    loss_tag = base.get("loss", "huber")
    return [RunSpec(f"p2-{winner.variant}{'' if loss_tag == 'huber' else '-' + loss_tag}-{name}", winner.variant,
                    {**base, **change, "seed": 0}, ["phase2", winner.variant, name])
            for name, change in changes.items()]


# --------------------------------------------------------------------------- helpers


def rank_key(m: dict[str, float]) -> tuple[float, ...]:
    return tuple(-1e9 if (v := m.get(k)) is None or math.isnan(v) else v for k in RANK_KEYS)


def read_json(path: Path) -> Any:
    return json.loads(path.read_text())


def write_json(path: Path, obj: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, indent=2, default=float))


def load_data(out: Path, device: torch.device) -> dict[str, TripleSet]:
    state = torch.load(out / "data.pt", weights_only=False)
    return {split: TripleSet.from_state(s).to(device) for split, s in state.items()}


def wandb_run(args: argparse.Namespace, name: str, job_type: str, config: dict[str, Any], tags: list[str]):
    import wandb

    return wandb.init(project=args.wandb_project, entity=args.wandb_entity, group=args.group, name=name,
                      job_type=job_type, config=config, tags=tags, dir=str(args.out), reinit="finish_previous")


def comparison_table(results: dict[str, dict[str, float]]):
    import wandb

    names = list(results)
    metric_names = list(dict.fromkeys(m for r in results.values() for m in r))
    return wandb.Table(columns=["metric", *names],
                       data=[[m, *[results[n].get(m) for n in names]] for m in metric_names])


def val_ranking(model: torch.nn.Module, val: TripleSet, config: ClassifierConfig) -> dict[str, float]:
    """Validation Spearman and ranking metrics for monitoring (cheap: ~20k pairs)."""
    scored = score_dataset(val.loader(4096), model, negative_weight=config.negative_weight,
                           apply_sigmoid=config.is_binary, device=val.labels.device)
    result = metrics.ranking_metrics(scored.model_scores["classifier"], scored.labels, scored.groups,
                                     ks=config.ranking_ks, relevance_threshold=config.relevance_threshold)
    result["spearman"] = metrics.spearman(scored.model_scores["classifier"], scored.labels)
    return result


class WandbCallback(TrainingCallback):
    """Per-batch training loss and per-epoch losses, alpha and validation ranking metrics."""

    def __init__(self, run, val: TripleSet, config: ClassifierConfig, steps_per_epoch: int) -> None:
        self.run, self.val, self.config = run, val, config
        self.steps_per_epoch = steps_per_epoch
        self.epoch_start = time.time()

    def on_batch_end(self, step: int, loss: float) -> None:
        if step % BATCH_LOG_EVERY == 0:
            self.run.log({"train/batch_loss": loss, "train/step": step, "epoch_float": step / self.steps_per_epoch})

    def on_epoch_end(self, epoch: int, history: TrainingHistory, model: torch.nn.Module, improved: bool) -> None:
        ranking = val_ranking(model, self.val, self.config)
        row = {"epoch": epoch, "train/loss": history.train_loss[-1], "val/loss": history.val_loss[-1],
               "val/best_loss": min(history.val_loss), "best_epoch": history.best_epoch,
               "epoch_time_s": time.time() - self.epoch_start,
               **{f"val/{k}": v for k, v in ranking.items() if k != "num_groups"}}
        if history.selection_metric != "val_loss":
            row["val/selection"] = history.val_selection[-1]
        alpha = getattr(model, "alpha", None)
        if isinstance(alpha, torch.Tensor):
            row["alpha"] = float(alpha.detach())
        self.run.log(row)
        self.epoch_start = time.time()
        logger.info("epoch %d: train %.5f val %.5f ndcg@10 %.4f%s", epoch, row["train/loss"], row["val/loss"],
                    ranking.get("ndcg@10", float("nan")), " (best)" if improved else "")


# --------------------------------------------------------------------------- stages


def cmd_prepare(args: argparse.Namespace) -> None:
    from data.hf_dataset import load_events

    from .data import load_triples

    users = load_events(args.repo, name="users")
    triples = load_triples(args.emb, users)
    torch.save({split: t.state() for split, t in triples.items()}, args.out / "data.pt")
    summary = {split: t.summary() for split, t in triples.items()}
    meta = read_json(Path(args.emb) / "meta.json")
    write_json(args.out / "data_summary.json", {"splits": summary, "embeddings": meta, "repo": args.repo})
    logger.info("prepared: %s", json.dumps(summary))


def cmd_baseline(args: argparse.Namespace) -> None:
    device = resolve_device("auto")
    data = load_data(args.out, device)
    dim = data["train"].positive.shape[1]
    run = wandb_run(args, "baseline-lambda", "baseline", {"lambdas": LAMBDAS, "embedding_model": EMBEDDING_MODEL,
                                                          **{f"data/{k}": v for k, v in read_json(
                                                              args.out / "data_summary.json")["splits"].items()}},
                    ["baseline"])
    grid = {}
    for lam in LAMBDAS:
        config = ClassifierConfig(embedding_dim=dim, negative_weight=lam)
        grid[lam] = evaluate(None, data["validation"].loader(4096), config)[BASELINE]
        run.log({"lambda": lam, **{f"val/{k}": v for k, v in grid[lam].items()}})
        logger.info("baseline lambda=%.2f: %s", lam, {k: round(v, 4) for k, v in grid[lam].items()})
    best = max(grid, key=lambda lam: rank_key(grid[lam]))
    result = {"best_lambda": best, "default_lambda": 1.0, "validation": {str(k): v for k, v in grid.items()}}
    write_json(args.out / "baseline.json", result)
    run.summary.update({"best_lambda": best, **{f"best/{k}": v for k, v in grid[best].items()},
                        **{f"lambda1/{k}": v for k, v in grid[1.0].items()}})
    run.log({"baseline_grid": comparison_table({f"lambda={k:g}": v for k, v in grid.items()})})
    run.finish()


def plan_for(args: argparse.Namespace) -> list[RunSpec]:
    if args.phase == 1:
        return phase1_plan()
    chosen = read_json(args.out / "phase1.json")["winner_spec"]
    return phase2_plan(RunSpec(**chosen), read_json(args.out / "baseline.json")["best_lambda"])


def cmd_train(args: argparse.Namespace) -> None:
    device = resolve_device("auto")
    data = load_data(args.out, device)
    dim = data["train"].positive.shape[1]
    specs = plan_for(args)[args.worker_index::args.num_workers]
    for spec in specs:
        run_dir = args.out / "runs" / spec.name
        if (run_dir / "val_metrics.json").exists():
            logger.info("%s already done, skipping", spec.name)
            continue
        config = spec.config(dim)
        train_loader = data["train"].loader(config.batch_size, shuffle=True, seed=config.seed)
        val_loader = data["validation"].loader(4096)
        run = wandb_run(args, spec.name, "train", {**config.to_dict(), "run": spec.name, "variant": spec.variant,
                                                   "phase": args.phase, "embedding_model": EMBEDDING_MODEL,
                                                   "train_pairs": len(data["train"]),
                                                   "val_pairs": len(data["validation"])}, spec.tags)
        run.define_metric("epoch")
        run.define_metric("train/step")
        run.define_metric("train/batch_loss", step_metric="train/step")
        for key in ("train/loss", "val/*", "alpha", "best_epoch", "epoch_time_s"):
            run.define_metric(key, step_metric="epoch")
        callback = WandbCallback(run, data["validation"], config, len(train_loader))
        score_fn = None
        if config.selection_metric != "val_loss":
            score_fn = lambda m, c=config: val_ranking(m, data["validation"], c)[c.selection_metric]  # noqa: E731
        started = time.time()
        result = train(config, train_loader, val_loader, run_dir / "best.pt", callbacks=[callback],
                       val_score_fn=score_fn)
        results = evaluate(result.model, val_loader, config)
        val_metrics = results["classifier"]
        write_json(run_dir / "val_metrics.json", {"run": spec.name, "variant": spec.variant, "spec": spec.__dict__,
                                                   "validation": val_metrics,
                                                   "baseline_same_lambda": results[BASELINE],
                                                   "history": result.history.to_dict(),
                                                   "seconds": time.time() - started})
        run.summary.update({"best_epoch": result.history.best_epoch, "best_val_loss": result.history.best_val_loss,
                            "stopped_early": result.history.stopped_early,
                            "epochs_run": result.history.epochs_run,
                            **{f"final_val/{k}": v for k, v in val_metrics.items()}})
        run.log({"validation_comparison": comparison_table({spec.name: val_metrics, "cosine_baseline": results[BASELINE]})})
        run.finish()
        logger.info("%s: %s", spec.name, format_comparison(results))


def phase_results(out: Path, prefix: str) -> dict[str, dict[str, Any]]:
    return {p.parent.name: read_json(p) for p in sorted((out / "runs").glob(f"{prefix}*/val_metrics.json"))}


def cmd_select(args: argparse.Namespace) -> None:
    """Phase 1: average each variant over seeds and pick the winner on validation."""
    results = phase_results(args.out, "p1-")
    groups: dict[str, list[dict[str, Any]]] = {}
    for r in results.values():
        groups.setdefault(r["run"].rsplit("-s", 1)[0], []).append(r)
    table = {}
    for name, runs in groups.items():
        vals = {k: [r["validation"][k] for r in runs] for k in ("ndcg@10", "recall@10", "spearman", "ndcg@5",
                                                                "precision@10", "mae", "rmse")}
        table[name] = {**{f"{k}_mean": statistics.mean(v) for k, v in vals.items()},
                       **{f"{k}_std": statistics.pstdev(v) for k, v in vals.items()}, "seeds": len(runs)}
    winner = max(table, key=lambda n: rank_key({k: table[n][f"{k}_mean"] for k in RANK_KEYS}))
    spec = next(r["spec"] for r in groups[winner] if r["spec"]["overrides"].get("seed") == 0)
    write_json(args.out / "phase1.json", {"winner": winner, "winner_spec": spec, "table": table})

    # training.md step 3, literally: the seed-0 models side by side with the default-lambda baseline.
    device = resolve_device("auto")
    data = load_data(args.out, device)
    models = {name.removeprefix("p1-"): load_checkpoint(args.out / "runs" / f"{name}-s0" / "best.pt", device)[0]
              for name in groups if (args.out / "runs" / f"{name}-s0" / "best.pt").exists()}
    config = ClassifierConfig(embedding_dim=data["train"].positive.shape[1])
    comparison = evaluate(models, data["validation"].loader(4096), config)
    (args.out / "phase1_comparison.txt").write_text(format_comparison(comparison) + "\n")
    logger.info("phase 1, seed 0, validation:\n%s", format_comparison(comparison))

    run = wandb_run(args, "select-phase1", "select", {"phase": 1}, ["select"])
    run.log({"phase1_by_variant": comparison_table(table), "phase1_seed0_comparison": comparison_table(comparison)})
    run.summary.update({"winner": winner, **{f"winner/{k}": v for k, v in table[winner].items()}})
    run.finish()
    logger.info("phase 1 winner: %s  %s", winner, table[winner])


def cmd_final(args: argparse.Namespace) -> None:
    device = resolve_device("auto")
    data = load_data(args.out, device)
    phase1 = read_json(args.out / "phase1.json")
    baseline = read_json(args.out / "baseline.json")
    candidates = {**{k: v for k, v in phase_results(args.out, "p1-").items()
                     if k.rsplit("-s", 1)[0] == phase1["winner"] and k.endswith("-s0")},
                  **phase_results(args.out, "p2-")}
    best = max(candidates, key=lambda n: rank_key(candidates[n]["validation"]))
    model, ckpt = load_checkpoint(args.out / "runs" / best / "best.pt", device)
    config = ClassifierConfig.from_dict(ckpt["config"])

    # The single test evaluation (training.md "Final evaluation"): the chosen model plus the cosine
    # baseline at the model's lambda, and the baseline at its validation-tuned lambda.
    test_loader = data["test"].loader(4096)
    test = evaluate(model, test_loader, config)
    tuned = baseline["best_lambda"]
    test[f"cosine_baseline_lambda{tuned:g}"] = evaluate(None, test_loader, config.replace(negative_weight=tuned))[BASELINE]
    val = evaluate(model, data["validation"].loader(4096), config)

    final_dir = args.out / "final"
    final_dir.mkdir(exist_ok=True)
    shutil.copy(args.out / "runs" / best / "best.pt", final_dir / "best.pt")
    report = {"chosen_run": best, "config": ckpt["config"], "best_epoch": ckpt["epoch"], "val_loss": ckpt["val_loss"],
              "embedding_model": EMBEDDING_MODEL, "validation": val, "test": test,
              "tuned_baseline_lambda": tuned, "phase1_winner": phase1["winner"],
              "candidates_validation": {k: v["validation"] for k, v in candidates.items()}}
    write_json(final_dir / "results.json", report)
    (final_dir / "comparison.txt").write_text(
        f"chosen run: {best} (epoch {ckpt['epoch']})\n\nvalidation\n{format_comparison(val)}\n\n"
        f"test (evaluated once)\n{format_comparison(test)}\n")

    run = wandb_run(args, "final-test", "final", {"chosen_run": best, **ckpt["config"]}, ["final"])
    run.log({"final_validation": comparison_table(val), "final_test": comparison_table(test),
             "phase2_and_winner_validation": comparison_table({k: v["validation"] for k, v in candidates.items()})})
    run.summary.update({"chosen_run": best, **{f"test/{k}": v for k, v in test["classifier"].items()},
                        **{f"test_baseline_tuned/{k}": v for k, v in test[f"cosine_baseline_lambda{tuned:g}"].items()}})
    run.finish()
    logger.info("final: %s\n%s", best, (final_dir / "comparison.txt").read_text())


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("stage", choices=["prepare", "baseline", "train", "select", "final"])
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--emb", type=Path, help="embed.py output dir (prepare)")
    parser.add_argument("--repo", default="karthiksing05/sidequestz-event-embedding-text")
    parser.add_argument("--phase", type=int, default=1, choices=[1, 2])
    parser.add_argument("--worker-index", type=int, default=0)
    parser.add_argument("--num-workers", type=int, default=1)
    parser.add_argument("--group", default=os.environ.get("WANDB_RUN_GROUP", "recipe"))
    parser.add_argument("--wandb-project", default=os.environ.get("WANDB_PROJECT", "sidequestz-compatibility"))
    parser.add_argument("--wandb-entity", default=os.environ.get("WANDB_ENTITY"))
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(name)s %(message)s")
    args.out.mkdir(parents=True, exist_ok=True)
    {"prepare": cmd_prepare, "baseline": cmd_baseline, "train": cmd_train, "select": cmd_select,
     "final": cmd_final}[args.stage](args)


if __name__ == "__main__":
    main()
