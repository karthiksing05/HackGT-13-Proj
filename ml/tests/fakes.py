"""Small embedders for the API tests: deterministic, dimension 8, no network, no torch."""

from __future__ import annotations

import hashlib

import numpy as np

from embedding import Embedder, EmbedderError, ProviderStatus

DIM = 8


def fake_vector(text: str, dim: int = DIM) -> np.ndarray:
    """A deterministic non-unit vector for a text (all entries positive, so rows never collide with zero)."""
    seed = int(hashlib.sha1(text.encode()).hexdigest()[:8], 16)
    return (np.random.RandomState(seed).rand(dim) * 3 + 0.1).astype(np.float32)


class FakeEmbedder(Embedder):
    """Records every call; `fail` raises an EmbedderError; `bad_shape` returns the wrong width."""

    name = "fake"
    model = "fake-model"
    dim = DIM

    def __init__(self, *, fail: bool = False, bad_shape: bool = False, status: str = "unknown") -> None:
        self.calls: list[tuple[list[str], str]] = []
        self.fail = fail
        self.bad_shape = bad_shape
        self._status = ProviderStatus(self.name, status)

    def embed(self, texts, kind="activity"):
        self.calls.append((list(texts), kind))
        if self.fail:
            raise EmbedderError("unavailable", "fake outage", retryable=True, provider=self.name)
        width = self.dim - 1 if self.bad_shape else self.dim
        out = np.zeros((len(texts), width), dtype=np.float32)
        for i, text in enumerate(texts):
            if text and text.strip():
                vector = fake_vector(text, width)
                out[i] = vector / np.linalg.norm(vector)
        return out

    def status(self):
        return {self.name: self._status}
