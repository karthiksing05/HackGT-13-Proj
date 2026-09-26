"""Feature construction and cosine scoring over (positive, negative, event) embeddings.

Every function accepts a single example (`[D]`) or a batch (`[B, D]`); all
operations act on the last dimension. All cosine logic used by the models,
the residual baseline and standalone baseline evaluation lives here.
"""

import torch
import torch.nn.functional as F

COSINE_EPS = 1e-8

# pos, neg, event, |pos-event|, |neg-event|, pos*event, neg*event
_INTERACTION_BLOCKS = 7
# Columns of `compute_similarity_features`.
POSITIVE_COSINE, NEGATIVE_COSINE, COSINE_DIFFERENCE = 0, 1, 2
SIMILARITY_FEATURE_DIM = 3


def interaction_feature_dim(embedding_dim: int) -> int:
    """Width of `build_embedding_interaction_features` output: 7 * D."""
    return _INTERACTION_BLOCKS * embedding_dim


def feature_dim(embedding_dim: int) -> int:
    """Width of the flat `build_features` output: 7 * D + 2."""
    return interaction_feature_dim(embedding_dim) + 2


def cosine_similarity(a: torch.Tensor, b: torch.Tensor) -> torch.Tensor:
    """Cosine similarity along the last dimension: `[..., D] x [..., D] -> [...]`.

    Zero vectors yield 0 rather than NaN.
    """
    return F.cosine_similarity(a, b, dim=-1, eps=COSINE_EPS)


def build_embedding_interaction_features(
    positive: torch.Tensor, negative: torch.Tensor, event: torch.Tensor
) -> torch.Tensor:
    """High-dimensional interaction features `[..., 7 * D]`, no cosine terms.

    Layout: [pos, neg, event, |pos - event|, |neg - event|, pos * event, neg * event].
    """
    _check_shapes(positive, negative, event)
    return torch.cat(
        [
            positive,
            negative,
            event,
            (positive - event).abs(),
            (negative - event).abs(),
            positive * event,
            negative * event,
        ],
        dim=-1,
    )


def compute_similarity_features(positive: torch.Tensor, negative: torch.Tensor, event: torch.Tensor) -> torch.Tensor:
    """Explicit similarity features `[..., 3]`: [cos(pos, event), cos(neg, event), their difference].

    The difference is unweighted; `negative_weight` only enters the baseline score.
    """
    _check_shapes(positive, negative, event)
    pos_cos = cosine_similarity(positive, event)
    neg_cos = cosine_similarity(negative, event)
    return torch.stack([pos_cos, neg_cos, pos_cos - neg_cos], dim=-1)


def baseline_from_similarity(similarity: torch.Tensor, negative_weight: float = 1.0) -> torch.Tensor:
    """cos(pos, event) - negative_weight * cos(neg, event) from `compute_similarity_features` output."""
    return similarity[..., POSITIVE_COSINE] - negative_weight * similarity[..., NEGATIVE_COSINE]


def compute_cosine_baseline(
    positive: torch.Tensor,
    negative: torch.Tensor,
    event: torch.Tensor,
    negative_weight: float = 1.0,
) -> torch.Tensor:
    """Untrained baseline: cos(pos, event) - negative_weight * cos(neg, event), shape `[...]`."""
    return baseline_from_similarity(compute_similarity_features(positive, negative, event), negative_weight)


# Original name, kept for existing callers.
cosine_baseline_score = compute_cosine_baseline


def build_features(
    positive: torch.Tensor,
    negative: torch.Tensor,
    event: torch.Tensor,
    similarity: torch.Tensor | None = None,
) -> torch.Tensor:
    """Flat-MLP input `[..., 7 * D + 2]`: interaction features plus cos(pos, event), cos(neg, event).

    Pass `similarity` (from `compute_similarity_features`) to reuse already computed cosines.
    """
    if similarity is None:
        similarity = compute_similarity_features(positive, negative, event)
    interactions = build_embedding_interaction_features(positive, negative, event)
    return torch.cat([interactions, similarity[..., [POSITIVE_COSINE, NEGATIVE_COSINE]]], dim=-1)


def _check_shapes(positive: torch.Tensor, negative: torch.Tensor, event: torch.Tensor) -> None:
    if not (positive.shape == negative.shape == event.shape):
        raise ValueError(
            "positive, negative and event embeddings must share a shape, got "
            f"{tuple(positive.shape)}, {tuple(negative.shape)}, {tuple(event.shape)}"
        )
    if positive.dim() == 0:
        raise ValueError("embeddings must have at least one dimension")
