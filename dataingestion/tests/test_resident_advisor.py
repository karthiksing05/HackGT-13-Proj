from datetime import datetime, timezone

import pytest

from ingest.adapters.resident_advisor import ResidentAdvisorAdapter, parse_cost, precise_enough
from ingest.enrich.geocode import parse_result
from ingest.pipeline import finalize

from .conftest import load_fixture


@pytest.fixture
def adapter(city):
    a = ResidentAdvisorAdapter(city)
    a.geocoder = None  # RA coords only; geocoding is covered below with a stub
    return a


class StubGeocoder:
    def __init__(self, result):
        self.result = result
        self.calls = []

    def geocode(self, text):
        self.calls.append(text)
        return self.result


@pytest.fixture
def lunchbox_geo():
    return parse_result(load_fixture("google_geocode/lunchbox.json")["results"][0])


@pytest.fixture
def events():
    listings = load_fixture("resident_advisor/event_listings_page.json")["data"]["eventListings"]["data"]
    return [x["event"] for x in listings]


def by_id(events, event_id):
    return next(e for e in events if e["id"] == event_id)


def test_normalizes_live_fixture(adapter, events):
    acts = [a for e in events for a in adapter.normalize(e)]
    assert acts, "every fixture event was skipped"
    for a in acts:
        lng, lat = a.location.coordinates
        assert -85.5 < lng < -83.5 and 33 < lat < 34.5, "coordinates must be [lng, lat]"
        assert a.kind == "event" and a.start.tzinfo is not None
        assert a.sourceKeys == [f"ra:{a.sources[0].id}"]
        assert a.url.startswith("https://ra.co/events/")
        assert "music" in a.tags


def test_local_times_convert_to_utc(adapter, events):
    (a,) = adapter.normalize(by_id(events, "2540250"))  # Pisces, 22:00-03:00 local
    assert a.start == datetime(2026, 9, 26, 2, 0, tzinfo=timezone.utc)  # 22:00 EDT
    assert a.end == datetime(2026, 9, 26, 7, 0, tzinfo=timezone.utc)
    assert a.timezone == "America/New_York"


def test_venue_and_price(adapter, events):
    (a,) = adapter.normalize(by_id(events, "2522070"))
    assert a.venueName == "Sunset Atlanta"
    assert a.address.formatted == "728 Monroe Dr NE Suite B, Atlanta, GA 30308"
    assert (a.price.min, a.price.max, a.price.tier) == (12, 14, 1)
    assert a.category == "nightclub"
    assert {"late_night", "21_plus"} <= set(a.tags)


def test_lineup_leads_description(adapter, events):
    (a,) = adapter.normalize(by_id(events, "2542315"))
    assert a.description.startswith("Lineup: ")


def test_skips_secret_and_rounded_venues(adapter, events):
    assert list(adapter.normalize(by_id(events, "2522066"))) == []  # TBA venue at 0,0
    assert list(adapter.normalize(by_id(events, "2524690"))) == []  # Lunchbox rounded to 34,-84


def test_popularity_is_min_max_scaled(adapter, events):
    adapter.attending_range = (0, 515)
    (a,) = adapter.normalize(by_id(events, "2542315"))
    assert a.popularity == round(147 / 515, 3)


def test_finalize_sets_duration_and_expiry(adapter, events):
    for e in events:
        for a in adapter.normalize(e):
            finalize(a)
            assert a.duration.p75Min >= a.duration.medianMin
            assert a.expiresAt > a.start
            if (a.end - a.start).total_seconds() > 4 * 3600:
                assert a.attendance == "drop_in"


@pytest.mark.parametrize(
    "cost, expected",
    [
        ("10", (10, 10)),
        ("$0", (0, 0)),
        ("$12 - $23", (12, 23)),
        ("$47.70", (47.7, 47.7)),
        ("$10 before 11pm, $20 after", (10, 20)),
        ("Free", (0, 0)),
        (None, (None, None)),
        ("TBA", (None, None)),
    ],
)
def test_parse_cost(cost, expected):
    assert parse_cost(cost) == expected


def test_precise_enough():
    assert precise_enough(33.75, -84.37)
    assert precise_enough(33.792085, -84.388001)
    assert not precise_enough(0, 0)
    assert not precise_enough(34, -84)


def test_listing_dates_cover_horizon(adapter):
    now = datetime(2026, 9, 26, 1, 30, tzinfo=timezone.utc)  # 21:30 Friday in Atlanta
    first, last = adapter.listing_dates(now)
    assert first.isoformat() == "2026-09-25"
    assert (last - first).days == adapter.horizon_days - 1


def test_geocode_rescues_rounded_venue(adapter, events, lunchbox_geo):
    adapter.geocoder = StubGeocoder(lunchbox_geo)
    (a,) = adapter.normalize(by_id(events, "2524690"))  # RA says 34,-84
    assert a.location.coordinates == (-84.390056, 33.7523792)
    assert adapter.geocoder.calls == ["50 Lower Alabama Street, Atlanta, GA 30303"]
    # source's formatted string is kept; geocoder fills the missing parts
    assert a.address.formatted == "50 Lower Alabama Street, Atlanta, GA 30303"
    assert (a.address.street, a.address.region, a.address.postalCode) == ("50 Lower Alabama Street", "GA", "30303")


def test_geocode_far_from_ra_coords_is_ignored(adapter, events, lunchbox_geo):
    adapter.geocoder = StubGeocoder(lunchbox_geo)  # downtown, but Flo Bar is on Cheshire Bridge (33.81,-84.35)
    (a,) = adapter.normalize(by_id(events, "2530443"))
    assert a.location.coordinates == (-84.35, 33.81)


def test_secret_venue_is_not_geocoded(adapter, events, lunchbox_geo):
    adapter.geocoder = StubGeocoder(lunchbox_geo)
    assert list(adapter.normalize(by_id(events, "2522066"))) == []
    assert adapter.geocoder.calls == []
