"""The ingestion pipeline's embedding-text contract, for backfilling MongoDB activities.

`_price_line`, `event_data`, `clean` and `check` are copied unchanged from
`dataingestion/ingest/agent/embed_text.py` (branch `dataingenstion`, commit 46ac640), because the
ingestion package itself needs Gemini and its research agent. `event_data` builds the model input
from an activity document; `clean` and `check` enforce the prompt's output format, so backfilled
texts pass exactly the checks the pipeline's own texts pass. Documents must be read with
`MongoClient(..., tz_aware=True)`, as the pipeline does, or Starts/Ends come out in the wrong zone.

One addition, `backfill_input`: places also get their opening hours when a real source gave them
(`hoursSource` google/osm/nps; `default` hours are assumed, not known). The pipeline's web research
normally says what a place is like; without it, hours are the only evidence for Timing.
"""
from __future__ import annotations

import hashlib
import re
from zoneinfo import ZoneInfo

PROMPT_NAME = "event_embedding_text.md"
SECTIONS = ["Interests", "Activities", "Social", "Environment", "Pace", "Cost", "Timing", "Experience"]
PLACEHOLDERS = ["unknown", "not provided", "n/a", "unspecified", "null", "none listed", "not specified"]
_FENCE = re.compile(r"^```[a-z]*\s*|\s*```$", re.I)


# ---- copied from the pipeline (embed_text.py @ 46ac640) ----------------------------------------

def _price_line(price: dict | None) -> str | None:
    if not price:
        return None
    lo, hi, cur = price.get("min"), price.get("max"), price.get("currency") or ""
    if price.get("isFree") or (lo == 0 and not hi):
        return "Free"
    sym = "$" if cur == "USD" else f"{cur} "
    if lo is not None and hi is not None and hi != lo:
        return f"{sym}{lo:g}-{sym}{hi:g}"
    amount = lo if lo is not None else hi
    return f"{sym}{amount:g}" if amount is not None else None


def event_data(doc: dict, research: dict | None) -> str:
    """{{EVENT_DATA}}: every non-empty fact, no ids or URLs, weekday + time but no calendar
    date (Timing is about time of day and week, and a date would make series differ)."""
    tz = ZoneInfo(doc.get("timezone") or "UTC")
    tags = set(doc.get("tags") or [])
    lines = [doc.get("name", "").strip()]

    def add(label, value):
        if value not in (None, "", [], {}):
            lines.append(f"{label}: {value}")

    add("Type", doc.get("kind"))
    add("Category", (doc.get("category") or "").replace("_", " ") if doc.get("category") != "other" else None)
    add("Source classification", doc.get("sourceCategory"))
    add("Venue", doc.get("venueName"))
    addr = doc.get("address") or {}
    add("Area", ", ".join(x for x in (addr.get("locality"), addr.get("region")) if x))
    if doc.get("start"):
        start = doc["start"].astimezone(tz)
        add("Starts", start.strftime("%A %-I:%M %p"))
        if doc.get("end"):
            end = doc["end"].astimezone(tz)
            span_h = (doc["end"] - doc["start"]).total_seconds() / 3600
            add("Ends", end.strftime("%A %-I:%M %p") + (f" ({span_h:.0f} hours later)" if span_h >= 1 else ""))
    add("Attendance", {"drop_in": "come and go any time during the event", "fixed_start": "arrive at the start"}.get(doc.get("attendance")))
    add("Price", _price_line(doc.get("price")))
    add("Age", "21+" if "21_plus" in tags else None)
    add("Rating", f"{doc['rating']} from {doc['ratingCount']} reviews" if doc.get("rating") and doc.get("ratingCount") else None)
    add("Description", " ".join((doc.get("description") or "")[:1500].split()))
    notes = (research or {}).get("notes")
    if notes:
        lines.append("Web research (facts found online about this event, its performers and venue):")
        lines.append(notes.strip())
    return "\n".join(lines)


def clean(text: str) -> str:
    text = _FENCE.sub("", text.strip()).strip()
    return "\n".join(line.rstrip() for line in text.splitlines()).strip()


def check(text: str) -> str | None:
    """Enforce the prompt's format rules in code. Returns a failure reason or None."""
    if not text:
        return "the output is empty"
    current, counts = None, {}
    for line in text.splitlines():
        s = line.strip()
        if not s:
            continue
        if s.endswith(":") and not s.startswith("-"):
            name = s[:-1].strip()
            if name not in SECTIONS:
                return f"'{name}' is not an allowed section (allowed: {', '.join(SECTIONS)})"
            if name in counts:
                return f"section '{name}' appears twice"
            current, counts[name] = name, 0
        elif s.startswith("- "):
            if current is None:
                return "a bullet appears before any section header"
            item = s[2:].strip().lower()
            if any(item == p or item.startswith(p + " ") for p in PLACEHOLDERS):
                return f"it uses a placeholder value ('{s[2:].strip()}')"
            if "http" in item or "www." in item:
                return "it includes a URL"
            if len(item.split()) > 8:
                return f"'{s[2:].strip()}' is a sentence, not a short phrase"
            counts[current] += 1
        else:
            return f"'{s[:60]}' is neither a section header nor a '- ' bullet"
    empty = [k for k, n in counts.items() if n == 0]
    if empty:
        return f"empty sections: {', '.join(empty)}"
    if not counts:
        return "no sections"
    return None


def retry_suffix(reason: str, text: str) -> str:
    """What the pipeline appends to the prompt when it regenerates a rejected text."""
    return f"\n\nYour previous output was rejected because {reason}. Previous output:\n{text}\n\nFix it and return only the structured text."


# ---- backfill additions ------------------------------------------------------------------------

DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
REAL_HOURS = {"google", "osm", "nps"}
_WEEK = 7 * 1440
_BEFORE_HOURS = ("Attendance:", "Price:", "Age:", "Rating:", "Description:", "Web research")


def _clock(minute: int) -> str:
    h, m = divmod(minute % 1440, 60)
    return f"{h % 12 or 12}:{m:02d} {'AM' if h < 12 else 'PM'}"


def hours_text(weekly: list[dict] | None) -> str | None:
    """`weeklyHours` (minutes since Sunday 00:00 local, day 0 = Sunday, close < open wraps past
    Saturday) as e.g. 'Mon-Fri 11:00 AM-9:00 PM; Sat-Sun 10:00 AM-11:00 PM'."""
    if not weekly:
        return None
    per_day: dict[int, list[str]] = {d: [] for d in range(7)}
    for iv in sorted(weekly, key=lambda iv: iv["open"]):
        span = (iv["close"] - iv["open"]) % _WEEK or _WEEK
        if span >= _WEEK:
            return "open 24 hours every day"
        per_day[iv["open"] // 1440 % 7].append(
            "open 24 hours" if span >= 1440 else f"{_clock(iv['open'])}-{_clock(iv['open'] + span)}")
    groups: list[list] = []  # [first position, last position, schedule], Monday first
    for pos, day in enumerate([1, 2, 3, 4, 5, 6, 0]):
        schedule = ", ".join(per_day[day])
        if not schedule:
            continue
        if groups and groups[-1][2] == schedule and groups[-1][1] == pos - 1:
            groups[-1][1] = pos
        else:
            groups.append([pos, pos, schedule])
    if not groups:
        return None
    if len(groups) == 1 and groups[0][:2] == [0, 6]:
        return f"daily {groups[0][2]}"
    name = lambda pos: DAY_NAMES[(pos + 1) % 7]
    return "; ".join(f"{name(a)}{'' if a == b else '-' + name(b)} {s}" for a, b, s in groups)


def backfill_input(doc: dict) -> str:
    """`event_data(doc, None)`, plus an 'Opening hours' line for places with sourced hours."""
    data = event_data(doc, None)
    hours = hours_text(doc.get("weeklyHours")) if doc.get("kind") == "place" and doc.get("hoursSource") in REAL_HOURS else None
    if not hours:
        return data
    lines = data.split("\n")
    at = next((i for i, line in enumerate(lines) if i and line.startswith(_BEFORE_HOURS)), len(lines))
    return "\n".join(lines[:at] + [f"Opening hours: {hours}"] + lines[at:])


def template_hash(template: str) -> str:
    return hashlib.sha1(template.encode()).hexdigest()[:12]


def input_hash(template: str, data: str) -> str:
    """The pipeline's `embeddingTextMeta.inputHash`: it skips a document whose hash is unchanged."""
    return hashlib.sha1(f"{template_hash(template)}\n{data}".encode()).hexdigest()


def text_hash(text: str) -> str:
    """`embeddingTextHash`: embeddings are recomputed only when this changes."""
    return hashlib.sha1(text.encode()).hexdigest()
