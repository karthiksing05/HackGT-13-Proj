"""Stateless update of one user preference embedding (positive or negative) from one event.

Served as `POST /v1/compatibility/user-embedding/update` by `api`. The caller
persists the result; nothing is fetched or stored.
"""

from .updater import DEFAULT_ALPHA, EmbeddingUpdateError, MovingAverageUpdater, UserEmbeddingUpdater, l2_normalize

__all__ = [
    "DEFAULT_ALPHA",
    "EmbeddingUpdateError",
    "MovingAverageUpdater",
    "UserEmbeddingUpdater",
    "l2_normalize",
]
