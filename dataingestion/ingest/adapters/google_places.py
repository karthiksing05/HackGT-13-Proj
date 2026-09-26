"""Google Places API (New) (§5.2): places with hours, price and rating.

Nearby Search over a grid of the city bbox (recursing into saturated cells for
culture/outdoors/fun), then Text Search for "{term} in {area}" queries. Every
real request is reserved against the quota counter first; responses are cached
on disk for a week so development reruns cost nothing.
"""

import logging
from datetime import datetime, timezone
from typing import Iterable

import httpx

from ..config import MissingConfig
from ..enrich.classify import google_category
from ..geo import Cell, grid
from ..http import Http
from ..models import Activity, Address, GeoPoint, HoursInterval, Price, SourceRef
from ..keys import KeyRing
from ..quota import QuotaExceeded
from .base import Adapter

log = logging.getLogger(__name__)

NEARBY_URL = "https://places.googleapis.com/v1/places:searchNearby"
TEXT_URL = "https://places.googleapis.com/v1/places:searchText"
MAX_RESULTS = 20
CACHE_TTL = 7 * 24 * 3600

# Enterprise SKU. Don't add fields: Atmosphere fields bill at a pricier SKU.
PLACE_FIELDS = (
    "places.id,places.displayName,places.primaryType,places.types,places.location,"
    "places.formattedAddress,places.addressComponents,places.regularOpeningHours,"
    "places.priceLevel,places.priceRange,places.rating,places.userRatingCount,"
    "places.websiteUri,places.googleMapsUri,places.businessStatus"
)
NEARBY_MASK = PLACE_FIELDS
TEXT_MASK = PLACE_FIELDS + ",nextPageToken"

PRICE_LEVELS = {
    "PRICE_LEVEL_FREE": 0,
    "PRICE_LEVEL_INEXPENSIVE": 1,
    "PRICE_LEVEL_MODERATE": 2,
    "PRICE_LEVEL_EXPENSIVE": 3,
    "PRICE_LEVEL_VERY_EXPENSIVE": 4,
}
MINUTES_PER_WEEK = 7 * 24 * 60


class GooglePlacesError(RuntimeError):
    pass


def weekly_hours(opening_hours: dict | None) -> list[HoursInterval] | None:
    """regularOpeningHours.periods -> minutes since Sunday 00:00 local.
    A period with an open but no close means open 24/7."""
    periods = (opening_hours or {}).get("periods") or []
    if not periods:
        return None
    out = []
    for p in periods:
        o, c = p.get("open"), p.get("close")
        if o is None:
            continue
        if c is None:
            return [HoursInterval(open=0, close=MINUTES_PER_WEEK)]
        out.append(
            HoursInterval(
                open=o["day"] * 1440 + o.get("hour", 0) * 60 + o.get("minute", 0),
                close=c["day"] * 1440 + c.get("hour", 0) * 60 + c.get("minute", 0),
            )
        )
    return out or None


def address_from(place: dict) -> Address:
    parts: dict[str, dict] = {}
    for comp in place.get("addressComponents") or []:
        for t in comp.get("types", []):
            parts.setdefault(t, comp)

    def long(t):
        return (parts.get(t) or {}).get("longText")

    def short(t):
        return (parts.get(t) or {}).get("shortText")

    street = " ".join(x for x in (long("street_number"), long("route")) if x) or None
    return Address(
        formatted=place.get("formattedAddress"),
        street=street,
        locality=long("locality") or long("postal_town"),
        region=short("administrative_area_level_1"),
        postalCode=long("postal_code"),
        countryCode=short("country"),
    )


def _units(money: dict | None) -> float | None:
    if not money or money.get("units") is None:
        return None
    return float(money["units"]) + money.get("nanos", 0) / 1e9


class GooglePlacesAdapter(Adapter):
    name = "google_places"

    def __init__(self, *args, **kw):
        super().__init__(*args, **kw)
        self.gp = self.cfg["google_places"]
        self.cells = grid(**self.city.bbox.model_dump(), cell_km=self.gp["cell_km"])
        self._seen: set[str] = set()
        self.http = Http(self.name)
        self.keys: KeyRing | None = None
        try:
            self.keys = KeyRing(
                "GOOGLE_MAPS_API_KEY", self.db,
                "Google Places needs a key on a GCP project with billing enabled. "
                "Until then, switch adapters.osm_places on in the city config (§5.2).",
            )
        except MissingConfig:
            if not self.dry_run:
                raise
        if not self.dry_run and self.db is None:
            raise GooglePlacesError("google_places needs Mongo for the quota counter")

    # ---- budget ----

    def text_queries(self) -> list[str]:
        areas = self.city.neighborhoods or [self.city.name]
        return [f"{term} in {area}" for term in self.city.query_terms.get("places", []) for area in areas]

    def estimate(self) -> dict[str, int]:
        """Base calls that would hit the network. Requests already in the disk cache are
        free, so a rerun within the cache TTL estimates ~0. Recursion into saturated cells
        adds more, and those calls still stop at the quota cap."""
        nearby = sum(
            not self._cached(NEARBY_URL, self._nearby_body(cell, spec["types"]), NEARBY_MASK)
            for spec in self.gp["type_groups"].values()
            for cell in self.cells
        )
        text = sum(not self._cached(TEXT_URL, self._text_body(q), TEXT_MASK) for q in self.text_queries())
        return {"google_nearby": nearby, "google_text": text * self.gp["text_max_pages"]}

    def _cached(self, url: str, body: dict, mask: str) -> bool:
        if self.keys is None:
            return False
        headers = {"X-Goog-Api-Key": self.keys.cache_key, "X-Goog-FieldMask": mask}
        return self.http.is_cached("POST", url, None, body, headers, CACHE_TTL)

    # ---- fetch ----

    def fetch(self) -> Iterable[dict]:
        try:
            for group, spec in self.gp["type_groups"].items():
                for cell in self.cells:
                    yield from self._nearby(cell, spec["types"], spec["recurse"])
        except QuotaExceeded as e:
            self.errors.append(f"nearby search stopped: {e}")
        try:
            for query in self.text_queries():
                yield from self._text(query)
        except QuotaExceeded as e:
            self.errors.append(f"text search stopped: {e}")

    def _nearby(self, cell: Cell, types: list[str], recurse: bool) -> Iterable[dict]:
        body = self._nearby_body(cell, types)
        places = self._post(NEARBY_URL, body, NEARBY_MASK, "google_nearby").get("places", [])
        yield from self._new(places)
        if recurse and len(places) >= MAX_RESULTS and cell.size_km / 2 >= self.gp["min_cell_km"]:
            for sub in cell.split():
                yield from self._nearby(sub, types, recurse)

    def _nearby_body(self, cell: Cell, types: list[str]) -> dict:
        lat, lng = cell.center
        return {
            "includedTypes": types,
            "maxResultCount": MAX_RESULTS,
            "rankPreference": "POPULARITY",
            "locationRestriction": {
                "circle": {"center": {"latitude": lat, "longitude": lng}, "radius": round(cell.radius_m, 1)}
            },
            "languageCode": self.city.language,
            "regionCode": self.city.country_code,
        }

    def _text(self, query: str) -> Iterable[dict]:
        body = self._text_body(query)
        for _ in range(self.gp["text_max_pages"]):
            resp = self._post(TEXT_URL, body, TEXT_MASK, "google_text")
            yield from self._new(resp.get("places", []))
            token = resp.get("nextPageToken")
            if not token:
                return
            body = {**body, "pageToken": token}

    def _text_body(self, query: str) -> dict:
        bbox = self.city.bbox
        return {
            "textQuery": query,
            "pageSize": self.gp["text_page_size"],
            "locationRestriction": {
                "rectangle": {
                    "low": {"latitude": bbox.south, "longitude": bbox.west},
                    "high": {"latitude": bbox.north, "longitude": bbox.east},
                }
            },
            "languageCode": self.city.language,
            "regionCode": self.city.country_code,
        }

    def _new(self, places: list[dict]) -> Iterable[dict]:
        for p in places:
            if p["id"] not in self._seen:
                self._seen.add(p["id"])
                yield p

    def _post(self, url: str, body: dict, mask: str, sku: str) -> dict:
        # Cached under key 1 whichever key sends it (keys.py), so rotation never refetches.
        headers = {"X-Goog-Api-Key": self.keys.cache_key, "X-Goog-FieldMask": mask}
        while True:
            try:
                return self.http.post_json(
                    url, body, headers=headers, cache_ttl=CACHE_TTL,
                    before_network=lambda: self.keys.reserve(sku),
                    auth=lambda: {"X-Goog-Api-Key": self.keys.key},
                )
            except httpx.HTTPStatusError as e:
                # Out of quota or refused (billing, API not enabled) on this account: try the next one.
                if e.response.status_code in (403, 429) and self.keys.rotate(self._explain(e)):
                    continue
                raise GooglePlacesError(self._explain(e)) from None

    @staticmethod
    def _explain(e: httpx.HTTPStatusError) -> str:
        try:
            msg = e.response.json()["error"]["message"]
        except Exception:
            msg = e.response.text[:300]
        if "billing" in msg.lower():
            return f"Google Places: billing is not enabled on the key's GCP project ({msg}). Use osm_places until it is."
        if e.response.status_code in (400, 401, 403):
            return f"Google Places rejected the request/key (HTTP {e.response.status_code}): {msg}"
        return f"Google Places HTTP {e.response.status_code}: {msg}"

    # ---- normalize ----

    def normalize(self, raw: dict) -> Iterable[Activity]:
        loc = raw.get("location")
        name = ((raw.get("displayName") or {}).get("text") or "").strip()
        types = raw.get("types") or []
        category = google_category(raw.get("primaryType"), types)
        if (
            raw.get("businessStatus") != "OPERATIONAL"
            or not loc
            or not name
            or category is None
            or (raw.get("userRatingCount") or 0) < self.gp["min_rating_count"]
        ):
            return self.skip(
                "not operational" if raw.get("businessStatus") != "OPERATIONAL"
                else "unmapped type (e.g. food)" if category is None
                else "too few ratings" if loc and name else "no location or name"
            )
        tier = PRICE_LEVELS.get(raw.get("priceLevel"))
        pr = raw.get("priceRange") or {}
        lo, hi = _units(pr.get("startPrice")), _units(pr.get("endPrice"))
        currency = (pr.get("startPrice") or pr.get("endPrice") or {}).get("currencyCode") or self.city.currency
        price = None
        if tier is not None or lo is not None or hi is not None:
            price = Price(min=lo, max=hi, currency=currency, tier=tier, isFree=True if tier == 0 else None)

        url = raw.get("googleMapsUri")
        return [
            Activity(
                kind="place",
                city=self.city.slug,
                name=name,
                category=category,
                sourceCategory=raw.get("primaryType") or (types[0] if types else None),
                location=GeoPoint.at(loc["latitude"], loc["longitude"]),
                address=address_from(raw),
                timezone=self.city.timezone,
                weeklyHours=weekly_hours(raw.get("regularOpeningHours")),
                hoursSource="google" if raw.get("regularOpeningHours") else None,
                price=price,
                rating=raw.get("rating"),
                ratingCount=raw.get("userRatingCount"),
                url=raw.get("websiteUri") or url,
                sourceKeys=[f"google:{raw['id']}"],
                sources=[SourceRef(name=self.name, id=raw["id"], url=url, fetchedAt=datetime.now(timezone.utc))],
                googlePlaceId=raw["id"],
            )
        ]
