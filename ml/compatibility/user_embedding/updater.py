"""Strategies that fold one event embedding into a user preference embedding.

The route depends only on `UserEmbeddingUpdater`, so the moving average below
can be swapped for recency weighting, historical aggregation or a learned
update without changing the API contract.
"""

from abc import ABC, abstractmethod
from collections.abc import Sequence

import numpy as np

DEFAULT_ALPHA = 0.8


class EmbeddingUpdateError(ValueError):
    """The inputs are well-formed but cannot be combined (maps to HTTP 400)."""


class UserEmbeddingUpdater(ABC):
    @abstractmethod
    def update(self, current_embedding: Sequence[float], event_embedding: Sequence[float]) -> list[float]:
        """Return the updated embedding. Raises `EmbeddingUpdateError` for incompatible inputs."""


class MovingAverageUpdater(UserEmbeddingUpdater):
    """u_new = normalize(alpha * u_current + (1 - alpha) * e).

    `alpha` is how strongly existing preferences are preserved. An all-zero
    current embedding (no signal yet) becomes the event direction; an all-zero
    combination (e.g. both inputs zero) is returned as the zero vector.
    """

    def __init__(self, alpha: float = DEFAULT_ALPHA) -> None:
        if not 0.0 <= alpha <= 1.0:
            raise ValueError(f"alpha must lie in [0, 1], got {alpha}")
        self.alpha = alpha

    def update(self, current_embedding: Sequence[float], event_embedding: Sequence[float]) -> list[float]:
        if len(current_embedding) != len(event_embedding):
            raise EmbeddingUpdateError(
                f"event_embedding dimension {len(event_embedding)} != embedding dimension {len(current_embedding)}."
            )
        current = np.asarray(current_embedding, dtype=np.float64)
        event = np.asarray(event_embedding, dtype=np.float64)
        combined = self.alpha * current + (1.0 - self.alpha) * event
        if not np.all(np.isfinite(combined)):
            raise EmbeddingUpdateError("Embedding values are too large to combine.")
        return l2_normalize(combined).tolist()


def l2_normalize(vector: np.ndarray) -> np.ndarray:
    """`vector / ||vector||`, or the zero vector unchanged instead of NaNs."""
    # Dividing by the largest magnitude first keeps the norm from overflowing.
    scale = np.max(np.abs(vector), initial=0.0)
    if scale == 0:
        return np.zeros_like(vector)
    scaled = vector / scale
    return scaled / np.linalg.norm(scaled)
