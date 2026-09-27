"""Create `freetime.pitch_activities` and insert the validated export, safely and resumably.

    cd dataingestion
    .venv/bin/python -m pitch.load [--uri mongodb://127.0.0.1:27017/?directConnection=true] [--dry-run]

- Runs `pitch.validate` on the export first and stops if it reports an error.
- A missing collection is created without a validator (neither activities nor demo_activities
  has one) and given demo_activities' indexes under the same names and options. `expiresAt_1` is
  a plain index, never a TTL, so the catalog cannot expire.
- An existing collection is inspected first: every document already in it must be one of the
  export's and identical to it (a resumed run); anything else stops the load before any write.
  Nothing is ever dropped, replaced or deleted.
- `--update-fields` is the one exception to "identical": a stored document may differ from the export
  in UPDATABLE fields only (ticket links, descriptions, tags). Those are set, guarded by the stored
  values, and an embedding text the change made stale is removed, to be rewritten.
- Only the export's missing documents are inserted, unordered, in batches; each batch's inserted
  _ids are appended to pitch/out/insert_log.json, so a retry after a partial failure inserts only
  what is still missing.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from collections import Counter
from datetime import datetime, timezone
from urllib.parse import urlsplit

import bson
from pymongo import ASCENDING, GEOSPHERE, MongoClient
from pymongo.errors import BulkWriteError

from .generate import DEFAULT_URI, OUT
from .validate import EXPECTED_INDEXES, load_export

COLLECTION = "pitch_activities"
TEXT_FIELDS = ("embeddingText", "embeddingTextHash", "embeddingTextMeta")
UPDATABLE = {"ticketUrl", "description", "summary", "tags"}


def without_texts(d: dict) -> dict:
    return {k: v for k, v in d.items() if k not in TEXT_FIELDS}
LOG = OUT / "insert_log.json"
BATCH = 50

INDEXES = [  # demo_activities' indexes, same names and options, no TTL
    ([("location", GEOSPHERE)], {"name": "location_2dsphere"}),
    ([("sourceKeys", ASCENDING)], {"name": "sourceKeys_1", "unique": True}),
    ([("city", ASCENDING), ("kind", ASCENDING), ("start", ASCENDING)], {"name": "city_1_kind_1_start_1"}),
    ([("category", ASCENDING)], {"name": "category_1"}),
    ([("recurrence.seriesKey", ASCENDING)], {"name": "recurrence.seriesKey_1", "sparse": True}),
    ([("expiresAt", ASCENDING)], {"name": "expiresAt_1"}),
    ([("name", ASCENDING)], {"name": "name_1"}),
]


def log(event: dict) -> None:
    entries = json.loads(LOG.read_text()) if LOG.exists() else []
    entries.append({"at": datetime.now(timezone.utc).isoformat(), **event})
    LOG.write_text(json.dumps(entries, indent=1, default=str) + "\n")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--uri", default=DEFAULT_URI)
    ap.add_argument("--dry-run", action="store_true", help="inspect and report; write nothing")
    ap.add_argument("--partial-texts", action="store_true",
                    help="allow documents without an embedding text yet (they get one later with --update-texts)")
    ap.add_argument("--update-fields", action="store_true",
                    help=f"also update stored documents that differ from the export only in {sorted(UPDATABLE)}")
    ap.add_argument("--update-texts", action="store_true",
                    help="also set the embedding texts the export now has on documents already in the collection")
    args = ap.parse_args()
    host = urlsplit(args.uri).hostname or "?"

    check = subprocess.run([sys.executable, "-m", "pitch.validate"] + (["--partial-texts"] if args.partial_texts else []),
                           capture_output=True, text=True)
    if check.returncode != 0:
        sys.exit("validation failed; nothing written:\n" + check.stdout[-2000:])
    print(check.stdout.strip().splitlines()[-1])

    docs = load_export()
    by_id = {d["_id"]: d for d in docs}
    db = MongoClient(args.uri, tz_aware=True, serverSelectionTimeoutMS=10000)["freetime"]
    exists = COLLECTION in db.list_collection_names()
    coll = db[COLLECTION]
    present, text_updates, field_updates = [], [], []
    if exists:
        for d in coll.find({}):
            mine = by_id.get(d["_id"])
            if mine is None:
                sys.exit(f"{COLLECTION} holds a document that is not in the export ({d['_id']}, {d.get('name')!r}); stopping without writing")
            if bson.encode(without_texts(d)) != bson.encode(without_texts(mine)):
                changed = {k for k in set(d) | set(mine) if k not in TEXT_FIELDS
                           and bson.encode({"v": d.get(k)}) != bson.encode({"v": mine.get(k)})}
                if not (args.update_fields and changed <= UPDATABLE):
                    sys.exit(f"{COLLECTION} holds a different version of {d.get('name')!r} ({sorted(changed)}); "
                             "stopping without writing" + ("" if changed <= UPDATABLE else "; only --update-fields fields may differ"))
                field_updates.append((d, mine, sorted(changed)))
            if mine.get("embeddingText") and mine.get("embeddingTextHash") != d.get("embeddingTextHash"):
                text_updates.append((d, mine))
            present.append(d["_id"])
        for i in coll.list_indexes():
            if "expireAfterSeconds" in i:
                sys.exit(f"{COLLECTION} has a TTL index ({i['name']}); remove it before loading")
    missing = [d for d in docs if d["_id"] not in set(present)]
    print(f"{COLLECTION} {'exists' if exists else 'does not exist'} on {host}; {len(present)} of {len(docs)} already there, "
          f"{len(missing)} to insert, {len(text_updates)} with a newer embedding text in the export, "
          f"{len(field_updates)} with updated fields ({dict(Counter(k for *_, ks in field_updates for k in ks))})")
    if args.dry_run:
        return
    if text_updates and not args.update_texts:
        print("rerun with --update-texts to set those texts")

    if not exists:
        db.create_collection(COLLECTION)
        log({"step": "create_collection", "db": "freetime", "collection": COLLECTION, "host": host, "validator": None})
    have = {i["name"] for i in coll.list_indexes()}
    for keys, opts in INDEXES:
        if opts["name"] not in have:
            coll.create_index(keys, **opts)
            log({"step": "create_index", "name": opts["name"], "keys": keys, "options": opts})
    for i in coll.list_indexes():
        want = EXPECTED_INDEXES.get(i["name"])
        if want is None or dict(i["key"]) != want[0] or "expireAfterSeconds" in i:
            sys.exit(f"unexpected index {i['name']}: {dict(i)}")

    inserted = 0
    for start in range(0, len(missing), BATCH):
        chunk = missing[start : start + BATCH]
        try:
            res = coll.insert_many(chunk, ordered=False)
            ids = [str(x) for x in res.inserted_ids]
            log({"step": "insert", "inserted": ids})
            inserted += len(ids)
        except BulkWriteError as e:
            failed = {chunk[w["index"]]["_id"] for w in e.details.get("writeErrors", [])}
            ok = [str(d["_id"]) for d in chunk if d["_id"] not in failed]
            log({"step": "insert", "inserted": ok, "failed": [str(x) for x in failed],
                 "errors": [w.get("errmsg") for w in e.details.get("writeErrors", [])][:10]})
            sys.exit(f"batch at {start}: {len(ok)} inserted, {len(failed)} failed; rerun to insert only what is missing")
    if field_updates:
        done = stale = 0
        for d, mine, changed in field_updates:
            guard = {"_id": d["_id"], **{k: d.get(k) for k in changed}}  # only while the stored values are the ones read
            update = {"$set": {k: mine.get(k) for k in changed}}
            if d.get("embeddingTextHash") and not mine.get("embeddingText"):
                update["$unset"] = {k: "" for k in TEXT_FIELDS}  # written for the old fields: rewrite it
            res = coll.update_one(guard, update)
            done += res.modified_count
            stale += res.modified_count and "$unset" in update
        log({"step": "update_fields", "updated": done, "planned": len(field_updates), "stale_texts_removed": stale})
        print(f"updated fields on {done} of {len(field_updates)} documents; removed {stale} stale embedding texts")
    if args.update_texts and text_updates:
        done = 0
        for d, mine in text_updates:
            # Guarded: only while the stored text is still the one read above.
            guard = {"_id": d["_id"], "embeddingTextHash": d["embeddingTextHash"]} if d.get("embeddingTextHash") \
                else {"_id": d["_id"], "embeddingTextHash": {"$exists": False}}
            res = coll.update_one(guard, {"$set": {k: mine[k] for k in TEXT_FIELDS}})
            done += res.modified_count
        log({"step": "update_texts", "updated": done, "planned": len(text_updates)})
        print(f"set embedding texts on {done} of {len(text_updates)} documents")
    print(f"inserted {inserted}; {COLLECTION} now holds {coll.count_documents({})} documents, "
          f"{coll.count_documents({'embeddingTextHash': {'$type': 'string'}})} with an embedding text")


if __name__ == "__main__":
    main()
