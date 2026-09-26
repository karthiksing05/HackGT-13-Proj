"""HTTP layer for ranking. Routes only translate; logic lives in `EventRankingService`."""

from fastapi import APIRouter, Depends

from ..deps import get_ranking_service
from ..helpers.ranking import EventRankingService
from ..schemas.ranking import RankEventsRequest, RankEventsResponse

router = APIRouter()


@router.post("/v1/events/rank", response_model=RankEventsResponse)
async def rank_events(
    request: RankEventsRequest,
    ranking_service: EventRankingService = Depends(get_ranking_service),
) -> RankEventsResponse:
    outcome = await ranking_service.rank_and_rerank(
        user=request.user,
        events=request.events,
        options=request.options,
        search_embedding=request.search_embedding,
        search_text=request.search_text,
    )
    return RankEventsResponse(
        events=outcome.events, model_version=ranking_service.model.version, reranked=outcome.reranked
    )
