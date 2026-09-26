"""Request and response schemas for `POST /v1/compatibility/user-embedding/update`.

Pydantic rejects missing fields, non-numeric or non-finite values, empty
vectors and unknown kinds (422). The cross-field dimension check lives in the
updater (400), matching the split used by `/v1/events/rank`.
"""

from typing import Annotated, Literal

from pydantic import BaseModel, Field

# Strict: JSON numbers only, so "0.1" strings and booleans are rejected.
FiniteFloat = Annotated[float, Field(allow_inf_nan=False, strict=True)]
Vector = Annotated[list[FiniteFloat], Field(min_length=1)]
EmbeddingKind = Literal["positive", "negative"]


class UpdateUserEmbeddingRequest(BaseModel):
    embedding: Vector
    kind: EmbeddingKind
    event_embedding: Vector


class UpdateUserEmbeddingResponse(BaseModel):
    embedding: list[float]
    kind: EmbeddingKind
