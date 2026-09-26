"""Ticketmaster Discovery API v2 (§5.1): ticketed events, including TicketWeb inventory."""

import logging
from datetime import datetime, timedelta, timezone
from typing import Iterable
from zoneinfo import ZoneInfo

from ..config import require_env
from ..enrich.classify import ticketmaster_category
from ..enrich.price import price_from_range
from ..geo import geohash
from ..http import Http
from ..models import Activity, Address, GeoPoint, SourceRef
from .base import Adapter

log = logging.getLogger(__name__)

EVENTS_URL = "https://app.ticketmaster.com/discovery/v2/events.json"
PAGE_SIZE = 200
DEEP_PAGING_LIMIT = 1000  # size * page may not exceed this
MIN_WINDOW = timedelta(hours=1)
SKIP_STATUSES = {"cancelled", "postponed"}
CACHE_TTL = 3600  # re-use responses for an hour while developing


def _iso(dt: datetime) -> str:
    return dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _parse(ts: str | None) -> datetime | None:
    return datetime.fromisoformat(ts.replace("Z", "+00:00")) if ts else None


def _clean(value: str | None) -> str | None:
    value = (value or "").strip()
    return value or None


class TicketmasterAdapter(Adapter):
    name = "ticketmaster"

    def __init__(self, *args, horizon_days: int | None = None, **kw):
        super().__init__(*args, **kw)
        self.api_key = require_env("TICKETMASTER_API_KEY", "Get one at developer.ticketmaster.com.")
        self.http = Http(self.name)
        self.horizon_days = horizon_days or self.cfg["events_horizon_days"]
        self.radius_km = self.city.ticketmaster.get("radius_km", 40)
        self._seen: set[str] = set()

    # ---- fetch ----

    def day_windows(self, now: datetime | None = None) -> list[tuple[datetime, datetime]]:
        """Day windows in UTC split at local midnight, covering [now, today + horizon).
        Day-sized so each window stays under the 1,000-item deep-paging cap."""
        tz = ZoneInfo(self.city.timezone)
        now = (now or datetime.now(timezone.utc)).astimezone(tz).replace(microsecond=0)
        today = now.replace(hour=0, minute=0, second=0)
        bounds = [now] + [today + timedelta(days=i) for i in range(1, self.horizon_days + 1)]
        return [(a.astimezone(timezone.utc), b.astimezone(timezone.utc)) for a, b in zip(bounds, bounds[1:])]

    def fetch(self) -> Iterable[dict]:
        for start, end in self.day_windows():
            yield from self._fetch_window(start, end)

    def _fetch_window(self, start: datetime, end: datetime) -> Iterable[dict]:
        page = 0
        while True:
            body = self._get_page(start, end, page)
            info = body.get("page", {})
            total = info.get("totalElements", 0)
            if page == 0 and total > DEEP_PAGING_LIMIT and end - start > MIN_WINDOW:
                mid = start + (end - start) / 2
                yield from self._fetch_window(start, mid)
                yield from self._fetch_window(mid, end)
                return
            for event in body.get("_embedded", {}).get("events", []):
                # A window query also returns long-running items (season passes) that
                # merely overlap it; keep only events that start inside the window.
                ev_start = _parse(event.get("dates", {}).get("start", {}).get("dateTime"))
                if event["id"] in self._seen or (ev_start and not start <= ev_start < end):
                    continue
                self._seen.add(event["id"])
                yield event
            page += 1
            if page >= info.get("totalPages", 0) or (page + 1) * PAGE_SIZE > DEEP_PAGING_LIMIT:
                if total > DEEP_PAGING_LIMIT:
                    self.errors.append(f"{_iso(start)}..{_iso(end)}: {total} events, only first {DEEP_PAGING_LIMIT} fetched")
                return

    def _get_page(self, start: datetime, end: datetime, page: int) -> dict:
        params = {
            "apikey": self.api_key,
            "geoPoint": geohash(self.city.center.lat, self.city.center.lng),
            "radius": self.radius_km,
            "unit": "km",
            "startDateTime": _iso(start),
            "endDateTime": _iso(end),
            "size": PAGE_SIZE,
            "page": page,
            "sort": "date,asc",
        }
        return self.http.get_json(EVENTS_URL, params, cache_ttl=CACHE_TTL)

    # ---- normalize ----

    def normalize(self, raw: dict) -> Iterable[Activity]:
        dates = raw.get("dates", {})
        start_info = dates.get("start", {})
        status = dates.get("status", {}).get("code")
        start = _parse(start_info.get("dateTime"))
        if (
            raw.get("test")
            or status in SKIP_STATUSES
            or start is None
            or start_info.get("timeTBA")
            or start_info.get("dateTBA")
            or start_info.get("noSpecificTime")
        ):
            return self.skip("cancelled, test or no start time")

        venue = (raw.get("_embedded", {}).get("venues") or [{}])[0]
        loc = venue.get("location") or {}
        try:
            point = GeoPoint.at(float(loc["latitude"]), float(loc["longitude"]))
        except (KeyError, TypeError, ValueError):
            point = None  # the pipeline geocodes the venue address (§6.1)

        cls = next((c for c in raw.get("classifications", []) if c.get("primary")), None) or (
            raw.get("classifications") or [{}]
        )[0]
        segment = (cls.get("segment") or {}).get("name")
        genre = (cls.get("genre") or {}).get("name")
        sub_genre = (cls.get("subGenre") or {}).get("name")
        source_category = " / ".join(x for x in (segment, genre, sub_genre) if x and x != "Undefined") or None

        ranges = raw.get("priceRanges") or []
        mins = [r["min"] for r in ranges if r.get("min") is not None]
        maxs = [r["max"] for r in ranges if r.get("max") is not None]
        currency = next((r["currency"] for r in ranges if r.get("currency")), self.city.currency)

        description = _clean(raw.get("description") or raw.get("info") or raw.get("pleaseNote"))
        url = raw.get("url")

        return [
            Activity(
                kind="event",
                city=self.city.slug,
                name=raw["name"].strip(),
                description=description[:2000] if description else None,
                category=ticketmaster_category(segment, genre),
                sourceCategory=source_category,
                location=point,
                address=Address(
                    street=_clean((venue.get("address") or {}).get("line1")),
                    locality=_clean((venue.get("city") or {}).get("name")),
                    region=_clean((venue.get("state") or {}).get("stateCode")),
                    postalCode=_clean(venue.get("postalCode")),
                    countryCode=_clean((venue.get("country") or {}).get("countryCode")),
                ),
                venueName=_clean(venue.get("name")),
                start=start,
                end=_parse(dates.get("end", {}).get("dateTime")),
                attendance="fixed_start",
                timezone=dates.get("timezone") or venue.get("timezone") or self.city.timezone,
                price=price_from_range(
                    min(mins) if mins else None, max(maxs) if maxs else None, currency, self.city.price_tiers
                ),
                url=url,
                ticketUrl=url,
                imageUrl=self._best_image(raw.get("images", [])),
                sourceKeys=[f"ticketmaster:{raw['id']}"],
                sources=[SourceRef(name=self.name, id=raw["id"], url=url, fetchedAt=datetime.now(timezone.utc))],
            )
        ]

    @staticmethod
    def _best_image(images: list[dict]) -> str | None:
        wide = [i for i in images if i.get("ratio") == "16_9"] or images
        return max(wide, key=lambda i: i.get("width") or 0)["url"] if wide else None
