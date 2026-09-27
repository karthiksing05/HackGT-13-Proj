"""Build the Atlanta pitch catalog (`freetime.pitch_activities`) from specs.py.

    cd dataingestion
    .venv/bin/python -m pitch.generate [--uri mongodb://127.0.0.1:27017/?directConnection=true] [--refresh]

Synthetic demonstration data built on real venues. The venue records named in specs.py are
read from `freetime.activities` (read-only; cached in pitch/out/inspiration_records.json so a
rebuild needs no database), and each document is built with the pipeline's own model and
helpers, the way `demo/generate.py` built `demo_activities`: `ingest.models.Activity`,
`Duration.lognormal` / the category priors, `price_from_range` on the city's tiers,
`pipeline.finalize` for `expiresAt`, and the demo's `slug` and `popularity`. Embedding texts
come from pitch/out/embedding_texts.json (written by `python -m pitch.texts`) and are only
attached while they still match the document they were written for.

Writes pitch/out/pitch_activities.json (canonical Extended JSON, BSON types preserved) and
pitch/manifest.json (per document: planning group, price class, the real records it was built
on, and which fields were copied, adjusted or invented).
"""

from __future__ import annotations

import argparse
import hashlib
import unicodedata
import json
import re
from dataclasses import asdict
from datetime import datetime, timedelta, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

from bson import ObjectId, json_util
from bson.json_util import CANONICAL_JSON_OPTIONS, JSONOptions, JSONMode

from demo.generate import popularity, slug
from ingest.agent.embed_text import PROMPT_PATH, event_data
from ingest.enrich.duration import EVENT_TIMES_SIGMA, MAX_FIXED_SPAN_MIN, prior
from ingest.enrich.price import price_from_range
from ingest.models import Activity, Address, Duration, GeoPoint, HoursInterval, Recurrence, SourceRef, Trail
from ingest.pipeline import finalize

from . import specs
from .specs import Event, Place

HERE = Path(__file__).resolve().parent
OUT = HERE / "out"
EXPORT = OUT / "pitch_activities.json"
RECORDS_CACHE = OUT / "inspiration_records.json"
TEXTS_CACHE = OUT / "embedding_texts.json"
MANIFEST = HERE / "manifest.json"

DEFAULT_URI = "mongodb://127.0.0.1:27017/?directConnection=true"
CITY = "atlanta"
TZ_NAME = "America/New_York"
TZ = ZoneInfo(TZ_NAME)
TIERS = [0, 15, 40, 80]  # cities/atlanta.yaml price_tiers
RANGE = ("2026-09-27", "2026-10-10")  # local dates: the pitch day (DEMO_DATE) and the 13 days after it
GENERATED_AT = datetime(2026, 9, 27, 7, 0, tzinfo=timezone.utc)  # createdAt, updatedAt, sources[].fetchedAt
PITCH_BASE = "https://pitch.sidequestz.example"  # like demo's saltlight.example: names the synthetic source, never resolves
# Paid items are sold on the sandbox ticket site the checkout agent buys from (MERCHANT_HOST), at
# /{slug}/tickets. The site needs a listing per slug: pitch/TICKETS_HANDOFF.md, pitch/out/ticket_listings.json.
TICKET_BASE = "https://events.sidequestz.tech"
TEMPLATE_HASH = hashlib.sha1(PROMPT_PATH.read_text().encode()).hexdigest()[:12]
TEXT_PROMPT = f"{PROMPT_PATH.name}@{TEMPLATE_HASH}"

# The planner's category defaults (Backend/pkg/itinerary/hours.go) for places whose real record
# has no hours: written out with hoursSource "default", as demo_activities does for its hikes.
DEFAULT_DAILY_HOURS = {
    "park": (6 * 60, 22 * 60), "hike": (6 * 60, 20 * 60), "landmark": (0, 24 * 60), "viewpoint": (0, 24 * 60),
    "museum": (10 * 60, 17 * 60), "gallery": (11 * 60, 18 * 60), "garden": (8 * 60, 19 * 60),
    "shopping": (10 * 60, 21 * 60), "market": (8 * 60, 14 * 60), "rec_venue": (10 * 60, 22 * 60),
    "zoo_aquarium": (9 * 60, 17 * 60), "restaurant": (11 * 60, 22 * 60), "cafe": (7 * 60, 18 * 60),
}
WEEK_MIN = 7 * 24 * 60

# Event-listing venue names that read badly on a stop card; the record keeps its own.
VENUE_NAMES = {
    "The Eastern-GA": "The Eastern",
    "Tabernacle Presented by iTHINK Financial": "Tabernacle",
    "The Masquerade  - Altar": "The Masquerade - Altar",
    "The Dinner Detective True Crime Murder Mystery Dinner Show - Atlanta, GA": "The Dinner Detective",
}
DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]
RRULE_DAYS = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"]
ABBREVIATIONS = {"dr", "mr", "mrs", "ms", "st", "vs", "no", "jr", "sr"}
PRICE_TAGS = {"free", "cheap", "splurge"}


def price_tags(tags: tuple, price) -> list[str]:
    """The spec's tags with the price tags derived from the price, one rule for every document:
    free when nothing is paid, cheap for tier 1 up to $20, splurge for tier 3+ or a top price of $90+."""
    out = [t for t in tags if t not in PRICE_TAGS]
    if price.isFree:
        out.append("free")
    elif price.tier == 1 and (price.max or price.min) <= 20:
        out.append("cheap")
    elif price.tier >= 3 or (price.max or 0) >= 90:
        out.append("splurge")
    return out


# ------------------------------------------------------------------------ inspiration records
def load_records(uri: str, refresh: bool) -> dict[str, dict]:
    """The real venue records specs.py names, by hex _id. Read-only on freetime.activities."""
    refs = sorted({s.ref for s in specs.PLACES} | {e.ref for e in specs.EVENTS})
    cached: dict[str, dict] = {}
    if RECORDS_CACHE.exists() and not refresh:
        cached = {str(d["_id"]): d for d in json_util.loads(RECORDS_CACHE.read_text())}
    missing = [r for r in refs if r not in cached]
    if missing:
        from pymongo import MongoClient

        coll = MongoClient(uri, tz_aware=True, serverSelectionTimeoutMS=10000)["freetime"]["activities"]
        projection = {"embedding": 0, "embeddingText": 0, "embeddingMeta": 0, "embeddingTextMeta": 0, "blurb": 0}
        for d in coll.find({"_id": {"$in": [ObjectId(r) for r in missing]}}, projection):
            cached[str(d["_id"])] = d
        still = [r for r in refs if r not in cached]
        if still:
            raise SystemExit(f"these venue records are not in freetime.activities: {still}")
        OUT.mkdir(parents=True, exist_ok=True)
        RECORDS_CACHE.write_text(json_util.dumps([cached[r] for r in refs], indent=1, json_options=CANONICAL_JSON_OPTIONS))
    return {r: cached[r] for r in refs}


# ------------------------------------------------------------------------ helpers
def first_sentence(text: str) -> str:
    """demo/generate.py's summary rule (the description's first sentence), minding abbreviations."""
    for m in re.finditer(r"\.\s+(?=[A-Z0-9$])", text):
        word = text[: m.start()].rsplit(" ", 1)[-1].lower()
        if word not in ABBREVIATIONS:
            return text[: m.start() + 1]
    return text if text.endswith(".") else text + "."


def local(date: str, hhmm: str) -> datetime:
    return datetime.fromisoformat(f"{date}T{hhmm}").replace(tzinfo=TZ)


def object_id(source_key: str) -> ObjectId:
    return ObjectId(hashlib.sha1(source_key.encode()).hexdigest()[:24])  # demo/generate.py's rule


def address_of(rec: dict) -> tuple[Address | None, str | None]:
    """The record's address; a listing that left `formatted` empty gets it composed from its own parts."""
    a = rec.get("address")
    if not a:
        return None, None
    note = None
    if not a.get("formatted") and a.get("street"):
        region = " ".join(x for x in (a.get("region"), a.get("postalCode")) if x)
        a = {**a, "formatted": ", ".join(x for x in (a.get("street"), a.get("locality"), region) if x)}
        note = "address.formatted composed from the record's street, locality, region and postal code"
    return Address(**{k: a.get(k) for k in Address.model_fields}), note


def hours_of(rec: dict, category: str) -> tuple[list[HoursInterval], str, str | None]:
    """Real opening hours as recorded, except two encodings the planner cannot read."""
    wh = rec.get("weeklyHours") or []
    if not wh:
        o, c = DEFAULT_DAILY_HOURS[category]
        if (o, c) == (0, 24 * 60):
            return [HoursInterval(open=0, close=WEEK_MIN)], "default", "no hours in the record: open all day, the planner's default for this category"
        ivs = [HoursInterval(open=d * 1440 + o, close=d * 1440 + c) for d in range(7)]
        return ivs, "default", f"no hours in the record: the planner's default for {category} ({o // 60:02d}:00-{c // 60:02d}:00), hoursSource default"
    if any(iv["open"] == iv["close"] for iv in wh):
        # Google's "open 24 hours" was stored as {open: 0, close: 0}, which the planner reads as closed.
        return [HoursInterval(open=0, close=WEEK_MIN)], rec.get("hoursSource") or "google", \
            "Google's 24-hour listing ({open: 0, close: 0}) re-encoded as [{open: 0, close: 10080}] (DATA_COLLECTION_SPEC §4.1)"
    return [HoursInterval(open=iv["open"], close=iv["close"]) for iv in wh], rec.get("hoursSource") or "google", None


def price_of(spec_price: tuple | None, rec: dict, tags: tuple) -> tuple:
    """(price, source): the spec's price, the record's Google price range, or free for a public
    park or trail the spec tags free (no admission; the record has no price)."""
    if spec_price is not None:
        return price_from_range(float(spec_price[0]), float(spec_price[1]), "USD", TIERS), "spec"
    p = rec.get("price") or {}
    if p.get("min") is not None:
        return price_from_range(float(p["min"]), float(p["max"] if p.get("max") is not None else p["min"]), "USD", TIERS), "record"
    if "free" in tags:
        return price_from_range(0.0, 0.0, "USD", TIERS), "free_public"
    raise SystemExit(f"{rec['name']}: the record has no price; give the spec one")


def venue_name_of(rec: dict) -> str:
    name = rec["name"] if rec["kind"] == "place" else rec.get("venueName") or rec["name"]
    return VENUE_NAMES.get(name, name)


def recurrence_of(ev: Event, series_slug: str) -> Recurrence | None:
    if len(ev.dates) < 2:
        return None
    starts = [local(d, ev.time) for d in sorted(ev.dates)]
    dow = (starts[0].weekday() + 1) % 7  # 0 = Sunday, as weeklyHours
    for a, b in zip(starts, starts[1:]):
        if (b - a) != timedelta(days=7):
            raise SystemExit(f"{ev.name}: series dates must be consecutive weeks: {ev.dates}")
    clock = starts[0].strftime("%-I:%M %p").replace(":00 ", " ")
    span = f"{starts[0]:%b} {starts[0].day} – {starts[-1]:%b} {starts[-1].day}, {starts[-1].year}"
    return Recurrence(
        seriesKey=f"pitch:series:{series_slug}", frequency="weekly", rule=f"FREQ=WEEKLY;BYDAY={RRULE_DAYS[dow]}",
        daysOfWeek=[dow], localStartTime=ev.time, until=starts[-1].astimezone(timezone.utc),
        text=f"{DAY_NAMES[dow]}s at {clock}, {span}", origin="source_rule",
    )


def ticket_url(ticket_slug: str, price) -> str | None:
    """The ticket page of a paid item (a series' dates get their own slugs); free items have none."""
    url_slug = re.sub(r"[^a-z0-9]+", "-", unicodedata.normalize("NFKD", ticket_slug).encode("ascii", "ignore").decode().lower()).strip("-")
    return None if price.isFree else f"{TICKET_BASE}/{url_slug}/tickets"


def source(key_id: str, path: str) -> dict:
    return dict(sourceKeys=[f"pitch:{key_id}"],
                sources=[SourceRef(name="pitch", id=key_id, url=f"{PITCH_BASE}/{path}", fetchedAt=GENERATED_AT)])


def inspiration(rec: dict) -> dict:
    src = (rec.get("sources") or [{}])[0]
    return {"collection": "activities", "_id": str(rec["_id"]), "name": rec["name"], "kind": rec["kind"],
            "category": rec.get("category"), "source": src.get("name"), "sourceId": src.get("id")}


# ------------------------------------------------------------------------ builders
def build_place(p: Place, rec: dict) -> tuple[Activity, dict]:
    name = p.name or rec["name"]
    key = slug(name)
    hike = p.category == "hike"
    if hike and rec.get("category") != "hike":
        raise SystemExit(f"{name}: a hike must come from a trail record")
    addr, addr_note = address_of(rec)
    hours, hours_source, hours_note = hours_of(rec, p.category)
    price, price_source = price_of(p.price, rec, p.tags)
    duration = Duration(**rec["duration"]) if hike else prior(p.category)
    trail = Trail(**rec["trail"]) if hike else None
    act = Activity(
        kind="place", city=CITY, name=name, summary=first_sentence(p.description), description=p.description,
        category=p.category, sourceCategory=rec.get("sourceCategory") or rec.get("category"), tags=price_tags(p.tags, price),
        location=GeoPoint(coordinates=tuple(rec["location"]["coordinates"])), address=addr,
        venueName=rec.get("venueName") if hike else name,
        attendance="drop_in", timezone=TZ_NAME, weeklyHours=hours, hoursSource=hours_source,
        duration=duration, price=price, rating=rec.get("rating"), ratingCount=rec.get("ratingCount"),
        popularity=popularity(name, rec.get("ratingCount")), trail=trail,
        url=rec.get("url"), ticketUrl=ticket_url(key, price), imageUrl=None, googlePlaceId=rec.get("googlePlaceId"),
        **source(key, key),
    )
    copied = ["location", "address", "weeklyHours", "hoursSource", "rating", "ratingCount", "url", "googlePlaceId", "sourceCategory"]
    if hike:
        copied += ["trail", "duration", "venueName"]
    else:
        copied += ["venueName"]
    if price_source == "record":
        copied.append("price.min/max (Google price range per person)")
    adjusted = [n for n in (addr_note, hours_note) if n]
    if p.name:
        adjusted.append(f"name: '{rec['name']}' shown as '{name}'")
    if p.category != rec.get("category"):
        adjusted.append(f"category: '{rec.get('category')}' in the real record, '{p.category}' here (sourceCategory keeps '{rec.get('sourceCategory')}')")
    invented = ["description", "summary", "tags", "popularity (demo formula)"]
    if price_source == "spec" and price.isFree:
        invented.append("price (free admission, as stated in the spec; the record has no price)")
    elif price_source == "spec":
        invented.append("price (estimated typical price; not from the record)")
    elif price_source == "free_public":
        invented.append("price (free: a public park or trail with no admission; the record has no price)")
    if not hike:
        invented.append("duration (category prior)")
    manifest = {"inspiration": [inspiration(rec)], "copiedFromInspiration": copied, "adjusted": adjusted, "invented": invented}
    return act, manifest


def build_events(e: Event, rec: dict) -> list[tuple[Activity, dict]]:
    base = slug(e.name)
    rec_venue = rec["name"] if rec["kind"] == "place" else rec.get("venueName") or rec["name"]
    venue = e.venue or venue_name_of(rec)
    addr, addr_note = address_of(rec)
    recurrence = recurrence_of(e, base)
    price = price_from_range(float(e.price[0]), float(e.price[1]), "USD", TIERS)
    out = []
    for date in e.dates:
        start = local(date, e.time)
        end = start + timedelta(minutes=e.minutes)
        if e.attendance == "fixed_start":
            if e.minutes > MAX_FIXED_SPAN_MIN or e.category == "market":
                raise SystemExit(f"{e.name}: a fixed start needs a span of at most {MAX_FIXED_SPAN_MIN} min and no market category")
            duration = Duration.lognormal(e.minutes, EVENT_TIMES_SIGMA, "event_times")
        else:
            duration = prior(e.category)
        key = f"{base}@{date}"
        act = Activity(
            kind="event", city=CITY, name=e.name, summary=first_sentence(e.description), description=e.description,
            category=e.category, sourceCategory=e.category, tags=price_tags(e.tags, price),
            location=GeoPoint(coordinates=tuple(rec["location"]["coordinates"])), address=addr, venueName=venue,
            start=start.astimezone(timezone.utc), end=end.astimezone(timezone.utc), attendance=e.attendance,
            timezone=TZ_NAME, weeklyHours=None, hoursSource=None, recurrence=recurrence, duration=duration,
            price=price, rating=None, ratingCount=None, popularity=popularity(e.name, None), trail=None,
            url=None, ticketUrl=ticket_url(base if len(e.dates) == 1 else f"{base}-{start:%b}-{start.day}".lower(), price),
            imageUrl=None, googlePlaceId=None,
            **source(key, f"{base}/{date}"),
        )
        finalize(act)  # expiresAt = (end or start + p75) + 6 h, the pipeline's rule
        adjusted = [n for n in (addr_note,) if n]
        if venue != rec_venue:
            adjusted.append(f"venueName: '{rec_venue}' shown as '{venue}'")
        manifest = {
            "inspiration": [inspiration(rec)],
            "copiedFromInspiration": ["location", "address", "venueName (the venue only)"],
            "adjusted": adjusted,
            "invented": ["name", "description", "summary", "tags", "start", "end", "attendance", "price", "duration",
                         "popularity (demo formula)"] + (["recurrence (a two-week run of a weekly series)"] if recurrence else []),
        }
        out.append((act, manifest))
    return out


def to_doc(act: Activity) -> dict:
    d = act.model_dump(mode="python")
    d["location"]["coordinates"] = list(d["location"]["coordinates"])
    key = act.sourceKeys[0]
    return {"_id": object_id(key), **d, "createdAt": GENERATED_AT, "updatedAt": GENERATED_AT}


def text_input(doc: dict) -> tuple[str, str]:
    """The embed-text step's {{EVENT_DATA}} for this document and its input hash (no web research)."""
    data = event_data(doc, None)
    return data, hashlib.sha1(f"{TEMPLATE_HASH}\n{data}".encode()).hexdigest()


def attach_texts(docs: list[dict]) -> tuple[int, list[str]]:
    texts = json.loads(TEXTS_CACHE.read_text()) if TEXTS_CACHE.exists() else {}
    attached, stale = 0, []
    for d in docs:
        t = texts.get(d["sourceKeys"][0])
        _, h = text_input(d)
        if not t:
            continue
        if t["inputHash"] != h or t["prompt"] != TEXT_PROMPT:
            stale.append(d["sourceKeys"][0])
            continue
        d["embeddingText"] = t["text"]
        d["embeddingTextHash"] = hashlib.sha1(t["text"].encode()).hexdigest()
        d["embeddingTextMeta"] = {
            "model": t["model"], "prompt": t["prompt"], "grounded": False, "sources": [],
            "inputHash": t["inputHash"], "generatedAt": datetime.fromisoformat(t["generatedAt"]),
        }
        attached += 1
    return attached, stale


def build(uri: str = DEFAULT_URI, refresh: bool = False, with_texts: bool = True) -> tuple[list[dict], list[dict]]:
    records = load_records(uri, refresh)
    docs, manifest = [], []
    for p in specs.PLACES:
        act, m = build_place(p, records[p.ref])
        doc = to_doc(act)
        docs.append(doc)
        manifest.append({"group": p.group, **m, "_doc": doc})
    for e in specs.EVENTS:
        for act, m in build_events(e, records[e.ref]):
            doc = to_doc(act)
            docs.append(doc)
            manifest.append({"group": e.group, **m, "_doc": doc})
    keys = [d["sourceKeys"][0] for d in docs]
    if len(set(keys)) != len(keys):
        dup = sorted({k for k in keys if keys.count(k) > 1})
        raise SystemExit(f"duplicate source keys: {dup}")
    if with_texts:
        attach_texts(docs)
    rows = []
    for m in manifest:
        d = m.pop("_doc")
        start = d["start"].astimezone(TZ).strftime("%a %Y-%m-%d %H:%M") if d.get("start") else None
        end = d["end"].astimezone(TZ).strftime("%a %Y-%m-%d %H:%M") if d.get("end") else None
        paid = (d["price"]["min"] or 0) > 0
        rows.append({
            "_id": str(d["_id"]), "sourceKey": d["sourceKeys"][0], "name": d["name"], "group": m["group"],
            "kind": d["kind"], "category": d["category"], "price": "paid" if paid else "free",
            "priceMin": d["price"]["min"], "priceMax": d["price"]["max"], "localStart": start, "localEnd": end,
            "venue": d["venueName"], "seriesKey": (d.get("recurrence") or {}).get("seriesKey"),
            "inspiration": m["inspiration"], "copiedFromInspiration": m["copiedFromInspiration"],
            "adjusted": m["adjusted"], "invented": m["invented"],
        })
    return docs, rows


def write(docs: list[dict], rows: list[dict]) -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    EXPORT.write_text(json_util.dumps(docs, indent=1, json_options=CANONICAL_JSON_OPTIONS))
    MANIFEST.write_text(json.dumps({
        "dataset": "freetime.pitch_activities",
        "note": "Synthetic demonstration data for the SideQuests pitch. Venues are real records of freetime.activities; "
                "event names, dates, times, prices and descriptions were invented and are not verified public listings.",
        "city": CITY, "timezone": TZ_NAME, "range": RANGE, "generatedAt": GENERATED_AT.isoformat(),
        "embeddingTextPrompt": TEXT_PROMPT, "documents": rows,
    }, indent=1, ensure_ascii=False) + "\n")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--uri", default=DEFAULT_URI, help="read-only source of the venue records (freetime.activities)")
    ap.add_argument("--refresh", action="store_true", help="re-read the venue records instead of using the cache")
    args = ap.parse_args()
    docs, rows = build(args.uri, args.refresh)
    write(docs, rows)
    with_text = sum(1 for d in docs if d.get("embeddingText"))
    paid = sum(1 for r in rows if r["price"] == "paid")
    print(f"wrote {len(docs)} documents ({paid} paid, {len(docs) - paid} free; {with_text} with embedding texts) to {EXPORT}")
    print(f"manifest: {MANIFEST}")


if __name__ == "__main__":
    main()
