"""The `Embedding` value type passed between encoders and compatibility models."""

from dataclasses import dataclass, field
from datetime import datetime, timezone

import numpy as np

# Tolerance used when checking that a vector flagged `normalized` has unit norm.
NORM_TOLERANCE = 1e-5


class EmbeddingError(ValueError):
    """Raised for a malformed embedding (wrong shape, NaNs, bad normalization)."""


class IncompatibleEmbeddingsError(ValueError):
    """Raised when two embeddings cannot be compared (dimension or version mismatch)."""


@dataclass(frozen=True)
class Embedding:
    """A vector plus the metadata needed to tell whether it can be compared to another.

    `model_version` identifies the encoder that produced the vector. Embeddings
    from different versions live in different vector spaces, so stored
    embeddings must keep it to detect staleness after an encoder change.

    The vector is copied to a read-only float array on construction.
    """

    vector: np.ndarray
    model_version: str
    normalized: bool = False
    source_id: str | None = None
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))

    def __post_init__(self) -> None:
        if not self.model_version:
            raise EmbeddingError("model_version must be a non-empty string")

        vector = np.array(self.vector, dtype=np.float64)
        if vector.ndim != 1 or vector.size == 0:
            raise EmbeddingError(f"vector must be a non-empty 1-D array, got shape {vector.shape}")
        if not np.all(np.isfinite(vector)):
            raise EmbeddingError("vector contains NaN or infinite values")
        if self.normalized and abs(np.linalg.norm(vector) - 1.0) > NORM_TOLERANCE:
            raise EmbeddingError("vector is flagged normalized but does not have unit norm")

        vector.setflags(write=False)
        object.__setattr__(self, "vector", vector)

    @property
    def dimension(self) -> int:
        return int(self.vector.shape[0])

    @classmethod
    def from_unnormalized(cls, vector: np.ndarray, model_version: str, **kwargs) -> "Embedding":
        """Build a unit-norm embedding from a raw vector (e.g. an embedding API response)."""
        vector = np.asarray(vector, dtype=np.float64)
        norm = np.linalg.norm(vector)
        if norm == 0:
            raise EmbeddingError("cannot normalize a zero vector")
        return cls(vector=vector / norm, model_version=model_version, normalized=True, **kwargs)
