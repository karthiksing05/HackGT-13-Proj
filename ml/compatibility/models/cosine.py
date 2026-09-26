import numpy as np

from ..schemas import Embedding, EmbeddingError, ScoringResult, UserEmbedding
from .base import CompatibilityModel

DEFAULT_NEGATIVE_WEIGHT = 0.5


class CosineCompatibilityModel(CompatibilityModel):
    """Score = cos(positive, event) - negative_weight * cos(negative, event).

    The penalty term is 0 for users without a negative embedding, so the score
    is plain cosine similarity in [-1, 1]; with one it lies in
    [-1 - negative_weight, 1 + negative_weight].

    For normalized embeddings each term is a single matrix-vector product.
    Embeddings not flagged `normalized` are divided by their norms at scoring time.
    """

    name = "cosine"

    def __init__(self, negative_weight: float = DEFAULT_NEGATIVE_WEIGHT, require_same_version: bool = True) -> None:
        super().__init__(require_same_version=require_same_version)
        if negative_weight < 0:
            raise ValueError(f"negative_weight must be >= 0, got {negative_weight}")
        self.negative_weight = negative_weight

    def _score_batch(self, user_embedding: UserEmbedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        events = np.stack([e.vector for e in event_embeddings])
        unnormalized = [i for i, e in enumerate(event_embeddings) if not e.normalized]
        if unnormalized:
            events = events.copy()
            for i in unnormalized:
                events[i] = _unit(events[i], event_embeddings[i])

        positive = _similarities(events, user_embedding.positive)
        negative = (
            _similarities(events, user_embedding.negative)
            if user_embedding.negative is not None
            else np.zeros_like(positive)
        )
        scores = positive - self.negative_weight * negative

        return [
            ScoringResult(
                score=float(score),
                metadata={
                    "model": self.name,
                    "model_version": user_embedding.model_version,
                    "negative_weight": self.negative_weight,
                    "component_scores": {"positive": float(pos), "negative": float(neg)},
                },
            )
            for score, pos, neg in zip(scores, positive, negative)
        ]


def _similarities(unit_events: np.ndarray, embedding: Embedding) -> np.ndarray:
    """Cosine similarity of `embedding` against each row of an already-normalized matrix."""
    return np.clip(unit_events @ _unit(embedding.vector, embedding), -1.0, 1.0)


def _unit(vector: np.ndarray, embedding: Embedding) -> np.ndarray:
    if embedding.normalized:
        return vector
    norm = np.linalg.norm(vector)
    if norm == 0:
        raise EmbeddingError(f"cosine similarity is undefined for a zero vector (source_id={embedding.source_id!r})")
    return vector / norm
