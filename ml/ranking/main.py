"""ASGI entry point, configured from the environment.

    cd ml && uvicorn ranking.main:app

    RANKING_MODEL          "cosine" (default) or "classifier"
    RANKING_CHECKPOINT     classifier checkpoint path (required for "classifier")
    RANKING_MODEL_VERSION  overrides the reported model_version
    RANKING_DEVICE         torch device for the classifier (default "cpu")
"""

import os

from compatibility import CompatibilityModel, CosineCompatibilityModel

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


app = create_app(EventRankingService(load_model()))
