"""HTTP layer for ranking. Routes only translate; logic lives in `EventRankingService`."""

from fastapi import APIRouter, Depends

from ..deps import get_ranking_service
from ..helpers.ranking import EventRankingService
from ..schemas.ranking import RankEventsRequest, RankEventsResponse

router = APIRouter()


@router.post("/v1/events/rank", response_model=RankEventsResponse)
def rank_events(
    request: RankEventsRequest,
    ranking_service: EventRankingService = Depends(get_ranking_service),
) -> RankEventsResponse:
    events = ranking_service.rank(user=request.user, events=request.events, options=request.options)
    return RankEventsResponse(events=events, model_version=ranking_service.model.version)
