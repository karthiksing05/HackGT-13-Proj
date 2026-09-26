"""Run one adapter: fetch -> normalize -> locate (geocode) -> region -> duration -> upsert,
recorded in `runs` (§6). Enrichment, dedupe, series detection and embeddings slot in later."""

import json
import logging
import traceback
from datetime import datetime, timedelta, timezone

from pymongo.database import Database

from .adapters import ADAPTERS
from .config import OUT_DIR, City
from .db import upsert_activity
from .enrich import duration
from .enrich.geocode import fill_region, locate, make_geocoder
from .models import Activity, RunStats
from .quota import key_api, remaining

log = logging.getLogger(__name__)

EXPIRY_GRACE = timedelta(hours=6)


class BudgetExceeded(RuntimeError):
    pass


def finalize(act: Activity) -> Activity:
    duration.resolve(act)
    if act.kind == "event" and act.start:
        end = act.end or act.start + timedelta(minutes=act.duration.p75Min)
        act.expiresAt = end + EXPIRY_GRACE
    return act


def run_adapter(
    name: str, city: City, db: Database | None, dry_run: bool = False, limit: int | None = None
) -> RunStats:
    stats = RunStats(source=name, city=city.slug, startedAt=datetime.now(timezone.utc), dryRun=dry_run)
    sample: list[dict] = []
    adapter = None
    try:
        adapter = ADAPTERS[name](city, db=db, dry_run=dry_run)
        geocoder = make_geocoder(city, db)
        est = adapter.estimate()
        if est is not None:
            keys = getattr(adapter, "keys", None)
            _check_budget(est, db, stats, len(keys.keys) if keys else 1)
            if dry_run:
                return stats  # paid API: dry run only estimates, never fetches
        for raw in adapter.fetch():
            stats.fetched += 1
            for act in adapter.normalize(raw):
                if locate(act, geocoder):
                    stats.geocoded += 1
                if act.location is None:
                    adapter.skip("no coordinates and the address didn't geocode")
                    continue
                fill_region(act, city)
                finalize(act)
                if dry_run:
                    sample.append(act.model_dump(mode="json"))
                else:
                    result = upsert_activity(db, act)
                    setattr(stats, result, getattr(stats, result) + 1)
            if limit and stats.fetched >= limit:
                break
    except BudgetExceeded as e:
        stats.errors.append(str(e))
    except Exception as e:  # fail soft (§2.8): log to runs, let run-all continue
        log.debug(traceback.format_exc())
        stats.errors.append(f"{type(e).__name__}: {e}")
    finally:
        if adapter is not None:
            stats.skipped = adapter.skipped
            stats.skipReasons = dict(adapter.skip_reasons)
            stats.errors = adapter.errors + stats.errors
        stats.finishedAt = datetime.now(timezone.utc)
        if dry_run and sample:
            OUT_DIR.mkdir(exist_ok=True)
            path = OUT_DIR / f"{name}_{city.slug}_dryrun.json"
            path.write_text(json.dumps(sample, indent=1))
            log.info("wrote %d normalized docs to %s", len(sample), path)
        if db is not None and not dry_run:
            db.runs.insert_one(stats.model_dump())
    return stats


def _check_budget(est: dict[str, int], db: Database | None, stats: RunStats, n_keys: int = 1) -> None:
    for api, calls in est.items():
        left = sum(remaining(db, key_api(api, i)) for i in range(n_keys)) if db is not None else None
        msg = f"{api}: ~{calls} calls estimated, {left if left is not None else '?'} left this period"
        log.info(msg)
        if left is not None and calls > left:
            raise BudgetExceeded(f"aborting: {msg}")


# ---- backfill + coverage over the whole city (not just this run's records) ----

# Fields the stage may change on an existing doc. Enrichment fields are never touched.
BACKFILL_FIELDS = ("location", "address", "duration", "attendance", "expiresAt")

# (label, filter that means "present", applies-to filter)
COVERAGE: list[tuple[str, dict, dict]] = [
    ("start time", {"start": {"$ne": None}}, {"kind": "event"}),
    ("end time", {"end": {"$ne": None}}, {"kind": "event"}),
    ("coordinates", {"location.coordinates": {"$exists": True}}, {}),
    ("street address", {"$or": [{"address.street": {"$ne": None}}, {"address.formatted": {"$ne": None}}]}, {}),
    ("city + state", {"address.locality": {"$ne": None}, "address.region": {"$ne": None}}, {}),
    ("venue name", {"venueName": {"$ne": None}}, {"kind": "event"}),
    ("opening hours", {"weeklyHours": {"$ne": None}}, {"kind": "place"}),
    ("duration", {"duration.p75Min": {"$ne": None}}, {}),
    ("price tier or free", {"$or": [{"price.tier": {"$ne": None}}, {"price.isFree": {"$ne": None}}]}, {}),
    ("link", {"url": {"$ne": None}}, {}),
    ("blurb", {"blurb.text": {"$exists": True}}, {}),
    ("blurb from web research", {"blurb.grounded": True}, {}),
    ("embedding text", {"embeddingText": {"$exists": True}}, {"kind": "event"}),
]


def backfill(db: Database, city: City) -> dict[str, int]:
    """Fill what's still missing on existing docs: coordinates/address parts (geocoding),
    region (city default), duration and expiry. Source values are never overwritten."""
    geocoder = make_geocoder(city, db)
    counts = {"checked": 0, "updated": 0, "geocoded": 0, "unlocatable": 0}
    for doc in db.activities.find({"city": city.slug}, {"embedding": 0}):
        counts["checked"] += 1
        try:
            act = Activity.model_validate(doc)
        except Exception as e:
            log.warning("skipping malformed doc %s: %s", doc.get("_id"), e)
            continue
        before = act.model_dump(mode="python", include=set(BACKFILL_FIELDS))
        if locate(act, geocoder):
            counts["geocoded"] += 1
        if act.location is None:
            counts["unlocatable"] += 1
            continue
        fill_region(act, city)
        finalize(act)
        after = act.model_dump(mode="python", include=set(BACKFILL_FIELDS))
        changed = {k: v for k, v in after.items() if v != before[k] and v is not None}
        if changed:
            changed["updatedAt"] = datetime.now(timezone.utc)
            db.activities.update_one({"_id": doc["_id"]}, {"$set": changed})
            counts["updated"] += 1
    return counts


def coverage(db: Database, city: City) -> list[tuple[str, int, int]]:
    rows = []
    for label, present, scope in COVERAGE:
        base = {"city": city.slug, **scope}
        total = db.activities.count_documents(base)
        rows.append((label, db.activities.count_documents({**base, **present}), total))
    return rows
