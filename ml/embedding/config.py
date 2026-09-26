"""`EmbeddingSettings.from_env()` and `build_embedder()`: the provider chain the service runs with.

    EMBED_PROVIDER=auto    Caching(Fallback([Vertex (if configured, gated on its probe), HF (if HF_TOKEN), Local]))
    EMBED_PROVIDER=vertex  Caching(Fallback([Vertex, Local if EMBED_LOCAL_FALLBACK]))
    EMBED_PROVIDER=hf      Caching(Fallback([HFRouter, Local if EMBED_LOCAL_FALLBACK]))
    EMBED_PROVIDER=local   Caching(Local)

An explicit provider that is not configured (no token, no key file) fails at startup on purpose;
in `auto` the missing ones are skipped and logged.
"""

from __future__ import annotations

import logging
import os
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Literal

from .base import DIM, MODEL, EmbedderError, Embedder
from .cache import DEFAULT_MAX_ENTRIES, EmbeddingCache, NullCache, SqliteEmbeddingCache
from .fallback import CachingEmbedder, FallbackEmbedder
from .hf_router import DEFAULT_ROUTER_BASE, DEFAULT_ROUTES, HFRouterEmbedder
from .local import LocalEmbedder
from .stats import EmbedStats
from .vertex import DEFAULT_ENDPOINT_ID, DEFAULT_INPUT_KEY, DEFAULT_LOCATION, DEFAULT_PROJECT, VertexEmbedder

logger = logging.getLogger(__name__)

ProviderName = Literal["auto", "vertex", "hf", "local"]
PROVIDERS: tuple[str, ...] = ("auto", "vertex", "hf", "local")
DEFAULT_CACHE_PATH = ".cache/embeddings.sqlite"


def _bool(value: str | None, default: bool) -> bool:
    if value is None or value.strip() == "":
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


def _int(value: str | None, default: int) -> int:
    return int(value) if value not in (None, "") else default


def _float(value: str | None, default: float) -> float:
    return float(value) if value not in (None, "") else default


@dataclass
class EmbeddingSettings:
    provider: ProviderName = "auto"
    model: str = MODEL
    dim: int = DIM
    hf_token: str | None = None
    hf_routes: tuple[str, ...] = DEFAULT_ROUTES
    router_base: str = DEFAULT_ROUTER_BASE
    hf_timeout: float = 20.0
    hf_max_batch: int = 32
    hf_max_retries: int = 3
    hf_auth_backoff: float = 600.0
    local_fallback: bool = True
    local_threads: int | None = 8
    local_device: str = "cpu"
    warmup: bool = True
    cache_path: str | None = DEFAULT_CACHE_PATH
    cache_max_entries: int = DEFAULT_MAX_ENTRIES
    max_chars: int = 2000
    vertex_credentials: str | None = None
    vertex_project: str = DEFAULT_PROJECT
    vertex_location: str = DEFAULT_LOCATION
    vertex_endpoint_id: str = DEFAULT_ENDPOINT_ID
    vertex_host: str | None = None
    vertex_input_key: str = DEFAULT_INPUT_KEY
    vertex_timeout: float = 20.0
    vertex_max_batch: int = 16
    extra: dict = field(default_factory=dict)

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "EmbeddingSettings":
        env = os.environ if env is None else env
        provider = (env.get("EMBED_PROVIDER") or "auto").strip().lower()
        if provider not in PROVIDERS:
            raise ValueError(f"EMBED_PROVIDER must be one of {PROVIDERS}, got {provider!r}")
        routes = tuple(r.strip() for r in (env.get("HF_EMBED_ROUTES") or ",".join(DEFAULT_ROUTES)).split(",") if r.strip())
        threads = _int(env.get("EMBED_LOCAL_THREADS"), 8)
        return cls(
            provider=provider,  # type: ignore[arg-type]
            model=env.get("EMBED_MODEL") or MODEL,
            dim=_int(env.get("EMBED_DIM"), DIM),
            hf_token=env.get("HF_TOKEN") or None,
            hf_routes=routes or DEFAULT_ROUTES,
            router_base=env.get("HF_ROUTER_BASE") or DEFAULT_ROUTER_BASE,
            hf_timeout=_float(env.get("HF_EMBED_TIMEOUT"), 20.0),
            hf_max_batch=_int(env.get("HF_EMBED_MAX_BATCH"), 32),
            hf_max_retries=_int(env.get("HF_EMBED_MAX_RETRIES"), 3),
            hf_auth_backoff=_float(env.get("HF_AUTH_BACKOFF"), 600.0),
            local_fallback=_bool(env.get("EMBED_LOCAL_FALLBACK"), True),
            local_threads=threads if threads > 0 else None,
            local_device=env.get("EMBED_LOCAL_DEVICE") or "cpu",
            warmup=_bool(env.get("EMBED_WARMUP"), True),
            cache_path=(env.get("EMBED_CACHE_PATH") if "EMBED_CACHE_PATH" in env else DEFAULT_CACHE_PATH) or None,
            cache_max_entries=_int(env.get("EMBED_CACHE_MAX_ENTRIES"), DEFAULT_MAX_ENTRIES),
            max_chars=_int(env.get("EMBED_MAX_CHARS"), 2000),
            vertex_credentials=env.get("GOOGLE_APPLICATION_CREDENTIALS") or None,
            vertex_project=env.get("VERTEX_PROJECT") or DEFAULT_PROJECT,
            vertex_location=env.get("VERTEX_LOCATION") or DEFAULT_LOCATION,
            vertex_endpoint_id=env.get("VERTEX_ENDPOINT_ID") or DEFAULT_ENDPOINT_ID,
            vertex_host=env.get("VERTEX_HOST") or None,
            vertex_input_key=env.get("VERTEX_INPUT_KEY") or DEFAULT_INPUT_KEY,
            vertex_timeout=_float(env.get("VERTEX_TIMEOUT"), 20.0),
            vertex_max_batch=_int(env.get("VERTEX_MAX_BATCH"), 16),
        )

    def vertex_configured(self) -> bool:
        return bool(
            self.vertex_credentials
            and os.path.isfile(self.vertex_credentials)
            and self.vertex_project
            and self.vertex_location
            and self.vertex_endpoint_id
        )

    def redacted(self) -> dict:
        """Settings for a log line: no secrets."""
        return {
            "provider": self.provider,
            "model": self.model,
            "dim": self.dim,
            "hf_token": "set" if self.hf_token else "unset",
            "hf_routes": list(self.hf_routes),
            "vertex": "configured" if self.vertex_configured() else "unconfigured",
            "local_fallback": self.local_fallback,
            "local_threads": self.local_threads,
            "cache_path": self.cache_path,
            "warmup": self.warmup,
        }


def build_cache(settings: EmbeddingSettings) -> EmbeddingCache:
    if not settings.cache_path:
        return NullCache()
    return SqliteEmbeddingCache(settings.cache_path, settings.cache_max_entries)


def build_embedder(settings: EmbeddingSettings, *, stats: EmbedStats | None = None, cache: EmbeddingCache | None = None) -> Embedder:
    cache = build_cache(settings) if cache is None else cache

    def local() -> Embedder:
        return LocalEmbedder(model=settings.model, dim=settings.dim, threads=settings.local_threads, device=settings.local_device)

    def hf() -> Embedder:
        return HFRouterEmbedder(
            settings.hf_token,
            model=settings.model,
            dim=settings.dim,
            routes=settings.hf_routes,
            router_base=settings.router_base,
            timeout=settings.hf_timeout,
            max_batch=settings.hf_max_batch,
            max_retries=settings.hf_max_retries,
            max_chars=settings.max_chars,
            auth_backoff=settings.hf_auth_backoff,
        )

    def vertex(require_probe: bool) -> Embedder:
        return VertexEmbedder(
            project=settings.vertex_project,
            location=settings.vertex_location,
            endpoint_id=settings.vertex_endpoint_id,
            host=settings.vertex_host,
            input_key=settings.vertex_input_key,
            credentials_path=settings.vertex_credentials,
            model=settings.model,
            dim=settings.dim,
            timeout=settings.vertex_timeout,
            max_batch=settings.vertex_max_batch,
            max_chars=settings.max_chars,
            require_probe=require_probe,
        )

    def fallback(chain: list[Embedder]) -> Embedder:
        return FallbackEmbedder(chain, auth_reset_after=settings.hf_auth_backoff, stats=stats)

    if settings.provider == "local":
        inner: Embedder = local()
    elif settings.provider == "hf":
        inner = fallback([hf()] + ([local()] if settings.local_fallback else []))
    elif settings.provider == "vertex":
        inner = fallback([vertex(require_probe=False)] + ([local()] if settings.local_fallback else []))
    else:
        chain: list[Embedder] = []
        if settings.vertex_configured():
            try:
                chain.append(vertex(require_probe=True))
            except EmbedderError as exc:
                logger.warning("vertex provider skipped: %s", exc.message)
        else:
            logger.info("vertex provider skipped: no service account configured")
        if settings.hf_token:
            chain.append(hf())
        else:
            logger.info("hf provider skipped: HF_TOKEN is not set")
        chain.append(local())
        inner = fallback(chain)
    logger.info("embedding settings: %s; chain: %s", settings.redacted(), [p.name for p in getattr(inner, "chain", [inner])])
    return CachingEmbedder(inner, cache, stats=stats)
