"""ASGI entry point, configured from the environment (and a `.env` found from the working directory).

    cd ml && uvicorn api.main:app

    RANKING_MODEL          "classifier" (default) or "cosine"
    RANKING_CHECKPOINT     classifier checkpoint: a local path or hf://<owner>/<repo>/<file>
                           (default: the final model bundled at checkpoints/compatibility_classifier.pt)
    HF_TOKEN               read access to a private hf:// checkpoint (not needed by default)
    RANKING_MODEL_VERSION  overrides the reported model_version
    RANKING_DEVICE         torch device for the classifier (default "cpu")
    USER_EMBEDDING_ALPHA   preference retention for /v1/compatibility/user-embedding/update (default 0.8)
    SEARCH_WEIGHT          how far a request's search_embedding pulls the user's positive embedding
                           in /v1/events/rank (default 0.6)
    TYPESAFE_API_KEY       enables the Jev rerank in /v1/events/rank (unset: model order only);
                           TYPESAFE_DEFAULT_MODEL / TYPESAFE_BASE_URL are read by the SDK
    RERANK_TOP_K           how many of the best model-ranked events Jev reorders (default 20)
"""

import os

from dotenv import find_dotenv, load_dotenv

from compatibility import CompatibilityModel, CosineCompatibilityModel
from compatibility.user_embedding import DEFAULT_ALPHA, MovingAverageUpdater

from .app import create_app
from .checkpoints import DEFAULT_CHECKPOINT, resolve_checkpoint
from .helpers.ranking import DEFAULT_RERANK_TOP_K, DEFAULT_SEARCH_WEIGHT, EventRankingService


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


load_dotenv(find_dotenv(usecwd=True))
app = create_app(
    EventRankingService(
        load_model(),
        search_weight=float(os.environ.get("SEARCH_WEIGHT", DEFAULT_SEARCH_WEIGHT)),
        jev=load_jev(),
        rerank_top_k=int(os.environ.get("RERANK_TOP_K", DEFAULT_RERANK_TOP_K)),
    ),
    MovingAverageUpdater(float(os.environ.get("USER_EMBEDDING_ALPHA", DEFAULT_ALPHA))),
)
