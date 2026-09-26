"""Embedding-text agent: structured semantic text for the ML model's event embeddings.

  event fields + web research notes (agent/research.py) -> prompts/event_embedding_text.md -> checks -> save

Output goes to `activities.embeddingText`; `embeddingTextHash` (sha1 of the text) tells the
embedding stage when to re-embed (§6.5). The prompt file is the ML owner's contract: editing
it changes the input hash, so every activity regenerates on the next run.
"""

import hashlib
import logging
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

from pymongo.database import Database

from .research import Researcher, facts_packet, input_hash

log = logging.getLogger(__name__)

PROMPT_PATH = Path(__file__).parent / "prompts" / "event_embedding_text.md"
SECTIONS = ["Interests", "Activities", "Social", "Environment", "Pace", "Cost", "Timing", "Experience"]
PLACEHOLDERS = ["unknown", "not provided", "n/a", "unspecified", "null", "none listed", "not specified"]
_FENCE = re.compile(r"^```[a-z]*\s*|\s*```$", re.I)


@dataclass
class Outcome:
    status: str  # written | skipped | failed
    reason: str = ""
    text: str | None = None
    event_data: str | None = None
    research: dict | None = None


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


class EmbedTextAgent:
    def __init__(self, db: Database | None, cfg: dict, city_name: str, dry_run: bool = False,
                 researcher: Researcher | None = None):
        self.db = db
        self.dry_run = dry_run
        self.template = PROMPT_PATH.read_text()
        self.template_hash = hashlib.sha1(self.template.encode()).hexdigest()[:12]
        self.researcher = researcher or Researcher(db, cfg, city_name)

    @property
    def grounded_off(self) -> str | None:
        return self.researcher.grounded_off

    def run(self, doc: dict, force: bool = False) -> Outcome:
        facts = facts_packet(doc)
        research = self.researcher.research(doc, facts, input_hash(facts))
        data = event_data(doc, research)
        h = hashlib.sha1(f"{self.template_hash}\n{data}".encode()).hexdigest()
        if not force and (doc.get("embeddingTextMeta") or {}).get("inputHash") == h:
            return Outcome("skipped", "embedding text is current")

        prompt = self.template.replace("{{EVENT_DATA}}", data)
        retry, reason, model = "", "", None
        for _ in range(2):  # regenerate once with the failure reason
            raw, model = self.researcher.write(prompt + retry, temperature=0.2)
            text = clean(raw)
            reason = check(text)
            if reason is None:
                break
            retry = f"\n\nYour previous output was rejected because {reason}. Previous output:\n{text}\n\nFix it and return only the structured text."
        else:
            return Outcome("failed", reason, event_data=data, research=research)

        if not self.dry_run and self.db is not None:
            self.db.activities.update_one({"_id": doc["_id"]}, {"$set": {
                "embeddingText": text,
                "embeddingTextHash": hashlib.sha1(text.encode()).hexdigest(),
                "embeddingTextMeta": {
                    "model": model,
                    "prompt": f"{PROMPT_PATH.name}@{self.template_hash}",
                    "grounded": bool(research),
                    "sources": [s["url"] for s in (research or {}).get("sources", [])],
                    "inputHash": h,
                    "generatedAt": datetime.now(timezone.utc),
                },
            }})
        return Outcome("written", text=text, event_data=data, research=research)
