"""Jev reranking stage.

Takes the top-k candidates from embedding retrieval and reorders them by Jev's
predicted user preference. Does not perform retrieval itself.

Failure behavior (the request never fails because of scoring problems):
- Empty candidate list: returns [] without calling Jev.
- Duplicate event IDs: only the first occurrence is kept (logged), since
  scores are keyed by event ID.
- Jev call raises: logged, and candidates are returned in retrieval order.
- Missing answer / malformed score for an event: that event is placed after
  all scored events. Unscored events keep their relative retrieval order.
"""

import logging

from .context import event_to_context, user_to_context
from .jev import JevClient, build_jev_request, call_jev, parse_jev_scores
from .models import Event, EventContext, EventScore, User, UserContext

logger = logging.getLogger(__name__)


async def rerank_events(user: User, events: list[Event], jev: JevClient = call_jev) -> list[Event]:
    """Return the original `Event` objects sorted by descending Jev score."""
    user_context = user_to_context(user)
    candidates = _dedupe([(event, event_to_context(event)) for event in events])
    if not candidates:
        return []

    try:
        scores = await score_events(user_context, [ctx for _, ctx in candidates], jev)
    except Exception:
        logger.exception("Jev scoring failed; falling back to retrieval order")
        return [event for event, _ in candidates]

    # sorted() is stable, so ties and unscored events keep retrieval order.
    ranked = sorted(candidates, key=lambda pair: _sort_key(scores.get(pair[1].event_id)))
    return [event for event, _ in ranked]


async def score_events(
    user: UserContext, events: list[EventContext], jev: JevClient = call_jev
) -> dict[str, EventScore]:
    """Score each event independently with Jev. Depends only on contexts."""
    response = await jev(build_jev_request(user, events))
    return parse_jev_scores(response, [e.event_id for e in events])


def _sort_key(score: EventScore | None) -> tuple[bool, float]:
    # Rank by score only; confidence is kept for debugging/analytics.
    if score is None:
        return (True, 0.0)
    return (False, -score.score)


def _dedupe(candidates: list[tuple[Event, EventContext]]) -> list[tuple[Event, EventContext]]:
    seen: set[str] = set()
    unique = []
    for event, ctx in candidates:
        if ctx.event_id in seen:
            logger.warning("Dropping duplicate candidate event %s", ctx.event_id)
            continue
        seen.add(ctx.event_id)
        unique.append((event, ctx))
    return unique
