"""The learned compatibility classifier.

`CompatibilityClassifier` is the public model. It wraps a backbone network
(`FlatMLP` or `LateFusionMLP`) and optionally adds a residual cosine prior:

    score = alpha * (cos(pos, event) - lambda * cos(neg, event)) + backbone(...)

Backbones share the contract `forward(pos, neg, event, similarity=None) -> [B]`,
where `similarity` is optional precomputed `compute_similarity_features` output.
"""

from dataclasses import dataclass
from typing import Sequence

import torch
from torch import nn

from .config import ClassifierConfig
from .features import (
    COSINE_DIFFERENCE,
    NEGATIVE_COSINE,
    POSITIVE_COSINE,
    SIMILARITY_FEATURE_DIM,
    baseline_from_similarity,
    build_embedding_interaction_features,
    build_features,
    compute_similarity_features,
    feature_dim,
    interaction_feature_dim,
)


class FlatMLP(nn.Module):
    """Original architecture: one MLP over [7D interaction features, cos(pos, event), cos(neg, event)]."""

    def __init__(
        self,
        embedding_dim: int,
        hidden_dims: Sequence[int] = (512, 128),
        dropout: float | Sequence[float] = (0.2, 0.1),
    ) -> None:
        super().__init__()
        if isinstance(dropout, (int, float)):
            dropout = [float(dropout)] * len(hidden_dims)
        if len(dropout) != len(hidden_dims):
            raise ValueError(f"dropout has {len(dropout)} entries but hidden_dims has {len(hidden_dims)}")

        self.embedding_dim = embedding_dim
        layers: list[nn.Module] = []
        in_dim = feature_dim(embedding_dim)
        for width, rate in zip(hidden_dims, dropout):
            layers += [nn.Linear(in_dim, width), nn.ReLU(), nn.Dropout(rate)]
            in_dim = width
        layers.append(nn.Linear(in_dim, 1))
        self.mlp = nn.Sequential(*layers)

    def forward(
        self,
        positive: torch.Tensor,
        negative: torch.Tensor,
        event: torch.Tensor,
        similarity: torch.Tensor | None = None,
    ) -> torch.Tensor:
        return self.mlp(build_features(positive, negative, event, similarity)).squeeze(-1)


class LateFusionMLP(nn.Module):
    """Compress 7D interaction features first, then fuse with the 3 explicit cosine features.

        7D -> embedding branch -> output_dim ─┐
                                              ├─ concat -> final head -> 1
        [pos_cos, neg_cos, cos_diff] (3) ─────┘

    The branch is Linear/ReLU/Dropout per hidden layer, then Linear/ReLU to
    `branch_output_dim`; the head is Linear/ReLU per hidden layer, then Linear to 1.
    """

    def __init__(
        self,
        embedding_dim: int,
        branch_hidden_dims: Sequence[int] = (256,),
        branch_output_dim: int = 64,
        branch_dropout: float = 0.2,
        head_hidden_dims: Sequence[int] = (32,),
    ) -> None:
        super().__init__()
        self.embedding_dim = embedding_dim

        branch: list[nn.Module] = []
        in_dim = interaction_feature_dim(embedding_dim)
        for width in branch_hidden_dims:
            branch += [nn.Linear(in_dim, width), nn.ReLU(), nn.Dropout(branch_dropout)]
            in_dim = width
        branch += [nn.Linear(in_dim, branch_output_dim), nn.ReLU()]
        self.embedding_branch = nn.Sequential(*branch)

        head: list[nn.Module] = []
        in_dim = branch_output_dim + SIMILARITY_FEATURE_DIM
        for width in head_hidden_dims:
            head += [nn.Linear(in_dim, width), nn.ReLU()]
            in_dim = width
        head.append(nn.Linear(in_dim, 1))
        self.head = nn.Sequential(*head)

    def forward(
        self,
        positive: torch.Tensor,
        negative: torch.Tensor,
        event: torch.Tensor,
        similarity: torch.Tensor | None = None,
    ) -> torch.Tensor:
        if similarity is None:
            similarity = compute_similarity_features(positive, negative, event)
        compressed = self.embedding_branch(build_embedding_interaction_features(positive, negative, event))
        return self.head(torch.cat([compressed, similarity], dim=-1)).squeeze(-1)


@dataclass
class ScoreComponents:
    """Intermediate values of one forward pass, each `[B]` (alpha is a 0-d tensor).

    Always `final_score == alpha * baseline_score + learned_correction`; without
    the residual baseline alpha is 0 and the correction is the whole score.
    """

    positive_cosine: torch.Tensor
    negative_cosine: torch.Tensor
    cosine_difference: torch.Tensor
    baseline_score: torch.Tensor
    learned_correction: torch.Tensor
    final_score: torch.Tensor
    alpha: torch.Tensor

    def as_dict(self) -> dict[str, torch.Tensor]:
        return dict(self.__dict__)


class CompatibilityClassifier(nn.Module):
    """Scores (positive user, negative user, event) embedding triples.

    `forward` takes three `[B, D]` tensors and returns raw scores `[B]`. No
    sigmoid is applied: scores are regression outputs for continuous labels,
    or logits when trained with BCEWithLogitsLoss.

    With `use_residual_baseline`, the backbone's output layer is zero-initialized
    so an untrained model scores exactly `initial_alpha * baseline`. alpha is an
    unconstrained `nn.Parameter` when `learnable_alpha`, otherwise a buffer;
    either way it is saved in the state dict.
    """

    def __init__(
        self,
        backbone: nn.Module,
        *,
        negative_weight: float = 1.0,
        use_residual_baseline: bool = False,
        learnable_alpha: bool = True,
        initial_alpha: float = 1.0,
    ) -> None:
        super().__init__()
        self.backbone = backbone
        self.embedding_dim: int = backbone.embedding_dim
        self.negative_weight = negative_weight
        self.use_residual_baseline = use_residual_baseline
        if use_residual_baseline:
            alpha = torch.tensor(float(initial_alpha))
            if learnable_alpha:
                self.alpha = nn.Parameter(alpha)
            else:
                self.register_buffer("alpha", alpha)
            _zero_init_output(backbone)

    @classmethod
    def from_config(cls, config: ClassifierConfig) -> "CompatibilityClassifier":
        if config.architecture == "flat":
            backbone: nn.Module = FlatMLP(config.embedding_dim, config.hidden_dims, config.dropout)
        else:
            backbone = LateFusionMLP(
                config.embedding_dim,
                config.embedding_branch_hidden_dims,
                config.embedding_branch_output_dim,
                config.embedding_branch_dropout,
                config.final_head_hidden_dims,
            )
        return cls(
            backbone,
            negative_weight=config.negative_weight,
            use_residual_baseline=config.use_residual_baseline,
            learnable_alpha=config.learnable_alpha,
            initial_alpha=config.initial_alpha,
        )

    def forward(self, positive: torch.Tensor, negative: torch.Tensor, event: torch.Tensor) -> torch.Tensor:
        return self.score_components(positive, negative, event).final_score

    def score_components(self, positive: torch.Tensor, negative: torch.Tensor, event: torch.Tensor) -> ScoreComponents:
        """Forward pass that also returns the cosine, baseline and correction terms."""
        if positive.shape[-1] != self.embedding_dim:
            raise ValueError(f"expected embedding dim {self.embedding_dim}, got {positive.shape[-1]}")
        similarity = compute_similarity_features(positive, negative, event)
        baseline = baseline_from_similarity(similarity, self.negative_weight)
        correction = self.backbone(positive, negative, event, similarity=similarity)
        if self.use_residual_baseline:
            alpha = self.alpha
            final = alpha * baseline + correction
        else:
            alpha = torch.zeros((), device=correction.device)
            final = correction
        return ScoreComponents(
            positive_cosine=similarity[..., POSITIVE_COSINE],
            negative_cosine=similarity[..., NEGATIVE_COSINE],
            cosine_difference=similarity[..., COSINE_DIFFERENCE],
            baseline_score=baseline,
            learned_correction=correction,
            final_score=final,
            alpha=alpha,
        )


def _zero_init_output(module: nn.Module) -> None:
    """Zero the last Linear layer so the module initially outputs 0."""
    last = [m for m in module.modules() if isinstance(m, nn.Linear)][-1]
    nn.init.zeros_(last.weight)
    nn.init.zeros_(last.bias)
