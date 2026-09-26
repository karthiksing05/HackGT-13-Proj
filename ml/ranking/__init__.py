"""Stateless event ranking: one user + candidate events -> ranked event ids and scores.

Served as `POST /v1/events/rank` (see `api.py`; run `uvicorn ranking.main:app`).
Distinct from `reranking`, the Jev LLM reranking stage.
"""

from .api import create_app
from .filters import (
    AvailabilityFilter,
    ExcludedCategoryFilter,
    HardFilter,
    MaxDistanceFilter,
    MaxPriceFilter,
    UpcomingFilter,
    default_filters,
)
from .schemas import EventInput, RankedEvent, RankEventsRequest, RankEventsResponse, RankingOptions, UserInput
from .service import EventRankingService, InferenceError, RankingRequestError

__all__ = [
    "AvailabilityFilter",
    "EventInput",
    "EventRankingService",
    "ExcludedCategoryFilter",
    "HardFilter",
    "InferenceError",
    "MaxDistanceFilter",
    "MaxPriceFilter",
    "RankEventsRequest",
    "RankEventsResponse",
    "RankedEvent",
    "RankingOptions",
    "RankingRequestError",
    "UpcomingFilter",
    "UserInput",
    "create_app",
    "default_filters",
]
