"""Request and response schemas for `POST /v1/events/rank`.

Pydantic rejects wrong types, missing fields, non-finite numbers, empty
embeddings and naive datetimes (422). Checks that depend on the loaded model
or on several fields at once (dimensions, duplicate ids, `min_score` range)
live in `EventRankingService.validate` (400).
"""

from typing import Annotated

from pydantic import AwareDatetime, BaseModel, Field

FiniteFloat = Annotated[float, Field(allow_inf_nan=False)]
Vector = Annotated[list[FiniteFloat], Field(min_length=1)]
Latitude = Annotated[float, Field(ge=-90, le=90)]
Longitude = Annotated[float, Field(ge=-180, le=180)]
NonNegative = Annotated[float, Field(ge=0, allow_inf_nan=False)]


class UserInput(BaseModel):
    """A user's preference embeddings plus optional hard-filter constraints.

    A missing optional field disables the corresponding filter. An all-zero
    `negative_embedding` means the user has no negative signal.

    `positive_text` / `negative_text` are the eight-section likes and dislikes
    the embeddings were made from. The Jev rerank reads them; without
    `positive_text` there is no rerank.
    """

    positive_embedding: Vector
    negative_embedding: Vector
    positive_text: str | None = None
    negative_text: str | None = None

    max_price: NonNegative | None = None
    latitude: Latitude | None = None
    longitude: Longitude | None = None
    max_distance_miles: NonNegative | None = None
    available_start: AwareDatetime | None = None
    available_end: AwareDatetime | None = None
    excluded_categories: list[str] = Field(default_factory=list)


class EventInput(BaseModel):
    """A candidate event. `id` is opaque to this service and never used for lookups.

    `description` is the event's eight-section embedding text, read by the Jev
    rerank. Events without one can't be judged and go after judged ones.
    """

    id: Annotated[str, Field(min_length=1)]
    embedding: Vector
    description: str | None = None

    price: NonNegative | None = None
    start_time: AwareDatetime | None = None
    end_time: AwareDatetime | None = None
    latitude: Latitude | None = None
    longitude: Longitude | None = None
    category: str | None = None


class RankingOptions(BaseModel):
    """`rerank_top_k` is how many of the best model-ranked events Jev reorders
    (default: the service's setting); `rerank: false` skips Jev. `limit` is
    applied after the rerank.
    """

    min_score: FiniteFloat | None = None
    limit: Annotated[int, Field(gt=0)] | None = None
    rerank: bool = True
    rerank_top_k: Annotated[int, Field(gt=0)] | None = None


class RankEventsRequest(BaseModel):
    """`search_embedding` embeds what the user asked for in this search (the
    eight-section preference paragraph). It is blended into the user's positive
    embedding for this request only and never stored; absent or all-zero means
    no search. `search_text` is that same paragraph, for the Jev rerank.
    """

    user: UserInput
    events: list[EventInput]
    search_embedding: Vector | None = None
    search_text: str | None = None
    options: RankingOptions = Field(default_factory=RankingOptions)


class RankedEvent(BaseModel):
    """`score` is the compatibility model's; `rerank_score` is Jev's 0-4 score,
    null for events Jev didn't judge.
    """

    event_id: str
    score: float
    rerank_score: float | None = None


class RankEventsResponse(BaseModel):
    """`reranked` is true only when Jev actually reordered the top events."""

    events: list[RankedEvent]
    model_version: str
    reranked: bool = False
