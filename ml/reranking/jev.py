"""Jev-specific request building and response parsing.

Everything here works only with `UserContext` / `EventContext`, never with
the application's `User` / `Event` models.
"""

import logging
import math
from typing import Any, Awaitable, Callable

from typesafe_sdk import AsyncTypeSafeClient

from .models import EventContext, EventScore, SearchContext, UserContext

logger = logging.getLogger(__name__)

JevClient = Callable[[dict[str, Any]], Awaitable[dict[str, Any]]]

# One scale, shared by every candidate so scores are comparable across events.
# Each level describes a concrete situation and must make sense on its own:
# Jev evaluates levels independently and never sees their numeric index.
# All levels measure a single dimension: would this user enjoy / want to attend?
EVENT_MATCH_CRITERIA: list[str] = [
    (
        "The event clearly conflicts with the user's stated interests or preferences, "
        "and the user would most likely avoid it."
    ),
    (
        "The event has little connection to the user's stated interests, or has drawbacks "
        "relative to their preferences that make attending unlikely, though it does not "
        "directly conflict with them."
    ),
    (
        "The event neither matches nor conflicts with the user's stated interests and "
        "preferences; there is no particular reason to expect the user to seek it out or avoid it."
    ),
    (
        "The event matches some of the user's interests or preferences and there is a plausible "
        "reason they would enjoy it, but the fit is partial or there are minor reasons they may pass on it."
    ),
    (
        "The event strongly matches the user's stated interests and preferences, "
        "with clear reasons to expect that they would enjoy or want to attend it."
    ),
]

# Used instead of EVENT_MATCH_CRITERIA when the user describes what they want
# from this search. Same rules: independent levels, one dimension. The search
# leads and the profile is background.
SEARCH_MATCH_CRITERIA: list[str] = [
    (
        "The event has nothing to do with what the user is looking for right now, or clearly "
        "conflicts with their preferences, and they would most likely skip it."
    ),
    (
        "The event has little connection to what the user is looking for right now, or has "
        "drawbacks relative to their preferences that make attending unlikely."
    ),
    (
        "The event only loosely relates to what the user is looking for right now, or fits it but "
        "clashes with their usual preferences; there is no strong reason to expect them to pick it or avoid it."
    ),
    (
        "The event fits what the user is looking for right now and there is a plausible reason they "
        "would enjoy it, but the fit is partial or there are minor drawbacks relative to their preferences."
    ),
    (
        "The event clearly fits what the user is looking for right now and suits their usual "
        "preferences, with clear reasons to expect that they would want to attend it."
    ),
]
assert len(SEARCH_MATCH_CRITERIA) == len(EVENT_MATCH_CRITERIA), "both scales must share MAX_SCORE"

# Jev's Score primitive returns a continuous value in [0, len(criteria) - 1].
MAX_SCORE = len(EVENT_MATCH_CRITERIA) - 1

# One request scores every candidate, so allow more than the SDK's 10s default.
JEV_TIMEOUT_SECONDS = 60.0


def question_key(event_id: str) -> str:
    return f"event_{event_id}"


def build_jev_state(
    user: UserContext, events: list[EventContext], search: SearchContext | None = None
) -> dict[str, Any]:
    user_state = {"id": user.user_id, "description": user.description}
    if user.dislikes:
        user_state["dislikes"] = user.dislikes
    state: dict[str, Any] = {"user": user_state}
    if search is not None:
        state["search"] = {"description": search.description}
    state["events"] = [{"id": e.event_id, "description": e.description} for e in events]
    return state


def build_jev_questions(
    events: list[EventContext], search: SearchContext | None = None
) -> dict[str, dict[str, Any]]:
    criteria = EVENT_MATCH_CRITERIA if search is None else SEARCH_MATCH_CRITERIA
    return {
        question_key(e.event_id): {
            "type": "score",
            "instructions": _instructions(e.event_id, search),
            "criteria": criteria,
        }
        for e in events
    }


def build_jev_request(
    user: UserContext, events: list[EventContext], search: SearchContext | None = None
) -> dict[str, Any]:
    # The SDK accepts a JSON object as state and serializes it for us.
    return {
        "state": build_jev_state(user, events, search),
        "questions": build_jev_questions(events, search),
    }


def _instructions(event_id: str, search: SearchContext | None) -> str:
    if search is None:
        return (
            f'How likely is the user to enjoy or want to attend the event with id "{event_id}"? '
            "Judge only that event, based on the user's stated interests and preferences."
        )
    return (
        "The user is currently looking for what the search description says. "
        f'How likely is the user to want to attend the event with id "{event_id}" for this search? '
        "Judge only that event. Treat the search as the main requirement and the user's profile "
        "(interests, preferences and dislikes) as background; where they conflict, follow the search."
    )


async def call_jev(request: dict[str, Any]) -> dict[str, Any]:
    """Send a request to Jev via the TypeSafe SDK and return the response as plain JSON.

    Reads the API key from TYPESAFE_API_KEY (and optionally TYPESAFE_DEFAULT_MODEL /
    TYPESAFE_BASE_URL). SDK errors propagate; the reranker handles them.
    """
    async with AsyncTypeSafeClient(timeout=JEV_TIMEOUT_SECONDS) as client:
        response = await client.system_one(state=request["state"], questions=request["questions"])
    return response.model_dump(mode="json")


def parse_jev_scores(response: dict[str, Any], event_ids: list[str]) -> dict[str, EventScore]:
    """Extract one `EventScore` per event that received a valid answer.

    Events whose answer is missing or malformed are logged and left out of the
    result; the caller decides where unscored events go.
    """
    answers = response.get("answers") if isinstance(response, dict) else None
    if not isinstance(answers, dict):
        logger.warning("Jev response has no 'answers' object; no events scored")
        return {}

    scores: dict[str, EventScore] = {}
    for event_id in event_ids:
        answer = answers.get(question_key(event_id))
        if not isinstance(answer, dict):
            logger.warning("Jev returned no answer for event %s", event_id)
            continue

        score = answer.get("score", "0.0")
        if not _is_number(score) or not 0 <= score <= MAX_SCORE:
            logger.warning("Jev returned malformed score for event %s: %r", event_id, score)
            continue

        confidence = answer.get("confidence", "")
        scores[event_id] = EventScore(
            event_id=event_id,
            score=float(score),
            confidence=float(confidence) if _is_number(confidence) else None,
        )
    return scores


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)
