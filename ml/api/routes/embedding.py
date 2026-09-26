"""HTTP layer for raw text embedding. Routes only translate; the work is in `EmbeddingService`."""

from fastapi import APIRouter, Depends

from ..deps import get_embedding_service
from ..errors import EmbeddingUnavailableError
from ..helpers.embedding import EmbeddingService
from ..schemas.embedding import EmbedRequest, EmbedResponse

router = APIRouter()


@router.post("/v1/embed", response_model=EmbedResponse)
def embed(
    request: EmbedRequest,
    service: EmbeddingService | None = Depends(get_embedding_service),
) -> EmbedResponse:
    if service is None:
        raise EmbeddingUnavailableError()
    outcome = service.embed(request.texts, request.kind)
    return EmbedResponse(
        embeddings=outcome.rows(), model=service.model, dim=service.dim, provider=outcome.provider, cached=outcome.cached
    )
