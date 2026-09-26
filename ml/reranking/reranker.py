"""Jev reranking stage.

Takes the top-k candidates from embedding retrieval and reorders them by Jev's
predicted user preference. Does not perform retrieval itself.

`rerank_events` works on the placeholder domain models; `rerank_contexts` is
the same stage for callers that already have the texts (e.g. the ranking API,
which receives eight-section texts directly).

Failure behavior (the request never fails because of scoring problems):
- Empty candidate list: returns [] without calling Jev.
- Duplicate event IDs: only the first occurrence is kept (logged), since
  scores are keyed by event ID.
- Jev call raises: logged, and candidates are returned in retrieval order.
- Missing answer / malformed score for an event: that event is placed after
  all scored events. Unscored events keep their relative retrieval order.
"""

import logging
from dataclasses import dataclass, field

from .context import event_to_context, user_to_context
from .jev import JevClient, build_jev_request, call_jev, parse_jev_scores
from .models import Event, EventContext, EventScore, SearchContext, User, UserContext

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class RerankResult:
    """Contexts in reranked order, the scores Jev returned, and whether Jev was used.

    `reranked` is False when Jev was skipped or failed; `events` is then in
    the input order.
    """

    events: list[EventContext]
    scores: dict[str, EventScore] = field(default_factory=dict)
    reranked: bool = False


async def rerank_contexts(
    user: UserContext,
    events: list[EventContext],
    search: SearchContext | None = None,
    jev: JevClient = call_jev,
) -> RerankResult:
    """Reorder `events` by descending Jev score. Event ids must be unique."""
    if not events:
        return RerankResult(events=[])

    try:
        scores = await score_events(user, events, search, jev)
    except Exception:
        logger.exception("Jev scoring failed; falling back to retrieval order")
        return RerankResult(events=list(events))

    # sorted() is stable, so ties and unscored events keep retrieval order.
    ranked = sorted(events, key=lambda ctx: _sort_key(scores.get(ctx.event_id)))
    return RerankResult(events=ranked, scores=scores, reranked=True)


async def rerank_events(
    user: User, events: list[Event], search: SearchContext | None = None, jev: JevClient = call_jev
) -> list[Event]:
    """Return the original `Event` objects sorted by descending Jev score.

    `search`, when given, is what the user wants from this search; Jev ranks
    by it first and uses the user's profile as background.
    """
    candidates = _dedupe([(event, event_to_context(event)) for event in events])
    result = await rerank_contexts(user_to_context(user), [ctx for _, ctx in candidates], search, jev)
    by_id = {ctx.event_id: event for event, ctx in candidates}
    return [by_id[ctx.event_id] for ctx in result.events]


async def score_events(
    user: UserContext,
    events: list[EventContext],
    search: SearchContext | None = None,
    jev: JevClient = call_jev,
) -> dict[str, EventScore]:
    """Score each event independently with Jev. Depends only on contexts."""
    response = await jev(build_jev_request(user, events, search))
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
