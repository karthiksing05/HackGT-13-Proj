"""The local provider: lazy loading with a fake model (always), and the real Qwen model against
the goldens when `ML_TEST_LOCAL_EMBEDDER=1` (downloads/loads Qwen3-Embedding-0.6B)."""

import json
import os
import unittest
from pathlib import Path

import numpy as np

from embedding import CANARY_TEXT, DIM, EmbedderError, LocalEmbedder

FIXTURES = Path(__file__).parent / "fixtures"


class FakeSentenceTransformer:
    def __init__(self, dim: int = 8, fail: bool = False) -> None:
        self.dim = dim
        self.fail = fail
        self.calls: list[list[str]] = []
        self.max_seq_length = None

    def encode(self, texts, **kwargs):
        self.calls.append(list(texts))
        if self.fail:
            raise RuntimeError("cuda exploded")
        return np.array([[len(t)] + [1.0] * (self.dim - 1) for t in texts], dtype=np.float32)


class LocalEmbedderFakeModelTests(unittest.TestCase):
    def test_loads_lazily_once_and_normalizes(self):
        model = FakeSentenceTransformer()
        loads = []

        def loader():
            loads.append(1)
            return model

        embedder = LocalEmbedder(dim=8, loader=loader)
        self.assertEqual(embedder.status()["local"].status, "not_loaded")
        self.assertEqual(loads, [])
        vectors = embedder.embed(["abc", "", "abcdef"])
        self.assertEqual(loads, [1])
        self.assertEqual(model.calls, [["abc", "abcdef"]])
        np.testing.assert_allclose(np.linalg.norm(vectors.astype(np.float64), axis=1), [1.0, 0.0, 1.0], atol=1e-6)
        embedder.embed(["x"])
        self.assertEqual(loads, [1])
        status = embedder.status()["local"]
        self.assertEqual(status.status, "loaded")
        self.assertIsNotNone(status.last_ok_at)

    def test_load_failure_is_unavailable(self):
        def loader():
            raise OSError("no weights")

        embedder = LocalEmbedder(dim=8, loader=loader)
        with self.assertLogs("embedding.local", level="ERROR"), self.assertRaises(EmbedderError) as ctx:
            embedder.embed(["a"])
        self.assertEqual(ctx.exception.kind, "unavailable")
        self.assertEqual(embedder.status()["local"].status, "error")

    def test_encode_failure_is_retryable(self):
        embedder = LocalEmbedder(dim=8, loader=lambda: FakeSentenceTransformer(fail=True))
        with self.assertRaises(EmbedderError) as ctx:
            embedder.embed(["a"])
        self.assertTrue(ctx.exception.retryable)
        self.assertIn("encode failed", embedder.status()["local"].detail)

    def test_wrong_width_is_bad_response(self):
        embedder = LocalEmbedder(dim=8, loader=lambda: FakeSentenceTransformer(dim=5))
        with self.assertRaises(EmbedderError) as ctx:
            embedder.embed(["a"])
        self.assertEqual(ctx.exception.kind, "bad_response")

    def test_warmup_loads_and_embeds_the_canary(self):
        model = FakeSentenceTransformer()
        embedder = LocalEmbedder(dim=8, loader=lambda: model)
        embedder.warmup()
        self.assertEqual(model.calls, [[CANARY_TEXT]])
        self.assertEqual(embedder.status()["local"].status, "loaded")


@unittest.skipUnless(os.environ.get("ML_TEST_LOCAL_EMBEDDER") == "1", "set ML_TEST_LOCAL_EMBEDDER=1 to load Qwen3-Embedding-0.6B")
class LocalEmbedderGoldenTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.golden = json.loads((FIXTURES / "qwen3_golden.json").read_text())
        cls.texts = [t["text"] for t in json.loads((FIXTURES / "golden_texts.json").read_text())["texts"]]
        cls.embedder = LocalEmbedder()
        cls.vectors = cls.embedder.embed(cls.texts, "activity").astype(np.float64)

    def test_dimension_and_unit_norm(self):
        self.assertEqual(self.vectors.shape, (len(self.texts), DIM))
        np.testing.assert_allclose(np.linalg.norm(self.vectors, axis=1), 1.0, atol=1e-5)
        self.assertEqual(self.embedder.status()["local"].status, "loaded")

    def test_cosine_against_the_golden(self):
        golden = np.asarray(self.golden["embeddings"], dtype=np.float64)
        cosines = np.sum(self.vectors * golden, axis=1) / np.linalg.norm(golden, axis=1)
        for name, cosine in zip(self.golden["texts"], cosines):
            with self.subTest(text=name):
                self.assertGreaterEqual(cosine, 0.999)

    def test_parity_with_the_stored_catalog_vector(self):
        reference = self.golden.get("reference_activity")
        if reference is None:
            self.skipTest("golden has no stored reference vector")
        stored = np.asarray(reference["embedding"], dtype=np.float64)
        cosine = float(self.vectors[-1] @ stored / np.linalg.norm(stored))
        self.assertGreaterEqual(cosine, 0.995)  # fp32 CPU vs the bf16 GPU backfill

    def test_blank_is_zero_and_canary_matches(self):
        vectors = self.embedder.embed(["", CANARY_TEXT])
        self.assertEqual(vectors[0].tolist(), [0.0] * DIM)
        # Alone vs in a batch of five: padding changes the arithmetic slightly (0.9999 observed).
        self.assertGreaterEqual(float(vectors[1].astype(np.float64) @ self.vectors[0]), 0.999)


if __name__ == "__main__":
    unittest.main()
