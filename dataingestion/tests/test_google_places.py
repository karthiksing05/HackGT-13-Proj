import pytest

from ingest.adapters.google_places import GooglePlacesAdapter, weekly_hours
from ingest.config import MissingConfig

from .conftest import load_fixture


@pytest.fixture
def adapter(city):
    return GooglePlacesAdapter(city, dry_run=True)


@pytest.fixture
def places():
    return {p["id"]: p for p in load_fixture("google_places/searchNearby_documented_shape.json")["places"]}


def test_museum(adapter, places):
    (a,) = adapter.normalize(places["FIXTURE_museum_1"])
    assert a.kind == "place" and a.category == "museum"
    assert a.location.coordinates == (-84.3876, 33.7701)
    assert a.address.street == "100 Example Street Northwest"
    assert (a.address.locality, a.address.region, a.address.postalCode, a.address.countryCode) == (
        "Atlanta", "GA", "30308", "US",
    )
    assert [(h.open, h.close) for h in a.weeklyHours] == [(2 * 1440 + 600, 2 * 1440 + 1020), (720, 1050)]
    assert a.hoursSource == "google"
    assert (a.price.min, a.price.max, a.price.tier) == (20, 30, 2)
    assert a.googlePlaceId == "FIXTURE_museum_1" and a.sourceKeys == ["google:FIXTURE_museum_1"]


def test_open_24_7_and_free(adapter, places):
    (a,) = adapter.normalize(places["FIXTURE_park_24h"])
    assert [(h.open, h.close) for h in a.weeklyHours] == [(0, 10080)]
    assert a.price.tier == 0 and a.price.isFree is True
    assert a.category == "park"


def test_saturday_night_wraps(places):
    (h,) = weekly_hours(places["FIXTURE_bar_wraps"]["regularOpeningHours"])
    assert h.open == 6 * 1440 + 18 * 60 and h.close == 120 and h.close < h.open


def test_skips_food_places(adapter, places):
    assert list(adapter.normalize(places["FIXTURE_restaurant_generic"])) == []
    cafe = dict(places["FIXTURE_museum_1"], primaryType="coffee_shop", types=["coffee_shop", "cafe"])
    assert list(adapter.normalize(cafe)) == []


def test_missing_hours(adapter, places):
    raw = {k: v for k, v in places["FIXTURE_museum_1"].items() if k not in ("regularOpeningHours", "priceLevel", "priceRange")}
    (a,) = adapter.normalize(raw)
    assert a.weeklyHours is None and a.hoursSource is None and a.price is None


def test_skips_non_operational(adapter, places):
    assert list(adapter.normalize(places["FIXTURE_closed"])) == []


def test_skips_unmapped_primary_type(adapter, places):
    """A salon that also carries a secondary art_gallery type is not a gallery."""
    assert list(adapter.normalize(places["FIXTURE_salon"])) == []


def test_skips_low_rating_count(adapter, places):
    raw = dict(places["FIXTURE_museum_1"], userRatingCount=3)
    assert list(adapter.normalize(raw)) == []


def test_live_fixture(adapter):
    raws = load_fixture("google_places/searchNearby_live.json")["places"]
    acts = [a for r in raws for a in adapter.normalize(r)]
    assert acts
    for a in acts:
        assert a.address.region == "GA" and a.googlePlaceId


def test_estimate_fits_free_caps(adapter):
    est = adapter.estimate()
    assert 80 <= len(adapter.cells) <= 140  # ~3 km grid over the Atlanta bbox
    assert est["google_nearby"] <= 900 and est["google_text"] <= 900


def test_missing_key_fails_fast(city, monkeypatch):
    monkeypatch.delenv("GOOGLE_MAPS_API_KEY", raising=False)
    monkeypatch.delenv("GOOGLE_CLOUD_API_KEY", raising=False)
    monkeypatch.delenv("GOOGLE_CLOUD_API_KEYS", raising=False)
    monkeypatch.delenv("GOOGLE_MAPS_API_KEYS", raising=False)
    with pytest.raises(MissingConfig, match="billing"):
        GooglePlacesAdapter(city)
