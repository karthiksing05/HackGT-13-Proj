from collections.abc import Sequence

from reranking.context import user_dislikes_description, user_to_context
from reranking.models import User

from ..schemas import UserEmbedding
from .base import Encoder, TextEmbedFn, embed_texts

# Any encoder of users. Swap in a separately trained user tower by
# implementing `Encoder[User, UserEmbedding]` directly.
UserEncoder = Encoder[User, UserEmbedding]


class TextUserEncoder(UserEncoder):
    """Embeds a user's likes and dislikes as separate texts with a (possibly shared) model.

    Users with no dislikes get `negative=None`.
    """

    def __init__(self, embed_fn: TextEmbedFn, model_version: str) -> None:
        self._embed_fn = embed_fn
        self.model_version = model_version

    def encode_many(self, users: Sequence[User]) -> list[UserEmbedding]:
        if not users:
            return []

        # One embed_fn call for the whole batch: all positive texts first, then
        # the negative texts of users that have any.
        texts = [user_to_context(user).description for user in users]
        source_ids = [user.id for user in users]
        negative_row: dict[int, int] = {}
        for i, user in enumerate(users):
            negative_text = user_dislikes_description(user)
            if negative_text is not None:
                negative_row[i] = len(texts)
                texts.append(negative_text)
                source_ids.append(user.id)

        embeddings = embed_texts(self._embed_fn, texts, self.model_version, source_ids)
        return [
            UserEmbedding(
                positive=embeddings[i],
                negative=embeddings[negative_row[i]] if i in negative_row else None,
            )
            for i in range(len(users))
        ]
