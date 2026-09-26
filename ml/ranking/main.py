"""ASGI entry point, configured from the environment.

    cd ml && uvicorn ranking.main:app

    RANKING_MODEL          "cosine" (default) or "classifier"
    RANKING_CHECKPOINT     classifier checkpoint path (required for "classifier")
    RANKING_MODEL_VERSION  overrides the reported model_version
    RANKING_DEVICE         torch device for the classifier (default "cpu")
    USER_EMBEDDING_ALPHA   preference retention for /v1/compatibility/user-embedding/update (default 0.8)
"""

import os

from compatibility import CompatibilityModel, CosineCompatibilityModel
from compatibility.user_embedding import DEFAULT_ALPHA, MovingAverageUpdater

from .api import create_app
from .service import EventRankingService


def load_model() -> CompatibilityModel:
    kind = os.environ.get("RANKING_MODEL", "cosine")
    version = os.environ.get("RANKING_MODEL_VERSION")
    if kind == "cosine":
        model: CompatibilityModel = CosineCompatibilityModel()
    elif kind == "classifier":
        # Imported here so cosine-only deployments do not need torch.
        from compatibility.classifier import ClassifierCompatibilityModel

        checkpoint = os.environ.get("RANKING_CHECKPOINT")
        if not checkpoint:
            raise RuntimeError("RANKING_CHECKPOINT must be set when RANKING_MODEL=classifier")
        model = ClassifierCompatibilityModel.from_checkpoint(
            checkpoint, device=os.environ.get("RANKING_DEVICE", "cpu")
        )
    else:
        raise RuntimeError(f"unknown RANKING_MODEL {kind!r}; expected 'cosine' or 'classifier'")
    if version:
        model.version = version
    return model


app = create_app(
    EventRankingService(load_model()),
    MovingAverageUpdater(float(os.environ.get("USER_EMBEDDING_ALPHA", DEFAULT_ALPHA))),
)
