#!/usr/bin/env python3
"""Build saltlight_2026-09-26.json from the Saltlight catalog in MongoDB.

The catalog of record is MongoDB's demo_activities: 100 documents, each with
its embeddingText and the 1024-d Qwen/Qwen3-Embedding-0.6B vector the ML
service stores for it (dataingestion/demo/saltlight_harbor.json has neither).
The fixture keeps both, so the tests rank the real catalog in the real
embedding space; test users, search vectors and synthetic activities are
built from these vectors (see helpers_test.go).

The database is only read. Go's models.Activity decodes plain JSON, so ids
become hex strings and dates RFC 3339. Fields the planner never reads
(description, sources, the embedding metadata, recurrence) are dropped. The
output is deterministic: documents sorted by _id, keys sorted, vectors
rounded to 6 decimals and written one per line.

Run from the repo root, with the ML virtualenv (it has pymongo):

    ml/.venv/bin/python Backend/pkg/planner/testdata/make_fixture.py

MONGO_URI (default mongodb://127.0.0.1:27017), MONGO_DB (freetime) and
MONGO_COLLECTION (demo_activities) point it elsewhere.
"""
import datetime
import json
import os
import pathlib

import pymongo
from bson import ObjectId

DST = pathlib.Path(__file__).resolve().parent / "saltlight_2026-09-26.json"

MODEL = "Qwen/Qwen3-Embedding-0.6B"
DIM = 1024
DECIMALS = 6

DROP = {"description", "sources", "sourceKeys", "embeddingTextHash", "embeddingTextMeta",
        "embeddingMeta", "embeddingModel", "recurrence"}


def plain(value):
    if isinstance(value, ObjectId):
        return str(value)
    if isinstance(value, datetime.datetime):
        if value.tzinfo is not None:
            value = value.astimezone(datetime.timezone.utc).replace(tzinfo=None)
        if value.microsecond:
            return value.strftime("%Y-%m-%dT%H:%M:%S.") + f"{value.microsecond // 1000:03d}Z"
        return value.strftime("%Y-%m-%dT%H:%M:%SZ")
    if isinstance(value, dict):
        return {k: plain(v) for k, v in value.items() if k not in DROP}
    if isinstance(value, list):
        return [plain(v) for v in value]
    return value


def check(doc):
    name = doc.get("name", doc["_id"])
    vec = doc.get("embedding")
    if doc.get("embeddingModel") != MODEL or not vec or len(vec) != DIM:
        raise SystemExit(f"{name}: expected a {DIM}-d {MODEL} vector, got {len(vec or [])}-d {doc.get('embeddingModel')!r}")
    if not (doc.get("embeddingText") or "").strip():
        raise SystemExit(f"{name}: no embeddingText")


def main():
    uri = os.environ.get("MONGO_URI", "mongodb://127.0.0.1:27017")
    db = os.environ.get("MONGO_DB", "freetime")
    coll = os.environ.get("MONGO_COLLECTION", "demo_activities")
    client = pymongo.MongoClient(uri, serverSelectionTimeoutMS=5000)
    docs = list(client[db][coll].find({}).sort("_id", pymongo.ASCENDING))
    if not docs:
        raise SystemExit(f"{db}.{coll} is empty")
    out = []
    vectors = {}
    for doc in docs:
        check(doc)
        d = plain(doc)
        # Written compactly after the rest (one line per vector), below.
        vectors[d["_id"]] = [round(x, DECIMALS) + 0.0 for x in doc["embedding"]]
        d["embedding"] = "@vector:" + d["_id"]
        out.append(d)
    text = json.dumps(out, indent=1, sort_keys=True)
    for id_, vec in vectors.items():
        text = text.replace(json.dumps("@vector:" + id_), json.dumps(vec, separators=(",", ":")))
    DST.write_text(text + "\n")
    print(f"wrote {len(out)} activities ({DIM}-d {MODEL} vectors) from {db}.{coll} to {DST}")


if __name__ == "__main__":
    main()
