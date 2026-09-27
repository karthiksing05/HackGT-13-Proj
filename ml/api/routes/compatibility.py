"""HTTP layer for taste matches. Routes only translate; the math lives in `MatchService`."""

from fastapi import APIRouter, Depends

from ..deps import get_match_service
from ..helpers.compatibility import MatchService
from ..schemas.compatibility import (
    ItineraryMatchRequest,
    ItineraryMatchResponse,
    UserMatchRequest,
    UserMatchResponse,
)

router = APIRouter()


@router.post("/v1/compatibility/users", response_model=UserMatchResponse)
def match_users(request: UserMatchRequest, service: MatchService = Depends(get_match_service)) -> UserMatchResponse:
    return UserMatchResponse(results=service.match_users(request.user, request.candidates))


@router.post("/v1/compatibility/itineraries", response_model=ItineraryMatchResponse)
def match_itineraries(
    request: ItineraryMatchRequest, service: MatchService = Depends(get_match_service)
) -> ItineraryMatchResponse:
    return ItineraryMatchResponse(
        results=service.match_itineraries(request.user, request.itineraries), model_version=service.model.version
    )
