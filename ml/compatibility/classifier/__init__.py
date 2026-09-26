"""Supervised compatibility classifier over precomputed embeddings.

(positive user, negative user, event) embeddings -> engineered features ->
small network -> compatibility score, optionally as a learned correction to
the cosine baseline; plus training, evaluation and the baseline itself.
Requires PyTorch; the parent `compatibility` package does not import this
module, so it stays optional there.
"""

from .config import MODEL_VARIANTS, ClassifierConfig
from .evaluate import ScoredData, evaluate, format_comparison, score_dataset, score_metrics, summarize_components
from .features import (
    build_embedding_interaction_features,
    build_features,
    compute_cosine_baseline,
    compute_similarity_features,
    cosine_baseline_score,
    cosine_similarity,
    feature_dim,
    interaction_feature_dim,
)
from .model import CompatibilityClassifier, FlatMLP, LateFusionMLP, ScoreComponents
from .train import (
    EarlyStopping,
    TrainingHistory,
    TrainingCallback,
    TrainingResult,
    load_checkpoint,
    resolve_device,
    save_checkpoint,
    set_seed,
    train,
)

__all__ = [
    "MODEL_VARIANTS",
    "ClassifierConfig",
    "CompatibilityClassifier",
    "EarlyStopping",
    "FlatMLP",
    "LateFusionMLP",
    "ScoreComponents",
    "ScoredData",
    "TrainingHistory",
    "TrainingCallback",
    "TrainingResult",
    "build_embedding_interaction_features",
    "build_features",
    "compute_cosine_baseline",
    "compute_similarity_features",
    "cosine_baseline_score",
    "cosine_similarity",
    "evaluate",
    "feature_dim",
    "format_comparison",
    "interaction_feature_dim",
    "load_checkpoint",
    "resolve_device",
    "save_checkpoint",
    "score_dataset",
    "score_metrics",
    "set_seed",
    "summarize_components",
    "train",
]
