"""Deterministic eight-section texts from the app's structured inputs.

`build_profile_texts` renders a user's likes (positive) and dislikes (negative) and
`build_search_text` one plan request, all in the format the activity texts and the classifier's
training data use: sections in a fixed order, `Name:` then `- bullet` lines, a blank line between
sections, empty sections omitted, bullets lowercase and at most eight words, no duplicates, no
trailing newline. Everything is table-driven (`lexicon.py`) so the same input always renders the
same bytes; change `TEMPLATE_VERSION` when the tables or rules change so stored hashes go stale.
"""

from __future__ import annotations

import re
from collections.abc import Iterable, Mapping
from dataclasses import dataclass, field
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from .lexicon import (
    ACTIVITY_TAG_PHRASES,
    BUDGET,
    CATEGORY_PHRASES,
    COMPANY,
    FLEXIBILITY_NEG,
    LEADING_FILLERS,
    PACE,
    PREFER_FREE,
    RATING_ALIASES,
    RATING_DISLIKES,
    RATING_KEYS,
    RATING_LIKES,
    RATING_TAGS_NEG,
    RATING_TAGS_POS,
    ROUTES,
    SPEND,
    TAGS,
    TRAILING_FILLERS,
    WHO,
)
from .validate import CAPS, MAX_BULLET_WORDS, PLACEHOLDERS, SECTIONS, ProfileTextError, check

MAX_FREE_TEXT_PHRASES = 8
MAX_RATED_EVENTS = 20

# --- inputs (plain dataclasses; the API's pydantic models convert to these) -----------------------


@dataclass
class RatedEventInput:
    stars: int
    tags: list[str] = field(default_factory=list)
    category: str | None = None
    activity_tags: list[str] = field(default_factory=list)


@dataclass
class ProfileInput:
    ratings: Mapping[str, object] = field(default_factory=dict)
    company: str | None = None
    pace: str | None = None
    spend: str | None = None
    flexibility: str | None = None
    prefer_free: bool = False
    perfect_afternoon: str = ""
    never_do: str = ""
    plan_around: str = ""
    facebook_interests: list[str] = field(default_factory=list)
    rated_events: list[RatedEventInput] = field(default_factory=list)


@dataclass
class SearchInput:
    mood_text: str = ""
    tags: list[str] = field(default_factory=list)
    who: str | None = None
    pace: str | None = None
    budget: int | None = None
    start_time: datetime | None = None
    back_by: datetime | None = None
    timezone: str | None = None


@dataclass(frozen=True)
class ProfileTexts:
    positive: str
    negative: str


# --- sections ------------------------------------------------------------------------------------


class Sections:
    """Bullets per section, deduplicated, capped at `CAPS` in insertion (= priority) order."""

    def __init__(self) -> None:
        self._bullets: dict[str, list[str]] = {name: [] for name in SECTIONS}

    def add(self, section: str, *phrases: str) -> None:
        bullets = self._bullets[section]
        for phrase in phrases:
            bullet = clean_bullet(phrase)
            if bullet and bullet not in bullets and len(bullets) < CAPS[section]:
                bullets.append(bullet)

    def add_map(self, mapping: Mapping[str, Iterable[str]]) -> None:
        for section, phrases in mapping.items():
            self.add(section, *phrases)

    def render(self) -> str:
        blocks = [f"{name}:\n" + "\n".join(f"- {b}" for b in bullets) for name, bullets in self._bullets.items() if bullets]
        return "\n\n".join(blocks)


def clean_bullet(phrase: str) -> str | None:
    """Lowercase, single-spaced, at most eight words; None when nothing usable is left."""
    words = phrase.lower().split()
    if not words:
        return None
    return " ".join(words[:MAX_BULLET_WORDS])


def take(lexicon: Mapping[str, list[str]], n: int) -> dict[str, list[str]]:
    """The first `n` bullets of every section in a lexicon entry."""
    return {section: list(bullets[:n]) for section, bullets in lexicon.items()}


# --- normalizing the app's enums -----------------------------------------------------------------

_CAMEL = re.compile(r"(?<=[a-z0-9])(?=[A-Z])")


def normalize_rating_key(key: object) -> str | None:
    """`liveMusic`, `live-music`, `Live Music` -> `live_music`; unknown keys -> None."""
    s = _CAMEL.sub("_", str(key).strip())
    s = re.sub(r"[\s\-]+", "_", s).lower().strip("_")
    s = RATING_ALIASES.get(s, s)
    return s if s in RATING_KEYS else None


def normalize_ratings(ratings: Mapping[str, object] | None) -> dict[str, int]:
    """Known keys only, values clamped to 1..5; later duplicates (aliases) do not override."""
    out: dict[str, int] = {}
    for key, value in (ratings or {}).items():
        name = normalize_rating_key(key)
        if name is None or name in out or isinstance(value, bool):
            continue
        try:
            number = int(round(float(value)))
        except (TypeError, ValueError):
            continue
        out[name] = max(1, min(5, number))
    return out


def normalize_enum(value: object) -> str | None:
    if value is None:
        return None
    s = re.sub(r"[\s\-]+", "_", str(value).strip().lower())
    return s or None


def normalize_pace(value: object) -> str | None:
    pace = normalize_enum(value)
    return "relaxed" if pace == "chill" else pace


def normalize_tag(value: object) -> str:
    """Quick-pick and rating tags: lowercase, single spaces (`Meet people` -> `meet people`)."""
    return " ".join(re.sub(r"[_\-]+", " ", str(value).strip().lower()).split())


def normalize_category(value: object) -> str | None:
    if value is None:
        return None
    s = re.sub(r"[\s\-]+", "_", str(value).strip().lower())
    return s or None


# --- free text -----------------------------------------------------------------------------------

_SPLIT = re.compile(r"[.;!?\n]+|,|\b(?:and|then|or)\b", re.IGNORECASE)
_URL = re.compile(r"(?:https?://|www\.)\S+", re.IGNORECASE)  # removed before splitting on "."
_QUOTES = re.compile(r"[\"“”„‟«»()\[\]{}]")
_EDGE_PUNCT = " \t\r\n.,;:!?-–—'’`\"“”"
_LEADING = sorted(LEADING_FILLERS, key=len, reverse=True)
_TRAILING = sorted(TRAILING_FILLERS, key=len, reverse=True)


def clean_phrase(raw: str) -> str | None:
    """One free-text fragment -> a bullet, or None when it carries no signal."""
    s = _QUOTES.sub(" ", raw.lower())
    s = " ".join(s.split()).strip(_EDGE_PUNCT)
    s = _strip_fillers(s)
    if len(s) < 3:
        return None
    if any(s == p or s.startswith(p + " ") for p in PLACEHOLDERS):
        return None
    if "http" in s or "www." in s:
        return None
    return " ".join(s.split()[:MAX_BULLET_WORDS])


def _strip_fillers(s: str) -> str:
    changed = True
    while changed and s:
        changed = False
        for filler in _LEADING:
            if s == filler:
                return ""
            if s.startswith(filler + " "):
                s = s[len(filler) + 1 :].strip(_EDGE_PUNCT)
                changed = True
                break
        for filler in _TRAILING:
            if s == filler:
                return ""
            if s.endswith(" " + filler):
                s = s[: -len(filler) - 1].strip(_EDGE_PUNCT)
                changed = True
                break
    return s


def phrases(text: str | None) -> list[str]:
    """Split free text into at most eight cleaned, deduplicated phrases."""
    if not text or not text.strip():
        return []
    out: list[str] = []
    for fragment in _SPLIT.split(_URL.sub(" ", text)):
        phrase = clean_phrase(fragment) if fragment else None
        if phrase and phrase not in out:
            out.append(phrase)
        if len(out) >= MAX_FREE_TEXT_PHRASES:
            break
    return out


def route(phrase: str) -> str:
    """The section a free-text phrase belongs to, by keyword; `Interests` by default."""
    for section, pattern in ROUTES:
        if pattern.search(phrase):
            return section
    return "Interests"


# --- builders ------------------------------------------------------------------------------------


def build_profile_texts(inp: ProfileInput) -> ProfileTexts:
    positive, negative = Sections(), Sections()
    ratings = normalize_ratings(inp.ratings)

    likes = sorted(((k, v) for k, v in ratings.items() if v >= 4), key=lambda kv: (-kv[1], RATING_KEYS.index(kv[0])))
    for key, value in likes:
        positive.add_map(take(RATING_LIKES[key], 2 if value == 5 else 1))
    dislikes = sorted(((k, v) for k, v in ratings.items() if v <= 2), key=lambda kv: (kv[1], RATING_KEYS.index(kv[0])))
    for key, value in dislikes:
        negative.add_map(take(RATING_DISLIKES[key], 2 if value == 1 else 1))

    company = normalize_enum(inp.company)
    if company in COMPANY:
        positive.add_map(COMPANY[company])
    pace = normalize_pace(inp.pace)
    if pace in PACE:
        positive.add_map(PACE[pace])
    if inp.prefer_free:
        positive.add("Cost", *PREFER_FREE)
    spend = normalize_enum(inp.spend)
    if spend in SPEND:
        positive.add("Cost", *SPEND[spend])
    flexibility = normalize_enum(inp.flexibility)
    if flexibility in FLEXIBILITY_NEG:
        negative.add_map(FLEXIBILITY_NEG[flexibility])

    for phrase in phrases(inp.perfect_afternoon) + phrases(inp.plan_around):
        positive.add(route(phrase), phrase)
    for phrase in phrases(inp.never_do):
        negative.add(route(phrase), phrase)

    for interest in inp.facebook_interests:
        phrase = clean_phrase(interest)
        if phrase:
            positive.add("Interests", phrase)

    for event in inp.rated_events[:MAX_RATED_EVENTS]:
        category = CATEGORY_PHRASES.get(normalize_category(event.category) or "")
        target = positive if event.stars >= 4 else negative if event.stars <= 2 else None
        if target is not None:
            if category:
                target.add("Interests", category)
            for tag in event.activity_tags:
                target.add_map(ACTIVITY_TAG_PHRASES.get(normalize_tag(tag), {}))
        for tag in event.tags:
            name = normalize_tag(tag)
            positive.add_map(RATING_TAGS_POS.get(name, {}))
            negative.add_map(RATING_TAGS_NEG.get(name, {}))

    return ProfileTexts(_render(positive), _render(negative))


def build_search_text(inp: SearchInput) -> str:
    s = Sections()
    for tag in inp.tags:
        name = normalize_tag(tag)
        if name in TAGS:
            s.add_map(TAGS[name])
        elif name:
            s.add("Interests", name)
    who = normalize_enum(inp.who)
    if who in WHO:
        s.add_map(WHO[who])
    pace = normalize_pace(inp.pace)
    if pace in PACE:
        s.add_map(PACE[pace])
    if inp.budget is not None and inp.budget in BUDGET:
        s.add("Cost", *BUDGET[inp.budget])
    s.add("Timing", *timing_bullets(inp.start_time, inp.back_by, inp.timezone))
    for phrase in phrases(inp.mood_text):
        s.add(route(phrase), phrase)
    return _render(s)


def timing_bullets(start_time: datetime | None, back_by: datetime | None, tz_name: str | None) -> list[str]:
    """`"friday afternoon"`, `"weekday afternoon"` and the outing length, from the local start time."""
    if start_time is None:
        return []
    local = start_time.astimezone(_zone(tz_name))
    period = _period(local.hour)
    weekday = local.strftime("%A").lower()
    out = [f"{weekday} {period}", f"{'weekend' if local.weekday() >= 5 else 'weekday'} {period}"]
    if back_by is not None:
        hours = (back_by - start_time).total_seconds() / 3600
        if hours > 0:
            out.append("short outing" if hours < 2 else "multi-hour outing" if hours <= 5 else "full-day outing")
    return out


def _zone(name: str | None) -> ZoneInfo | timezone:
    if name:
        try:
            return ZoneInfo(name)
        except (KeyError, ValueError, OSError):
            pass
    return timezone.utc


def _period(hour: int) -> str:
    if hour < 12:
        return "morning"
    if hour < 17:
        return "afternoon"
    if hour < 21:
        return "evening"
    return "night"


def _render(sections: Sections) -> str:
    text = sections.render()
    if text:
        reason = check(text)
        if reason is not None:
            raise ProfileTextError(f"rendered text fails the format check: {reason}")
    return text
