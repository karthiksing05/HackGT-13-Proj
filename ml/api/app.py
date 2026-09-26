"""App factory: wires helpers onto `app.state`, mounts routers and maps errors to HTTP."""

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from compatibility.user_embedding import EmbeddingUpdateError, MovingAverageUpdater, UserEmbeddingUpdater

from .errors import EmbeddingUnavailableError, InferenceError, RankingRequestError
from .helpers.embedding import EmbeddingService
from .helpers.health import HealthState
from .helpers.profile import ProfileService
from .helpers.ranking import EventRankingService
from .routes import embedding_router, health_router, profile_router, ranking_router, user_embedding_router


def create_app(
    ranking_service: EventRankingService,
    user_embedding_updater: UserEmbeddingUpdater | None = None,
    *,
    embedding_service: EmbeddingService | None = None,
    profile_service: ProfileService | None = None,
    health: HealthState | None = None,
) -> FastAPI:
    """Without `embedding_service` the ranking routes work as before and `/v1/embed`,
    `/v1/user-profile` and `/v1/search-profile` answer 503 (texts still render with `embed: false`)."""
    app = FastAPI(title="SideQuestz event ranking")
    app.state.ranking_service = ranking_service
    app.state.user_embedding_updater = user_embedding_updater or MovingAverageUpdater()
    app.state.embedding_service = embedding_service
    app.state.profile_service = profile_service or ProfileService(embedding_service)
    app.state.health = health or HealthState(ranking_service=ranking_service, embedding_service=embedding_service)
    app.include_router(ranking_router)
    app.include_router(user_embedding_router)
    app.include_router(embedding_router)
    app.include_router(profile_router)
    app.include_router(health_router)

    @app.exception_handler(RequestValidationError)
    def schema_invalid(_: Request, exc: RequestValidationError) -> JSONResponse:
        # FastAPI's default echoes each rejected `input`, which can be a whole
        # embedding and, for NaN/inf values, is not even JSON-serializable.
        errors = [{k: e[k] for k in ("loc", "msg", "type") if k in e} for e in exc.errors()]
        return JSONResponse(status_code=422, content={"detail": errors})

    @app.exception_handler(RankingRequestError)
    @app.exception_handler(EmbeddingUpdateError)
    def invalid_request(_: Request, exc: ValueError) -> JSONResponse:
        return JSONResponse(status_code=400, content={"detail": str(exc)})

    @app.exception_handler(InferenceError)
    def inference_failed(_: Request, exc: InferenceError) -> JSONResponse:
        return JSONResponse(status_code=500, content={"detail": str(exc)})

    @app.exception_handler(EmbeddingUnavailableError)
    def embedding_unavailable(_: Request, exc: EmbeddingUnavailableError) -> JSONResponse:
        return JSONResponse(status_code=503, content={"detail": str(exc)})

    return app
