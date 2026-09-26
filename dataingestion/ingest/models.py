"""Pydantic v2 models for the `activities` schema (§4.1) and `runs` (§4.5).

Field names are camelCase on purpose: they are the Mongo field names the
planner, ML and frontend code read.
"""

import math
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, Field

Kind = Literal["event", "place"]
Attendance = Literal["fixed_start", "drop_in"]


class GeoPoint(BaseModel):
    type: Literal["Point"] = "Point"
    coordinates: tuple[float, float]  # [lng, lat]: lng FIRST

    @classmethod
    def at(cls, lat: float, lng: float) -> "GeoPoint":
        return cls(coordinates=(lng, lat))


class Address(BaseModel):
    formatted: str | None = None
    street: str | None = None
    locality: str | None = None
    region: str | None = None
    postalCode: str | None = None
    countryCode: str | None = None


class HoursInterval(BaseModel):
    open: int   # minutes since Sunday 00:00 local, 0..10079
    close: int  # may be < open when the interval wraps past Saturday 23:59


class Recurrence(BaseModel):
    seriesKey: str
    frequency: Literal["daily", "weekly", "biweekly", "monthly", "irregular"]
    rule: str | None = None
    daysOfWeek: list[int] = []
    localStartTime: str | None = None
    until: datetime | None = None
    text: str | None = None
    origin: Literal["source_rule", "source_series", "llm", "detected"]


class Duration(BaseModel):
    medianMin: float
    sigma: float
    p75Min: float
    source: Literal["event_times", "source_field", "google_typical", "trail_model", "llm", "category_prior"]

    @classmethod
    def lognormal(cls, median_min: float, sigma: float, source: str) -> "Duration":
        p75 = median_min * math.exp(0.674 * sigma)
        return cls(medianMin=round(median_min, 1), sigma=sigma, p75Min=round(p75, 1), source=source)


class Price(BaseModel):
    min: float | None = None
    max: float | None = None
    currency: str
    tier: int | None = None
    isFree: bool | None = None


class Trail(BaseModel):
    lengthKm: float
    ascentM: float | None = None  # None when elevation data wasn't available
    descentM: float | None = None
    loop: bool
    geometry: dict


class SourceRef(BaseModel):
    name: str
    id: str
    url: str | None = None
    fetchedAt: datetime


class Activity(BaseModel):
    """What an adapter produces. Enrichment fields (summary, tags, embedding,
    blurb) are filled by later pipeline stages and never overwritten here."""

    kind: Kind
    city: str
    name: str
    summary: str | None = None
    description: str | None = None
    category: str = "other"
    sourceCategory: str | None = None  # raw source classification, input to the LLM classifier (§6.3)
    tags: list[str] = []
    location: GeoPoint | None = None  # adapters may leave it empty; the pipeline geocodes the address (§6.1)
    address: Address | None = None
    venueName: str | None = None

    start: datetime | None = None
    end: datetime | None = None
    attendance: Attendance | None = None
    timezone: str
    weeklyHours: list[HoursInterval] | None = None
    hoursSource: Literal["google", "osm", "nps", "default"] | None = None  # default = assumed, not from a source
    recurrence: Recurrence | None = None
    duration: Duration | None = None
    price: Price | None = None

    rating: float | None = None
    ratingCount: int | None = None
    popularity: float | None = None
    trail: Trail | None = None

    url: str | None = None
    ticketUrl: str | None = None
    imageUrl: str | None = None

    sourceKeys: list[str] = Field(min_length=1)
    sources: list[SourceRef] = Field(min_length=1)
    googlePlaceId: str | None = None
    expiresAt: datetime | None = None


class RunStats(BaseModel):
    source: str
    city: str
    startedAt: datetime
    finishedAt: datetime | None = None
    dryRun: bool = False
    fetched: int = 0
    skipped: int = 0
    inserted: int = 0
    updated: int = 0
    known: int = 0           # only_new runs: already in the DB, left untouched
    linked: int = 0          # only_new runs: another source's listing of a known event
    newIds: list[str] = []   # only_new runs: _ids inserted, for the research/writing stages
    merged: list[dict] = []
    skipReasons: dict[str, int] = {}
    geocoded: int = 0
    errors: list[str] = []
