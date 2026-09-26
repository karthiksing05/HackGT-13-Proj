"""`CompatibilityModel` adapter for a trained `CompatibilityClassifier`.

Lets the classifier replace the cosine baseline anywhere a
`CompatibilityModel` is accepted (e.g. the ranking API).
"""

from pathlib import Path

import numpy as np
import torch

from ..models import CompatibilityModel
from ..schemas import Embedding, ScoringResult, UserEmbedding
from .model import CompatibilityClassifier
from .train import load_checkpoint, resolve_device

DEFAULT_VERSION = "classifier-v1"


class ClassifierCompatibilityModel(CompatibilityModel):
    """Scores a batch of events with one forward pass of a `CompatibilityClassifier`.

    Vectors are L2-normalized before scoring to match the training embeddings.
    A user without a negative embedding gets a zero vector, the training
    convention for "no dislikes".

    With `binary` (a checkpoint trained with BCE), raw logits are passed
    through a sigmoid so scores are probabilities in [0, 1]; otherwise the raw
    regression output is returned and `score_range` is None.
    """

    name = "classifier"

    def __init__(
        self,
        classifier: CompatibilityClassifier,
        *,
        version: str = DEFAULT_VERSION,
        binary: bool = False,
        device: str | torch.device = "cpu",
        require_same_version: bool = True,
    ) -> None:
        super().__init__(require_same_version=require_same_version)
        self.device = resolve_device(device)
        self.classifier = classifier.to(self.device).eval()
        self.version = version
        self.embedding_dim = classifier.embedding_dim
        self.binary = binary
        self.score_range = (0.0, 1.0) if binary else None

    @classmethod
    def from_checkpoint(
        cls, path: str | Path, *, version: str = DEFAULT_VERSION, device: str | torch.device = "cpu"
    ) -> "ClassifierCompatibilityModel":
        classifier, checkpoint = load_checkpoint(path, device=device)
        binary = checkpoint["config"].get("loss") == "bce"
        return cls(classifier, version=version, binary=binary, device=device)

    def _score_batch(self, user_embedding: UserEmbedding, event_embeddings: list[Embedding]) -> list[ScoringResult]:
        events = _unit_rows(np.stack([e.vector for e in event_embeddings]))
        positive = _unit_rows(user_embedding.positive.vector[None, :])
        negative = (
            _unit_rows(user_embedding.negative.vector[None, :])
            if user_embedding.negative is not None
            else np.zeros_like(positive)
        )

        batch = len(event_embeddings)
        with torch.no_grad():
            components = self.classifier.score_components(
                self._tensor(positive).expand(batch, -1),
                self._tensor(negative).expand(batch, -1),
                self._tensor(events),
            )
        final = components.final_score
        scores = torch.sigmoid(final) if self.binary else final

        return [
            ScoringResult(
                score=score,
                metadata={
                    "model": self.name,
                    "version": self.version,
                    "model_version": user_embedding.model_version,
                    "component_scores": {"positive": pos, "negative": neg, "baseline": base, "raw": raw},
                },
            )
            for score, pos, neg, base, raw in zip(
                scores.tolist(),
                components.positive_cosine.tolist(),
                components.negative_cosine.tolist(),
                components.baseline_score.tolist(),
                final.tolist(),
            )
        ]

    def _tensor(self, array: np.ndarray) -> torch.Tensor:
        return torch.as_tensor(array, dtype=torch.float32, device=self.device)


def _unit_rows(matrix: np.ndarray) -> np.ndarray:
    """L2-normalize each row; zero rows stay zero."""
    norms = np.linalg.norm(matrix, axis=-1, keepdims=True)
    return np.divide(matrix, norms, out=np.zeros_like(matrix), where=norms > 0)
