"""Deterministic hard filters applied before compatibility scoring.

Each filter decides whether one event is admissible for one user. A filter
whose inputs are missing (on the user or the event) keeps the event: absent
data never removes a candidate.
"""

import math
from abc import ABC, abstractmethod
from collections.abc import Callable
from datetime import datetime, timezone

from .schemas import EventInput, UserInput

Clock = Callable[[], datetime]

EARTH_RADIUS_MILES = 3958.8


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


class HardFilter(ABC):
    name: str

    @abstractmethod
    def keep(self, user: UserInput, event: EventInput) -> bool:
        """True if `event` may be shown to `user`."""


class MaxPriceFilter(HardFilter):
    name = "max_price"

    def keep(self, user: UserInput, event: EventInput) -> bool:
        if user.max_price is None or event.price is None:
            return True
        return event.price <= user.max_price


class MaxDistanceFilter(HardFilter):
    name = "max_distance"

    def keep(self, user: UserInput, event: EventInput) -> bool:
        coordinates = (user.latitude, user.longitude, event.latitude, event.longitude)
        if user.max_distance_miles is None or any(c is None for c in coordinates):
            return True
        return haversine_miles(*coordinates) <= user.max_distance_miles


class UpcomingFilter(HardFilter):
    """Drops events that have already finished (by `end_time`, else `start_time`)."""

    name = "upcoming"

    def __init__(self, clock: Clock = utc_now) -> None:
        self.clock = clock

    def keep(self, user: UserInput, event: EventInput) -> bool:
        finish = event.end_time or event.start_time
        return finish is None or finish >= self.clock()


class AvailabilityFilter(HardFilter):
    """Keeps events that fall inside the user's [available_start, available_end] window."""

    name = "availability"

    def keep(self, user: UserInput, event: EventInput) -> bool:
        if user.available_start is not None and event.start_time is not None:
            if event.start_time < user.available_start:
                return False
        finish = event.end_time or event.start_time
        if user.available_end is not None and finish is not None:
            if finish > user.available_end:
                return False
        return True


class ExcludedCategoryFilter(HardFilter):
    name = "excluded_category"

    def keep(self, user: UserInput, event: EventInput) -> bool:
        if not user.excluded_categories or event.category is None:
            return True
        excluded = {c.strip().casefold() for c in user.excluded_categories}
        return event.category.strip().casefold() not in excluded


def default_filters(clock: Clock = utc_now) -> list[HardFilter]:
    return [
        MaxPriceFilter(),
        MaxDistanceFilter(),
        UpcomingFilter(clock),
        AvailabilityFilter(),
        ExcludedCategoryFilter(),
    ]


def haversine_miles(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    phi1, phi2 = math.radians(lat1), math.radians(lat2)
    d_phi = phi2 - phi1
    d_lambda = math.radians(lon2 - lon1)
    a = math.sin(d_phi / 2) ** 2 + math.cos(phi1) * math.cos(phi2) * math.sin(d_lambda / 2) ** 2
    return 2 * EARTH_RADIUS_MILES * math.asin(math.sqrt(min(1.0, a)))
