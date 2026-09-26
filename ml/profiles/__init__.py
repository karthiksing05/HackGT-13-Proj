"""Eight-section user-profile and search texts from the app's structured inputs (pure; no FastAPI).

Python owns the text format: the templates, the validator and the hash live here so the Go
backend never builds a text itself. `TEMPLATE_VERSION` prefixes every `profile_text_hash`; Go
re-embeds a user whose stored hash carries another prefix.
"""

import hashlib

from .text import (
    ProfileInput,
    ProfileTexts,
    RatedEventInput,
    SearchInput,
    build_profile_texts,
    build_search_text,
    normalize_ratings,
    phrases,
    route,
    timing_bullets,
)
from .validate import CAPS, SECTIONS, ProfileTextError, check, text_hash

TEMPLATE_VERSION = "profile-v1"


def profile_hash(positive: str, negative: str) -> str:
    """`"profile-v1:" + sha1(positive + "\\n---\\n" + negative)`, stored as `users.profileTextHash`."""
    return f"{TEMPLATE_VERSION}:" + hashlib.sha1(f"{positive}\n---\n{negative}".encode()).hexdigest()


__all__ = [
    "CAPS",
    "SECTIONS",
    "TEMPLATE_VERSION",
    "ProfileInput",
    "ProfileTextError",
    "ProfileTexts",
    "RatedEventInput",
    "SearchInput",
    "build_profile_texts",
    "build_search_text",
    "check",
    "normalize_ratings",
    "phrases",
    "profile_hash",
    "route",
    "text_hash",
    "timing_bullets",
]
