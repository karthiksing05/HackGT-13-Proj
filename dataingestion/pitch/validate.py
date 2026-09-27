"""Validate the pitch catalog: the export before insertion, or the collection after it.

    cd dataingestion
    .venv/bin/python -m pitch.validate                    # pitch/out/pitch_activities.json
    .venv/bin/python -m pitch.validate --mongo [URI]      # also freetime.pitch_activities (read-only)

Checks the brief's list: counts (250; 150 paid / 100 free by the app's own price reading; the
planning groups), schema and BSON types against demo_activities (pitch/demo_schema.json, refreshed
from the database when --mongo is given), the pipeline's pydantic model, vocabularies, uniqueness,
geography and fidelity to the real venue records, times (UTC storage, local plausibility, midnight
crossings, durations, expiry, recurrence), prices against flags and tags, embedding texts (hash,
prompt, input hash, format) and that the export holds no vector fields. With --mongo it also recounts the
collection, compares every stored document with the export byte for byte (BSON) and checks the
indexes (no TTL). Exit status 1 when anything fails; warnings are listed but do not fail.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import re
import sys
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
from zoneinfo import ZoneInfo

import bson
from bson import ObjectId, json_util

from ingest.agent.embed_text import check as text_check
from ingest.models import Activity

from . import specs
from .generate import (DEFAULT_URI, EXPORT, MANIFEST, RANGE, RECORDS_CACHE, TEXT_PROMPT, TZ, WEEK_MIN,
                       text_input)
from .texts import strict

HERE = EXPORT.parent.parent
DEMO_SCHEMA = HERE / "demo_schema.json"
REPORT = EXPORT.parent / "validation_report.json"

CATEGORIES = {"museum", "gallery", "park", "garden", "zoo_aquarium", "landmark", "viewpoint", "hike", "cafe", "restaurant",
              "bar", "nightclub", "live_music", "comedy", "theater", "cinema", "sports_event", "rec_venue", "market",
              "shopping", "festival", "class_workshop", "tour", "community_event", "other"}
TAGS = {"indoor", "outdoor", "free", "cheap", "splurge", "low_energy", "medium_energy", "high_energy", "solo_friendly",
        "date", "group", "late_night", "daytime", "family", "21_plus", "food", "drinks", "music", "art", "nature",
        "active", "learning", "touristy", "local_favorite"}
PLACE_CATEGORIES = {"park", "hike", "bar", "landmark", "museum", "gallery", "garden", "shopping", "rec_venue",
                    "zoo_aquarium", "market", "viewpoint", "tour", "restaurant", "cafe", "bakery", "dessert",
                    "food_hall", "brewery", "nightclub"}  # Backend/pkg/itinerary/nodes.go placeCategories
PLACEHOLDER_WORDS = ("lorem", "ipsum", "tbd", "placeholder", "todo", "xxx", "n/a", "unknown")
TIME_WINDOWS = {  # (group, category) -> allowed local start hours [from, to)
    ("nightlife", "nightclub"): (15, 24), ("nightlife", "community_event"): (16, 21),
    ("music", "live_music"): (11, 23),
    ("dinner", "restaurant"): (17, 20), ("brunch", "restaurant"): (9, 14), ("brunch", "community_event"): (7, 11),
    ("brunch", "tour"): (8, 12), ("outdoor", "tour"): (7, 18), ("culture", "tour"): (10, 19),
    ("culture", "gallery"): (11, 20), ("culture", "museum"): (10, 22), ("classes", "class_workshop"): (9, 20),
    ("active", "sports_event"): (6, 20), ("active", "rec_venue"): (9, 22), ("active", "class_workshop"): (7, 20),
    ("active", "tour"): (8, 17), ("active", "community_event"): (7, 20), ("stage", "theater"): (11, 21),
    ("stage", "comedy"): (18, 23), ("stage", "cinema"): (18, 24), ("community", "market"): (7, 18),
    ("community", "festival"): (10, 18), ("community", "community_event"): (8, 19),
}


class Report:
    def __init__(self):
        self.errors: list[str] = []
        self.warnings: list[str] = []
        self.facts: dict = {}

    def error(self, msg: str):
        self.errors.append(msg)

    def warn(self, msg: str):
        self.warnings.append(msg)


# ------------------------------------------------------------------------ helpers
def bson_type(v) -> str:
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "bool"
    if isinstance(v, bson.int64.Int64):
        return "long"
    if isinstance(v, int):
        return "int" if -2**31 <= v < 2**31 else "long"
    if isinstance(v, float):
        return "double"
    if isinstance(v, str):
        return "string"
    if isinstance(v, datetime):
        return "date"
    if isinstance(v, ObjectId):
        return "objectId"
    if isinstance(v, list):
        return "array"
    if isinstance(v, dict):
        return "object"
    return type(v).__name__


def inventory(docs) -> dict[str, set]:
    out: dict[str, set] = defaultdict(set)

    def walk(v, path):
        out[path].add(bson_type(v))
        if isinstance(v, dict):
            for k, x in v.items():
                walk(x, f"{path}.{k}" if path else k)
        elif isinstance(v, list):
            for x in v:
                walk(x, path + "[]")
    for d in docs:
        for k, v in d.items():
            walk(v, k)
    return out


def app_cost(doc) -> tuple[int, bool]:
    """Backend/pkg/planner/price.go activityCost: (cents, known)."""
    p = doc.get("price")
    if p is None:
        return 0, False
    if p.get("min") is not None and not math.isnan(p["min"]) and not math.isinf(p["min"]):
        return (0, True) if p["min"] <= 0 else (int(round(p["min"] * 100)), True)
    if p.get("isFree"):
        return 0, True
    if (p.get("cents") or 0) > 0:
        return p["cents"], True
    return 0, False


def tier_for(amount: float) -> int:
    for i, bound in enumerate([0, 15, 40, 80]):
        if amount <= bound:
            return i
    return 4


def local(dt: datetime) -> datetime:
    return dt.astimezone(TZ)


def load_export() -> list[dict]:
    return json_util.loads(EXPORT.read_text(), json_options=json_util.JSONOptions(tz_aware=True, tzinfo=timezone.utc))


# ------------------------------------------------------------------------ checks
def check_counts(docs, manifest, r: Report):
    r.facts["documents"] = len(docs)
    if len(docs) != 250:
        r.error(f"{len(docs)} documents, expected 250")
    paid = free = unknown = 0
    for d in docs:
        cents, known = app_cost(d)
        if not known:
            unknown += 1
        elif cents > 0:
            paid += 1
        else:
            free += 1
    r.facts["paid_free_by_app_semantics"] = {"paid": paid, "free": free, "unknown": unknown}
    if (paid, free, unknown) != (150, 100, 0):
        r.error(f"paid/free by the planner's activityCost: {paid}/{free} ({unknown} unknown), expected 150/100")
    by_key = {m["sourceKey"]: m for m in manifest["documents"]}
    groups = defaultdict(Counter)
    for d in docs:
        m = by_key.get(d["sourceKeys"][0])
        if not m:
            r.error(f"{d['name']}: not in the manifest")
            continue
        groups[m["group"]]["paid" if app_cost(d)[0] > 0 else "free"] += 1
    table = {}
    for g, (paid_t, free_t) in specs.GROUP_TARGETS.items():
        got = (groups[g]["paid"], groups[g]["free"])
        table[g] = {"paid": got[0], "free": got[1], "total": sum(got), "target": [paid_t, free_t]}
        if got != (paid_t, free_t):
            r.error(f"group {g}: {got[0]} paid / {got[1]} free, expected {paid_t} / {free_t}")
    r.facts["groups"] = table
    r.facts["kinds"] = dict(Counter(d["kind"] for d in docs))
    r.facts["categories"] = dict(sorted(Counter(f"{d['kind']}:{d['category']}" for d in docs).items()))


def check_schema(docs, demo_inv: dict[str, set] | None, r: Report, texts: bool = True):
    fields = set(Activity.model_fields)
    for d in docs:
        try:
            Activity.model_validate({k: v for k, v in d.items() if k in fields})
        except Exception as e:  # pydantic's message names the field
            r.error(f"{d['name']}: fails ingest.models.Activity: {str(e)[:300]}")
        for f in ("embedding", "embeddingModel", "embeddingMeta"):
            if f in d:
                r.error(f"{d['name']}: has {f}; vectors live only in the database, never in the export")
    inv = inventory(docs)
    r.facts["schema_paths"] = {p: sorted(t) for p, t in sorted(inv.items()) if not p.startswith(("trail.geometry.coordinates[][]", "embedding[]"))}
    if demo_inv is None:
        r.warn("no demo_activities schema to compare with (run once with --mongo)")
        return
    allowed_extra = {  # path -> types pitch may add to demo's, with the reason
        "url": ({"null"}, "events have no web page (synthetic listings)"),
        "address": ({"null"}, "OpenStreetMap trails have no street address"),
        "address.street": ({"null"}, "some real Google records have no street"),
        "address.formatted": ({"null"}, "as in the real records"),
        "address.postalCode": ({"null"}, "as in the real records"),
        "venueName": ({"null"}, "some OpenStreetMap trails have no park name"),
        "googlePlaceId": ({"string"}, "the real Google place id of real-venue places"),
        "recurrence": ({"object"}, "weekly series"),
    }
    for path, types in inv.items():
        if path.startswith(("recurrence.", "trail.ascentM", "trail.descentM")) or "[]" in path and path.startswith("recurrence"):
            continue
        demo_types = demo_inv.get(path)
        if demo_types is None:
            if not path.startswith("recurrence"):
                r.error(f"field {path} ({sorted(types)}) does not exist in demo_activities")
            continue
        extra = types - demo_types
        ok_extra, _ = allowed_extra.get(path, (set(), ""))
        if extra - ok_extra:
            r.error(f"field {path}: types {sorted(types)} vs demo_activities {sorted(demo_types)}")
    for path in demo_inv:
        if path.split(".")[0].split("[")[0] in ("embedding", "embeddingMeta", "embeddingModel"):
            continue
        if not texts and path.startswith("embeddingText"):
            continue
        if path not in inv and not path.startswith(("trail.", "recurrence")):
            r.error(f"demo_activities field {path} is missing from the pitch documents")
    top_demo = {p for p in demo_inv if "." not in p and "[" not in p} - {"embedding", "embeddingMeta", "embeddingModel"}
    if not texts:
        top_demo -= {"embeddingText", "embeddingTextHash", "embeddingTextMeta"}
    for d in docs:
        missing = top_demo - set(d)
        if missing:
            r.error(f"{d['name']}: missing top-level fields {sorted(missing)}")
    r.facts["schema_differences_from_demo"] = {p: v[1] for p, v in allowed_extra.items() if p in inv and inv[p] & v[0]}


def check_vocab_and_content(docs, r: Report):
    for d in docs:
        n = d["name"]
        if d["category"] not in CATEGORIES:
            r.error(f"{n}: category {d['category']!r} is outside the vocabulary")
        bad = set(d["tags"]) - TAGS
        if bad:
            r.error(f"{n}: tags outside the vocabulary: {sorted(bad)}")
        if len(set(d["tags"])) != len(d["tags"]):
            r.error(f"{n}: repeated tags")
        t = set(d["tags"])
        if len(t & {"indoor", "outdoor"}) != 1:
            r.error(f"{n}: needs exactly one of indoor/outdoor")
        if len(t & {"low_energy", "medium_energy", "high_energy"}) != 1:
            r.error(f"{n}: needs exactly one energy tag")
        if d["kind"] == "place" and d["category"] not in PLACE_CATEGORIES:
            r.error(f"{n}: a place of category {d['category']} is never scheduled")
        if d["kind"] == "place" and not ((d.get("rating") or 0) >= 4 or (d.get("rating") is None and d["category"] == "hike")):
            r.error(f"{n}: a place rated {d.get('rating')} is dropped by the planner (needs 4+, unrated hikes excepted)")
        for f in ("name", "summary", "description"):
            v = d.get(f)
            if not isinstance(v, str) or not v.strip():
                r.error(f"{n}: empty {f}")
            elif any(w in v.lower().split() for w in PLACEHOLDER_WORDS):
                r.error(f"{n}: {f} looks like a placeholder: {v[:60]}")
        if len(d["summary"].split()) > 25:
            r.error(f"{n}: summary has {len(d['summary'].split())} words (at most 25)")
        if not d["description"].startswith(d["summary"].rstrip(".")):
            r.error(f"{n}: summary is not the description's first sentence")
        if len(d["description"]) > 2000:
            r.error(f"{n}: description over 2000 characters")
        if d.get("imageUrl") is not None:
            r.error(f"{n}: imageUrl must stay null (no approved image exists)")
        m = TICKET_RE.fullmatch(d.get("ticketUrl") or "")
        if d["price"]["isFree"] != (d.get("ticketUrl") is None) or (d.get("ticketUrl") and not m):
            r.error(f"{n}: paid items need a ticketUrl {TICKET_RE.pattern}, free ones none")
        elif m and (m[1] in RESERVED_TICKET_SLUGS or m[1] in saltlight_slugs()):
            r.error(f"{n}: ticket slug {m[1]!r} is reserved or taken by a Saltlight listing on the ticket site")
        if [s["name"] for s in d["sources"]] != ["pitch"] or d["sourceKeys"] != [f"pitch:{d['sources'][0]['id']}"]:
            r.error(f"{n}: provenance must name only the synthetic 'pitch' source")
        if not d["sources"][0]["url"].startswith("https://pitch.sidequestz.example/"):
            r.error(f"{n}: sources[].url must be the synthetic source's reserved .example address")


TICKET_RE = re.compile(r"https://events\.sidequestz\.tech/([a-z0-9]+(?:-[a-z0-9]+)*)/tickets")
RESERVED_TICKET_SLUGS = {"api", "t", "dashboard", "_demo", "healthz", "sandbox", "events", "static", "favicon", "orders"}  # Events/pkg/models


def saltlight_slugs() -> set[str]:
    """Slugs the ticket site already sells (its seeded Saltlight events)."""
    seed = HERE.parents[1] / "Events" / "pkg" / "store" / "seed_data.go"
    return set(re.findall(r'Slug:\s*"([^"]+)"', seed.read_text())) if seed.exists() else set()


def check_tickets(docs, r: Report):
    slugs = Counter(TICKET_RE.fullmatch(d["ticketUrl"])[1] for d in docs if d.get("ticketUrl") and TICKET_RE.fullmatch(d["ticketUrl"]))
    for s_, n in slugs.items():
        if n > 1:
            r.error(f"ticket slug {s_!r} is used by {n} documents")
    r.facts["ticket_links"] = sum(slugs.values())


def check_identity(docs, r: Report):
    ids = [d["_id"] for d in docs]
    keys = [k for d in docs for k in d["sourceKeys"]]
    if len(set(ids)) != len(ids):
        r.error("duplicate _id")
    if len(set(keys)) != len(keys):
        r.error("duplicate sourceKeys")
    for d in docs:
        want = ObjectId(hashlib.sha1(d["sourceKeys"][0].encode()).hexdigest()[:24])
        if d["_id"] != want:
            r.error(f"{d['name']}: _id is not sha1(sourceKey)[:24]")
    triples = Counter((d["name"], d["venueName"], d.get("start")) for d in docs)
    for t, c in triples.items():
        if c > 1:
            r.error(f"duplicate listing: {t}")
    names = Counter(d["name"] for d in docs if d["kind"] == "place")
    for name, c in names.items():
        if c > 1:
            r.error(f"place {name!r} appears {c} times")


def check_geo_and_fidelity(docs, manifest, r: Report):
    recs = {str(x["_id"]): x for x in json_util.loads(RECORDS_CACHE.read_text())}
    by_key = {m["sourceKey"]: m for m in manifest["documents"]}
    refs = Counter()
    for d in docs:
        lng, lat = d["location"]["coordinates"]
        if d["location"]["type"] != "Point" or not (-85.0 <= lng <= -83.9 and 33.4 <= lat <= 34.2):
            r.error(f"{d['name']}: coordinates {lng},{lat} are not [lng, lat] in metro Atlanta")
        m = by_key[d["sourceKeys"][0]]
        rec = recs[m["inspiration"][0]["_id"]]
        if d["kind"] == "place":
            refs[rec["_id"]] += 1
        if d["location"]["coordinates"] != list(rec["location"]["coordinates"]):
            r.error(f"{d['name']}: location differs from its real venue record")
        ra, da = rec.get("address"), d.get("address")
        if (ra is None) != (da is None):
            r.error(f"{d['name']}: address presence differs from the real record")
        elif ra:
            for k in ("street", "locality", "region", "postalCode", "countryCode"):
                if ra.get(k) != da.get(k):
                    r.error(f"{d['name']}: address.{k} differs from the real record")
            if ra.get("formatted") and ra["formatted"] != da["formatted"]:
                r.error(f"{d['name']}: address.formatted differs from the real record")
        if d["kind"] == "place":
            for k in ("rating", "ratingCount", "googlePlaceId", "url"):
                if d.get(k) != rec.get(k):
                    r.error(f"{d['name']}: {k} differs from the real record")
            if rec.get("weeklyHours") and not any(iv["open"] == iv["close"] for iv in rec["weeklyHours"]):
                if d["weeklyHours"] != rec["weeklyHours"]:
                    r.error(f"{d['name']}: weeklyHours differ from the real record")
            if d["category"] == "hike":
                if d["trail"] != rec["trail"] or d["duration"] != rec["duration"]:
                    r.error(f"{d['name']}: trail or duration differs from the real trail record")
        else:
            for k in ("url", "rating", "ratingCount", "googlePlaceId", "weeklyHours", "hoursSource"):
                if d.get(k) is not None:
                    r.error(f"{d['name']}: an event must leave {k} null")
    for ref, c in refs.items():
        if c > 1:
            r.error(f"real venue {ref} backs {c} place documents")


def check_times(docs, manifest, r: Report):
    by_key = {m["sourceKey"]: m for m in manifest["documents"]}
    lo, hi = (datetime.fromisoformat(RANGE[0]).date(), datetime.fromisoformat(RANGE[1]).date())
    per_day, buckets = Counter(), Counter()
    for d in docs:
        n = d["name"]
        try:
            ZoneInfo(d["timezone"])
        except Exception:
            r.error(f"{n}: timezone {d['timezone']!r} is not an IANA zone")
        du = d["duration"]
        if abs(du["p75Min"] - du["medianMin"] * math.exp(0.674 * du["sigma"])) > 0.15:  # trail_model rounds after p75
            r.error(f"{n}: p75Min does not follow the lognormal from medianMin and sigma")
        if d["kind"] == "place":
            if any(d.get(k) is not None for k in ("start", "end", "expiresAt", "recurrence")):
                r.error(f"{n}: a place must leave start, end, expiresAt and recurrence null")
            wh = d.get("weeklyHours") or []
            if not wh or d.get("hoursSource") not in ("google", "osm", "nps", "default"):
                r.error(f"{n}: a place needs weeklyHours and an hoursSource")
            for iv in wh:
                if not (0 <= iv["open"] < WEEK_MIN and 0 <= iv["close"] <= WEEK_MIN and iv["open"] != iv["close"]):
                    r.error(f"{n}: bad hours interval {iv}")
            if d["attendance"] != "drop_in":
                r.error(f"{n}: places are drop_in, as in demo_activities")
            continue
        s, e = d["start"], d["end"]
        if not (isinstance(s, datetime) and isinstance(e, datetime)) or e <= s:
            r.error(f"{n}: needs start < end as dates")
            continue
        ls, le = local(s), local(e)
        if not lo <= ls.date() <= hi:
            r.error(f"{n}: starts {ls:%Y-%m-%d}, outside {RANGE}")
        span = (e - s).total_seconds() / 60
        if le.date() != ls.date() and not (le.date() - ls.date() == timedelta(days=1) and le.hour < 6):
            r.error(f"{n}: crosses midnight into {le:%a %H:%M}")
        if d["attendance"] == "fixed_start":
            if du["source"] != "event_times" or abs(du["medianMin"] - span) > 0.05 or du["sigma"] != 0.1:
                r.error(f"{n}: a fixed start takes its duration from the event times")
            if span > 240:
                r.error(f"{n}: a fixed start longer than four hours")
        elif d["attendance"] == "drop_in":
            if du["source"] != "category_prior" or du["medianMin"] > span:
                r.error(f"{n}: a drop-in's stay is the category prior and fits the window")
        else:
            r.error(f"{n}: attendance {d['attendance']!r}")
        want_exp = (e if e else s + timedelta(minutes=du["p75Min"])) + timedelta(hours=6)
        if d["expiresAt"] != want_exp:
            r.error(f"{n}: expiresAt should be end + 6 h (pipeline.finalize)")
        group = by_key[d["sourceKeys"][0]]["group"]
        win = TIME_WINDOWS.get((group, d["category"]))
        if win and not (win[0] <= ls.hour < win[1]):
            r.error(f"{n}: a {group}/{d['category']} starting {ls:%H:%M} is implausible (allowed {win[0]}:00-{win[1]}:00)")
        elif not win:
            r.warn(f"{n}: no plausibility window for {group}/{d['category']}")
        t = set(d["tags"])
        if "daytime" in t and not (ls.hour >= 6 and le.date() == ls.date() and (le.hour, le.minute) <= (18, 30)):
            r.error(f"{n}: tagged daytime but runs {ls:%H:%M}-{le:%H:%M}")
        late = le.date() > ls.date() or le.hour >= 23
        if late != ("late_night" in t):
            r.error(f"{n}: late_night tag {'missing' if late else 'wrong'} for {ls:%H:%M}-{le:%H:%M}")
        per_day[f"{ls:%a %m-%d}"] += 1
        buckets["morning" if ls.hour < 12 else "afternoon" if ls.hour < 17 else "evening" if ls.hour < 21 else "late night"] += 1
    r.facts["events_per_day"] = dict(sorted(per_day.items(), key=lambda x: datetime.strptime(x[0][4:], "%m-%d")))
    r.facts["event_start_buckets"] = dict(buckets)


def check_recurrence(docs, r: Report):
    series = defaultdict(list)
    for d in docs:
        if d.get("recurrence"):
            series[d["recurrence"]["seriesKey"]].append(d)
    for key, occ in series.items():
        occ.sort(key=lambda x: x["start"])
        rec0 = occ[0]["recurrence"]
        if len(occ) < 2:
            r.error(f"series {key} has one occurrence")
        for d in occ:
            ls = local(d["start"])
            dow = (ls.weekday() + 1) % 7
            rc = d["recurrence"]
            if rc != rec0:
                r.error(f"series {key}: occurrences disagree on the recurrence")
            if rc["daysOfWeek"] != [dow] or rc["localStartTime"] != f"{ls:%H:%M}" or not rc["rule"].endswith(
                    ["SU", "MO", "TU", "WE", "TH", "FR", "SA"][dow]):
                r.error(f"series {key}: {ls:%a %H:%M} does not match the rule {rc['rule']} {rc['localStartTime']}")
            if (d["name"], d["venueName"]) != (occ[0]["name"], occ[0]["venueName"]):
                r.error(f"series {key}: occurrences differ in name or venue")
        for a, b in zip(occ, occ[1:]):
            if b["start"] - a["start"] != timedelta(days=7):
                r.error(f"series {key}: occurrences are not one week apart")
        if rec0["until"] != occ[-1]["start"]:
            r.error(f"series {key}: until is not the last occurrence")
        if rec0["frequency"] != "weekly" or rec0["origin"] != "source_rule":
            r.error(f"series {key}: frequency/origin")
    r.facts["series"] = {k: len(v) for k, v in series.items()}


def check_prices(docs, r: Report):
    tiers = Counter()
    for d in docs:
        n, p, t = d["name"], d["price"], set(d["tags"])
        if not all(isinstance(p.get(k), float) for k in ("min", "max")) or not isinstance(p.get("tier"), int):
            r.error(f"{n}: price.min/max must be doubles and tier an int")
            continue
        if p["currency"] != "USD" or p["max"] < p["min"] or p["min"] < 0:
            r.error(f"{n}: price {p}")
        if p["tier"] != tier_for(p["min"]):
            r.error(f"{n}: tier {p['tier']} does not match ${p['min']:g} on Atlanta's tiers")
        if p["isFree"] != (p["min"] == 0 and p["max"] == 0):
            r.error(f"{n}: isFree disagrees with the amounts")
        if ("free" in t) != p["isFree"]:
            r.error(f"{n}: the free tag disagrees with the price")
        if "cheap" in t and not (p["tier"] == 1 and p["max"] <= 20):
            r.error(f"{n}: cheap tag on ${p['min']:g}-${p['max']:g}")
        if ("splurge" in t) != (p["tier"] >= 3 or p["max"] >= 90):
            r.error(f"{n}: splurge tag disagrees with ${p['min']:g}-${p['max']:g}")
        desc = d["description"].lower()
        if p["isFree"] and not any(w in desc for w in ("free", "no cover", "donation")):
            r.error(f"{n}: a free listing should say so in its description")
        if not p["isFree"] and "free admission" in desc and "$" not in desc:
            r.error(f"{n}: a paid listing's description says free admission")
        if not p["isFree"] and "$" not in d["description"]:
            r.warn(f"{n}: a paid listing's description does not state the price")
        tiers[p["tier"]] += 1
    r.facts["price_tiers"] = dict(sorted(tiers.items()))


def check_texts(docs, r: Report, partial: bool = False):
    n_text = pending = 0
    for d in docs:
        n = d["name"]
        text = d.get("embeddingText")
        if not text:
            if partial:
                pending += 1
                if any(k in d for k in ("embeddingTextHash", "embeddingTextMeta")):
                    r.error(f"{n}: text metadata without a text")
            else:
                r.error(f"{n}: no embeddingText")
            continue
        n_text += 1
        if d.get("embeddingTextHash") != hashlib.sha1(text.encode()).hexdigest():
            r.error(f"{n}: embeddingTextHash is not sha1(embeddingText)")
        meta = d.get("embeddingTextMeta") or {}
        _, h = text_input(d)
        if meta.get("prompt") != TEXT_PROMPT or meta.get("inputHash") != h:
            r.error(f"{n}: the embedding text was written for another prompt or input (stale)")
        if meta.get("grounded") is not False or meta.get("sources") != [] or not meta.get("model") or not isinstance(meta.get("generatedAt"), datetime):
            r.error(f"{n}: embeddingTextMeta {meta}")
        reason = text_check(text) or strict(text)
        if reason:
            r.error(f"{n}: embedding text rejected: {reason}")
    r.facts["embedding_texts"] = {"documents_with_text": n_text, "pending": pending,
                                  "models": dict(Counter((d.get("embeddingTextMeta") or {}).get("model") for d in docs))}
    if partial and pending:
        r.warn(f"{pending} documents have no embedding text yet (partial mode)")


def check_pitch_day(docs, r: Report):
    day = datetime.fromisoformat(RANGE[0]).date()
    ev = [d for d in docs if d["kind"] == "event" and local(d["start"]).date() == day]
    r.facts["pitch_day"] = {
        "date": str(day), "events": len(ev),
        "free_events": sum(1 for d in ev if d["price"]["isFree"]),
        "by_start": dict(Counter("morning" if local(d["start"]).hour < 12 else "afternoon" if local(d["start"]).hour < 17
                                 else "evening" if local(d["start"]).hour < 21 else "late" for d in ev)),
    }
    open_at = {}
    for hh in (9, 13, 16, 19, 22):
        t = datetime.combine(day, datetime.min.time()).replace(hour=hh, tzinfo=TZ)
        minute = ((t.weekday() + 1) % 7) * 1440 + hh * 60
        c = 0
        for d in docs:
            if d["kind"] != "place":
                continue
            for iv in d["weeklyHours"]:
                o, cl = iv["open"], iv["close"] if iv["close"] > iv["open"] else iv["close"] + WEEK_MIN
                if o <= minute < cl or o <= minute + WEEK_MIN < cl:
                    c += 1
                    break
        open_at[f"{hh:02d}:00"] = c
    r.facts["pitch_day"]["places_open_at"] = open_at
    if len(ev) < 20:
        r.error(f"the pitch day has only {len(ev)} events")


# ------------------------------------------------------------------------ database
def demo_schema(uri: str | None) -> dict[str, set] | None:
    if uri:
        from pymongo import MongoClient

        db = MongoClient(uri, tz_aware=True, serverSelectionTimeoutMS=10000)["freetime"]
        inv = inventory(db["demo_activities"].find({}))
        DEMO_SCHEMA.write_text(json.dumps({p: sorted(t) for p, t in sorted(inv.items())}, indent=1) + "\n")
        return inv
    if DEMO_SCHEMA.exists():
        return {p: set(t) for p, t in json.loads(DEMO_SCHEMA.read_text()).items()}
    return None


EXPECTED_INDEXES = {
    "_id_": ({"_id": 1}, {}),
    "location_2dsphere": ({"location": "2dsphere"}, {}),
    "sourceKeys_1": ({"sourceKeys": 1}, {"unique": True}),
    "city_1_kind_1_start_1": ({"city": 1, "kind": 1, "start": 1}, {}),
    "category_1": ({"category": 1}, {}),
    "recurrence.seriesKey_1": ({"recurrence.seriesKey": 1}, {"sparse": True}),
    "expiresAt_1": ({"expiresAt": 1}, {}),
    "name_1": ({"name": 1}, {}),
}


def check_database(uri: str, docs, r: Report):
    from pymongo import MongoClient

    coll = MongoClient(uri, tz_aware=True, serverSelectionTimeoutMS=10000)["freetime"]["pitch_activities"]
    n = coll.count_documents({})
    r.facts["mongo_count"] = n
    if n != 250:
        r.error(f"freetime.pitch_activities holds {n} documents, expected 250")
    paid = coll.count_documents({"price.min": {"$gt": 0}})
    free = coll.count_documents({"$or": [{"price.isFree": True}, {"price.min": 0}]})
    r.facts["mongo_paid_free"] = {"paid": paid, "free": free}
    if (paid, free) != (150, 100):
        r.error(f"in MongoDB: {paid} paid / {free} free")
    stored = {d["_id"]: d for d in coll.find({})}
    vectors = 0
    for s in stored.values():  # vectors are written after the load (Raven or embed_missing), so compared apart
        if "embedding" not in s:
            continue
        vectors += 1
        v, meta = s.pop("embedding"), s.pop("embeddingMeta", {}) or {}
        model = s.pop("embeddingModel", None)
        norm = math.sqrt(sum(x * x for x in v)) if all(isinstance(x, float) and math.isfinite(x) for x in v) else float("nan")
        if len(v) != 1024 or not abs(norm - 1) < 1e-6 or model != "Qwen/Qwen3-Embedding-0.6B":
            r.error(f"{s['name']}: embedding must be 1024 unit-norm floats from Qwen/Qwen3-Embedding-0.6B")
        if meta.get("textHash") != s.get("embeddingTextHash"):
            r.error(f"{s['name']}: embedding was computed from another text (embeddingMeta.textHash); re-embed it")
    r.facts["mongo_vectors"] = vectors
    if 0 < vectors < len(stored):
        r.warn(f"{len(stored) - vectors} documents have no embedding yet")
    for d in docs:
        s = stored.get(d["_id"])
        if s is None:
            r.error(f"{d['name']}: not in the collection")
        elif bson.encode(s) != bson.encode(d):
            r.error(f"{d['name']}: the stored document differs from the export")
    extra = set(stored) - {d["_id"] for d in docs}
    if extra:
        r.error(f"{len(extra)} documents in the collection are not in the export")
    idx = {i["name"]: i for i in coll.list_indexes()}
    r.facts["indexes"] = {k: {kk: vv for kk, vv in v.items() if kk not in ("v", "ns")} for k, v in idx.items()}
    for name, (key, opts) in EXPECTED_INDEXES.items():
        i = idx.get(name)
        if i is None or dict(i["key"]) != key:
            r.error(f"index {name} missing or on other keys")
            continue
        for k, v in opts.items():
            if i.get(k) != v:
                r.error(f"index {name}: {k} should be {v}")
    for name, i in idx.items():
        if "expireAfterSeconds" in i:
            r.error(f"index {name} is a TTL index; it would delete the catalog")
    info = coll.database.command("listCollections", filter={"name": "pitch_activities"})["cursor"]["firstBatch"][0]
    r.facts["collection_options"] = info.get("options", {})


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--mongo", nargs="?", const=DEFAULT_URI, default=None, help="also check the collection (read-only)")
    ap.add_argument("--no-texts", action="store_true", help="skip the embedding-text checks (before texts exist)")
    ap.add_argument("--partial-texts", action="store_true", help="check the texts that exist; allow documents without one")
    args = ap.parse_args()
    docs = load_export()
    manifest = json.loads(MANIFEST.read_text())
    r = Report()
    check_counts(docs, manifest, r)
    check_schema(docs, demo_schema(args.mongo), r, texts=not (args.no_texts or args.partial_texts))
    check_vocab_and_content(docs, r)
    check_identity(docs, r)
    check_tickets(docs, r)
    check_geo_and_fidelity(docs, manifest, r)
    check_times(docs, manifest, r)
    check_recurrence(docs, r)
    check_prices(docs, r)
    if not args.no_texts:
        check_texts(docs, r, partial=args.partial_texts)
    check_pitch_day(docs, r)
    if args.mongo:
        check_database(args.mongo, docs, r)
    REPORT.write_text(json.dumps({"errors": r.errors, "warnings": r.warnings, "facts": r.facts}, indent=1, default=str) + "\n")
    for w in r.warnings:
        print("warning:", w)
    for e in r.errors:
        print("ERROR:", e)
    print(json.dumps({k: r.facts[k] for k in ("documents", "paid_free_by_app_semantics", "kinds") if k in r.facts}))
    print(f"{len(r.errors)} errors, {len(r.warnings)} warnings; report: {REPORT}")
    sys.exit(1 if r.errors else 0)


if __name__ == "__main__":
    main()
