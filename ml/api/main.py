"""ASGI entry point, configured from the environment (and a `.env` found from the working directory).

    cd ml && uvicorn api.main:app

Ranking:
    RANKING_MODEL          "classifier" (default) or "cosine"
    RANKING_CHECKPOINT     classifier checkpoint: a local path or hf://<owner>/<repo>/<file>
                           (default: the final model bundled at checkpoints/compatibility_classifier.pt)
    RANKING_MODEL_VERSION  overrides the reported model_version
    RANKING_DEVICE         torch device for the classifier (default "cpu")
    USER_EMBEDDING_ALPHA   preference retention for /v1/compatibility/user-embedding/update (default 0.8)
    SEARCH_WEIGHT          how far a request's search_embedding pulls the user's positive embedding
                           in /v1/events/rank (default 0.6)
    TYPESAFE_API_KEY       enables the Jev rerank in /v1/events/rank (unset: model order only);
                           TYPESAFE_DEFAULT_MODEL / TYPESAFE_BASE_URL are read by the SDK
    RERANK_TOP_K           how many of the best model-ranked events Jev scores (default 20; 12 in ml.service)

Embedding (`embedding.EmbeddingSettings.from_env`; see README "Embedding providers"):
    EMBED_PROVIDER         auto (default) | vertex | hf | local
    EMBED_MODEL / EMBED_DIM  Qwen/Qwen3-Embedding-0.6B / 1024 (must match the activities' embeddingModel)
    HF_TOKEN               HF router (needs "Make calls to Inference Providers"); also read access to
                           a private hf:// checkpoint
    HF_EMBED_ROUTES, HF_ROUTER_BASE, HF_EMBED_TIMEOUT, HF_EMBED_MAX_BATCH, HF_EMBED_MAX_RETRIES, HF_AUTH_BACKOFF
    GOOGLE_APPLICATION_CREDENTIALS, VERTEX_PROJECT, VERTEX_LOCATION, VERTEX_ENDPOINT_ID, VERTEX_HOST, VERTEX_INPUT_KEY
    EMBED_LOCAL_FALLBACK, EMBED_LOCAL_THREADS, EMBED_LOCAL_DEVICE, EMBED_WARMUP
    EMBED_CACHE_PATH (empty disables), EMBED_CACHE_MAX_ENTRIES, EMBED_MAX_CHARS
"""

import logging
import os
import threading

from dotenv import find_dotenv, load_dotenv

from compatibility import CompatibilityModel, CosineCompatibilityModel
from compatibility.user_embedding import DEFAULT_ALPHA, MovingAverageUpdater
from embedding import Embedder, EmbeddingSettings, EmbedStats, build_embedder

from .app import create_app
from .checkpoints import DEFAULT_CHECKPOINT, resolve_checkpoint
from .helpers.embedding import EmbeddingService
from .helpers.health import HealthState
from .helpers.profile import ProfileService
from .helpers.ranking import DEFAULT_RERANK_TOP_K, DEFAULT_SEARCH_WEIGHT, EventRankingService

logger = logging.getLogger(__name__)


def load_model() -> CompatibilityModel:
    kind = os.environ.get("RANKING_MODEL", "classifier")
    version = os.environ.get("RANKING_MODEL_VERSION")
    if kind == "cosine":
        model: CompatibilityModel = CosineCompatibilityModel()
    elif kind == "classifier":
        # Imported here so cosine-only deployments do not need torch.
        from compatibility.classifier import ClassifierCompatibilityModel

        checkpoint = resolve_checkpoint(os.environ.get("RANKING_CHECKPOINT") or DEFAULT_CHECKPOINT)
        model = ClassifierCompatibilityModel.from_checkpoint(
            checkpoint, device=os.environ.get("RANKING_DEVICE", "cpu")
        )
    else:
        raise RuntimeError(f"unknown RANKING_MODEL {kind!r}; expected 'classifier' or 'cosine'")
    if version:
        model.version = version
    return model


def load_jev():
    """Jev's client when TYPESAFE_API_KEY is set, else None (no rerank)."""
    if not os.environ.get("TYPESAFE_API_KEY"):
        return None
    from reranking.jev import call_jev

    return call_jev


def load_embedder(stats: EmbedStats) -> tuple[Embedder, EmbeddingSettings]:
    """The provider chain from the environment; with EMBED_WARMUP the slow parts (weights, first
    remote call, the Vertex probe) run on a daemon thread so startup never blocks."""
    settings = EmbeddingSettings.from_env()
    embedder = build_embedder(settings, stats=stats)
    if settings.warmup:
        start_warmup(embedder)
    return embedder, settings


def start_warmup(embedder: Embedder) -> threading.Thread:
    def run() -> None:
        try:
            embedder.warmup()
            logger.info("embedding warmup done: %s", {k: v.status for k, v in embedder.status().items()})
        except Exception:  # never fatal; /healthz shows what is wrong
            logger.exception("embedding warmup failed")

    thread = threading.Thread(target=run, name="embed-warmup", daemon=True)
    thread.start()
    return thread


load_dotenv(find_dotenv(usecwd=True))
_stats = EmbedStats()
_embedder, _settings = load_embedder(_stats)
_embedding = EmbeddingService(_embedder, _stats)
_ranking = EventRankingService(
    load_model(),
    search_weight=float(os.environ.get("SEARCH_WEIGHT", DEFAULT_SEARCH_WEIGHT)),
    jev=load_jev(),
    rerank_top_k=int(os.environ.get("RERANK_TOP_K", DEFAULT_RERANK_TOP_K)),
)
app = create_app(
    _ranking,
    MovingAverageUpdater(float(os.environ.get("USER_EMBEDDING_ALPHA", DEFAULT_ALPHA))),
    embedding_service=_embedding,
    profile_service=ProfileService(_embedding),
    health=HealthState(ranking_service=_ranking, embedding_service=_embedding, mode=_settings.provider),
)
