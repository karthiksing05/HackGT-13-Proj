"""Request and response schemas for `POST /v1/embed`.

Pydantic rejects an empty list, more than 64 texts, texts over 8000 characters and unknown kinds
(422). Blank texts are allowed and come back as zero vectors.
"""

from typing import Annotated, Literal

from pydantic import BaseModel, Field

Text = Annotated[str, Field(max_length=8000)]
EmbedKind = Literal["user", "search", "activity"]


class EmbedRequest(BaseModel):
    """`kind` is for logging and metrics only; it never changes the text (no instruction prefix)."""

    texts: Annotated[list[Text], Field(min_length=1, max_length=64)]
    kind: EmbedKind = "activity"


class EmbedResponse(BaseModel):
    """One unit-norm `dim`-d vector per text (all zeros for a blank text). `cached` counts the
    texts served from the cache; `provider` names who embedded the rest."""

    embeddings: list[list[float]]
    model: str
    dim: int
    provider: str
    cached: int
