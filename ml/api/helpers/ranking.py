"""Ranking pipeline: validate -> hard filters -> (search blend) -> batch score -> threshold
-> (Jev scores the model's top k) -> sort -> limit.

There is one sort, after Jev: events Jev scored come first by Jev score, then
the rest; the model score breaks ties and orders everything Jev didn't score.
The model score before that only picks which k events Jev sees.

Stateless: everything needed comes in with the request, and nothing is
fetched or stored. The scoring approach is whatever `CompatibilityModel` is
injected, so swapping cosine for the classifier needs no change here. The
rerank is `reranking.rerank_contexts`, run only when a Jev client is
configured and the request carries the user's `positive_text`.
"""

import asyncio
import heapq
import logging
import math
from dataclasses import dataclass

import numpy as np

from compatibility import CompatibilityModel, Embedding, UserEmbedding
from compatibility.user_embedding import l2_normalize
from reranking import EventContext, UserContext, rerank_contexts, search_to_context
from reranking.jev import JevClient

from ..errors import InferenceError, RankingRequestError
from ..schemas.ranking import EventInput, RankedEvent, RankingOptions, UserInput
from .filters import HardFilter, default_filters

logger = logging.getLogger(__name__)

# Request vectors carry no encoder version; they are all assumed to come from
# the same encoder, so one shared tag satisfies the model's version check.
REQUEST_EMBEDDING_VERSION = "request"

# How far a search pulls the user's positive embedding for that request:
# positive = normalize((1 - w) * positive + w * search). The search leads.
DEFAULT_SEARCH_WEIGHT = 0.6

# How many of the best model-ranked events Jev reorders. One Jev request
# scores them all, so this bounds both latency and cost.
DEFAULT_RERANK_TOP_K = 20

# Jev's state needs a user id; the request has none, and Jev only uses it as a label.
RERANK_USER_ID = "user"


@dataclass(frozen=True)
class RankingOutcome:
    events: list[RankedEvent]
    reranked: bool = False


class EventRankingService:
    def __init__(
        self,
        model: CompatibilityModel,
        filters: list[HardFilter] | None = None,
        search_weight: float = DEFAULT_SEARCH_WEIGHT,
        jev: JevClient | None = None,
        rerank_top_k: int = DEFAULT_RERANK_TOP_K,
    ) -> None:
        if not 0.0 <= search_weight <= 1.0:
            raise ValueError(f"search_weight must lie in [0, 1], got {search_weight}")
        if rerank_top_k < 1:
            raise ValueError(f"rerank_top_k must be >= 1, got {rerank_top_k}")
        self.model = model
        self.filters = default_filters() if filters is None else filters
        self.search_weight = search_weight
        self.jev = jev
        self.rerank_top_k = rerank_top_k

    async def rank_and_rerank(
        self,
        user: UserInput,
        events: list[EventInput],
        options: RankingOptions,
        search_embedding: list[float] | None = None,
        search_text: str | None = None,
    ) -> RankingOutcome:
        """Score with the model, let Jev score the model's top k, then sort and limit.

        Model scoring runs in a worker thread so it doesn't block the event
        loop. The rerank never fails the request: if it is skipped or Jev
        fails, the result is in model order with `reranked=False`.
        """
        scored = await asyncio.to_thread(self._scored, user, events, options, search_embedding)
        reranked = False
        if self.jev is not None and options.rerank and _has_text(user.positive_text) and scored:
            scored, reranked = await self._rerank(user, events, scored, options, search_text)
        return RankingOutcome(events=_limit(_sort(scored), options), reranked=reranked)

    def rank(
        self,
        user: UserInput,
        events: list[EventInput],
        options: RankingOptions,
        search_embedding: list[float] | None = None,
    ) -> list[RankedEvent]:
        """Rank `events` for `user` by the model alone, best first.

        Raises `RankingRequestError` or `InferenceError`. `search_embedding`,
        if given and non-zero, is blended into the user's positive embedding
        for this call only.
        """
        return _limit(_sort(self._scored(user, events, options, search_embedding)), options)

    def _scored(
        self,
        user: UserInput,
        events: list[EventInput],
        options: RankingOptions,
        search_embedding: list[float] | None,
    ) -> list[RankedEvent]:
        """validate -> filters -> score -> threshold. Unsorted; ordering happens in `_sort`."""
        self.validate(user, events, options, search_embedding)

        candidates = [event for event in events if all(f.keep(user, event) for f in self.filters)]
        if not candidates:
            return []

        ranked = [
            RankedEvent(event_id=event.id, score=score)
            for event, score in zip(candidates, self._score(user, candidates, search_embedding))
        ]
        if options.min_score is not None:
            ranked = [result for result in ranked if result.score >= options.min_score]
        return ranked

    async def _rerank(
        self,
        user: UserInput,
        events: list[EventInput],
        scored: list[RankedEvent],
        options: RankingOptions,
        search_text: str | None,
    ) -> tuple[list[RankedEvent], bool]:
        """Attach Jev's score to each of the model's top k events. Doesn't reorder.

        Events without a description can't be judged, so they aren't sent to
        Jev and keep `rerank_score=None`.
        """
        k = options.rerank_top_k or self.rerank_top_k
        descriptions = {event.id: event.description.strip() for event in events if _has_text(event.description)}
        shortlist = heapq.nlargest(k, scored, key=lambda result: result.score)
        judged = [result for result in shortlist if result.event_id in descriptions]
        if not judged:
            return scored, False

        result = await rerank_contexts(
            UserContext(
                user_id=RERANK_USER_ID,
                description=user.positive_text.strip(),
                dislikes=user.negative_text.strip() if _has_text(user.negative_text) else None,
            ),
            [EventContext(event_id=r.event_id, description=descriptions[r.event_id]) for r in judged],
            search_to_context(search_text),
            self.jev,
        )
        if not result.reranked:
            return scored, False

        def with_jev_score(r: RankedEvent) -> RankedEvent:
            jev_score = result.scores.get(r.event_id)
            return r if jev_score is None else r.model_copy(update={"rerank_score": jev_score.score})

        return [with_jev_score(r) for r in scored], True

    def validate(
        self,
        user: UserInput,
        events: list[EventInput],
        options: RankingOptions,
        search_embedding: list[float] | None = None,
    ) -> None:
        """Model-dependent and cross-field checks that the request schema cannot express."""
        dimension = self.model.embedding_dim or len(user.positive_embedding)
        for field in ("positive_embedding", "negative_embedding"):
            if len(getattr(user, field)) != dimension:
                raise RankingRequestError(f"User embeddings must have dimension {dimension}.")
        if search_embedding is not None and len(search_embedding) != dimension:
            raise RankingRequestError(f"search_embedding must have dimension {dimension}.")
        if not any(user.positive_embedding):
            raise RankingRequestError("User positive_embedding must not be all zeros.")

        seen: set[str] = set()
        for event in events:
            if event.id in seen:
                raise RankingRequestError(f"Duplicate event id {event.id!r}.")
            seen.add(event.id)
            if len(event.embedding) != dimension:
                raise RankingRequestError(
                    f"Event embeddings must have dimension {dimension} (event {event.id!r} has {len(event.embedding)})."
                )
            if not any(event.embedding):
                raise RankingRequestError(f"Event {event.id!r} embedding must not be all zeros.")

        score_range = self.model.score_range
        if options.min_score is not None and score_range is not None:
            low, high = score_range
            if not low <= options.min_score <= high:
                raise RankingRequestError(f"min_score must lie in [{low}, {high}] for model {self.model.version}.")

    def _score(
        self, user: UserInput, events: list[EventInput], search_embedding: list[float] | None = None
    ) -> list[float]:
        """One batched model call; any model failure becomes an opaque `InferenceError`."""
        user_embedding = UserEmbedding(
            positive=_embedding(self._positive(user, search_embedding)),
            negative=_embedding(user.negative_embedding) if any(user.negative_embedding) else None,
        )
        event_embeddings = [_embedding(event.embedding, source_id=event.id) for event in events]
        try:
            results = self.model.score_many(user_embedding, event_embeddings)
        except Exception as exc:
            logger.exception("compatibility model %s failed on %d events", self.model.version, len(events))
            raise InferenceError("Compatibility inference failed.") from exc

        scores = [result.score for result in results]
        if len(scores) != len(events) or not all(math.isfinite(s) for s in scores):
            logger.error("compatibility model %s returned invalid scores for %d events", self.model.version, len(events))
            raise InferenceError("Compatibility inference failed.")
        return scores

    def _positive(self, user: UserInput, search_embedding: list[float] | None) -> list[float]:
        """The user's positive embedding, pulled toward the search when there is one.

        Same blend as the stored profile update (`MovingAverageUpdater` with
        alpha = 1 - w), but never persisted. Re-normalized because the
        classifier expects unit-norm inputs.
        """
        if search_embedding is None or not any(search_embedding):
            return user.positive_embedding
        w = self.search_weight
        blended = (1.0 - w) * np.asarray(user.positive_embedding) + w * np.asarray(search_embedding)
        return l2_normalize(blended).tolist()


def _sort(scored: list[RankedEvent]) -> list[RankedEvent]:
    """Jev-scored events first, by Jev score; then the rest. Model score breaks ties."""
    return sorted(
        scored,
        key=lambda r: (r.rerank_score is None, -(r.rerank_score or 0.0), -r.score),
    )


def _limit(ranked: list[RankedEvent], options: RankingOptions) -> list[RankedEvent]:
    return ranked if options.limit is None else ranked[: options.limit]


def _has_text(text: str | None) -> bool:
    return text is not None and bool(text.strip())


def _embedding(values: list[float], source_id: str | None = None) -> Embedding:
    return Embedding(vector=np.asarray(values), model_version=REQUEST_EMBEDDING_VERSION, source_id=source_id)
