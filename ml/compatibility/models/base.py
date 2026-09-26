"""Compatibility model interface: scores user embeddings against event embeddings.

Implementations only implement `_score_batch`; validation and the single-pair
case are handled here so every model enforces the same compatibility rules.
"""

from abc import ABC, abstractmethod

from ..schemas import Embedding, IncompatibleEmbeddingsError, ScoringResult, UserEmbedding


class CompatibilityModel(ABC):
    """Scores how well events match a user, given their embeddings.

    The user side is a `UserEmbedding` (positive signal plus optional negative
    signal); models are free to ignore the negative part.

    Class attributes describe the model to callers such as the ranking API:
    `version` identifies the scoring implementation (e.g. "cosine-v1"; not to
    be confused with an embedding's `model_version`, which names the encoder),
    `embedding_dim` is the dimension the model requires (None accepts any), and
    `score_range` bounds the scores it returns (None when unbounded).

    Args:
        require_same_version: If True (the default), user and event embeddings
            must share a `model_version`. Set False for models that are
            intentionally fed embeddings from different encoders (e.g. separate
            user/event towers), and override `validate` to check what matters.
    """

    version: str = "unversioned"
    embedding_dim: int | None = None
    score_range: tuple[float, float] | None = None

    def __init__(self, require_same_version: bool = True) -> None:
        self.require_same_version = require_same_version

    def score(self, user_embedding: UserEmbedding, event_embedding: Embedding) -> ScoringResult:
        return self.score_many(user_embedding, [event_embedding])[0]

    def score_many(self, user_embedding: UserEmbedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        """Score one user against many events. Results are in input order."""
        if not event_embeddings:
            return []
        for event_embedding in event_embeddings:
            self.validate(user_embedding, event_embedding)
        return self._score_batch(user_embedding, event_embeddings)

    def validate(self, user_embedding: UserEmbedding, event_embedding: Embedding) -> None:
        """Raise `IncompatibleEmbeddingsError` if the pair cannot be scored.

        Only the positive embedding is checked; `UserEmbedding` already
        guarantees the negative one matches it.
        """
        if self.embedding_dim is not None and user_embedding.dimension != self.embedding_dim:
            raise IncompatibleEmbeddingsError(
                f"model expects dimension {self.embedding_dim}, user embedding has {user_embedding.dimension}"
            )
        if user_embedding.dimension != event_embedding.dimension:
            raise IncompatibleEmbeddingsError(
                f"dimension mismatch: user={user_embedding.dimension}, "
                f"event={event_embedding.dimension} (source_id={event_embedding.source_id!r})"
            )
        if self.require_same_version and user_embedding.model_version != event_embedding.model_version:
            raise IncompatibleEmbeddingsError(
                f"model_version mismatch: user={user_embedding.model_version!r}, "
                f"event={event_embedding.model_version!r} (source_id={event_embedding.source_id!r})"
            )

    @abstractmethod
    def _score_batch(self, user_embedding: UserEmbedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        """Score an already-validated, non-empty batch."""
