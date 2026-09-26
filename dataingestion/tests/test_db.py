from datetime import datetime, timezone

import pytest

from ingest import quota
from ingest.db import ensure_indexes, upsert_activity
from ingest.models import Activity, GeoPoint, SourceRef


def make(source="ticketmaster", sid="abc", **kw):
    return Activity(
        kind="event",
        city="atlanta",
        name=kw.pop("name", "Show"),
        location=GeoPoint.at(33.77, -84.39),
        timezone="America/New_York",
        start=datetime(2026, 9, 27, 23, tzinfo=timezone.utc),
        sourceKeys=[f"{source}:{sid}"],
        sources=[SourceRef(name=source, id=sid, fetchedAt=datetime.now(timezone.utc))],
        **kw,
    )


def test_indexes_idempotent(test_db):
    assert ensure_indexes(test_db) == ensure_indexes(test_db)


def test_rerun_inserts_nothing(test_db):
    ensure_indexes(test_db)
    assert upsert_activity(test_db, make()) == "inserted"
    assert upsert_activity(test_db, make(name="Show (updated)")) == "updated"
    docs = list(test_db.activities.find())
    assert len(docs) == 1 and docs[0]["name"] == "Show (updated)" and len(docs[0]["sources"]) == 1


def test_update_keeps_enrichment(test_db):
    upsert_activity(test_db, make())
    test_db.activities.update_one({}, {"$set": {"summary": "LLM text", "tags": ["music"], "category": "comedy"}})
    upsert_activity(test_db, make())  # adapter's fallback category is "other"
    doc = test_db.activities.find_one()
    assert doc["summary"] == "LLM text" and doc["tags"] == ["music"] and doc["category"] == "comedy"


def test_merged_doc_only_fills_gaps(test_db):
    upsert_activity(test_db, make(source="eventbrite", sid="e1", name="Eventbrite name"))
    test_db.activities.update_one({}, {"$addToSet": {"sourceKeys": "ticketmaster:abc"}})
    upsert_activity(test_db, make(name="TM name", venueName="The Venue"))
    doc = test_db.activities.find_one()
    assert doc["name"] == "Eventbrite name" and doc["venueName"] == "The Venue"
    assert {s["name"] for s in doc["sources"]} == {"eventbrite", "ticketmaster"}


def test_quota_reserve_stops_at_cap(test_db, monkeypatch):
    monkeypatch.setitem(quota.CAPS, "google_text", ("month", 3))
    assert quota.reserve(test_db, "google_text", 2) == 2
    with pytest.raises(quota.QuotaExceeded):
        quota.reserve(test_db, "google_text", 2)
    assert quota.reserve(test_db, "google_text", 1) == 3
    assert quota.remaining(test_db, "google_text") == 0
