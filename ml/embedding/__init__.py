"""Text -> Qwen3-Embedding vectors, from whichever provider works: Vertex AI, the HF router, or local.

Pure (no FastAPI). `build_embedder(EmbeddingSettings.from_env())` gives the chain the API serves
with; every provider returns `(N, 1024)` float32 unit vectors with **no instruction prefix** and zero
rows for blank texts, which is exactly how the stored activity vectors and the classifier's training
embeddings were made.
"""

from .base import (
    CANARY_TEXT,
    DIM,
    MODEL,
    Embedder,
    EmbedderError,
    EmbedKind,
    EmbedResult,
    ProviderStatus,
    embed_with_blanks,
    unit_rows,
)
from .cache import EmbeddingCache, NullCache, SqliteEmbeddingCache
from .config import PROVIDERS, EmbeddingSettings, build_cache, build_embedder
from .fallback import CachingEmbedder, FallbackEmbedder
from .hf_router import HFRouterEmbedder, RouteMapping
from .local import LocalEmbedder
from .stats import EmbedStats
from .vertex import VertexEmbedder

__all__ = [
    "CANARY_TEXT",
    "DIM",
    "MODEL",
    "PROVIDERS",
    "CachingEmbedder",
    "Embedder",
    "EmbedderError",
    "EmbedKind",
    "EmbedResult",
    "EmbedStats",
    "EmbeddingCache",
    "EmbeddingSettings",
    "FallbackEmbedder",
    "HFRouterEmbedder",
    "LocalEmbedder",
    "NullCache",
    "ProviderStatus",
    "RouteMapping",
    "SqliteEmbeddingCache",
    "VertexEmbedder",
    "build_cache",
    "build_embedder",
    "embed_with_blanks",
    "unit_rows",
]
