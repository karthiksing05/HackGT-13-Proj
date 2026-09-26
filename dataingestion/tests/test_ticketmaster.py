from datetime import datetime, timezone

import pytest

from ingest.adapters.ticketmaster import TicketmasterAdapter
from ingest.pipeline import finalize

from .conftest import load_fixture


@pytest.fixture
def adapter(city, monkeypatch):
    monkeypatch.setenv("TICKETMASTER_API_KEY", "test")
    return TicketmasterAdapter(city)


@pytest.fixture
def events():
    return load_fixture("ticketmaster/events_page.json")["_embedded"]["events"]


def test_normalizes_live_fixture(adapter, events):
    acts = [a for e in events for a in adapter.normalize(e)]
    assert acts, "every fixture event was skipped"
    for a in acts:
        lng, lat = a.location.coordinates
        assert -85.5 < lng < -83.5 and 33 < lat < 34.5, "coordinates must be [lng, lat]"
        assert a.kind == "event" and a.start.tzinfo is not None
        assert a.sourceKeys == [f"ticketmaster:{a.sources[0].id}"]
        assert a.attendance == "fixed_start"


def test_venue_fields(adapter, events):
    raw = next(e for e in events if e["id"] == "rZ7HnEZ1A4ZxpK")
    (a,) = adapter.normalize(raw)
    assert a.venueName == "Midtown High School Theater"
    assert a.address.street == "929 Charles Allen Dr"
    assert (a.address.locality, a.address.region, a.address.postalCode, a.address.countryCode) == (
        "Atlanta", "GA", "30309", "US",
    )
    assert a.price.min == 124.85 and a.price.tier == 4


def test_skips_cancelled_and_tba(adapter, events):
    raw = dict(events[0], dates={**events[0]["dates"], "status": {"code": "cancelled"}})
    assert list(adapter.normalize(raw)) == []
    raw = dict(events[0], dates={**events[0]["dates"], "start": {"localDate": "2026-09-26", "timeTBA": True}})
    assert list(adapter.normalize(raw)) == []


def test_finalize_sets_duration_and_expiry(adapter, events):
    for e in events:
        for a in adapter.normalize(e):
            finalize(a)
            assert a.duration.p75Min >= a.duration.medianMin
            assert a.expiresAt > a.start


def test_day_windows_start_now_and_split_at_local_midnight(adapter):
    now = datetime(2026, 9, 26, 1, 30, tzinfo=timezone.utc)  # 21:30 Friday in Atlanta
    windows = adapter.day_windows(now)
    assert len(windows) == 7
    assert windows[0][0] == now
    assert windows[0][1] == datetime(2026, 9, 26, 4, 0, tzinfo=timezone.utc)  # Sat 00:00 EDT
    assert all(a < b for a, b in windows)
