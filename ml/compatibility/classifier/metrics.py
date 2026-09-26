"""Prediction and ranking metrics over 1-D score / label arrays.

Everything here is framework-agnostic: inputs are anything `np.asarray`
accepts (move tensors to CPU first).
"""

from collections import defaultdict
from typing import Hashable, Iterable, Sequence

import numpy as np


def mae(predictions, targets) -> float:
    p, t = _pair(predictions, targets)
    return float(np.mean(np.abs(p - t)))


def mse(predictions, targets) -> float:
    p, t = _pair(predictions, targets)
    return float(np.mean((p - t) ** 2))


def rmse(predictions, targets) -> float:
    return float(np.sqrt(mse(predictions, targets)))


def spearman(predictions, targets) -> float:
    """Spearman rank correlation (ties get average ranks). NaN if either side is constant."""
    p, t = _pair(predictions, targets)
    if p.size < 2:
        return float("nan")
    rp, rt = _average_ranks(p), _average_ranks(t)
    rp, rt = rp - rp.mean(), rt - rt.mean()
    denom = np.sqrt((rp**2).sum() * (rt**2).sum())
    return float((rp * rt).sum() / denom) if denom > 0 else float("nan")


def ndcg_at_k(scores, relevances, k: int) -> float:
    """NDCG@K with linear gains. Relevances must be >= 0. Returns NaN when no item is relevant."""
    s, r = _pair(scores, relevances)
    if np.any(r < 0):
        raise ValueError("NDCG requires non-negative relevances")
    discounts = 1.0 / np.log2(np.arange(2, min(k, s.size) + 2))
    dcg = (r[_top_k(s, k)] * discounts).sum()
    ideal = (np.sort(r)[::-1][:k] * discounts).sum()
    return float(dcg / ideal) if ideal > 0 else float("nan")


def precision_at_k(scores, relevant, k: int) -> float:
    """Fraction of the top min(K, n) items that are relevant."""
    s, rel = _pair(scores, relevant)
    top = _top_k(s, k)
    return float(rel[top].astype(bool).mean())


def recall_at_k(scores, relevant, k: int) -> float:
    """Fraction of all relevant items found in the top K. NaN when nothing is relevant."""
    s, rel = _pair(scores, relevant)
    rel = rel.astype(bool)
    total = rel.sum()
    return float(rel[_top_k(s, k)].sum() / total) if total > 0 else float("nan")


def ranking_metrics(
    scores,
    labels,
    groups: Sequence[Hashable],
    ks: Iterable[int] = (5, 10),
    relevance_threshold: float = 0.5,
    min_group_size: int = 2,
) -> dict[str, float]:
    """Per-group ranking metrics averaged over groups (e.g. one group per user).

    Within each group, items are ranked by `scores`. NDCG uses `labels` as
    graded relevance; Precision/Recall treat `labels >= relevance_threshold`
    as relevant. Groups smaller than `min_group_size` are skipped, and groups
    where a metric is undefined (no relevant items) are left out of that
    metric's mean. `num_groups` reports how many groups were evaluated.
    """
    s, t = _pair(scores, labels)
    if len(groups) != s.size:
        raise ValueError(f"got {len(groups)} group ids for {s.size} scores")

    members: dict[Hashable, list[int]] = defaultdict(list)
    for i, g in enumerate(groups):
        members[g].append(i)

    per_metric: dict[str, list[float]] = defaultdict(list)
    evaluated = 0
    for idx in members.values():
        if len(idx) < min_group_size:
            continue
        evaluated += 1
        gs, gt = s[idx], t[idx]
        relevant = gt >= relevance_threshold
        for k in ks:
            per_metric[f"ndcg@{k}"].append(ndcg_at_k(gs, gt, k))
            per_metric[f"precision@{k}"].append(precision_at_k(gs, relevant, k))
            per_metric[f"recall@{k}"].append(recall_at_k(gs, relevant, k))

    result = {name: _nanmean(values) for name, values in per_metric.items()}
    result["num_groups"] = float(evaluated)
    return result


def _top_k(scores: np.ndarray, k: int) -> np.ndarray:
    if k <= 0:
        raise ValueError(f"k must be positive, got {k}")
    return np.argsort(-scores, kind="stable")[:k]


def _average_ranks(x: np.ndarray) -> np.ndarray:
    order = np.argsort(x, kind="stable")
    ranks = np.empty(x.size, dtype=np.float64)
    ranks[order] = np.arange(x.size, dtype=np.float64)
    # Replace each run of tied values with the mean of its ranks.
    _, inverse, counts = np.unique(x, return_inverse=True, return_counts=True)
    sums = np.bincount(inverse, weights=ranks)
    return sums[inverse] / counts[inverse]


def _nanmean(values: list[float]) -> float:
    arr = np.asarray(values, dtype=np.float64)
    arr = arr[~np.isnan(arr)]
    return float(arr.mean()) if arr.size else float("nan")


def _pair(a, b) -> tuple[np.ndarray, np.ndarray]:
    a = np.asarray(a, dtype=np.float64).reshape(-1)
    b = np.asarray(b, dtype=np.float64).reshape(-1)
    if a.shape != b.shape:
        raise ValueError(f"length mismatch: {a.size} vs {b.size}")
    if a.size == 0:
        raise ValueError("metrics need at least one value")
    return a, b
