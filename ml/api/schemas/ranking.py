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
    """

    positive_embedding: Vector
    negative_embedding: Vector

    max_price: NonNegative | None = None
    latitude: Latitude | None = None
    longitude: Longitude | None = None
    max_distance_miles: NonNegative | None = None
    available_start: AwareDatetime | None = None
    available_end: AwareDatetime | None = None
    excluded_categories: list[str] = Field(default_factory=list)


class EventInput(BaseModel):
    """A candidate event. `id` is opaque to this service and never used for lookups."""

    id: Annotated[str, Field(min_length=1)]
    embedding: Vector

    price: NonNegative | None = None
    start_time: AwareDatetime | None = None
    end_time: AwareDatetime | None = None
    latitude: Latitude | None = None
    longitude: Longitude | None = None
    category: str | None = None


class RankingOptions(BaseModel):
    min_score: FiniteFloat | None = None
    limit: Annotated[int, Field(gt=0)] | None = None


class RankEventsRequest(BaseModel):
    user: UserInput
    events: list[EventInput]
    options: RankingOptions = Field(default_factory=RankingOptions)


class RankedEvent(BaseModel):
    event_id: str
    score: float


class RankEventsResponse(BaseModel):
    events: list[RankedEvent]
    model_version: str
