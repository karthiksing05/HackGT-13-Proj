"""Trail geometry, elevation and the hiking-time model (§5.10).

Coordinates are (lng, lat) tuples, the same order as GeoJSON and GeoPoint.
Duration is Tobler's hiking function over the elevation profile: v = 6·exp(−3.5·|s + 0.05|) km/h.
"""

import logging
import math
from statistics import median

import httpx
from pymongo.database import Database

from ..geo import haversine_km
from ..http import Http
from ..models import Duration
from ..quota import QuotaExceeded, reserve

log = logging.getLogger(__name__)

OPENTOPODATA_URL = "https://api.opentopodata.org/v1"
MAX_LOCATIONS = 100  # per OpenTopoData request
CACHE_TTL = 90 * 24 * 3600  # terrain doesn't change
TRAIL_SIGMA = 0.25


def length_km(coords: list[tuple[float, float]]) -> float:
    return sum(haversine_km(y1, x1, y2, x2) for (x1, y1), (x2, y2) in zip(coords, coords[1:]))


def resample(coords: list[tuple[float, float]], step_m: float, max_points: int) -> list[tuple[float, float]]:
    """Evenly spaced points along the line, both ends included. The spacing grows past
    step_m for long trails so no trail needs more than max_points elevation lookups."""
    total_m = length_km(coords) * 1000
    n = max(1, min(max_points - 1, math.ceil(total_m / step_m)))
    step = total_m / n
    out = [coords[0]]
    walked, target = 0.0, step
    for (x1, y1), (x2, y2) in zip(coords, coords[1:]):
        seg = haversine_km(y1, x1, y2, x2) * 1000
        while seg > 0 and len(out) < n and walked + seg >= target:
            f = (target - walked) / seg
            out.append((x1 + (x2 - x1) * f, y1 + (y2 - y1) * f))
            target += step
        walked += seg
    out.append(coords[-1])
    return out


def smooth(elevations: list[float]) -> list[float]:
    """Moving median, window 3: DEM noise would otherwise add phantom climb."""
    if len(elevations) < 3:
        return list(elevations)
    mid = [median(elevations[i - 1:i + 2]) for i in range(1, len(elevations) - 1)]
    return [elevations[0], *mid, elevations[-1]]


def climb(elevations: list[float]) -> tuple[float, float]:
    """(ascent, descent) in metres along the profile."""
    diffs = [b - a for a, b in zip(elevations, elevations[1:])]
    return sum(d for d in diffs if d > 0), -sum(d for d in diffs if d < 0)


def tobler_minutes(points: list[tuple[float, float]], elevations: list[float] | None = None) -> float:
    """One-way walking time. Without elevations every segment is treated as flat (5 km/h)."""
    hours = 0.0
    for i, ((x1, y1), (x2, y2)) in enumerate(zip(points, points[1:])):
        dx = haversine_km(y1, x1, y2, x2)
        if dx == 0:
            continue
        slope = (elevations[i + 1] - elevations[i]) / 1000 / dx if elevations else 0.0
        hours += dx / (6 * math.exp(-3.5 * abs(slope + 0.05)))
    return hours * 60


def hike_minutes(points: list[tuple[float, float]], elevations: list[float] | None, loop: bool) -> float:
    """A loop is walked once; anything else is out-and-back, with the slopes reversed on the way back."""
    out = tobler_minutes(points, elevations)
    if loop:
        return out
    return out + tobler_minutes(points[::-1], elevations[::-1] if elevations else None)


def trail_duration(minutes: float) -> Duration:
    return Duration.lognormal(minutes, TRAIL_SIGMA, "trail_model")


class Elevation:
    """OpenTopoData lookups, cached on disk and counted against the `opentopodata` quota.
    Without Mongo (a dry run with no DB) it answers from cache only, like the geocoder."""

    def __init__(self, dataset: str | None, db: Database | None):
        self.dataset = dataset
        self.db = db
        self.http = Http("opentopodata")
        self.disabled_reason: str | None = None

    def profile(self, points: list[tuple[float, float]]) -> list[float] | None:
        """Elevation in metres for each point, or None if any point has no data."""
        if not self.dataset or self.disabled_reason:
            return None
        url = f"{OPENTOPODATA_URL}/{self.dataset}"
        out: list[float] = []
        for i in range(0, len(points), MAX_LOCATIONS):
            chunk = points[i:i + MAX_LOCATIONS]
            params = {"locations": "|".join(f"{lat:.5f},{lng:.5f}" for lng, lat in chunk)}
            if self.db is None and not self.http.is_cached("GET", url, params, cache_ttl=CACHE_TTL):
                return None
            try:
                data = self.http.get_json(
                    url, params, cache_ttl=CACHE_TTL, before_network=lambda: reserve(self.db, "opentopodata")
                )
            except (QuotaExceeded, httpx.HTTPError) as e:
                # Stop calling for the rest of the run; remaining trails get a flat-ground estimate.
                self.disabled_reason = str(e)
                log.warning("elevation lookups off for this run: %s", e)
                return None
            elevations = [r.get("elevation") for r in data.get("results") or []]
            if data.get("status") != "OK" or len(elevations) != len(chunk) or None in elevations:
                return None  # outside the dataset (e.g. ned10m is US-only)
            out.extend(elevations)
        return out
