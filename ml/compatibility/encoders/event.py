from reranking.context import event_to_context
from reranking.models import Event

from .base import Encoder, TextEmbeddingEncoder

# Any encoder of events. Swap in a separately trained event tower by
# implementing `Encoder[Event]` directly.
EventEncoder = Encoder[Event]


class TextEventEncoder(TextEmbeddingEncoder[Event]):
    """Embeds an event's text description with a (possibly shared) text embedding model."""

    def to_text(self, event: Event) -> str:
        return event_to_context(event).description

    def source_id(self, event: Event) -> str:
        return event.id
