"""Thin orchestration: encode a user and events, then score them.

No ranking, persistence, or profile updates live here; callers decide what to
do with the returned scores.
"""

from reranking.models import Event, User

from .encoders import EventEncoder, UserEncoder
from .models import CompatibilityModel
from .schemas import ScoringResult


class CompatibilityService:
    def __init__(
        self,
        user_encoder: UserEncoder,
        event_encoder: EventEncoder,
        compatibility_model: CompatibilityModel,
    ) -> None:
        self.user_encoder = user_encoder
        self.event_encoder = event_encoder
        self.compatibility_model = compatibility_model

    def score_event(self, user: User, event: Event) -> ScoringResult:
        return self.compatibility_model.score(self.user_encoder.encode(user), self.event_encoder.encode(event))

    def score_events(self, user: User, events: list[Event]) -> list[ScoringResult]:
        """Score each event for `user`. Results are in the same order as `events`."""
        if not events:
            return []
        user_embedding = self.user_encoder.encode(user)
        event_embeddings = self.event_encoder.encode_many(events)
        return self.compatibility_model.score_many(user_embedding, event_embeddings)
