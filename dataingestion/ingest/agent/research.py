"""Shared web research for the writing agents (blurb, embedding text).

One web-search call per activity returns factual bullet notes plus source URLs. The
provider is config (`blurb.research_provider`): `muse` (Muse Spark's web_search tool, the
default) or `gemini` (Google Search grounding). Results live in the `research` collection,
keyed by activity, facts hash and provider, so every writer reuses them and each activity
is researched once per provider. Writing is always Gemini.
"""

import hashlib
import json
import logging
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from google.genai import errors
from pymongo.database import Database

from ..config import City, MissingConfig, load_city
from ..quota import QuotaExceeded, reserve
from .gemini import Gemini, GeminiQuotaExhausted, write_models
from .muse import Muse, MuseError, MuseUnavailable

log = logging.getLogger(__name__)

RESEARCH_PROMPT = """Research this {kind} in {city} so someone deciding whether to go knows what it is.
Search the web to look up the performers, organizer or exhibition and the venue{page_hint}.
Return short factual bullet points only, covering whichever of these you can confirm:
- what it is (genre, format, what happens there)
- who is performing or running it, and what they're known for
- the venue: what kind of place it is, its vibe, capacity or layout
- practical details: age limits, doors vs. start time, dress code, what to bring, typical length
Only include facts you found in a source. Skip anything you're unsure of. No opinions or hype.

Facts we already have:
{facts}"""


def _real_stay(duration: dict | None) -> int | None:
    if not duration or duration.get("source") in (None, "category_prior", "llm"):
        return None
    return round(duration["medianMin"])


def facts_packet(doc: dict) -> dict:
    """What the agents may state. Everything they write must trace back to this or the notes."""
    tz = ZoneInfo(doc.get("timezone") or "UTC")
    addr = doc.get("address") or {}
    price = doc.get("price") or {}
    facts = {
        "name": doc.get("name"),
        "kind": doc.get("kind"),
        "category": doc.get("category"),
        "sourceCategory": doc.get("sourceCategory"),
        "tags": doc.get("tags") or [],
        "venueName": doc.get("venueName"),
        "address": addr.get("formatted") or ", ".join(x for x in (addr.get("street"), addr.get("locality"), addr.get("region")) if x) or None,
        "price": {k: price.get(k) for k in ("min", "max", "currency", "isFree")} if price else None,
        "rating": doc.get("rating"),
        "ratingCount": doc.get("ratingCount"),
        # Category priors are our guesses, not facts about this activity: never let a writer state them.
        "typicalStayMinutes": _real_stay(doc.get("duration")),
        "attendance": doc.get("attendance"),
        "description": (doc.get("description") or "")[:1500] or None,
        "url": doc.get("url"),
    }
    for field in ("start", "end"):
        if doc.get(field):
            facts[f"{field}Local"] = doc[field].astimezone(tz).strftime("%a %Y-%m-%d %H:%M")
    return {k: v for k, v in facts.items() if v not in (None, [], {})}


def input_hash(facts: dict) -> str:
    """Dates are left out: blurbs never mention them, so every occurrence of a series
    shares one hash and one blurb (§4.6)."""
    stable = {k: v for k, v in facts.items() if k not in ("startLocal", "endLocal")}
    return hashlib.sha1(json.dumps(stable, sort_keys=True, default=str).encode()).hexdigest()


class Researcher:
    """Grounded research (cached in Mongo) plus a write call with model fallback."""

    def __init__(self, db: Database | None, cfg: dict, city_name: str, city: City | None = None):
        self.db = db
        self.cfg = cfg
        self.city_name = city_name
        self.city = city
        self.provider = cfg.get("research_provider") or "gemini"
        self._muse: Muse | None = None
        # Each model has its own free-tier daily quota, so a list gives more research per day.
        self.research_models = list(cfg.get("research_models") or [cfg.get("research_model")])
        self.write_models = write_models(cfg)
        rpm = {m: cfg["write_rpm"] for m in self.write_models}
        self.gemini = Gemini(rpm={**rpm, **{m: cfg["research_rpm"] for m in self.research_models}})
        self.grounded_off: str | None = None  # set once grounded quota runs out; later writes go ungrounded

    def research(self, doc: dict, facts: dict, h: str) -> dict | None:
        min_chars = self.cfg.get("skip_research_min_chars")
        if min_chars and len(doc.get("description") or "") >= min_chars:
            return None  # §6.6 step 2: enough to write from already
        key = {"activityId": doc["_id"], "inputHash": h}
        # Research saved before providers existed came from Gemini.
        provider_filter = {"$in": ["gemini", None]} if self.provider == "gemini" else self.provider
        if self.db is not None:
            cached = self.db.research.find_one({**key, "provider": provider_filter})
            if cached:
                return cached
        if self.grounded_off:
            return None
        url = doc.get("url")
        prompt = RESEARCH_PROMPT.format(
            kind="event" if doc.get("kind") == "event" else "place",
            city=self.city_name,
            page_hint=f", and read its page {url}" if url else "",
            facts=json.dumps(facts, indent=1, default=str),
        )
        res = self._research_muse(prompt) if self.provider == "muse" else self._research_gemini(prompt, url)
        if res is None or not res.text:
            return None
        research = {
            **key,
            "provider": self.provider,
            "notes": res.text,
            "sources": res.sources,
            "searchQueries": res.search_queries,
            "model": res.model,
            "createdAt": datetime.now(timezone.utc),
        }
        if self.db is not None:
            # Research is a cache of paid calls: keep it even on --dry-run.
            self.db.research.replace_one({**key, "provider": self.provider}, research, upsert=True)
        return research

    def _research_muse(self, prompt: str):
        try:
            if self._muse is None:
                self._muse = Muse(self.city or load_city(), self.cfg.get("muse") or {})
            if self.db is not None:
                reserve(self.db, "muse_research")
            return self._muse.research(prompt)
        except (MuseUnavailable, QuotaExceeded, MissingConfig) as e:
            self.grounded_off = str(e)
            log.warning("web research off for the rest of this run: %s", e)
        except MuseError as e:  # one bad response: this activity gets no research, the run goes on
            log.warning("Muse research failed: %s", e)
        return None

    def _research_gemini(self, prompt: str, url: str | None):
        res = None
        while res is None:
            if not self.research_models:
                self.grounded_off = "every research model is out of quota for today"
                log.warning("grounded research off for the rest of this run: %s", self.grounded_off)
                return None
            model = self.research_models[0]
            try:
                if self.db is not None:
                    reserve(self.db, "gemini_grounded")
                res = self.gemini.generate(model, prompt, search=True, url_context=bool(url))
            except QuotaExceeded as e:
                self.grounded_off = str(e)
                log.warning("grounded research off for the rest of this run: %s", e)
                return None
            except GeminiQuotaExhausted:
                log.warning("%s is out of research quota; trying the next research model", model)
                self.research_models.pop(0)
        return res

    def write(self, prompt: str, system: str | None = None, temperature: float = 0.4) -> tuple[str, str]:
        """Ungrounded text call; falls back through write models when one is overloaded."""
        last: Exception | None = None
        for model in list(self.write_models):
            try:
                return self.gemini.generate(model, prompt, system=system, temperature=temperature).text, model
            except GeminiQuotaExhausted as e:
                # Out of quota won't recover mid-run; stop paying the backoff on every activity.
                log.warning("%s is out of quota; dropping it for the rest of this run", model)
                if len(self.write_models) > 1:
                    self.write_models.remove(model)
                last = e
            except errors.ServerError as e:
                log.info("%s unavailable (%s); trying the next write model", model, str(e)[:80])
                last = e
        raise last
