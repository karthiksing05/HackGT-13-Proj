"""FastAPI dependencies that hand routes the helpers stored on `app.state`."""

from fastapi import Request

from compatibility.user_embedding import UserEmbeddingUpdater

from .helpers.compatibility import MatchService
from .helpers.embedding import EmbeddingService
from .helpers.health import HealthState
from .helpers.profile import ProfileService
from .helpers.ranking import EventRankingService


def get_ranking_service(request: Request) -> EventRankingService:
    return request.app.state.ranking_service


def get_match_service(request: Request) -> MatchService:
    return request.app.state.match_service


def get_user_embedding_updater(request: Request) -> UserEmbeddingUpdater:
    return request.app.state.user_embedding_updater


def get_embedding_service(request: Request) -> EmbeddingService | None:
    """None when the app was created without an embedder: the embed routes then answer 503."""
    return request.app.state.embedding_service


def get_profile_service(request: Request) -> ProfileService:
    return request.app.state.profile_service


def get_health(request: Request) -> HealthState:
    return request.app.state.health
