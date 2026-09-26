"""Quota counter for limited APIs (§7). Shared across cities."""

from datetime import datetime, timezone

from pymongo import ReturnDocument

from .config import load_global
from pymongo.database import Database
from pymongo.errors import DuplicateKeyError

# api -> (period, cap)
CAPS: dict[str, tuple[str, int]] = {
    "google_nearby": ("month", 900),
    "google_text": ("month", 900),
    "google_geocode": ("month", 9000),
    "google_route_matrix": ("month", 9000),
    "serpapi": ("month", 200),
    "opentopodata": ("day", 1000),  # public API limit
    "gemini_grounded": ("day", load_global().get("blurb", {}).get("grounded_daily_cap", 200)),
    "muse_research": ("day", load_global().get("blurb", {}).get("muse", {}).get("daily_cap", 300)),
}


class QuotaExceeded(RuntimeError):
    pass


def period_key(api: str, now: datetime | None = None) -> str:
    now = now or datetime.now(timezone.utc)
    period, _ = CAPS[api]
    return now.strftime("%Y-%m") if period == "month" else now.strftime("%Y-%m-%d")


def _ensure_row(db: Database, api: str, period: str) -> None:
    try:
        db.quota.update_one(
            {"api": api, "period": period},
            {"$setOnInsert": {"used": 0, "cap": CAPS[api][1]}},
            upsert=True,
        )
    except DuplicateKeyError:
        pass  # a concurrent reserve created it


def reserve(db: Database, api: str, n: int = 1) -> int:
    """Atomically take n calls from the current period. Returns the new used count."""
    period = period_key(api)
    _ensure_row(db, api, period)
    cap = CAPS[api][1]
    doc = db.quota.find_one_and_update(
        {"api": api, "period": period, "used": {"$lte": cap - n}},
        {"$inc": {"used": n}},
        return_document=ReturnDocument.AFTER,
    )
    if doc is None:
        raise QuotaExceeded(f"{api}: reserving {n} would exceed the {cap} cap for {period}")
    return doc["used"]


def remaining(db: Database, api: str) -> int:
    doc = db.quota.find_one({"api": api, "period": period_key(api)})
    return CAPS[api][1] - (doc["used"] if doc else 0)


def usage(db: Database) -> list[dict]:
    return [
        {"api": api, "period": period_key(api), "used": CAPS[api][1] - remaining(db, api), "cap": cap}
        for api, (_, cap) in CAPS.items()
    ]
