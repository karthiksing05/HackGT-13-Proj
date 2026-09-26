"""Blurb agent (§6.6): research an activity on the web, then write the detail-screen paragraph.

  facts packet -> grounded research (Search + the activity's own page) -> write -> code checks -> save

Research is shared with the embedding-text agent via agent/research.py.
"""

import json
import logging
import re
from dataclasses import dataclass
from datetime import datetime, timezone

from pymongo.database import Database

from .research import Researcher, facts_packet, input_hash  # noqa: F401 (re-exported)

log = logging.getLogger(__name__)

BANNED = ["amazing", "must-see", "must see", "unforgettable", "vibrant", "hidden gem"]
_NUMBER = re.compile(r"\d+(?:\.\d+)?")

WRITE_SYSTEM = """You write the short description shown when someone opens an activity in a free-time planner app.
Rules:
- {min_words}-{max_words} words, one paragraph, second person ("you"), present tense.
- Say what it is, what you'd do there, and one practical detail (typical stay, price, when to arrive, what to bring) only if it appears in the facts or notes.
- Use only facts from the FACTS and NOTES below. If unsure, leave it out. Never invent names, numbers or details.
- Don't mention the date, day of week or start time; the app already shows them.
- No hype words: amazing, must-see, unforgettable, vibrant, hidden gem. No exclamation marks.
- Plain text only: no markdown, no heading, no quotes around the paragraph."""

WRITE_PROMPT = """FACTS:
{facts}

NOTES (from web research; may be empty):
{notes}
{retry}
Write the paragraph."""


@dataclass
class Outcome:
    status: str  # written | skipped | failed
    reason: str = ""
    blurb: dict | None = None
    research: dict | None = None


def check(text: str, facts: dict, notes: str, min_words: int, max_words: int) -> str | None:
    """The §6.6 step-5 checks. Returns a failure reason, or None if the blurb passes."""
    words = len(text.split())
    if not min_words <= words <= max_words:
        return f"it has {words} words; it must have {min_words}-{max_words}"
    lower = text.lower()
    hype = [w for w in BANNED if w in lower]
    if hype:
        return f"it uses banned words: {', '.join(hype)}"
    if "!" in text:
        return "it uses an exclamation mark"
    known = (json.dumps(facts, default=str) + " " + notes).replace(",", "")
    invented = [n for n in _NUMBER.findall(text.replace(",", "")) if n not in known]
    if invented:
        return f"it states numbers not found in the facts or notes: {', '.join(invented)}"
    return None


class BlurbAgent:
    def __init__(self, db: Database | None, cfg: dict, city_name: str, dry_run: bool = False,
                 researcher: Researcher | None = None):
        self.db = db
        self.cfg = cfg
        self.dry_run = dry_run
        self.researcher = researcher or Researcher(db, cfg, city_name)

    @property
    def grounded_off(self) -> str | None:
        return self.researcher.grounded_off

    def run(self, doc: dict, force: bool = False) -> Outcome:
        facts = facts_packet(doc)
        h = input_hash(facts)
        current = doc.get("blurb") or {}
        if not force and current.get("inputHash") == h and current.get("grounded"):
            return Outcome("skipped", "blurb is current")

        research = self.researcher.research(doc, facts, h)
        if not force and current.get("inputHash") == h and not research:
            return Outcome("skipped", "blurb is current; still no web research to improve it")
        notes = (research or {}).get("notes", "")
        text, reason, model = self._write(facts, notes)
        if text is None:
            return Outcome("failed", reason, research=research)

        blurb = {
            "text": text,
            "sources": [s["url"] for s in (research or {}).get("sources", [])],
            "grounded": bool(research),
            "model": model,
            "inputHash": h,
            "generatedAt": datetime.now(timezone.utc),
        }
        if not self.dry_run and self.db is not None:
            self._save(doc, blurb)
        return Outcome("written", blurb=blurb, research=research)

    def _write(self, facts: dict, notes: str) -> tuple[str | None, str, str | None]:
        lo, hi = self.cfg["min_words"], self.cfg["max_words"]
        system = WRITE_SYSTEM.format(min_words=lo, max_words=hi)
        retry = ""
        reason = ""
        for _ in range(2):  # §6.6 step 5: regenerate once with the failure reason
            prompt = WRITE_PROMPT.format(facts=json.dumps(facts, indent=1, default=str), notes=notes or "(none)", retry=retry)
            text, model = self.researcher.write(prompt, system)
            text = " ".join(text.split()).strip('"')
            reason = check(text, facts, notes, lo, hi)
            if reason is None:
                return text, "", model
            retry = f"\nYour previous attempt was rejected because {reason}. Previous attempt:\n{text}\n"
        return None, reason, None

    def _save(self, doc: dict, blurb: dict) -> None:
        # Occurrences of one series share the blurb (§4.6); blurbs never mention dates.
        series = (doc.get("recurrence") or {}).get("seriesKey")
        q = {"recurrence.seriesKey": series} if series else {"_id": doc["_id"]}
        self.db.activities.update_many(q, {"$set": {"blurb": blurb}})
