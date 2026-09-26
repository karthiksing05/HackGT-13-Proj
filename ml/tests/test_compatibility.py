import unittest

import numpy as np

from compatibility import (
    CompatibilityModel,
    CompatibilityService,
    CosineCompatibilityModel,
    Embedding,
    EmbeddingError,
    Encoder,
    IncompatibleEmbeddingsError,
    ScoringResult,
    TextEventEncoder,
    TextUserEncoder,
    UserEmbedding,
)
from reranking import Event, User

VERSION = "test-v1"


def emb(values, version: str = VERSION, **kwargs) -> Embedding:
    return Embedding.from_unnormalized(np.array(values, dtype=float), version, **kwargs)


def user_emb(positive, negative=None, version: str = VERSION) -> UserEmbedding:
    return UserEmbedding(
        positive=emb(positive, version),
        negative=emb(negative, version) if negative is not None else None,
    )


class EmbeddingTests(unittest.TestCase):
    def test_dimension_and_read_only(self):
        e = emb([3, 4])
        self.assertEqual(e.dimension, 2)
        self.assertTrue(e.normalized)
        np.testing.assert_allclose(e.vector, [0.6, 0.8])
        with self.assertRaises(ValueError):
            e.vector[0] = 1.0

    def test_rejects_malformed_vectors(self):
        for bad in ([], [[1.0, 2.0]], [1.0, float("nan")]):
            with self.subTest(bad=bad), self.assertRaises(EmbeddingError):
                Embedding(vector=np.array(bad), model_version=VERSION)

    def test_rejects_false_normalized_flag(self):
        with self.assertRaises(EmbeddingError):
            Embedding(vector=np.array([1.0, 1.0]), model_version=VERSION, normalized=True)

    def test_rejects_zero_vector_normalization(self):
        with self.assertRaises(EmbeddingError):
            emb([0, 0])


class UserEmbeddingTests(unittest.TestCase):
    def test_negative_must_match_positive_dimension(self):
        with self.assertRaisesRegex(IncompatibleEmbeddingsError, "dimension"):
            UserEmbedding(positive=emb([1, 0]), negative=emb([1, 0, 0]))

    def test_negative_must_match_positive_version(self):
        with self.assertRaisesRegex(IncompatibleEmbeddingsError, "model_version"):
            UserEmbedding(positive=emb([1, 0], version="v1"), negative=emb([0, 1], version="v2"))


class CosineTests(unittest.TestCase):
    model = CosineCompatibilityModel()

    def test_identical_vectors(self):
        self.assertAlmostEqual(self.model.score(user_emb([1, 2, 3]), emb([1, 2, 3])).score, 1.0)

    def test_orthogonal_vectors(self):
        self.assertAlmostEqual(self.model.score(user_emb([1, 0]), emb([0, 1])).score, 0.0)

    def test_opposite_vectors(self):
        self.assertAlmostEqual(self.model.score(user_emb([1, 0]), emb([-1, 0])).score, -1.0)

    def test_unnormalized_embeddings_are_normalized_at_scoring(self):
        user = UserEmbedding(positive=Embedding(vector=np.array([2.0, 0.0]), model_version=VERSION))
        event = Embedding(vector=np.array([5.0, 5.0]), model_version=VERSION)
        self.assertAlmostEqual(self.model.score(user, event).score, np.sqrt(0.5))

    def test_batch_scoring_matches_single_scoring_in_order(self):
        user = user_emb([1, 0, 0], negative=[0, 1, 0])
        events = [emb([1, 0, 0]), emb([0, 1, 0]), emb([1, 1, 0]), emb([-1, 0, 0])]

        results = self.model.score_many(user, events)

        self.assertEqual(len(results), 4)
        np.testing.assert_allclose(
            [r.score for r in results],
            [1.0, -0.5, np.sqrt(0.5) * (1 - 0.5), -1.0],
            atol=1e-12,
        )
        for event, result in zip(events, results):
            self.assertAlmostEqual(result.score, self.model.score(user, event).score)

    def test_empty_batch(self):
        self.assertEqual(self.model.score_many(user_emb([1, 0]), []), [])

    def test_dimension_mismatch(self):
        with self.assertRaisesRegex(IncompatibleEmbeddingsError, "dimension"):
            self.model.score(user_emb([1, 0]), emb([1, 0, 0]))

    def test_dimension_mismatch_anywhere_in_batch(self):
        with self.assertRaises(IncompatibleEmbeddingsError):
            self.model.score_many(user_emb([1, 0]), [emb([1, 0]), emb([1, 0, 0])])

    def test_version_mismatch(self):
        with self.assertRaisesRegex(IncompatibleEmbeddingsError, "model_version"):
            self.model.score(user_emb([1, 0], version="v1"), emb([1, 0], version="v2"))

    def test_version_check_can_be_disabled(self):
        model = CosineCompatibilityModel(require_same_version=False)
        self.assertAlmostEqual(model.score(user_emb([1, 0], version="v1"), emb([1, 0], version="v2")).score, 1.0)


class NegativeSignalTests(unittest.TestCase):
    def test_score_is_positive_minus_weighted_negative(self):
        model = CosineCompatibilityModel(negative_weight=0.4)
        user = user_emb([1, 0], negative=[0, 1])
        result = model.score(user, emb([1, 1]))

        pos = neg = np.sqrt(0.5)
        self.assertAlmostEqual(result.score, pos - 0.4 * neg)
        self.assertAlmostEqual(result.metadata["component_scores"]["positive"], pos)
        self.assertAlmostEqual(result.metadata["component_scores"]["negative"], neg)
        self.assertEqual(result.metadata["negative_weight"], 0.4)

    def test_disliked_event_scores_below_neutral_event(self):
        model = CosineCompatibilityModel(negative_weight=1.0)
        user = user_emb([1, 0, 0], negative=[0, 1, 0])
        disliked, neutral = model.score_many(user, [emb([0, 1, 0]), emb([0, 0, 1])])
        self.assertAlmostEqual(disliked.score, -1.0)
        self.assertAlmostEqual(neutral.score, 0.0)

    def test_no_negative_embedding_means_no_penalty(self):
        model = CosineCompatibilityModel(negative_weight=1.0)
        result = model.score(user_emb([1, 0]), emb([1, 1]))
        self.assertAlmostEqual(result.score, np.sqrt(0.5))
        self.assertEqual(result.metadata["component_scores"]["negative"], 0.0)

    def test_zero_weight_ignores_negative(self):
        model = CosineCompatibilityModel(negative_weight=0.0)
        self.assertAlmostEqual(model.score(user_emb([1, 0], negative=[1, 0]), emb([1, 0])).score, 1.0)

    def test_negative_weight_must_be_non_negative(self):
        with self.assertRaises(ValueError):
            CosineCompatibilityModel(negative_weight=-0.1)


# --- Service tests with fakes ----------------------------------------------

USER = User(id="u1", name="Sam", interests=["jazz"], dislikes=["crowds"])
EVENTS = [
    Event(id="e1", name="Jazz Night", description="live jazz", category="Music"),
    Event(id="e2", name="Chess Club", description="board games", category="Games"),
]


class FakeEventEncoder(Encoder):
    """Maps events to fixed vectors by id and records what it was asked to encode."""

    def __init__(self, vectors: dict[str, list[float]], version: str = VERSION) -> None:
        self.vectors = vectors
        self.version = version
        self.calls: list[list[str]] = []

    def encode_many(self, items):
        self.calls.append([item.id for item in items])
        return [emb(self.vectors[item.id], version=self.version, source_id=item.id) for item in items]


class FakeUserEncoder(FakeEventEncoder):
    """Like FakeEventEncoder, but vectors are (positive, negative) pairs."""

    def encode_many(self, items):
        self.calls.append([item.id for item in items])
        result = []
        for item in items:
            positive, negative = self.vectors[item.id]
            result.append(
                UserEmbedding(
                    positive=emb(positive, version=self.version, source_id=item.id),
                    negative=emb(negative, version=self.version, source_id=item.id) if negative else None,
                )
            )
        return result


class RecordingModel(CompatibilityModel):
    """Returns a score derived from source ids so pass-through is observable."""

    def __init__(self) -> None:
        super().__init__()
        self.received: list[tuple[str, list[str]]] = []

    def _score_batch(self, user_embedding, event_embeddings):
        self.received.append((user_embedding.source_id, [e.source_id for e in event_embeddings]))
        return [ScoringResult(score=float(i), metadata={"event": e.source_id}) for i, e in enumerate(event_embeddings)]


class ServiceTests(unittest.TestCase):
    def setUp(self):
        self.user_encoder = FakeUserEncoder({"u1": ([1, 0], [0, 1])})
        self.event_encoder = FakeEventEncoder({"e1": [1, 0], "e2": [0, 1]})
        self.model = RecordingModel()
        self.service = CompatibilityService(self.user_encoder, self.event_encoder, self.model)

    def test_score_events_passes_data_through(self):
        results = self.service.score_events(USER, EVENTS)

        self.assertEqual(self.user_encoder.calls, [["u1"]])
        self.assertEqual(self.event_encoder.calls, [["e1", "e2"]])  # batched, one call
        self.assertEqual(self.model.received, [("u1", ["e1", "e2"])])
        self.assertEqual([r.metadata["event"] for r in results], ["e1", "e2"])

    def test_score_event(self):
        result = self.service.score_event(USER, EVENTS[1])
        self.assertEqual(self.model.received, [("u1", ["e2"])])
        self.assertEqual(result.metadata["event"], "e2")

    def test_empty_events_skips_encoding(self):
        self.assertEqual(self.service.score_events(USER, []), [])
        self.assertEqual(self.user_encoder.calls, [])

    def test_with_cosine_model(self):
        service = CompatibilityService(self.user_encoder, self.event_encoder, CosineCompatibilityModel(0.5))
        scores = [r.score for r in service.score_events(USER, EVENTS)]
        np.testing.assert_allclose(scores, [1.0, -0.5], atol=1e-12)  # e2 matches the negative signal

    def test_encoder_version_drift_is_detected(self):
        stale_events = FakeEventEncoder({"e1": [1, 0], "e2": [0, 1]}, version="test-v0")
        service = CompatibilityService(self.user_encoder, stale_events, CosineCompatibilityModel())
        with self.assertRaises(IncompatibleEmbeddingsError):
            service.score_events(USER, EVENTS)


class TextEncoderTests(unittest.TestCase):
    def setUp(self):
        self.calls: list[list[str]] = []

        def embed_fn(texts: list[str]) -> np.ndarray:
            self.calls.append(texts)
            return np.array([[len(t), 1.0, i] for i, t in enumerate(texts)], dtype=float)

        self.embed_fn = embed_fn

    def test_shared_embed_fn_produces_normalized_versioned_embeddings(self):
        user_encoder = TextUserEncoder(self.embed_fn, model_version="text-v1")
        event_encoder = TextEventEncoder(self.embed_fn, model_version="text-v1")

        user_embedding = user_encoder.encode(USER)
        event_embeddings = event_encoder.encode_many(EVENTS)

        self.assertEqual(len(self.calls[1]), 2)  # events embedded in one batch
        self.assertEqual(user_embedding.source_id, "u1")
        self.assertEqual([e.source_id for e in event_embeddings], ["e1", "e2"])
        for e in [user_embedding.positive, user_embedding.negative, *event_embeddings]:
            self.assertEqual(e.model_version, "text-v1")
            self.assertTrue(e.normalized)
            self.assertAlmostEqual(float(np.linalg.norm(e.vector)), 1.0)

    def test_likes_and_dislikes_embedded_separately_in_one_call(self):
        users = [
            User(id="a", name="A", interests=["jazz"], dislikes=["crowds"]),
            User(id="b", name="B", interests=["chess"]),
            User(id="c", name="C", interests=["hiking"], dislikes=["rain"]),
        ]
        results = TextUserEncoder(self.embed_fn, model_version="text-v1").encode_many(users)

        self.assertEqual(len(self.calls), 1)
        texts = self.calls[0]
        self.assertEqual(len(texts), 5)  # 3 positive + 2 negative
        self.assertIn("jazz", texts[0])
        self.assertNotIn("crowds", texts[0])  # dislikes never leak into the positive text
        self.assertIn("crowds", texts[3])
        self.assertIn("rain", texts[4])

        a, b, c = results
        self.assertIsNone(b.negative)
        self.assertEqual((a.negative.source_id, c.negative.source_id), ("a", "c"))
        self.assertFalse(np.allclose(a.positive.vector, a.negative.vector))

    def test_bad_embed_fn_output_shape(self):
        encoder = TextEventEncoder(lambda texts: np.zeros(3), model_version="text-v1")
        with self.assertRaises(EmbeddingError):
            encoder.encode_many(EVENTS)


if __name__ == "__main__":
    unittest.main()
