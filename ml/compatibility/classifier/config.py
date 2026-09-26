"""Configuration for the supervised compatibility classifier."""

from dataclasses import asdict, dataclass, fields
from typing import Any, Literal

LossType = Literal["huber", "mse", "bce"]
LOSS_TYPES: tuple[str, ...] = ("huber", "mse", "bce")
ArchitectureType = Literal["flat", "late_fusion"]
ARCHITECTURES: tuple[str, ...] = ("flat", "late_fusion")

# Config overrides for each model variant compared in ablations
# (the fourth variant, the cosine baseline alone, needs no model).
MODEL_VARIANTS: dict[str, dict[str, object]] = {
    "flat": {"architecture": "flat", "use_residual_baseline": False},
    "late_fusion": {"architecture": "late_fusion", "use_residual_baseline": False},
    "residual": {"architecture": "late_fusion", "use_residual_baseline": True},
}


@dataclass
class ClassifierConfig:
    """Model, training and evaluation settings.

    `architecture` picks the network:
    - "flat": the original MLP over [7D interaction features, 2 cosines],
      sized by `hidden_dims` / `dropout`. `dropout` is either one rate shared
      by every hidden layer or one rate per entry of `hidden_dims`.
    - "late_fusion": the 7D interaction features are compressed by the
      embedding branch (`embedding_branch_*`) to `embedding_branch_output_dim`
      dims, then joined with [cos(pos, event), cos(neg, event), difference]
      and scored by the final head (`final_head_hidden_dims`).

    With `use_residual_baseline`, the network output becomes a correction:
    score = alpha * baseline + network(...), where alpha starts at
    `initial_alpha` and is trained when `learnable_alpha`.

    `negative_weight` is the baseline's lambda, shared by the residual path
    and standalone baseline evaluation:
    baseline = cos(pos, event) - negative_weight * cos(neg, event).

    `device` is "auto" (cuda > mps > cpu) or anything `torch.device` accepts.

    `relevance_threshold` turns continuous labels into relevant / not relevant
    for Precision@K and Recall@K.
    """

    embedding_dim: int
    hidden_dims: tuple[int, ...] = (512, 128)
    dropout: float | tuple[float, ...] = (0.2, 0.1)

    architecture: ArchitectureType = "late_fusion"
    embedding_branch_hidden_dims: tuple[int, ...] = (256,)
    embedding_branch_output_dim: int = 64
    embedding_branch_dropout: float = 0.2
    final_head_hidden_dims: tuple[int, ...] = (32,)

    use_residual_baseline: bool = True
    learnable_alpha: bool = True
    initial_alpha: float = 1.0

    loss: LossType = "huber"
    huber_delta: float = 1.0

    learning_rate: float = 1e-3
    weight_decay: float = 1e-4
    batch_size: int = 128
    max_epochs: int = 50
    early_stopping_patience: int = 5
    early_stopping_min_delta: float = 0.0

    negative_weight: float = 1.0

    ranking_ks: tuple[int, ...] = (5, 10)
    relevance_threshold: float = 0.5

    device: str = "auto"
    seed: int = 0

    def __post_init__(self) -> None:
        if self.embedding_dim <= 0:
            raise ValueError(f"embedding_dim must be positive, got {self.embedding_dim}")

        self.hidden_dims = tuple(int(h) for h in self.hidden_dims)
        if any(h <= 0 for h in self.hidden_dims):
            raise ValueError(f"hidden_dims must be positive, got {self.hidden_dims}")

        if isinstance(self.dropout, (int, float)):
            self.dropout = (float(self.dropout),) * len(self.hidden_dims)
        self.dropout = tuple(float(p) for p in self.dropout)
        if len(self.dropout) != len(self.hidden_dims):
            raise ValueError(
                f"dropout has {len(self.dropout)} entries but hidden_dims has {len(self.hidden_dims)}"
            )
        if any(not 0.0 <= p < 1.0 for p in self.dropout):
            raise ValueError(f"dropout rates must be in [0, 1), got {self.dropout}")

        if self.architecture not in ARCHITECTURES:
            raise ValueError(f"architecture must be one of {ARCHITECTURES}, got {self.architecture!r}")
        self.embedding_branch_hidden_dims = tuple(int(h) for h in self.embedding_branch_hidden_dims)
        self.final_head_hidden_dims = tuple(int(h) for h in self.final_head_hidden_dims)
        if any(h <= 0 for h in (*self.embedding_branch_hidden_dims, *self.final_head_hidden_dims)):
            raise ValueError("embedding_branch_hidden_dims and final_head_hidden_dims must be positive")
        if self.embedding_branch_output_dim <= 0:
            raise ValueError(f"embedding_branch_output_dim must be positive, got {self.embedding_branch_output_dim}")
        if not 0.0 <= self.embedding_branch_dropout < 1.0:
            raise ValueError(f"embedding_branch_dropout must be in [0, 1), got {self.embedding_branch_dropout}")

        if self.loss not in LOSS_TYPES:
            raise ValueError(f"loss must be one of {LOSS_TYPES}, got {self.loss!r}")
        if self.batch_size <= 0 or self.max_epochs <= 0:
            raise ValueError("batch_size and max_epochs must be positive")
        if self.early_stopping_patience < 0:
            raise ValueError("early_stopping_patience must be >= 0")

        self.ranking_ks = tuple(int(k) for k in self.ranking_ks)
        if any(k <= 0 for k in self.ranking_ks):
            raise ValueError(f"ranking_ks must be positive, got {self.ranking_ks}")

    @property
    def is_binary(self) -> bool:
        """True when the model is trained on logits for 0/1 labels."""
        return self.loss == "bce"

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ClassifierConfig":
        """Inverse of `to_dict`; unknown keys are ignored so old checkpoints keep loading."""
        known = {f.name for f in fields(cls)}
        return cls(**{k: v for k, v in data.items() if k in known})

    def replace(self, **changes: Any) -> "ClassifierConfig":
        return ClassifierConfig.from_dict({**self.to_dict(), **changes})

    def with_variant(self, name: str) -> "ClassifierConfig":
        """This config switched to one of `MODEL_VARIANTS` ("flat", "late_fusion", "residual")."""
        if name not in MODEL_VARIANTS:
            raise ValueError(f"unknown variant {name!r}; expected one of {tuple(MODEL_VARIANTS)}")
        return self.replace(**MODEL_VARIANTS[name])

