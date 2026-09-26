from collections.abc import Sequence

from reranking.context import event_to_context
from reranking.models import Event

from ..schemas import Embedding
from .base import Encoder, TextEmbedFn, embed_texts

# Any encoder of events. Swap in a separately trained event tower by
# implementing `Encoder[Event, Embedding]` directly.
EventEncoder = Encoder[Event, Embedding]


class TextEventEncoder(EventEncoder):
    """Embeds an event's text description with a (possibly shared) text embedding model."""

    def __init__(self, embed_fn: TextEmbedFn, model_version: str) -> None:
        self._embed_fn = embed_fn
        self.model_version = model_version

    def encode_many(self, events: Sequence[Event]) -> list[Embedding]:
        texts = [event_to_context(event).description for event in events]
        return embed_texts(self._embed_fn, texts, self.model_version, [event.id for event in events])
