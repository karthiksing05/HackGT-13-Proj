"""Evaluate learned classifiers against the cosine baseline on supplied data.

Regression errors (MAE / MSE / RMSE) are only reported for learned models:
the baseline's scores live on a different scale ([-1 - lambda, 1 + lambda])
than the labels. Rank-based metrics (Spearman and, when batches carry a
group id such as `user_id`, NDCG / Precision / Recall @ K) are reported for
everything.

Models exposing `score_components` (i.e. `CompatibilityClassifier`) also
report how their score splits into alpha * baseline and learned correction.
"""

import logging
from dataclasses import dataclass, field
from typing import Any, Hashable, Mapping

import numpy as np
import torch
from torch import nn
from torch.utils.data import DataLoader, Dataset

from . import metrics
from .config import ClassifierConfig
from .features import compute_cosine_baseline
from .train import as_dataloader, resolve_device, unpack_batch

logger = logging.getLogger(__name__)

CLASSIFIER = "classifier"
BASELINE = "cosine_baseline"
DEFAULT_GROUP_KEY = "user_id"

Models = nn.Module | Mapping[str, nn.Module] | None


@dataclass
class ScoredData:
    """Scores for every sample of an evaluation set, in loader order."""

    labels: np.ndarray
    baseline_scores: np.ndarray
    groups: list[Hashable] | None
    """Group id per sample, or None if any batch lacked the group key."""
    model_scores: dict[str, np.ndarray] = field(default_factory=dict)
    """Final score per model name (sigmoid applied if requested)."""
    components: dict[str, dict[str, np.ndarray]] = field(default_factory=dict)
    """Per model with `score_components`: positive_cosine, negative_cosine,
    cosine_difference, baseline_score, learned_correction, final_score (raw,
    pre-sigmoid) as `[N]` arrays, plus alpha as a 0-d array."""

    @property
    def classifier_scores(self) -> np.ndarray | None:
        """Scores of a model passed on its own (not in a mapping)."""
        return self.model_scores.get(CLASSIFIER)


@torch.no_grad()
def score_dataset(
    data: Dataset | DataLoader,
    model: Models,
    *,
    negative_weight: float = 1.0,
    apply_sigmoid: bool = False,
    group_key: str | None = DEFAULT_GROUP_KEY,
    batch_size: int = 512,
    device: str | torch.device = "auto",
) -> ScoredData:
    """Run the model(s) and the cosine baseline over `data`.

    `model` is one module (reported as "classifier"), a mapping of name ->
    module, or None for the baseline alone. Set `apply_sigmoid` for models
    trained with BCEWithLogitsLoss so scores are probabilities comparable to
    0/1 labels.
    """
    device = resolve_device(device)
    loader = as_dataloader(data, batch_size, shuffle=False)
    models = _as_model_dict(model)
    for m in models.values():
        m.to(device).eval()

    labels, baseline_scores = [], []
    scores: dict[str, list[torch.Tensor]] = {name: [] for name in models}
    parts: dict[str, dict[str, list[torch.Tensor]]] = {}
    groups: list[Hashable] | None = [] if group_key else None
    for batch in loader:
        pos, neg, event, label = unpack_batch(batch, device)
        labels.append(label.cpu())
        baseline_scores.append(compute_cosine_baseline(pos, neg, event, negative_weight).cpu())
        for name, m in models.items():
            if hasattr(m, "score_components"):
                comps = m.score_components(pos, neg, event).as_dict()
                for key, value in comps.items():
                    parts.setdefault(name, {}).setdefault(key, []).append(value.detach().cpu())
                raw = comps["final_score"]
            else:
                raw = m(pos, neg, event)
            scores[name].append((torch.sigmoid(raw) if apply_sigmoid else raw).cpu())
        if groups is not None:
            groups = _extend_groups(groups, batch, group_key)

    if not labels:
        raise ValueError("evaluation data produced no samples")
    return ScoredData(
        labels=torch.cat(labels).numpy(),
        baseline_scores=torch.cat(baseline_scores).numpy(),
        groups=groups,
        model_scores={name: torch.cat(s).numpy() for name, s in scores.items()},
        components={
            name: {
                key: values[0].numpy() if key == "alpha" else torch.cat(values).numpy()
                for key, values in comps.items()
            }
            for name, comps in parts.items()
        },
    )


def summarize_components(components: Mapping[str, np.ndarray]) -> dict[str, float]:
    """Summary of how much the learned correction moves the score away from the baseline.

    The correction usually carries a constant offset (labels are rarely
    centered like the baseline), which does not affect ranking, so the
    comparison uses spreads: `correction_to_baseline_std_ratio` =
    std(correction) / std(alpha * baseline). Well below 1 means the network
    refines the cosine prior; well above 1 means it overwhelms it. NaN when
    alpha * baseline is constant (e.g. without the residual, where alpha is 0).
    """
    alpha = float(components["alpha"])
    baseline = components["baseline_score"]
    correction = components["learned_correction"]
    baseline_std = float(np.std(alpha * baseline))
    correction_std = float(np.std(correction))
    return {
        "alpha": alpha,
        "mean_baseline_score": float(baseline.mean()),
        "mean_learned_correction": float(correction.mean()),
        "mean_abs_correction": float(np.abs(correction).mean()),
        "std_weighted_baseline": baseline_std,
        "std_learned_correction": correction_std,
        "correction_to_baseline_std_ratio": correction_std / baseline_std if baseline_std > 0 else float("nan"),
    }


def score_metrics(
    scores: np.ndarray,
    labels: np.ndarray,
    groups: list[Hashable] | None = None,
    *,
    ks: tuple[int, ...] = (5, 10),
    relevance_threshold: float = 0.5,
    include_errors: bool = True,
) -> dict[str, float]:
    """Spearman, optional MAE/MSE/RMSE, and ranking metrics when `groups` is given."""
    result: dict[str, float] = {"spearman": metrics.spearman(scores, labels)}
    if include_errors:
        result.update(
            mae=metrics.mae(scores, labels),
            mse=metrics.mse(scores, labels),
            rmse=metrics.rmse(scores, labels),
        )
    if groups is not None:
        result.update(metrics.ranking_metrics(scores, labels, groups, ks, relevance_threshold))
    return result


def evaluate(
    model: Models,
    data: Dataset | DataLoader,
    config: ClassifierConfig,
    *,
    group_key: str | None = DEFAULT_GROUP_KEY,
    include_diagnostics: bool = True,
) -> dict[str, dict[str, float]]:
    """Metrics for each model and the cosine baseline on the same data.

    `model` is one module (reported as "classifier"), a mapping of variant
    name -> module (e.g. {"flat": ..., "late_fusion": ..., "residual": ...}),
    or None. The result always has a "cosine_baseline" entry. Ranking metrics
    are included only if every batch carries `group_key`. With
    `include_diagnostics`, models exposing `score_components` also get the
    `summarize_components` values, which are logged at INFO.
    """
    scored = score_dataset(
        data,
        model,
        negative_weight=config.negative_weight,
        apply_sigmoid=config.is_binary,
        group_key=group_key,
        batch_size=config.batch_size,
        device=config.device,
    )
    common: dict[str, Any] = dict(
        groups=scored.groups, ks=config.ranking_ks, relevance_threshold=config.relevance_threshold
    )

    results = {}
    for name, scores in scored.model_scores.items():
        results[name] = score_metrics(scores, scored.labels, **common)
        if include_diagnostics and name in scored.components:
            summary = summarize_components(scored.components[name])
            logger.info("%s components: %s", name, {k: round(v, 4) for k, v in summary.items()})
            results[name].update(summary)
    results[BASELINE] = score_metrics(scored.baseline_scores, scored.labels, include_errors=False, **common)
    return results


def format_comparison(results: dict[str, dict[str, float]]) -> str:
    """Render `evaluate` output as a plain-text table, one row per metric."""
    names = list(results)
    metric_names = list(dict.fromkeys(m for r in results.values() for m in r))
    width = max(len(m) for m in metric_names)
    lines = [f"{'metric':<{width}}  " + "  ".join(f"{n:>16}" for n in names)]
    for m in metric_names:
        cells = [results[n].get(m) for n in names]
        lines.append(f"{m:<{width}}  " + "  ".join(f"{'-' if c is None else f'{c:.4f}':>16}" for c in cells))
    return "\n".join(lines)


def _as_model_dict(model: Models) -> dict[str, nn.Module]:
    if model is None:
        return {}
    if isinstance(model, nn.Module):
        return {CLASSIFIER: model}
    if BASELINE in model:
        raise ValueError(f"{BASELINE!r} is reserved for the cosine baseline")
    return dict(model)


def _extend_groups(groups: list[Hashable], batch: Any, key: str) -> list[Hashable] | None:
    if not isinstance(batch, Mapping) or key not in batch:
        return None
    values = batch[key]
    groups.extend(values.tolist() if isinstance(values, torch.Tensor) else list(values))
    return groups
