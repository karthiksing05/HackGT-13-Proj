"""Resident Advisor (§5.5): club nights and electronic music, via the GraphQL endpoint ra.co itself uses.

Verified against a live response (fixtures/resident_advisor/event_listings_page.json):
- event.startTime/endTime are local wall-clock times with no offset; area.ianaTimeZone names the zone.
- `page` is 1-indexed.
- A multi-day event is listed once per listingDate, so listings repeat event ids.
- Venue coordinates are rounded by RA (usually 2 decimals, sometimes whole degrees);
  secret/TBA venues come back as 0,0. So venues with an address are geocoded (§6.1),
  and RA's own coords are only a fallback.
- `cost` is free text: "10", "$0", "$12 - $23", "$47.70".
"""

import logging
import re
from datetime import date, datetime, timedelta, timezone
from typing import Iterable
from zoneinfo import ZoneInfo

from ..config import ROOT, MissingConfig
from ..enrich.classify import ra_category
from ..enrich.geocode import fill_address, make_geocoder
from ..enrich.price import price_from_range
from ..geo import haversine_km
from ..http import Http
from ..models import Activity, Address, GeoPoint, SourceRef
from .base import Adapter

log = logging.getLogger(__name__)

GRAPHQL_URL = "https://ra.co/graphql"
QUERY_PATH = ROOT / "seeds" / "ra_event_listings.graphql"
PAGE_SIZE = 20
MAX_PAGES = 50
CACHE_TTL = 3600
BROWSER_UA = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/128.0 Safari/537.36"
)
LATE_NIGHT_HOUR = 21
MAX_GEOCODE_DISAGREEMENT_KM = 2.0  # 2-decimal rounding is off by <1 km; beyond this the geocode matched elsewhere

_TIME_OF_DAY = re.compile(r"\d{1,2}(?::\d{2})?\s*(?:am|pm)\b", re.I)
_AMOUNT = re.compile(r"\d+(?:\.\d+)?")


def _local(ts: str | None, tz: ZoneInfo) -> datetime | None:
    """RA LocalDateTime ("2026-09-25T22:00:00.000") -> aware UTC datetime."""
    if not ts:
        return None
    return datetime.fromisoformat(ts).replace(tzinfo=tz).astimezone(timezone.utc)


def _clean(value: str | None) -> str | None:
    value = (value or "").strip()
    return value or None


def parse_cost(cost: str | None) -> tuple[float | None, float | None]:
    """Free-text cost -> (min, max). Times of day ("10 before 11pm") are not prices."""
    text = _TIME_OF_DAY.sub(" ", (cost or "").replace(",", ""))
    amounts = [float(a) for a in _AMOUNT.findall(text)]
    if amounts:
        return min(amounts), max(amounts)
    if "free" in text.lower():
        return 0.0, 0.0
    return None, None


def precise_enough(lat: float, lng: float) -> bool:
    """False for RA's 0,0 placeholder and for coords rounded to <2 decimals (>~5 km off)."""
    return not (round(lat, 1) == lat and round(lng, 1) == lng)


class ResidentAdvisorAdapter(Adapter):
    name = "resident_advisor"

    def __init__(self, *args, horizon_days: int | None = None, **kw):
        super().__init__(*args, **kw)
        ra = self.city.resident_advisor
        if not ra.get("area_id"):
            raise MissingConfig(f"resident_advisor.area_id is not set in cities/{self.city.slug}.yaml (§5.5).")
        if not QUERY_PATH.exists():
            raise MissingConfig(f"{QUERY_PATH.name} is missing; copy the query from the browser (§5.5).")
        self.area_id = int(ra["area_id"])
        self.referer = ra.get("referer") or "https://ra.co/events"
        self.query = QUERY_PATH.read_text()
        self.http = Http(self.name)
        self.horizon_days = horizon_days or self.cfg["events_horizon_days"]
        self.tz = ZoneInfo(self.city.timezone)
        self.attending_range: tuple[int, int] | None = None  # for min-max popularity (§5.5)
        self.geocoder = make_geocoder(self.city, self.db)

    # ---- fetch ----

    def listing_dates(self, now: datetime | None = None) -> tuple[date, date]:
        """Local dates [today, today + horizon - 1], matching Ticketmaster's day windows."""
        today = (now or datetime.now(timezone.utc)).astimezone(self.tz).date()
        return today, today + timedelta(days=self.horizon_days - 1)

    def fetch(self) -> Iterable[dict]:
        now = datetime.now(timezone.utc)
        first, last = self.listing_dates(now)
        events: dict[str, dict] = {}
        for page in range(1, MAX_PAGES + 1):
            listings = self._get_page(first, last, page)
            for listing in listings:
                event = listing.get("event")
                if event and event.get("id") and event["id"] not in events:
                    events[event["id"]] = event
            if len(listings) < PAGE_SIZE:
                break
        else:
            self.errors.append(f"stopped after {MAX_PAGES} pages; results may be incomplete")

        # Popularity is min-max scaled within the city, so it needs the whole result set first.
        counts = [e["attending"] for e in events.values() if e.get("attending") is not None]
        if counts:
            self.attending_range = (min(counts), max(counts))

        for event in events.values():
            tz = self._event_tz(event)
            end = _local(event.get("endTime"), tz) or _local(event.get("startTime"), tz)
            if end and end <= now:
                continue  # already over
            yield event

    def _get_page(self, first: date, last: date, page: int) -> list[dict]:
        body = {
            "operationName": "GET_EVENT_LISTINGS",
            "query": self.query,
            "variables": {
                "filters": {
                    "areas": {"eq": self.area_id},
                    "listingDate": {"gte": first.isoformat(), "lte": last.isoformat()},
                },
                "pageSize": PAGE_SIZE,
                "page": page,
                "sort": {"listingDate": {"order": "ASCENDING"}},
            },
        }
        headers = {"User-Agent": BROWSER_UA, "Referer": self.referer}
        data = self.http.post_json(GRAPHQL_URL, body, headers=headers, cache_ttl=CACHE_TTL)
        if data.get("errors"):
            raise RuntimeError(f"RA GraphQL error: {data['errors'][0].get('message')}")
        return ((data.get("data") or {}).get("eventListings") or {}).get("data") or []

    def _event_tz(self, event: dict) -> ZoneInfo:
        name = (event.get("area") or {}).get("ianaTimeZone")
        return ZoneInfo(name) if name else self.tz

    # ---- normalize ----

    def normalize(self, raw: dict) -> Iterable[Activity]:
        tz = self._event_tz(raw)
        start = _local(raw.get("startTime"), tz)
        if start is None or not _clean(raw.get("title")):
            return self.skip("no start time or title")
        end = _local(raw.get("endTime"), tz)
        if end is not None and end <= start:
            end = None

        venue = raw.get("venue") or {}
        address = Address(formatted=_clean(venue.get("address"))) if _clean(venue.get("address")) else None
        point, address = self._locate(venue, address)
        if point is None:
            return self.skip("secret venue or no usable address")  # retried next run (§6.1)

        url = f"https://ra.co{raw['contentUrl']}" if raw.get("contentUrl") else f"https://ra.co/events/{raw['id']}"
        genres = [g["name"] for g in raw.get("genres") or [] if g.get("name")]
        lo, hi = parse_cost(raw.get("cost"))

        return [
            Activity(
                kind="event",
                city=self.city.slug,
                name=raw["title"].strip(),
                description=self._description(raw),
                category=ra_category([g.get("slug") for g in raw.get("genres") or []], raw.get("isFestival")),
                sourceCategory=" / ".join(genres) or None,
                tags=self._tags(raw, start.astimezone(tz)),
                location=point,
                address=address,
                venueName=_clean(venue.get("name")),
                start=start,
                end=end,
                attendance="drop_in",  # duration.resolve() switches short events to fixed_start
                timezone=str(tz),
                price=price_from_range(lo, hi, self.city.currency, self.city.price_tiers),
                popularity=self._popularity(raw.get("attending")),
                url=url,
                ticketUrl=url if raw.get("isTicketed") else None,
                imageUrl=self._image(raw),
                sourceKeys=[f"ra:{raw['id']}"],
                sources=[SourceRef(name=self.name, id=raw["id"], url=url, fetchedAt=datetime.now(timezone.utc))],
            )
        ]

    def _locate(self, venue: dict, address: Address | None) -> tuple[GeoPoint | None, Address | None]:
        """Geocoded venue address first; RA's rounded coords only if they're usable."""
        loc = venue.get("location") or {}
        try:
            lat, lng = float(loc["latitude"]), float(loc["longitude"])
        except (KeyError, TypeError, ValueError):
            lat = lng = 0.0
        bbox = self.city.bbox
        ra_ok = precise_enough(lat, lng) and bbox.south <= lat <= bbox.north and bbox.west <= lng <= bbox.east

        geo = self.geocoder.geocode(address.formatted) if self.geocoder and address else None
        if geo and ra_ok and haversine_km(lat, lng, geo.lat, geo.lng) > MAX_GEOCODE_DISAGREEMENT_KM:
            log.info("geocode of %r is %.1f km from RA's venue coords; keeping RA's",
                     address.formatted, haversine_km(lat, lng, geo.lat, geo.lng))
            geo = None
        if geo:
            return geo.point, fill_address(address, geo.address)
        if ra_ok:
            return GeoPoint.at(lat, lng), address
        return None, address

    @staticmethod
    def _description(raw: dict) -> str | None:
        # Lineup first so truncation never cuts it off.
        names = [a["name"].strip() for a in raw.get("artists") or [] if _clean(a.get("name"))]
        parts = [f"Lineup: {', '.join(names)}." if names else None, _clean(raw.get("content"))]
        text = "\n\n".join(p for p in parts if p)
        return text[:2000] or None

    @staticmethod
    def _tags(raw: dict, local_start: datetime) -> list[str]:
        tags = ["music"]
        if local_start.hour >= LATE_NIGHT_HOUR or local_start.hour < 5:
            tags.append("late_night")
        if (raw.get("minimumAge") or 0) >= 21:
            tags.append("21_plus")
        return tags

    def _popularity(self, attending: int | None) -> float | None:
        if attending is None or self.attending_range is None:
            return None
        lo, hi = self.attending_range
        return round((attending - lo) / (hi - lo), 3) if hi > lo else None

    @staticmethod
    def _image(raw: dict) -> str | None:
        images = raw.get("images") or []
        front = next((i for i in images if i.get("type") == "FLYERFRONT" and i.get("filename")), None)
        return (front or {}).get("filename") or raw.get("flyerFront") or next(
            (i["filename"] for i in images if i.get("filename")), None
        )
