import json

import pytest

from ingest.enrich import geocode as geo_mod
from ingest.enrich.geocode import Geocoder, fill_address, fill_region, normalize_address, parse_result
from ingest.models import Activity, Address, GeoPoint, SourceRef

from .conftest import load_fixture


@pytest.fixture
def fixture():
    return load_fixture("google_geocode/lunchbox.json")


def test_parse_result(fixture):
    g = parse_result(fixture["results"][0])
    assert (g.lat, g.lng, g.location_type) == (33.7523792, -84.390056, "ROOFTOP")
    assert g.point.coordinates == (-84.390056, 33.7523792)
    a = g.address
    assert (a.street, a.locality, a.region, a.postalCode, a.countryCode) == (
        "50 Lower Alabama Street", "Atlanta", "GA", "30303", "US",
    )


def test_fill_address_never_overwrites_source_parts():
    src = Address(formatted="as the source wrote it", locality="Decatur")
    found = Address(formatted="Google's version", locality="Atlanta", region="GA", postalCode="30030")
    out = fill_address(src, found)
    assert (out.formatted, out.locality, out.region, out.postalCode) == ("as the source wrote it", "Decatur", "GA", "30030")


def test_normalize_address():
    assert normalize_address("50 Lower  Alabama St,Atlanta, GA") == normalize_address("50 lower alabama st, atlanta, ga")


@pytest.fixture
def geocoder(city, monkeypatch, tmp_path):
    monkeypatch.setenv("GOOGLE_MAPS_API_KEY", "test")
    monkeypatch.setattr(geo_mod, "CACHE_DIR", tmp_path)
    g = Geocoder(city, db=None)
    return g


def _seed_cache(g, text, data):
    import hashlib
    g.cache_dir.mkdir(parents=True, exist_ok=True)
    key = hashlib.sha1(normalize_address(text).encode()).hexdigest()
    (g.cache_dir / f"{key}.json").write_text(json.dumps(data))


def test_geocoder_reads_cache_without_db(geocoder, fixture):
    _seed_cache(geocoder, "50 Lower Alabama Street, Atlanta, GA 30303", fixture)
    g = geocoder.geocode("50 Lower Alabama Street,  Atlanta, GA 30303")
    assert g and g.address.postalCode == "30303"
    assert geocoder.lookups == 0


def test_geocoder_without_db_never_calls_network(geocoder):
    geocoder.http = None  # any network attempt would raise AttributeError
    assert geocoder.geocode("123 Nowhere St") is None


def test_geocoder_rejects_approximate_and_out_of_bbox(geocoder, fixture):
    approx = json.loads(json.dumps(fixture))
    approx["results"][0]["geometry"]["location_type"] = "APPROXIMATE"
    _seed_cache(geocoder, "approx", approx)
    assert geocoder.geocode("approx") is None

    far = json.loads(json.dumps(fixture))
    far["results"][0]["geometry"]["location"] = {"lat": 40.7, "lng": -74.0}
    _seed_cache(geocoder, "far", far)
    assert geocoder.geocode("far") is None


def test_fill_region_uses_city_default(city):
    from datetime import datetime, timezone

    act = Activity(
        kind="event", city="atlanta", name="x", location=GeoPoint.at(33.75, -84.38), timezone=city.timezone,
        address=Address(formatted="somewhere"), sourceKeys=["t:1"],
        sources=[SourceRef(name="t", id="1", fetchedAt=datetime.now(timezone.utc))],
    )
    fill_region(act, city)
    assert (act.address.region, act.address.countryCode) == ("GA", "US")
