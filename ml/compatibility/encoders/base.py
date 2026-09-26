"""Encoder interface: turns domain objects into `Embedding`s. Nothing else.

Encoders do not score, rank, or compare embeddings.
"""

from abc import ABC, abstractmethod
from collections.abc import Callable, Sequence
from typing import Generic, TypeVar

import numpy as np

from ..schemas import Embedding, EmbeddingError

T = TypeVar("T")

# Any text embedding backend: takes N strings, returns an (N, dim) array.
TextEmbedFn = Callable[[list[str]], np.ndarray]


class Encoder(ABC, Generic[T]):
    """Converts items of type `T` into embeddings."""

    @abstractmethod
    def encode_many(self, items: Sequence[T]) -> list[Embedding]:
        """Encode a batch of items, preserving order."""

    def encode(self, item: T) -> Embedding:
        return self.encode_many([item])[0]


class TextEmbeddingEncoder(Encoder[T]):
    """Encoder that renders items to text and embeds the text with `embed_fn`.

    Subclasses define how an item becomes text and what its source id is.
    Vectors are L2-normalized so cosine similarity reduces to a dot product.
    """

    def __init__(self, embed_fn: TextEmbedFn, model_version: str) -> None:
        self._embed_fn = embed_fn
        self.model_version = model_version

    @abstractmethod
    def to_text(self, item: T) -> str: ...

    @abstractmethod
    def source_id(self, item: T) -> str | None: ...

    def encode_many(self, items: Sequence[T]) -> list[Embedding]:
        if not items:
            return []
        vectors = np.asarray(self._embed_fn([self.to_text(item) for item in items]))
        if vectors.ndim != 2 or vectors.shape[0] != len(items):
            raise EmbeddingError(
                f"embed_fn returned shape {vectors.shape} for {len(items)} inputs; expected (N, dim)"
            )
        return [
            Embedding.from_unnormalized(vector, self.model_version, source_id=self.source_id(item))
            for item, vector in zip(items, vectors)
        ]
