"""`FallbackEmbedder`: try providers in order behind a circuit breaker; `CachingEmbedder`: the cache in front.

A provider that fails `open_after` times in a row is skipped for `reset_after` seconds
(`auth_reset_after` after an auth error, which a retry cannot fix), then gets one trial call. Every
fallback is logged at WARNING with the error kind; when the whole chain fails the error is logged
at ERROR and raised, and the API turns it into a 503.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Callable
from dataclasses import dataclass

import numpy as np

from .base import CANARY_TEXT, EmbedderError, EmbedKind, Embedder, EmbedResult, ProviderStatus
from .cache import EmbeddingCache, NullCache
from .stats import EmbedStats

logger = logging.getLogger(__name__)


@dataclass
class _Breaker:
    failures: int = 0
    opened_at: float | None = None
    open_for: float = 0.0


class FallbackEmbedder(Embedder):
    name = "fallback"

    def __init__(
        self,
        chain: list[Embedder],
        *,
        open_after: int = 3,
        reset_after: float = 60.0,
        auth_reset_after: float = 600.0,
        stats: EmbedStats | None = None,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if not chain:
            raise ValueError("FallbackEmbedder needs at least one provider")
        if open_after < 1:
            raise ValueError(f"open_after must be >= 1, got {open_after}")
        self.chain = list(chain)
        self.model = chain[0].model
        self.dim = chain[0].dim
        self.open_after = open_after
        self.reset_after = reset_after
        self.auth_reset_after = auth_reset_after
        self.stats = stats
        self._clock = clock
        self._breakers = {id(p): _Breaker() for p in self.chain}

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        return self.embed_result(texts, kind).vectors

    def embed_result(self, texts: list[str], kind: EmbedKind = "activity") -> EmbedResult:
        errors: list[str] = []
        for provider in self.chain:
            skip = self._skip_reason(provider)
            if skip:
                errors.append(f"{provider.name}: {skip}")
                continue
            try:
                result = provider.embed_result(texts, kind)
            except EmbedderError as exc:
                self._record_failure(provider, exc)
                errors.append(str(exc))
                if self.stats:
                    self.stats.fallback()
                logger.warning("embedding provider %s failed (kind=%s), trying the next one: %s", provider.name, exc.kind, exc.message)
                continue
            self._breakers[id(provider)] = _Breaker()
            return result
        summary = "; ".join(errors) or "no providers"
        if self.stats:
            self.stats.error()
        logger.error("every embedding provider failed: %s", summary)
        raise EmbedderError("unavailable", summary, retryable=True, provider=self.name)

    def probe(self) -> EmbedResult:
        return self.embed_result([CANARY_TEXT], "activity")

    def warmup(self) -> None:
        for provider in self.chain:
            try:
                provider.warmup()
            except Exception as exc:  # warmup only pre-pays costs; failures show up on /healthz
                logger.warning("warmup of embedding provider %s failed: %s", provider.name, exc)

    def status(self) -> dict[str, ProviderStatus]:
        out: dict[str, ProviderStatus] = {}
        for provider in self.chain:
            statuses = provider.status() or {provider.name: ProviderStatus(provider.name, "unknown")}
            remaining = self._open_remaining(provider)
            for key, st in statuses.items():
                detail = st.detail
                status = st.status
                if remaining is not None:
                    detail = f"circuit open for {remaining:.0f}s" + (f"; {detail}" if detail else "")
                    status = "error"
                out[key] = ProviderStatus(st.name, status, detail, st.last_ok_at, st.last_error_at, st.latency_ms)
        return out

    def ready(self) -> bool:
        return any(p.ready() for p in self.chain)

    # -- breaker ----------------------------------------------------------------------------------

    def _skip_reason(self, provider: Embedder) -> str | None:
        if not provider.ready():
            return "not ready"
        breaker = self._breakers[id(provider)]
        if breaker.opened_at is None:
            return None
        if self._clock() - breaker.opened_at < breaker.open_for:
            return "circuit open"
        # Half-open: one trial call; another failure reopens the circuit at once.
        breaker.opened_at = None
        breaker.failures = self.open_after - 1
        return None

    def _open_remaining(self, provider: Embedder) -> float | None:
        breaker = self._breakers[id(provider)]
        if breaker.opened_at is None:
            return None
        remaining = breaker.open_for - (self._clock() - breaker.opened_at)
        return remaining if remaining > 0 else None

    def _record_failure(self, provider: Embedder, error: EmbedderError) -> None:
        breaker = self._breakers[id(provider)]
        breaker.failures += 1
        if error.kind == "auth":
            breaker.opened_at, breaker.open_for = self._clock(), self.auth_reset_after
        elif breaker.failures >= self.open_after:
            breaker.opened_at, breaker.open_for = self._clock(), self.reset_after


class CachingEmbedder(Embedder):
    """Blank texts become zero rows, cached texts are read back, the misses go to `inner` in one call."""

    name = "cache"

    def __init__(self, inner: Embedder, cache: EmbeddingCache | None = None, *, stats: EmbedStats | None = None) -> None:
        self.inner = inner
        self.cache = cache or NullCache()
        self.model = inner.model
        self.dim = inner.dim
        self.stats = stats

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        return self.embed_result(texts, kind).vectors

    def embed_result(self, texts: list[str], kind: EmbedKind = "activity", *, bypass_cache: bool = False) -> EmbedResult:
        out = np.zeros((len(texts), self.dim), dtype=np.float32)
        lookup = [i for i, t in enumerate(texts) if t and t.strip()]
        if not lookup:
            return EmbedResult(out, "none", 0)
        lookup_texts = [texts[i] for i in lookup]
        cached = self.cache.get_many(self.model, lookup_texts) if not bypass_cache else [None] * len(lookup)
        misses = [i for i, row in zip(lookup, cached) if row is None]
        hits = 0
        for i, row in zip(lookup, cached):
            if row is not None:
                out[i] = row
                hits += 1
        provider = "cache"
        if misses:
            miss_texts = [texts[i] for i in misses]
            result = self.inner.embed_result(miss_texts, kind)
            if result.vectors.shape != (len(misses), self.dim):
                raise EmbedderError(
                    "bad_response", f"{result.provider} returned shape {result.vectors.shape}, expected {(len(misses), self.dim)}", retryable=False, provider=result.provider
                )
            out[misses] = result.vectors
            provider = result.provider
            if not bypass_cache:
                self.cache.put_many(self.model, miss_texts, result.vectors)
        return EmbedResult(out, provider, hits)

    def probe(self) -> EmbedResult:
        return self.embed_result([CANARY_TEXT], "activity", bypass_cache=True)

    def warmup(self) -> None:
        self.inner.warmup()

    def status(self) -> dict[str, ProviderStatus]:
        return self.inner.status()

    def ready(self) -> bool:
        return self.inner.ready()
