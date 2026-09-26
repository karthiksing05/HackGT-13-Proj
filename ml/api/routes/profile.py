"""HTTP layer for user-profile and search-profile texts. Logic lives in `ProfileService`."""

from fastapi import APIRouter, Depends

from ..deps import get_profile_service
from ..helpers.profile import ProfileService
from ..schemas.profile import SearchProfileRequest, SearchProfileResponse, UserProfileRequest, UserProfileResponse

router = APIRouter()


@router.post("/v1/user-profile", response_model=UserProfileResponse)
def user_profile(request: UserProfileRequest, service: ProfileService = Depends(get_profile_service)) -> UserProfileResponse:
    return service.user_profile(request)


@router.post("/v1/search-profile", response_model=SearchProfileResponse)
def search_profile(request: SearchProfileRequest, service: ProfileService = Depends(get_profile_service)) -> SearchProfileResponse:
    return service.search_profile(request)
