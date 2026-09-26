from datetime import datetime, timezone

import pytest

from ingest.agent.blurb import BlurbAgent
from ingest.agent.gemini import GeminiQuotaExhausted, Result
from ingest.agent.research import Researcher
from ingest.config import load_global
from ingest.enrich.geocode import address_query, locate, parse_result
from ingest.models import Activity, Address, GeoPoint, SourceRef
from ingest.pipeline import backfill, coverage

from .conftest import gemini_cfg, load_fixture
from .test_blurb import GOOD, event_doc


def act(**kw):
    base = dict(
        kind="event", city="atlanta", name="Show", timezone="America/New_York",
        start=datetime(2026, 9, 27, 0, 0, tzinfo=timezone.utc), sourceKeys=["t:1"],
        sources=[SourceRef(name="t", id="1", fetchedAt=datetime.now(timezone.utc))],
    )
    base.update(kw)
    return Activity(**base)


class StubGeocoder:
    def __init__(self, result=None):
        self.result = result or parse_result(load_fixture("google_geocode/lunchbox.json")["results"][0])
        self.calls = []

    def geocode(self, text):
        self.calls.append(text)
        return self.result


def test_address_query_uses_parts_but_not_a_bare_city():
    assert address_query(Address(street="50 Lower Alabama St", locality="Atlanta", region="GA", postalCode="30303")) == \
        "50 Lower Alabama St, Atlanta, GA 30303"
    assert address_query(Address(locality="Atlanta", region="GA")) is None


def test_locate_fills_missing_coordinates_from_address():
    a = act(address=Address(street="50 Lower Alabama St", locality="Atlanta", region="GA"))
    g = StubGeocoder()
    assert locate(a, g)
    assert a.location.coordinates == (-84.390056, 33.7523792)
    assert a.address.postalCode == "30303" and a.address.street == "50 Lower Alabama St"  # source part kept


def test_locate_never_moves_located_activity_with_full_address():
    a = act(location=GeoPoint.at(33.8, -84.4), address=Address(locality="Atlanta", region="GA"))
    g = StubGeocoder()
    assert not locate(a, g) and g.calls == []
    assert a.location.coordinates == (-84.4, 33.8)


class FakeGemini:
    def __init__(self, research_ok=True):
        self.research_ok, self.calls = research_ok, []

    def generate(self, model, prompt, **kw):
        self.calls.append((model, kw.get("search", False)))
        if kw.get("search"):
            if not self.research_ok:
                raise GeminiQuotaExhausted(f"{model}: daily")
            return Result(text="- Over 900 dealer tables\n- Pokemon", model=model, sources=[{"title": "x", "url": "https://a.example"}])
        return Result(text=GOOD, model=model)


def test_research_falls_through_models_then_turns_off():
    r = Researcher(None, gemini_cfg(), "Atlanta")
    r.gemini = FakeGemini(research_ok=False)
    assert r.research(event_doc(), {}, "h") is None
    assert [m for m, _ in r.gemini.calls] == gemini_cfg()["research_models"]
    assert r.grounded_off


def test_ungrounded_blurb_regenerates_once_research_exists():
    from ingest.agent.research import facts_packet, input_hash
    doc = event_doc()
    doc["blurb"] = {"inputHash": input_hash(facts_packet(doc)), "grounded": False}
    agent = BlurbAgent(None, gemini_cfg(), "Atlanta", dry_run=True)
    agent.researcher.gemini = FakeGemini(research_ok=False)
    assert agent.run(doc).status == "skipped"  # nothing new to write from
    agent = BlurbAgent(None, gemini_cfg(), "Atlanta", dry_run=True)
    agent.researcher.gemini = FakeGemini(research_ok=True)
    out = agent.run(doc)
    assert out.status == "written" and out.blurb["grounded"]


def test_backfill_fills_gaps_without_overwriting(test_db, city, monkeypatch):
    import ingest.pipeline as pl
    monkeypatch.setattr(pl, "make_geocoder", lambda c, db: StubGeocoder())
    a = act(location=GeoPoint.at(33.75, -84.39), address=Address(formatted="50 Lower Alabama Street, Atlanta, GA 30303"))
    test_db.activities.insert_one(a.model_dump(mode="python"))
    counts = backfill(test_db, city)
    assert counts["updated"] == 1 and counts["geocoded"] == 1
    doc = test_db.activities.find_one()
    assert doc["location"]["coordinates"] == [-84.39, 33.75]  # existing coords untouched
    assert doc["address"]["region"] == "GA" and doc["address"]["formatted"] == "50 Lower Alabama Street, Atlanta, GA 30303"
    assert doc["duration"]["p75Min"] > 0 and doc["expiresAt"] > doc["start"]
    assert backfill(test_db, city)["updated"] == 0  # idempotent
    rows = {label: (n, t) for label, n, t in coverage(test_db, city)}
    assert rows["coordinates"] == (1, 1) and rows["blurb"] == (0, 1)
