"""Response schema for `GET /healthz`."""

from typing import Literal

from pydantic import BaseModel


class HealthResponse(BaseModel):
    """`status` is `degraded` when no embedding provider can serve (ranking still works).

    `ranking`: `{"model_version", "embedding_dim"}`. `embedding`: `{"model", "dim", "mode",
    "provider", "providers": {name: {status, detail, last_ok_at, last_error_at, latency_ms}},
    "cache": {...}, "stats": {...}}`. `jev` says whether the Jev rerank is configured.
    """

    status: Literal["ok", "degraded"]
    uptime_seconds: float
    ranking: dict
    embedding: dict
    profile_template_version: str
    jev: bool
