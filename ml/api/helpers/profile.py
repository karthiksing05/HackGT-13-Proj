"""`ProfileService`: structured app inputs -> eight-section texts -> embeddings -> hash, in one place."""

from embedding import DIM, MODEL, EmbedKind
from profiles import TEMPLATE_VERSION, build_profile_texts, build_search_text, profile_hash

from ..errors import EmbeddingUnavailableError
from ..schemas.profile import SearchProfileRequest, SearchProfileResponse, UserProfileRequest, UserProfileResponse
from .embedding import EmbeddingService, EmbedOutcome


class ProfileService:
    def __init__(self, embedding: EmbeddingService | None) -> None:
        self.embedding = embedding
        self.model = embedding.model if embedding else MODEL
        self.dim = embedding.dim if embedding else DIM

    def user_profile(self, request: UserProfileRequest) -> UserProfileResponse:
        texts = build_profile_texts(request.to_input())
        positive = negative = None
        provider = "none"
        if request.embed:
            outcome = self._embed([texts.positive, texts.negative], "user")
            positive, negative = outcome.rows()
            provider = outcome.provider
        return UserProfileResponse(
            positive_text=texts.positive,
            negative_text=texts.negative,
            positive_embedding=positive,
            negative_embedding=negative,
            profile_text_hash=profile_hash(texts.positive, texts.negative),
            template_version=TEMPLATE_VERSION,
            model=self.model,
            dim=self.dim,
            provider=provider,
        )

    def search_profile(self, request: SearchProfileRequest) -> SearchProfileResponse:
        text = build_search_text(request.to_input())
        embedding = None
        provider = "none"
        if request.embed and text:
            outcome = self._embed([text], "search")
            embedding = outcome.rows()[0]
            provider = outcome.provider
        return SearchProfileResponse(search_text=text, search_embedding=embedding, model=self.model, dim=self.dim, provider=provider)

    def _embed(self, texts: list[str], kind: EmbedKind) -> EmbedOutcome:
        if self.embedding is None:
            raise EmbeddingUnavailableError()
        return self.embedding.embed(texts, kind)
