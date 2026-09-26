"""`EmbeddingService`: the one place the API turns texts into vectors.

Wraps whatever `Embedder` was built from the settings: blank texts are zero rows, the shape is
checked, vectors are re-normalized in float64 (exactly unit norm, like the stored activity
vectors), one INFO line is logged per call and the counters on `/healthz` are updated. Provider
failures become `EmbeddingUnavailableError` (503); the details stay in the log.
"""

from __future__ import annotations

import logging
import time
from dataclasses import dataclass

import numpy as np

from embedding import EmbedderError, EmbedKind, Embedder, EmbedStats

from ..errors import EmbeddingUnavailableError

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class EmbedOutcome:
    vectors: np.ndarray  # (N, dim) float64, unit rows (zero rows for blank texts)
    provider: str
    cached: int

    def rows(self) -> list[list[float]]:
        return self.vectors.tolist()


class EmbeddingService:
    def __init__(self, embedder: Embedder, stats: EmbedStats | None = None) -> None:
        self.embedder = embedder
        self.stats = stats or EmbedStats()
        self.model = embedder.model
        self.dim = embedder.dim

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> EmbedOutcome:
        return self._run(texts, kind, probe=False)

    def probe(self) -> EmbedOutcome:
        """Embed the canary through the providers, bypassing the cache (`/healthz?probe=1`)."""
        return self._run([], "activity", probe=True)

    def _run(self, texts: list[str], kind: EmbedKind, *, probe: bool) -> EmbedOutcome:
        started = time.perf_counter()
        try:
            result = self.embedder.probe() if probe else self.embedder.embed_result(texts, kind)
        except EmbedderError as exc:
            self.stats.error()
            logger.error("embed kind=%s n=%d failed: %s", kind, len(texts), exc)
            raise EmbeddingUnavailableError() from exc
        n = 1 if probe else len(texts)
        vectors = np.asarray(result.vectors, dtype=np.float64)
        if vectors.shape != (n, self.dim):
            self.stats.error()
            logger.error("embed kind=%s n=%d: provider %s returned shape %s", kind, n, result.provider, vectors.shape)
            raise EmbeddingUnavailableError()
        if not probe:
            blanks = [i for i, t in enumerate(texts) if not t or not t.strip()]
            vectors[blanks] = 0.0
        norms = np.linalg.norm(vectors, axis=1, keepdims=True)
        vectors = np.divide(vectors, norms, out=np.zeros_like(vectors), where=norms > 0)
        ms = (time.perf_counter() - started) * 1000.0
        logger.info("embed kind=%s n=%d cached=%d provider=%s ms=%.0f", kind, n, result.cached, result.provider, ms)
        self.stats.record(kind=kind, n=n, cached=result.cached, provider=result.provider, ms=ms)
        return EmbedOutcome(vectors, result.provider, result.cached)
