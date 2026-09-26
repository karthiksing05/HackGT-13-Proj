"""Mongo client, indexes (§4.2) and the idempotent activity upsert."""

import logging
from datetime import datetime, timezone
from functools import lru_cache

from pymongo import ASCENDING, GEOSPHERE, MongoClient
from pymongo.database import Database

from .config import env
from .models import Activity

log = logging.getLogger(__name__)

# Written by later pipeline stages (enrich, embed, blurb, series detection);
# an adapter rerun must never clobber them.
ENRICHMENT_FIELDS = {
    "summary", "tags", "recurrence", "blurb", "embedding", "embeddingModel",
    "embeddingText", "embeddingTextHash", "embeddingTextMeta",
}


@lru_cache
def get_db() -> Database:
    uri = env("MONGODB_URI", "mongodb://localhost:27017")
    client = MongoClient(uri, tz_aware=True, serverSelectionTimeoutMS=5000)
    return client[env("MONGODB_DB", "freetime")]


def ensure_indexes(db: Database) -> list[str]:
    """Idempotent: create_index is a no-op when the same index already exists.
    The Atlas Vector Search index (`activities_embedding`) is created once the
    embedding model and dimension are decided (§11)."""
    acts = db.activities
    names = [
        acts.create_index([("location", GEOSPHERE)]),
        acts.create_index([("sourceKeys", ASCENDING)], unique=True),
        acts.create_index([("city", ASCENDING), ("kind", ASCENDING), ("start", ASCENDING)]),
        acts.create_index([("category", ASCENDING)]),
        acts.create_index([("recurrence.seriesKey", ASCENDING)], sparse=True),
        acts.create_index([("expiresAt", ASCENDING)], expireAfterSeconds=0),
        db.runs.create_index([("source", ASCENDING), ("startedAt", ASCENDING)]),
        db.quota.create_index([("api", ASCENDING), ("period", ASCENDING)], unique=True),
    ]
    return names


def upsert_activity(db: Database, act: Activity, now: datetime | None = None) -> str:
    """Insert, or update the doc that already holds any of act.sourceKeys.

    Returns "inserted" or "updated". When the existing doc also has data from
    other sources (after a dedupe merge), this adapter only fills fields that are
    still empty; field precedence between sources belongs to dedupe (§6.4).
    """
    now = now or datetime.now(timezone.utc)
    doc = act.model_dump(mode="python")
    existing = db.activities.find_one({"sourceKeys": {"$in": act.sourceKeys}})

    if existing is None:
        doc.update(createdAt=now, updatedAt=now)
        db.activities.insert_one(doc)
        return "inserted"

    incoming_sources = {s["name"] for s in doc["sources"]}
    other_sources = {s["name"] for s in existing.get("sources", [])} - incoming_sources

    sets = {}
    for field, value in doc.items():
        if field in ENRICHMENT_FIELDS or field in ("sourceKeys", "sources") or value is None:
            continue
        if field == "category" and value == "other" and existing.get("category"):
            continue  # don't undo a classifier's decision with our fallback
        if other_sources and existing.get(field) is not None:
            continue
        sets[field] = value

    # Replace this source's entries, keep everyone else's.
    incoming_ids = {(s["name"], s["id"]) for s in doc["sources"]}
    sources = [s for s in existing.get("sources", []) if (s["name"], s["id"]) not in incoming_ids]
    sets["sources"] = sources + doc["sources"]
    sets["updatedAt"] = now

    db.activities.update_one(
        {"_id": existing["_id"]},
        {"$set": sets, "$addToSet": {"sourceKeys": {"$each": act.sourceKeys}}},
    )
    return "updated"
