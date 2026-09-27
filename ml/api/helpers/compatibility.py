"""Taste matches for the social features: user <-> user and user <-> itinerary.

User <-> user is symmetric cosine math over the stored likes (p) and dislikes
(n) vectors, "likes minus clashes":

    raw = cos(pA, pB) - clash_weight * (cos(pA, nB) + cos(nA, pB)) / 2

A missing (all-zero) dislikes vector contributes 0.

User <-> itinerary is the mean compatibility-model score (the same model the
ranking route uses) over the itinerary's stops, scored in one batched call.
The classifier is roughly on the 0-1 label scale.

Both report `percent` as the raw score clamped to [0, 1], as a whole number
(0.9 -> 90).

Stateless: everything comes in with the request and nothing is stored.
"""

import logging
import math

import numpy as np

from compatibility import CompatibilityModel, Embedding, UserEmbedding

from ..errors import InferenceError, RankingRequestError
from ..schemas.compatibility import (
    ItineraryInput,
    ItineraryMatch,
    MatchUser,
    UserCandidate,
    UserMatch,
)

logger = logging.getLogger(__name__)

REQUEST_EMBEDDING_VERSION = "request"

# How much one person liking what the other dislikes costs.
DEFAULT_CLASH_WEIGHT = 0.5


class MatchService:
    def __init__(
        self,
        model: CompatibilityModel,
        *,
        clash_weight: float = DEFAULT_CLASH_WEIGHT,
    ) -> None:
        if clash_weight < 0:
            raise ValueError(f"clash_weight must be >= 0, got {clash_weight}")
        self.model = model
        self.clash_weight = clash_weight

    def match_users(self, user: MatchUser, candidates: list[UserCandidate]) -> list[UserMatch]:
        """Every candidate's match with `user`, best first."""
        self._validate_user(user)
        dim = len(user.positive_embedding)
        _unique([c.id for c in candidates], "candidate")
        for c in candidates:
            for field in ("positive_embedding", "negative_embedding"):
                if len(getattr(c, field)) != dim:
                    raise RankingRequestError(f"Candidate {c.id!r} {field} must have dimension {dim}.")
        if not candidates:
            return []

        p_a = _unit_rows(np.asarray([user.positive_embedding]))[0]
        n_a = _unit_rows(np.asarray([user.negative_embedding]))[0]
        p_b = _unit_rows(np.asarray([c.positive_embedding for c in candidates]))
        n_b = _unit_rows(np.asarray([c.negative_embedding for c in candidates]))

        # Zero rows stay zero, so a missing dislikes vector adds nothing.
        likes = p_b @ p_a
        clashes = (n_b @ p_a + p_b @ n_a) / 2.0
        raw = np.clip(likes, -1.0, 1.0) - self.clash_weight * np.clip(clashes, -1.0, 1.0)

        results = [
            UserMatch(id=c.id, score=float(s), percent=_percent(float(s))) for c, s in zip(candidates, raw)
        ]
        return sorted(results, key=lambda r: (-r.score, r.id))

    def match_itineraries(self, user: MatchUser, itineraries: list[ItineraryInput]) -> list[ItineraryMatch]:
        """Each itinerary's mean stop score for `user`, best first; itineraries without events are omitted."""
        self._validate_user(user)
        dim = self.model.embedding_dim or len(user.positive_embedding)
        if len(user.positive_embedding) != dim:
            raise RankingRequestError(f"User embeddings must have dimension {dim}.")
        _unique([it.id for it in itineraries], "itinerary")
        for it in itineraries:
            _unique([e.id for e in it.events], f"event in itinerary {it.id!r}")
            for e in it.events:
                if len(e.embedding) != dim:
                    raise RankingRequestError(
                        f"Event embeddings must have dimension {dim} (event {e.id!r} has {len(e.embedding)})."
                    )
                if not any(e.embedding):
                    raise RankingRequestError(f"Event {e.id!r} embedding must not be all zeros.")

        kept = [it for it in itineraries if it.events]
        if not kept:
            return []

        # One batched call over every stop; the same stop in two itineraries is scored once.
        unique: dict[bytes, list[float]] = {}
        for it in kept:
            for e in it.events:
                unique.setdefault(_key(e.embedding), e.embedding)
        keys = list(unique)
        scores = dict(zip(keys, self._score(user, [unique[k] for k in keys])))

        results = []
        for it in kept:
            mean = float(np.mean([scores[_key(e.embedding)] for e in it.events]))
            results.append(
                ItineraryMatch(id=it.id, score=mean, percent=_percent(mean), scored_events=len(it.events))
            )
        return sorted(results, key=lambda r: (-r.score, r.id))

    def _score(self, user: MatchUser, vectors: list[list[float]]) -> list[float]:
        user_embedding = UserEmbedding(
            positive=_embedding(user.positive_embedding),
            negative=_embedding(user.negative_embedding) if any(user.negative_embedding) else None,
        )
        try:
            results = self.model.score_many(user_embedding, [_embedding(v) for v in vectors])
        except Exception as exc:
            logger.exception("compatibility model %s failed on %d stops", self.model.version, len(vectors))
            raise InferenceError("Compatibility inference failed.") from exc
        scores = [r.score for r in results]
        if len(scores) != len(vectors) or not all(math.isfinite(s) for s in scores):
            logger.error("compatibility model %s returned invalid scores", self.model.version)
            raise InferenceError("Compatibility inference failed.")
        return scores

    @staticmethod
    def _validate_user(user: MatchUser) -> None:
        if len(user.negative_embedding) != len(user.positive_embedding):
            raise RankingRequestError("User embeddings must have the same dimension.")
        if not any(user.positive_embedding):
            raise RankingRequestError("User positive_embedding must not be all zeros.")


def _percent(raw: float) -> int:
    return round(100 * min(1.0, max(0.0, raw)))


def _unit_rows(matrix: np.ndarray) -> np.ndarray:
    """L2-normalize each row; zero rows stay zero."""
    matrix = matrix.astype(np.float64)
    norms = np.linalg.norm(matrix, axis=-1, keepdims=True)
    return np.divide(matrix, norms, out=np.zeros_like(matrix), where=norms > 0)


def _unique(ids: list[str], what: str) -> None:
    seen: set[str] = set()
    for i in ids:
        if i in seen:
            raise RankingRequestError(f"Duplicate {what} id {i!r}.")
        seen.add(i)


def _key(vector: list[float]) -> bytes:
    return np.asarray(vector, dtype=np.float64).tobytes()


def _embedding(values: list[float]) -> Embedding:
    return Embedding(vector=np.asarray(values), model_version=REQUEST_EMBEDDING_VERSION)
