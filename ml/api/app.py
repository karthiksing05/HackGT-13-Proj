"""App factory: wires helpers onto `app.state`, mounts routers and maps errors to HTTP."""

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from compatibility.user_embedding import EmbeddingUpdateError, MovingAverageUpdater, UserEmbeddingUpdater

from .errors import InferenceError, RankingRequestError
from .helpers.ranking import EventRankingService
from .routes import ranking_router, user_embedding_router


def create_app(
    ranking_service: EventRankingService, user_embedding_updater: UserEmbeddingUpdater | None = None
) -> FastAPI:
    app = FastAPI(title="SideQuestz event ranking")
    app.state.ranking_service = ranking_service
    app.state.user_embedding_updater = user_embedding_updater or MovingAverageUpdater()
    app.include_router(ranking_router)
    app.include_router(user_embedding_router)

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

    return app
