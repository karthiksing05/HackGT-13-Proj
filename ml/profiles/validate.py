"""The eight-section format's rules, in code.

`check` is the ingestion pipeline's checker, copied unchanged from
`dataingestion/ingest/agent/embed_text.py` (the same copy lives in `datagen/activity_text.py`): the
profile and search texts this package renders must pass exactly the checks every activity text
passed, because the classifier only ever saw texts in that format. `tests/test_profile_text.py`
asserts this copy agrees with `datagen/activity_text.check` on a table of texts.
"""

from __future__ import annotations

import hashlib

SECTIONS = ["Interests", "Activities", "Social", "Environment", "Pace", "Cost", "Timing", "Experience"]
PLACEHOLDERS = ["unknown", "not provided", "n/a", "unspecified", "null", "none listed", "not specified"]
MAX_BULLET_WORDS = 8

# How many bullets a profile or search text keeps per section (in priority order of insertion).
CAPS = {
    "Interests": 10,
    "Activities": 8,
    "Social": 4,
    "Environment": 4,
    "Pace": 3,
    "Cost": 3,
    "Timing": 3,
    "Experience": 3,
}


class ProfileTextError(ValueError):
    """A rendered text failed `check`; a bug in the builders, never a caller error."""


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


def text_hash(text: str) -> str:
    """sha1 of a text, as `embeddingTextHash` on activities."""
    return hashlib.sha1(text.encode()).hexdigest()
