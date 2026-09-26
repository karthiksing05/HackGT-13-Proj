"""Encoder interface: turns domain objects into embeddings. Nothing else.

Encoders do not score, rank, or compare embeddings.
"""

from abc import ABC, abstractmethod
from collections.abc import Callable, Sequence
from typing import Generic, TypeVar

import numpy as np

from ..schemas import Embedding, EmbeddingError

T = TypeVar("T")
E = TypeVar("E")

# Any text embedding backend: takes N strings, returns an (N, dim) array.
TextEmbedFn = Callable[[list[str]], np.ndarray]


class Encoder(ABC, Generic[T, E]):
    """Converts items of type `T` into embeddings of type `E`."""

    @abstractmethod
    def encode_many(self, items: Sequence[T]) -> list[E]:
        """Encode a batch of items, preserving order."""

    def encode(self, item: T) -> E:
        return self.encode_many([item])[0]


def embed_texts(
    embed_fn: TextEmbedFn,
    texts: list[str],
    model_version: str,
    source_ids: Sequence[str | None],
) -> list[Embedding]:
    """Embed `texts` in one `embed_fn` call and wrap each row as a normalized `Embedding`.

    Vectors are L2-normalized so cosine similarity reduces to a dot product.
    """
    if not texts:
        return []
    vectors = np.asarray(embed_fn(texts))
    if vectors.ndim != 2 or vectors.shape[0] != len(texts):
        raise EmbeddingError(f"embed_fn returned shape {vectors.shape} for {len(texts)} inputs; expected (N, dim)")
    return [
        Embedding.from_unnormalized(vector, model_version, source_id=source_id)
        for vector, source_id in zip(vectors, source_ids)
    ]
