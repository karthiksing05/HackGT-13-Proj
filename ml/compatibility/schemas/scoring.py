"""The `ScoringResult` returned by compatibility models."""

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class ScoringResult:
    """A compatibility score for one user/event pair.

    `score` is the only field downstream code should rely on. `metadata` is a
    free-form bag for model-specific details (e.g. `model_version`,
    `raw_similarity`, `confidence`, `component_scores`); its keys vary by model.
    """

    score: float
    metadata: dict[str, Any] = field(default_factory=dict)
