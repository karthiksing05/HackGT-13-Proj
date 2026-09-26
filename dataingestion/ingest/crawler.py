"""Scheduled crawler (§8): look for new activities and run only those through the pipeline.

Each tier is a set of sources checked on its own interval (config.yaml `crawler.tiers`):
listing sites that add events all day (Ticketmaster, Resident Advisor) every hour, places and
trails, which rarely change, every week. A due tier fetches its sources with `only_new`, so
activities already in the DB (by source key, or another source's listing of the same event)
are left alone, and only the new ones are researched and written up (blurb, embedding text).

Last-run times live in Mongo (`crawl_state`), so a restarted crawler doesn't redo a weekly
tier early. New activities whose writing failed (e.g. Gemini quota) are retried on later ticks
for `retry_hours`; nothing older is picked up, so the crawler never re-spends on the backlog.
"""

import logging
import time
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone

from bson import ObjectId
from pymongo.database import Database

from .adapters import ADAPTERS
from .agent.research import CLAUDE, Researcher, _showings, facts_packet, input_hash
from .config import City, load_city, load_global
from .db import ensure_indexes
from .models import RunStats
from .pipeline import run_adapter

log = logging.getLogger(__name__)


@dataclass
class Tier:
    name: str
    sources: list[str]
    every: timedelta
    write: bool  # research + blurb/embedding text for its new activities
    kinds: list[str] = field(default_factory=lambda: ["event"])


def tiers(cfg: dict) -> list[Tier]:
    return [
        Tier(name, t["sources"], timedelta(minutes=t["every_minutes"]), t.get("write", True), t.get("write_kinds", ["event"]))
        for name, t in cfg["tiers"].items()
    ]


def last_run(db: Database, city: str, tier: str) -> datetime | None:
    doc = db.crawl_state.find_one({"city": city, "tier": tier})
    return doc["lastRunAt"] if doc else None


def due(db: Database, city: str, tier: Tier, now: datetime) -> bool:
    last = last_run(db, city, tier.name)
    return last is None or now - last >= tier.every


def mark_run(db: Database, city: str, tier: str, when: datetime, new: int) -> None:
    db.crawl_state.update_one({"city": city, "tier": tier},
                              {"$set": {"lastRunAt": when, "lastNew": new}}, upsert=True)


def reuse_showing_research(db: Database, doc: dict, provider: str) -> bool:
    """A new showing of a show we already researched (same series, or same name at the same
    venue) gets that research copied, keyed to its own facts hash, instead of a new web search."""
    h = input_hash(facts_packet(doc))
    key = {"activityId": doc["_id"], "inputHash": h, "provider": provider}
    if db.research.find_one(key):
        return True
    others = [s["_id"] for s in _showings(db, doc) if s["_id"] != doc["_id"]]
    if not others:
        return False
    prior = db.research.find_one({"activityId": {"$in": others}, "provider": provider}, sort=[("createdAt", -1)])
    if not prior:
        return False
    copy = {k: v for k, v in prior.items() if k != "_id"}
    db.research.replace_one(key, {**copy, **key, "copiedFrom": prior["activityId"],
                                  "createdAt": datetime.now(timezone.utc)}, upsert=True)
    return True


def pending(db: Database, city: str, kinds: list[str], ids: list[ObjectId], retry: timedelta,
            marker: str, now: datetime) -> list[dict]:
    """This tick's new activities, plus recent new ones still missing their write-up."""
    recent = {"createdAt": {"$gte": now - retry}, marker: {"$exists": False},
              "$or": [{"start": None}, {"start": {"$gte": now}}]}
    q = {"city": city, "kind": {"$in": kinds}, "$or": [{"_id": {"$in": ids}}, recent]}
    return list(db.activities.find(q, {"embedding": 0}).sort([("start", 1), ("ratingCount", -1)]))


def write_up(db: Database, city: City, docs: list[dict], cfg: dict, echo=print) -> dict[str, int]:
    """Research + blurb + embedding text for these docs only, failing soft per activity."""
    from concurrent.futures import ThreadPoolExecutor

    from .agent.blurb import BlurbAgent
    from .agent.embed_text import EmbedTextAgent

    agent_cfg = load_global()["blurb"]
    provider = agent_cfg.get("research_provider")
    counts = {"reused_research": 0, "written": 0, "skipped": 0, "failed": 0}
    for doc in docs:
        counts["reused_research"] += reuse_showing_research(db, doc, provider)
    if provider == CLAUDE:
        # `claude` notes only come from `ingest research import`; write what has notes.
        have = set(db.research.distinct("activityId", {"provider": CLAUDE, "activityId": {"$in": [d["_id"] for d in docs]}}))
        missing = len(docs) - len(have)
        docs = [d for d in docs if d["_id"] in have]
        if missing:
            echo(f"    {missing} new activities need Claude research: `ingest research todo --city {city.slug}`")

    researcher = Researcher(db, agent_cfg, city.name, city=city)
    agents = []
    if cfg.get("blurbs", True):
        agents.append(BlurbAgent(db, agent_cfg, city.name, researcher=researcher))
    if cfg.get("embed_text", True):
        agents.append(EmbedTextAgent(db, agent_cfg, city.name, researcher=researcher))

    def attempt(agent, doc):
        try:
            return agent.run(doc).status
        except Exception as e:  # one bad activity doesn't stop the tick
            log.warning("%s on %s: %s: %s", type(agent).__name__, doc["name"][:50], type(e).__name__, e)
            return "failed"

    with ThreadPoolExecutor(max_workers=max(1, cfg.get("workers", 1))) as pool:
        for agent in agents:
            for status in pool.map(lambda d: attempt(agent, d), docs):
                counts[status] += 1
    if researcher.grounded_off:
        echo(f"    web research stopped early: {researcher.grounded_off}")
    return counts


def _stats_line(s: RunStats) -> str:
    line = f"    {s.source}: fetched={s.fetched} new={s.inserted} known={s.known} same-event={s.linked} skipped={s.skipped}"
    return line + "".join(f"\n      error: {e}" for e in s.errors)


def tick(db: Database, cfg: dict, cities: list[str], force: set[str] | None = None, echo=print) -> dict[str, int]:
    """Run every due tier for every city once. Returns new activities per city."""
    force = force or set()
    new_by_city: dict[str, int] = {}
    for slug in cities:
        city = load_city(slug)
        for tier in tiers(cfg):
            now = datetime.now(timezone.utc)
            if tier.name not in force and not due(db, city.slug, tier, now):
                continue
            echo(f"[{now.astimezone():%Y-%m-%d %H:%M}] {city.slug}/{tier.name}")
            new_ids: list[ObjectId] = []
            for name in tier.sources:
                if name not in ADAPTERS:
                    echo(f"    unknown source {name!r}; skipping")
                    continue
                if not city.adapters.get(name, False):
                    continue  # not enabled in cities/<slug>.yaml
                stats = run_adapter(name, city, db, only_new=True)
                new_ids += [ObjectId(i) for i in stats.newIds]
                echo(_stats_line(stats))
            new_by_city[city.slug] = new_by_city.get(city.slug, 0) + len(new_ids)
            if tier.write and (cfg.get("blurbs", True) or cfg.get("embed_text", True)):
                marker = "embeddingText" if cfg.get("embed_text", True) else "blurb"
                docs = pending(db, city.slug, tier.kinds, new_ids, timedelta(hours=cfg["retry_hours"]), marker, now)
                if docs:
                    counts = write_up(db, city, docs, cfg, echo)
                    echo("    write-up (%d activities): %s" % (len(docs), " ".join(f"{k}={v}" for k, v in counts.items())))
            mark_run(db, city.slug, tier.name, now, len(new_ids))
    return new_by_city


def next_due(db: Database, cfg: dict, cities: list[str]) -> datetime:
    now = datetime.now(timezone.utc)
    times = [(last_run(db, c, t.name) or now) + t.every for c in cities for t in tiers(cfg)]
    return max(now, min(times))


def run_forever(db: Database, cities: list[str], force: set[str] | None = None, echo=print) -> None:
    cfg = load_global()["crawler"]
    ensure_indexes(db)
    while True:
        try:
            tick(db, cfg, cities, force, echo)
        except Exception as e:  # e.g. Mongo blip: log it and try again next tick
            log.exception("crawl tick failed: %s", e)
        force = None  # --now only applies to the first tick
        wake = next_due(db, cfg, cities)
        echo(f"next check at {wake.astimezone():%Y-%m-%d %H:%M}")
        time.sleep(max(60.0, (wake - datetime.now(timezone.utc)).total_seconds()))
