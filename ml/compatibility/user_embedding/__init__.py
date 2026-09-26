"""Stateless update of one user preference embedding (positive or negative) from one event.

Served as `POST /v1/compatibility/user-embedding/update` by the app in
`ranking.api`. The caller persists the result; nothing is fetched or stored.
`routes` is not imported here so non-HTTP users of the updater avoid FastAPI.
"""

from .schemas import EmbeddingKind, UpdateUserEmbeddingRequest, UpdateUserEmbeddingResponse
from .updater import DEFAULT_ALPHA, EmbeddingUpdateError, MovingAverageUpdater, UserEmbeddingUpdater, l2_normalize

__all__ = [
    "DEFAULT_ALPHA",
    "EmbeddingKind",
    "EmbeddingUpdateError",
    "MovingAverageUpdater",
    "UpdateUserEmbeddingRequest",
    "UpdateUserEmbeddingResponse",
    "UserEmbeddingUpdater",
    "l2_normalize",
]
