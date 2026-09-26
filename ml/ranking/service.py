"""Ranking pipeline: validate -> hard filters -> batch score -> threshold -> sort -> limit.

Stateless: everything needed comes in with the request, and nothing is
fetched or stored. The scoring approach is whatever `CompatibilityModel` is
injected, so swapping cosine for the classifier needs no change here.
"""

import logging
import math

import numpy as np

from compatibility import CompatibilityModel, Embedding, UserEmbedding

from .filters import HardFilter, default_filters
from .schemas import EventInput, RankedEvent, RankingOptions, UserInput

logger = logging.getLogger(__name__)

# Request vectors carry no encoder version; they are all assumed to come from
# the same encoder, so one shared tag satisfies the model's version check.
REQUEST_EMBEDDING_VERSION = "request"


class RankingRequestError(ValueError):
    """The request is well-formed but cannot be ranked (maps to HTTP 400)."""


class InferenceError(RuntimeError):
    """The compatibility model failed (maps to HTTP 500)."""


class EventRankingService:
    def __init__(self, model: CompatibilityModel, filters: list[HardFilter] | None = None) -> None:
        self.model = model
        self.filters = default_filters() if filters is None else filters

    def rank(self, user: UserInput, events: list[EventInput], options: RankingOptions) -> list[RankedEvent]:
        """Rank `events` for `user`, best first. Raises `RankingRequestError` or `InferenceError`."""
        self.validate(user, events, options)

        candidates = [event for event in events if all(f.keep(user, event) for f in self.filters)]
        if not candidates:
            return []

        ranked = [
            RankedEvent(event_id=event.id, score=score)
            for event, score in zip(candidates, self._score(user, candidates))
        ]
        if options.min_score is not None:
            ranked = [result for result in ranked if result.score >= options.min_score]
        ranked.sort(key=lambda result: result.score, reverse=True)
        if options.limit is not None:
            ranked = ranked[: options.limit]
        return ranked

    def validate(self, user: UserInput, events: list[EventInput], options: RankingOptions) -> None:
        """Model-dependent and cross-field checks that the request schema cannot express."""
        dimension = self.model.embedding_dim or len(user.positive_embedding)
        for field in ("positive_embedding", "negative_embedding"):
            if len(getattr(user, field)) != dimension:
                raise RankingRequestError(f"User embeddings must have dimension {dimension}.")
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

    def _score(self, user: UserInput, events: list[EventInput]) -> list[float]:
        """One batched model call; any model failure becomes an opaque `InferenceError`."""
        user_embedding = UserEmbedding(
            positive=_embedding(user.positive_embedding),
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


def _embedding(values: list[float], source_id: str | None = None) -> Embedding:
    return Embedding(vector=np.asarray(values), model_version=REQUEST_EMBEDDING_VERSION, source_id=source_id)
