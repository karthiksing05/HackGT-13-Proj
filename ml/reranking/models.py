"""Data classes used by the reranking stage.

`User` and `Event` are placeholder domain models. Their fields are arbitrary
and expected to change; only `context.py` should depend on them.

`UserContext`, `EventContext`, and `EventScore` are the stable types the Jev
ranking code is written against.
"""

from dataclasses import dataclass, field


# --- Domain models (placeholders, free to change) ---------------------------


@dataclass
class User:
    id: str
    name: str
    interests: list[str] = field(default_factory=list)
    dislikes: list[str] = field(default_factory=list)  # negative signal, e.g. "crowds", "EDM"
    preferred_environment: str | None = None  # e.g. "social", "quiet", "outdoor"
    budget: float | None = None  # max price the user usually wants to pay


@dataclass
class Event:
    id: str
    name: str
    description: str
    category: str
    location: str | None = None
    price: float | None = None


# --- Stable ranking contexts ------------------------------------------------


@dataclass(frozen=True)
class UserContext:
    user_id: str
    description: str


@dataclass(frozen=True)
class EventContext:
    event_id: str
    description: str


@dataclass(frozen=True)
class EventScore:
    event_id: str
    score: float
    confidence: float | None = None
