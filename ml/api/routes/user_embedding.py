"""HTTP layer for user-embedding updates. Routes only translate; the math lives in `UserEmbeddingUpdater`."""

from fastapi import APIRouter, Depends

from compatibility.user_embedding import UserEmbeddingUpdater

from ..deps import get_user_embedding_updater
from ..schemas.user_embedding import UpdateUserEmbeddingRequest, UpdateUserEmbeddingResponse

router = APIRouter()


@router.post("/v1/compatibility/user-embedding/update", response_model=UpdateUserEmbeddingResponse)
def update_user_embedding(
    request: UpdateUserEmbeddingRequest,
    updater: UserEmbeddingUpdater = Depends(get_user_embedding_updater),
) -> UpdateUserEmbeddingResponse:
    embedding = updater.update(request.embedding, request.event_embedding)
    return UpdateUserEmbeddingResponse(embedding=embedding, kind=request.kind)
