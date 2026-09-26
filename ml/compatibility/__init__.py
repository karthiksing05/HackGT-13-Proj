"""Compatibility scoring: user/event -> embeddings -> compatibility score.

The encoders and the compatibility model are injected into
`CompatibilityService`, so the scoring approach (cosine, learned head,
separate towers, ...) can change without touching callers.
"""

from .encoders import Encoder, EventEncoder, TextEventEncoder, TextUserEncoder, UserEncoder
from .models import CompatibilityModel, CosineCompatibilityModel
from .schemas import Embedding, EmbeddingError, IncompatibleEmbeddingsError, ScoringResult, UserEmbedding
from .service import CompatibilityService

__all__ = [
    "CompatibilityModel",
    "CompatibilityService",
    "CosineCompatibilityModel",
    "Embedding",
    "EmbeddingError",
    "Encoder",
    "EventEncoder",
    "IncompatibleEmbeddingsError",
    "ScoringResult",
    "TextEventEncoder",
    "TextUserEncoder",
    "UserEmbedding",
    "UserEncoder",
]
