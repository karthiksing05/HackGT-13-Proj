"""Qwen3-Embedding in-process with sentence-transformers: the safety net that always works.

Same settings as the training and backfill jobs (`data/embed.py`): `max_seq_length` 512,
`normalize_embeddings=True`, no prompt name (Qwen3's ST config has `default_prompt_name: null`, so
`encode(texts)` adds nothing). fp32 on CPU; the weights load lazily under a lock on the first call
or at `warmup()`, which takes 20-60 s on the VPS, and every encode is serialized so two concurrent
requests do not double the memory.
"""

from __future__ import annotations

import logging
import os
import threading
import time
from collections.abc import Callable
from typing import Any

import numpy as np

from .base import CANARY_TEXT, DIM, MODEL, EmbedderError, EmbedKind, Embedder, ProviderStatus, embed_with_blanks, unit_rows

logger = logging.getLogger(__name__)

DEFAULT_MAX_SEQ_LENGTH = 512
DEFAULT_BATCH_SIZE = 16


def default_threads() -> int:
    return max(1, (os.cpu_count() or 2) // 2)


class LocalEmbedder(Embedder):
    name = "local"

    def __init__(
        self,
        *,
        model: str = MODEL,
        dim: int = DIM,
        threads: int | None = None,
        device: str = "cpu",
        max_seq_length: int = DEFAULT_MAX_SEQ_LENGTH,
        batch_size: int = DEFAULT_BATCH_SIZE,
        loader: Callable[[], Any] | None = None,
    ) -> None:
        self.model = model
        self.dim = dim
        self.threads = threads or default_threads()
        self.device = device
        self.max_seq_length = max_seq_length
        self.batch_size = max(1, batch_size)
        self._loader = loader or self._load_sentence_transformer
        self._model: Any = None
        self._load_lock = threading.Lock()
        self._encode_lock = threading.Lock()
        self._status = ProviderStatus(self.name, "not_loaded")

    def _load_sentence_transformer(self) -> Any:
        import torch
        from sentence_transformers import SentenceTransformer

        torch.set_num_threads(self.threads)
        model = SentenceTransformer(self.model, device=self.device)
        model.max_seq_length = self.max_seq_length
        return model

    def load(self) -> Any:
        with self._load_lock:
            if self._model is not None:
                return self._model
            self._status.status, self._status.detail = "loading", None
            started = time.monotonic()
            try:
                self._model = self._loader()
            except Exception as exc:
                self._status.mark_error(f"load failed: {type(exc).__name__}: {exc}"[:200])
                logger.exception("local embedder failed to load %s", self.model)
                raise EmbedderError("unavailable", f"failed to load {self.model}: {exc}", retryable=False, provider=self.name) from exc
            seconds = time.monotonic() - started
            self._status.status = "loaded"
            self._status.detail = f"device={self.device} threads={self.threads} load_s={seconds:.1f}"
            logger.info("local embedder loaded %s on %s in %.1fs", self.model, self.device, seconds)
            return self._model

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        return embed_with_blanks(self._encode, texts, self.dim, self.name)

    def _encode(self, texts: list[str]) -> np.ndarray:
        model = self.load()
        started = time.monotonic()
        with self._encode_lock:
            try:
                vectors = model.encode(
                    texts,
                    batch_size=self.batch_size,
                    normalize_embeddings=True,
                    convert_to_numpy=True,
                    show_progress_bar=False,
                )
            except Exception as exc:
                self._status.mark_error(f"encode failed: {type(exc).__name__}: {exc}"[:200])
                raise EmbedderError("unavailable", f"encode failed: {exc}", retryable=True, provider=self.name) from exc
        vectors = np.asarray(vectors, dtype=np.float64)
        if vectors.ndim != 2 or vectors.shape != (len(texts), self.dim):
            self._status.mark_error(f"unexpected shape {vectors.shape}")
            raise EmbedderError("bad_response", f"expected {len(texts)}x{self.dim}, got {vectors.shape}", retryable=False, provider=self.name)
        self._status.mark_ok((time.monotonic() - started) * 1000.0, self._status.detail)
        self._status.status = "loaded"
        return unit_rows(vectors)

    def warmup(self) -> None:
        self.load()
        self.embed([CANARY_TEXT])

    def status(self) -> dict[str, ProviderStatus]:
        st = self._status
        return {self.name: ProviderStatus(st.name, st.status, st.detail, st.last_ok_at, st.last_error_at, st.latency_ms)}
