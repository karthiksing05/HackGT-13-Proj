"""Request and response schemas for `POST /v1/compatibility/users` and
`POST /v1/compatibility/itineraries`.

Pydantic rejects wrong types, missing fields, non-finite numbers and empty
embeddings (422). Dimension checks and duplicate ids live in `MatchService` (400).
"""

from typing import Annotated

from pydantic import BaseModel, Field

from .ranking import Vector

Id = Annotated[str, Field(min_length=1)]


class MatchUser(BaseModel):
    """A user's stored likes and dislikes embeddings. An all-zero
    `negative_embedding` means the user has no dislikes."""

    positive_embedding: Vector
    negative_embedding: Vector


class UserCandidate(MatchUser):
    """Another user to compare with. `id` is opaque to this service."""

    id: Id


class UserMatchRequest(BaseModel):
    user: MatchUser
    candidates: list[UserCandidate]


class UserMatch(BaseModel):
    """`score` is the raw likes-minus-clashes similarity; `percent` is it
    clamped to 0-1 as a whole-number percent."""

    id: str
    score: float
    percent: int


class UserMatchResponse(BaseModel):
    """Best match first."""

    results: list[UserMatch]


class MatchEvent(BaseModel):
    id: Id
    embedding: Vector


class ItineraryInput(BaseModel):
    """An itinerary's stops that have embeddings. Stops may repeat across
    itineraries; ids only need to be unique within one itinerary."""

    id: Id
    events: list[MatchEvent]


class ItineraryMatchRequest(BaseModel):
    user: MatchUser
    itineraries: list[ItineraryInput]


class ItineraryMatch(BaseModel):
    """`score` is the mean compatibility-model score over `scored_events`
    stops; `percent` is it clamped to 0-1 as a whole-number percent."""

    id: str
    score: float
    percent: int
    scored_events: int


class ItineraryMatchResponse(BaseModel):
    """Best match first. Itineraries without events are absent."""

    results: list[ItineraryMatch]
    model_version: str
