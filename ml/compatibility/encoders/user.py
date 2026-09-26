from reranking.context import user_to_context
from reranking.models import User

from .base import Encoder, TextEmbeddingEncoder

# Any encoder of users. Swap in a separately trained user tower by
# implementing `Encoder[User]` directly.
UserEncoder = Encoder[User]


class TextUserEncoder(TextEmbeddingEncoder[User]):
    """Embeds a user's text profile with a (possibly shared) text embedding model."""

    def to_text(self, user: User) -> str:
        return user_to_context(user).description

    def source_id(self, user: User) -> str:
        return user.id
