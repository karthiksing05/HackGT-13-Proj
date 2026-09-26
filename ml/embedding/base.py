"""The embedder interface every provider implements, plus the small vector helpers they share.

Every embedder turns texts into `(N, dim)` float32 unit vectors of `MODEL`'s space with **no
instruction prefix**: the stored activity vectors and the compatibility classifier were produced
without one (`datagen/mongo_backfill.py`, `data/embed.py`), so adding one would move users and
searches into a different space. `kind` is for logging and metrics only and never changes the text.
"""

from __future__ import annotations

import time
from abc import ABC, abstractmethod
from collections.abc import Callable
from dataclasses import dataclass
from typing import Literal

import numpy as np

MODEL = "Qwen/Qwen3-Embedding-0.6B"
DIM = 1024

# One eight-section text every provider is asked to embed at warmup and on `/healthz?probe=1`.
CANARY_TEXT = "Interests:\n- live jazz\n\nCost:\n- free admission"

EmbedKind = Literal["user", "search", "activity"]
ErrorKind = Literal["auth", "rate_limit", "unavailable", "bad_request", "bad_response", "timeout", "not_configured"]
StatusName = Literal["ok", "error", "unknown", "not_loaded", "loading", "loaded", "disabled"]


class EmbedderError(RuntimeError):
    """A provider could not embed. `retryable` says whether trying again later could help."""

    def __init__(
        self, kind: ErrorKind, message: str, *, retryable: bool, provider: str, retry_after: float | None = None
    ) -> None:
        super().__init__(f"{provider}: {kind}: {message}")
        self.kind = kind
        self.message = message
        self.retryable = retryable
        self.provider = provider
        self.retry_after = retry_after  # seconds, from a 429's Retry-After header


@dataclass
class ProviderStatus:
    """What `/healthz` reports for one provider (or one route of a provider)."""

    name: str
    status: StatusName
    detail: str | None = None
    last_ok_at: float | None = None  # epoch seconds
    last_error_at: float | None = None
    latency_ms: float | None = None

    def to_dict(self) -> dict:
        return {
            "status": self.status,
            "detail": self.detail,
            "last_ok_at": self.last_ok_at,
            "last_error_at": self.last_error_at,
            "latency_ms": None if self.latency_ms is None else round(self.latency_ms, 1),
        }

    def mark_ok(self, latency_ms: float, detail: str | None = None) -> None:
        self.status = "ok"
        self.detail = detail
        self.last_ok_at = time.time()
        self.latency_ms = latency_ms

    def mark_error(self, detail: str) -> None:
        self.status = "error"
        self.detail = detail
        self.last_error_at = time.time()


@dataclass(frozen=True)
class EmbedResult:
    """`vectors` is `(N, dim)` float32; `provider` names who produced the uncached rows."""

    vectors: np.ndarray
    provider: str
    cached: int = 0


class Embedder(ABC):
    name: str = "embedder"
    model: str = MODEL
    dim: int = DIM

    @abstractmethod
    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        """(N, dim) float32; every row unit-norm; blank texts -> zero rows. Raises EmbedderError."""

    def embed_result(self, texts: list[str], kind: EmbedKind = "activity") -> EmbedResult:
        """`embed` plus which provider served it; wrappers override this to report the real one."""
        return EmbedResult(self.embed(texts, kind), self.name)

    def probe(self) -> EmbedResult:
        """Embed the canary without any cache in front, for `/healthz?probe=1`."""
        return self.embed_result([CANARY_TEXT], "activity")

    def status(self) -> dict[str, ProviderStatus]:
        return {}

    def warmup(self) -> None:
        """Do the slow part (load weights, first request) before the first real call."""
        self.embed([CANARY_TEXT], "activity")

    def ready(self) -> bool:
        """False while a provider is gated (e.g. Vertex before its startup probe passed)."""
        return True


def unit_rows(matrix) -> np.ndarray:
    """L2-normalize each row in float64 (as `mongo_backfill.unit`); zero rows stay zero; float32 out."""
    m = np.asarray(matrix, dtype=np.float64)
    if m.ndim == 1:
        m = m[None, :]
    norms = np.linalg.norm(m, axis=1, keepdims=True)
    return np.divide(m, norms, out=np.zeros_like(m), where=norms > 0).astype(np.float32)


def embed_with_blanks(
    fn: Callable[[list[str]], np.ndarray], texts: list[str], dim: int = DIM, provider: str = "embedder"
) -> np.ndarray:
    """Call `fn` on the non-blank texts only and return `(N, dim)` with zero rows for the blanks.

    A blank text has no signal (a user without dislikes), and the classifier was trained with the
    zero vector for that case; providers would otherwise embed the empty string to some arbitrary
    direction (or reject it).
    """
    out = np.zeros((len(texts), dim), dtype=np.float32)
    keep = [i for i, t in enumerate(texts) if t and t.strip()]
    if keep:
        vectors = np.asarray(fn([texts[i] for i in keep]), dtype=np.float32)
        if vectors.shape != (len(keep), dim):
            raise EmbedderError(
                "bad_response", f"expected shape {(len(keep), dim)}, got {vectors.shape}", retryable=False, provider=provider
            )
        out[keep] = vectors
    return out
