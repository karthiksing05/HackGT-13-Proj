"""Google Geocoding API (§6.1): address string -> coordinates + address parts.

Every result (including "no match") is cached on disk by normalized address, so a
venue is looked up once no matter how many events it hosts. Only real network calls
are reserved against the `google_geocode` quota. Without Mongo (a dry run with no DB)
the geocoder answers from cache only, so it can never spend uncounted quota.
"""

import hashlib
import json
import logging
import re
import time
from dataclasses import dataclass

from pymongo.database import Database

from ..config import CACHE_DIR, City, MissingConfig, require_env
from ..http import Http
from ..models import Activity, Address, GeoPoint
from ..quota import QuotaExceeded, reserve

log = logging.getLogger(__name__)

GEOCODE_URL = "https://maps.googleapis.com/maps/api/geocode/json"
CACHE_TTL = 90 * 24 * 3600
CACHEABLE = {"OK", "ZERO_RESULTS"}  # never cache REQUEST_DENIED / OVER_QUERY_LIMIT
# APPROXIMATE means Google only matched a city or neighborhood centroid: useless for a venue.
PRECISE_TYPES = {"ROOFTOP", "RANGE_INTERPOLATED", "GEOMETRIC_CENTER"}


class GeocodeError(RuntimeError):
    pass


class CacheOnly(Exception):
    pass


@dataclass
class Geocoded:
    lat: float
    lng: float
    address: Address
    location_type: str
    place_id: str | None

    @property
    def point(self) -> GeoPoint:
        return GeoPoint.at(self.lat, self.lng)


def normalize_address(text: str) -> str:
    return re.sub(r"\s+", " ", text.replace(",", ", ")).strip().lower()


def parse_result(result: dict) -> Geocoded:
    parts: dict[str, dict] = {}
    for comp in result.get("address_components") or []:
        for t in comp.get("types", []):
            parts.setdefault(t, comp)

    def long(t):
        return (parts.get(t) or {}).get("long_name")

    def short(t):
        return (parts.get(t) or {}).get("short_name")

    geom = result["geometry"]
    return Geocoded(
        lat=geom["location"]["lat"],
        lng=geom["location"]["lng"],
        address=Address(
            formatted=result.get("formatted_address"),
            street=" ".join(x for x in (long("street_number"), long("route")) if x) or None,
            locality=long("locality") or long("postal_town") or long("sublocality"),
            region=short("administrative_area_level_1"),
            postalCode=long("postal_code"),
            countryCode=short("country"),
        ),
        location_type=geom.get("location_type", ""),
        place_id=result.get("place_id"),
    )


def fill_address(current: Address | None, found: Address) -> Address:
    """Fill only the parts the source left empty (§6.1: never overwrite source parts)."""
    if current is None:
        return found
    return Address(**{k: v if v is not None else getattr(found, k) for k, v in current.model_dump().items()})


class Geocoder:
    def __init__(self, city: City, db: Database | None):
        self.city = city
        self.db = db
        self.api_key = require_env("GOOGLE_MAPS_API_KEY", "Geocoding needs a key with the Geocoding API enabled.")
        self.http = Http("google_geocode")
        self.cache_dir = CACHE_DIR / "google_geocode"
        self.lookups = 0
        self.disabled_reason: str | None = None

    def geocode(self, text: str | None) -> Geocoded | None:
        """Best precise match inside the city bbox, or None."""
        if not text or not text.strip() or self.disabled_reason:
            return None
        try:
            data = self._lookup(text)
        except CacheOnly:
            return None
        except (QuotaExceeded, GeocodeError) as e:
            # Stop calling for the rest of the run; callers fall back to source coords.
            self.disabled_reason = str(e)
            log.warning("geocoding disabled for this run: %s", e)
            return None
        for result in data.get("results") or []:
            geo = parse_result(result)
            if geo.location_type in PRECISE_TYPES and self._in_bbox(geo.lat, geo.lng):
                return geo
        return None

    def _lookup(self, text: str) -> dict:
        key = normalize_address(text)
        path = self.cache_dir / f"{hashlib.sha1(key.encode()).hexdigest()}.json"
        if path.exists() and time.time() - path.stat().st_mtime < CACHE_TTL:
            try:
                return json.loads(path.read_text())
            except json.JSONDecodeError:
                pass
        if self.db is None:
            raise CacheOnly()
        bbox = self.city.bbox
        params = {
            "address": text,
            "key": self.api_key,
            "bounds": f"{bbox.south},{bbox.west}|{bbox.north},{bbox.east}",
            "region": self.city.country_code.lower(),
            "language": self.city.language,
        }
        data = self.http.get_json(GEOCODE_URL, params, before_network=lambda: reserve(self.db, "google_geocode"))
        self.lookups += 1
        status = data.get("status")
        if status not in CACHEABLE:
            raise GeocodeError(f"Google Geocoding {status}: {data.get('error_message') or 'no message'}")
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(data))
        return data

    def _in_bbox(self, lat: float, lng: float) -> bool:
        b = self.city.bbox
        return b.south <= lat <= b.north and b.west <= lng <= b.east


def make_geocoder(city: City, db: Database | None) -> Geocoder | None:
    """None when no key is configured, so adapters keep working without Google."""
    try:
        return Geocoder(city, db)
    except MissingConfig as e:
        log.warning("geocoding off: %s", e)
        return None


def fill_region(act: Activity, city: City) -> None:
    """Last resort (§6.1): coords but no region -> city.default_region."""
    if act.address and not act.address.region and city.default_region:
        if act.address.countryCode in (None, city.country_code):
            act.address.region = city.default_region
            act.address.countryCode = act.address.countryCode or city.country_code


def address_query(address: Address | None) -> str | None:
    """The text to geocode: the source's one-line address, or its parts joined."""
    if address is None:
        return None
    if address.formatted:
        return address.formatted
    parts = [address.street, address.locality, " ".join(x for x in (address.region, address.postalCode) if x)]
    return ", ".join(p for p in parts if p) if address.street else None  # a city alone is not a venue


def locate(act: Activity, geocoder: "Geocoder | None") -> bool:
    """Fill missing coordinates (and missing address parts) from the address. Returns True
    when a geocode result was used. Never moves an activity that already has coordinates."""
    # City and state matter to the planner; a missing ZIP alone isn't worth a lookup.
    missing_parts = act.address is not None and not (act.address.locality and act.address.region)
    if geocoder is None or (act.location is not None and not missing_parts):
        return False
    geo = geocoder.geocode(address_query(act.address))
    if geo is None:
        return False
    if act.location is None:
        act.location = geo.point
    act.address = fill_address(act.address, geo.address)
    return True
