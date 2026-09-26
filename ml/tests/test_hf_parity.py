"""Live parity of the HF router against the local goldens. Runs only with `ML_TEST_HF=1` and an
`HF_TOKEN` that has "Make calls to Inference Providers"; a token without it fails here on purpose."""

import json
import os
import unittest
from pathlib import Path

import numpy as np

from embedding import DIM, HFRouterEmbedder

FIXTURES = Path(__file__).parent / "fixtures"
MIN_COSINE = 0.995


@unittest.skipUnless(os.environ.get("ML_TEST_HF") == "1" and os.environ.get("HF_TOKEN"), "set ML_TEST_HF=1 and HF_TOKEN")
class HFParityTests(unittest.TestCase):
    def test_router_matches_local_goldens(self):
        golden = json.loads((FIXTURES / "qwen3_golden.json").read_text())
        texts = [t["text"] for t in json.loads((FIXTURES / "golden_texts.json").read_text())["texts"]]
        result = HFRouterEmbedder(os.environ["HF_TOKEN"]).embed_result(texts, "activity")
        self.assertEqual(result.vectors.shape, (len(texts), DIM))
        expected = np.asarray(golden["embeddings"], dtype=np.float64)
        cosines = np.sum(result.vectors.astype(np.float64) * expected, axis=1) / np.linalg.norm(expected, axis=1)
        for name, cosine in zip(golden["texts"], cosines):
            with self.subTest(text=name, provider=result.provider):
                self.assertGreaterEqual(cosine, MIN_COSINE)


if __name__ == "__main__":
    unittest.main()
