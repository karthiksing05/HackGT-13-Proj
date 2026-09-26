"""`HealthState`: what `GET /healthz` reports, with an optional live probe of the embedders."""

import time
from collections.abc import Callable

from profiles import TEMPLATE_VERSION

from ..errors import EmbeddingUnavailableError
from ..schemas.health import HealthResponse
from .embedding import EmbeddingService
from .ranking import EventRankingService

# Provider statuses that mean "cannot serve right now".
_DOWN = ("error", "disabled")


class HealthState:
    def __init__(
        self,
        *,
        ranking_service: EventRankingService,
        embedding_service: EmbeddingService | None = None,
        mode: str = "none",
        jev: bool | None = None,
        started_at: float | None = None,
        clock: Callable[[], float] = time.time,
    ) -> None:
        self.ranking_service = ranking_service
        self.embedding_service = embedding_service
        self.mode = mode
        self.jev = ranking_service.jev is not None if jev is None else jev
        self.started_at = clock() if started_at is None else started_at
        self._clock = clock

    def snapshot(self, probe: bool = False) -> HealthResponse:
        """The current state; with `probe`, first embeds the canary (raises `EmbeddingUnavailableError`)."""
        probed_provider = None
        if probe:
            if self.embedding_service is None:
                raise EmbeddingUnavailableError()
            probed_provider = self.embedding_service.probe().provider
        providers = self._providers()
        stats = self.embedding_service.stats.snapshot() if self.embedding_service else {}
        model = self.ranking_service.model
        return HealthResponse(
            status="ok" if self._can_embed(providers) else "degraded",
            uptime_seconds=round(max(0.0, self._clock() - self.started_at), 1),
            ranking={"model_version": model.version, "embedding_dim": model.embedding_dim},
            embedding={
                "model": self.embedding_service.model if self.embedding_service else None,
                "dim": self.embedding_service.dim if self.embedding_service else None,
                "mode": self.mode,
                "provider": probed_provider or stats.get("last_provider") or "none",
                "providers": providers,
                "cache": self._cache_info(),
                "stats": stats,
            },
            profile_template_version=TEMPLATE_VERSION,
            jev=self.jev,
        )

    def _providers(self) -> dict[str, dict]:
        if self.embedding_service is None:
            return {}
        return {name: status.to_dict() for name, status in self.embedding_service.embedder.status().items()}

    def _cache_info(self) -> dict:
        cache = getattr(self.embedding_service.embedder, "cache", None) if self.embedding_service else None
        return cache.info() if cache is not None else {"enabled": False}

    @staticmethod
    def _can_embed(providers: dict[str, dict]) -> bool:
        """Some provider is not known to be down (unprobed ones count until they fail)."""
        return any(p["status"] not in _DOWN for p in providers.values())
