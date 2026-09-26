"""HTTP layer for the ranking service. Routes only translate; logic lives in `EventRankingService`."""

from fastapi import APIRouter, Depends, FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from compatibility.user_embedding import MovingAverageUpdater, UserEmbeddingUpdater
from compatibility.user_embedding.routes import router as user_embedding_router

from .schemas import RankEventsRequest, RankEventsResponse
from .service import EventRankingService, InferenceError, RankingRequestError

router = APIRouter()


def get_ranking_service(request: Request) -> EventRankingService:
    return request.app.state.ranking_service


@router.post("/v1/events/rank", response_model=RankEventsResponse)
def rank_events(
    request: RankEventsRequest,
    ranking_service: EventRankingService = Depends(get_ranking_service),
) -> RankEventsResponse:
    events = ranking_service.rank(user=request.user, events=request.events, options=request.options)
    return RankEventsResponse(events=events, model_version=ranking_service.model.version)


def create_app(
    ranking_service: EventRankingService, user_embedding_updater: UserEmbeddingUpdater | None = None
) -> FastAPI:
    app = FastAPI(title="SideQuestz event ranking")
    app.state.ranking_service = ranking_service
    app.state.user_embedding_updater = user_embedding_updater or MovingAverageUpdater()
    app.include_router(router)
    app.include_router(user_embedding_router)

    @app.exception_handler(RequestValidationError)
    def schema_invalid(_: Request, exc: RequestValidationError) -> JSONResponse:
        # FastAPI's default echoes each rejected `input`, which can be a whole
        # embedding and, for NaN/inf values, is not even JSON-serializable.
        errors = [{k: e[k] for k in ("loc", "msg", "type") if k in e} for e in exc.errors()]
        return JSONResponse(status_code=422, content={"detail": errors})

    @app.exception_handler(RankingRequestError)
    def invalid_request(_: Request, exc: RankingRequestError) -> JSONResponse:
        return JSONResponse(status_code=400, content={"detail": str(exc)})

    @app.exception_handler(InferenceError)
    def inference_failed(_: Request, exc: InferenceError) -> JSONResponse:
        return JSONResponse(status_code=500, content={"detail": str(exc)})

    return app
