from datetime import datetime, timedelta, timezone

from bson import ObjectId

from ingest import crawler
from ingest.adapters import ADAPTERS
from ingest.adapters.base import Adapter
from ingest.db import ensure_indexes, insert_new
from ingest.models import Activity, GeoPoint, SourceRef

START = datetime.now(timezone.utc).replace(microsecond=0) + timedelta(days=2)


def act(key="tm:1", name="Khruangbin", start=START, venue="The Eastern", **kw):
    src, sid = key.split(":")
    return Activity(
        kind="event", city="atlanta", name=name, timezone="America/New_York", start=start,
        venueName=venue, location=GeoPoint.at(33.75, -84.38), sourceKeys=[key],
        sources=[SourceRef(name=src, id=sid, fetchedAt=datetime.now(timezone.utc))], **kw,
    )


def test_insert_new_skips_known_and_links_other_sources(test_db):
    ensure_indexes(test_db)
    outcome, _id = insert_new(test_db, act())
    assert outcome == "inserted"
    assert insert_new(test_db, act(description="changed"))[0] == "known"
    assert test_db.activities.find_one({"_id": _id}).get("description") is None  # left untouched

    # RA's listing of the same show: 15 minutes off, different punctuation/case.
    outcome, linked = insert_new(test_db, act("ra:9", name="KHRUANGBIN!", start=START + timedelta(minutes=15)))
    assert (outcome, linked) == ("linked", _id)
    doc = test_db.activities.find_one({"_id": _id})
    assert set(doc["sourceKeys"]) == {"tm:1", "ra:9"} and len(doc["sources"]) == 2
    assert insert_new(test_db, act("ra:9"))[0] == "known"

    # Same venue, one name contains the other.
    assert insert_new(test_db, act("ra:10", name="Khruangbin: A La Sala Tour"))[0] == "linked"
    # Next night, or another show at the same time, is new.
    assert insert_new(test_db, act("tm:2", start=START + timedelta(days=1)))[0] == "inserted"
    assert insert_new(test_db, act("tm:3", name="Comedy Night", venue="Laughing Skull"))[0] == "inserted"
    assert test_db.activities.count_documents({}) == 3


class FakeAdapter(Adapter):
    name = "fake_events"
    batch: list[Activity] = []

    def fetch(self):
        yield from ({"a": a} for a in self.batch)

    def normalize(self, raw):
        return [raw["a"]]


def test_tick_ingests_and_writes_up_only_new(test_db, monkeypatch):
    monkeypatch.setitem(ADAPTERS, "fake_events", FakeAdapter)
    from ingest.config import load_city

    city = load_city("atlanta")
    monkeypatch.setattr(crawler, "load_city", lambda slug: city.model_copy(update={"adapters": {"fake_events": True}}))
    written: list[list[str]] = []
    monkeypatch.setattr(crawler, "write_up", lambda db, c, docs, cfg, echo: written.append([d["name"] for d in docs]) or {})
    cfg = {"tiers": {"events": {"sources": ["fake_events"], "every_minutes": 60},
                     "places": {"sources": ["fake_events"], "every_minutes": 10080, "write": False}},
           "retry_hours": 48}
    ensure_indexes(test_db)

    FakeAdapter.batch = [act("tm:1"), act("tm:2", name="Other Show")]
    assert crawler.tick(test_db, cfg, ["atlanta"], echo=lambda *_: None) == {"atlanta": 2}
    assert written == [["Khruangbin", "Other Show"]]  # places tier doesn't write up

    # Not due yet: nothing runs.
    assert crawler.tick(test_db, cfg, ["atlanta"], echo=lambda *_: None) == {}

    # Forced: only the new one is new; the recent ones still lacking embedding text are retried.
    FakeAdapter.batch = [act("tm:1"), act("tm:3", name="Third Show", start=START + timedelta(hours=3))]
    test_db.activities.update_one({"name": "Other Show"}, {"$set": {"embeddingText": "done"}})
    written.clear()
    assert crawler.tick(test_db, cfg, ["atlanta"], force={"events"}, echo=lambda *_: None) == {"atlanta": 1}
    assert written == [["Khruangbin", "Third Show"]]
    run = test_db.runs.find_one({"source": "fake_events"}, sort=[("startedAt", -1)])
    assert (run["inserted"], run["known"]) == (1, 1)


def test_new_showing_reuses_research(test_db):
    first = test_db.activities.insert_one(act().model_dump()).inserted_id
    test_db.research.insert_one({"activityId": first, "inputHash": "old", "provider": "muse",
                                 "notes": "- psych trio", "sources": [], "createdAt": datetime.now(timezone.utc)})
    second = act("tm:2", start=START + timedelta(days=1)).model_dump()
    second["_id"] = test_db.activities.insert_one(second).inserted_id
    assert crawler.reuse_showing_research(test_db, second, "muse")
    copied = test_db.research.find_one({"activityId": second["_id"]})
    assert copied["notes"] == "- psych trio" and copied["copiedFrom"] == first
    lone = act("tm:3", name="Solo").model_dump()
    lone["_id"] = ObjectId()
    assert not crawler.reuse_showing_research(test_db, lone, "muse")
