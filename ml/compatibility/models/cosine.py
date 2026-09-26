import numpy as np

from ..schemas import Embedding, EmbeddingError, ScoringResult
from .base import CompatibilityModel


class CosineCompatibilityModel(CompatibilityModel):
    """Score = cosine similarity between user and event vectors, in [-1, 1].

    For normalized embeddings this is a single matrix-vector product. Embeddings
    not flagged `normalized` are divided by their norms at scoring time.
    """

    name = "cosine"

    def _score_batch(self, user_embedding: Embedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        user = _unit(user_embedding.vector, user_embedding)
        events = np.stack([e.vector for e in event_embeddings])

        unnormalized = [i for i, e in enumerate(event_embeddings) if not e.normalized]
        if unnormalized:
            events = events.copy()
            for i in unnormalized:
                events[i] = _unit(events[i], event_embeddings[i])

        similarities = np.clip(events @ user, -1.0, 1.0)
        return [
            ScoringResult(score=float(s), metadata={"model": self.name, "model_version": user_embedding.model_version})
            for s in similarities
        ]


def _unit(vector: np.ndarray, embedding: Embedding) -> np.ndarray:
    if embedding.normalized:
        return vector
    norm = np.linalg.norm(vector)
    if norm == 0:
        raise EmbeddingError(f"cosine similarity is undefined for a zero vector (source_id={embedding.source_id!r})")
    return vector / norm
