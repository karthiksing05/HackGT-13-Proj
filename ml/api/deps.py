"""FastAPI dependencies that hand routes the helpers stored on `app.state`."""

from fastapi import Request

from compatibility.user_embedding import UserEmbeddingUpdater

from .helpers.ranking import EventRankingService


def get_ranking_service(request: Request) -> EventRankingService:
    return request.app.state.ranking_service


def get_user_embedding_updater(request: Request) -> UserEmbeddingUpdater:
    return request.app.state.user_embedding_updater
