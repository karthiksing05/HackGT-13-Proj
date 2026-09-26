"""HTTP layer for user-embedding updates. Routes only translate; the math lives in `UserEmbeddingUpdater`."""

from fastapi import APIRouter, Depends, HTTPException, Request

from .schemas import UpdateUserEmbeddingRequest, UpdateUserEmbeddingResponse
from .updater import EmbeddingUpdateError, UserEmbeddingUpdater

router = APIRouter()


def get_user_embedding_updater(request: Request) -> UserEmbeddingUpdater:
    return request.app.state.user_embedding_updater


@router.post("/v1/compatibility/user-embedding/update", response_model=UpdateUserEmbeddingResponse)
def update_user_embedding(
    request: UpdateUserEmbeddingRequest,
    updater: UserEmbeddingUpdater = Depends(get_user_embedding_updater),
) -> UpdateUserEmbeddingResponse:
    try:
        embedding = updater.update(request.embedding, request.event_embedding)
    except EmbeddingUpdateError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return UpdateUserEmbeddingResponse(embedding=embedding, kind=request.kind)
