"""`POST /v1/embed` with a fake embedder: happy path, blanks, cache counts, 422 and 503."""

import math
import tempfile
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

from api import create_app
from api.helpers.embedding import EmbeddingService
from api.helpers.ranking import EventRankingService
from compatibility import CosineCompatibilityModel
from embedding import CachingEmbedder, EmbedStats, NullCache, SqliteEmbeddingCache

from .fakes import DIM, FakeEmbedder

URL = "/v1/embed"


def norm(vector) -> float:
    return math.sqrt(sum(x * x for x in vector))


def make_client(embedder=None, cache=None, stats=None):
    ranking = EventRankingService(CosineCompatibilityModel())
    if embedder is None:
        return TestClient(create_app(ranking))
    service = EmbeddingService(CachingEmbedder(embedder, cache or NullCache()), stats)
    return TestClient(create_app(ranking, embedding_service=service))


class EmbedApiTests(unittest.TestCase):
    def test_happy_path(self):
        embedder = FakeEmbedder()
        response = make_client(embedder).post(URL, json={"texts": ["Interests:\n- jazz", "Cost:\n- free"], "kind": "user"})
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual({k: body[k] for k in ("model", "dim", "provider", "cached")}, {"model": "fake-model", "dim": DIM, "provider": "fake", "cached": 0})
        self.assertEqual(len(body["embeddings"]), 2)
        for vector in body["embeddings"]:
            self.assertEqual(len(vector), DIM)
            self.assertAlmostEqual(norm(vector), 1.0, places=12)
        self.assertEqual(embedder.calls, [(["Interests:\n- jazz", "Cost:\n- free"], "user")])

    def test_kind_defaults_to_activity_and_never_changes_the_text(self):
        embedder = FakeEmbedder()
        make_client(embedder).post(URL, json={"texts": ["Interests:\n- jazz"]})
        self.assertEqual(embedder.calls, [(["Interests:\n- jazz"], "activity")])

    def test_blank_text_is_a_zero_vector(self):
        embedder = FakeEmbedder()
        body = make_client(embedder).post(URL, json={"texts": ["Interests:\n- jazz", "", "   "]}).json()
        self.assertAlmostEqual(norm(body["embeddings"][0]), 1.0)
        self.assertEqual(body["embeddings"][1], [0.0] * DIM)
        self.assertEqual(body["embeddings"][2], [0.0] * DIM)
        self.assertEqual(embedder.calls[0][0], ["Interests:\n- jazz"])  # blanks never reach a provider

    def test_cached_count_and_provider(self):
        with tempfile.TemporaryDirectory() as tmp:
            cache = SqliteEmbeddingCache(Path(tmp) / "c.sqlite")
            embedder = FakeEmbedder()
            client = make_client(embedder, cache)
            first = client.post(URL, json={"texts": ["a", "b"]}).json()
            second = client.post(URL, json={"texts": ["a", "b", "c"]}).json()
            cache.close()
        self.assertEqual((first["cached"], first["provider"]), (0, "fake"))
        self.assertEqual((second["cached"], second["provider"]), (2, "fake"))
        self.assertEqual(second["embeddings"][:2], first["embeddings"])
        self.assertEqual([c[0] for c in embedder.calls], [["a", "b"], ["c"]])

    def test_fully_cached_request_reports_the_cache(self):
        with tempfile.TemporaryDirectory() as tmp:
            cache = SqliteEmbeddingCache(Path(tmp) / "c.sqlite")
            client = make_client(FakeEmbedder(), cache)
            client.post(URL, json={"texts": ["a"]})
            body = client.post(URL, json={"texts": ["a"]}).json()
            cache.close()
        self.assertEqual((body["cached"], body["provider"]), (1, "cache"))

    def test_schema_errors_are_422(self):
        client = make_client(FakeEmbedder())
        cases = {
            "no body": {},
            "empty list": {"texts": []},
            "too many": {"texts": ["x"] * 65},
            "unknown kind": {"texts": ["x"], "kind": "query"},
            "too long": {"texts": ["x" * 8001]},
            "not strings": {"texts": [1, 2]},
        }
        for name, body in cases.items():
            with self.subTest(name):
                self.assertEqual(client.post(URL, json=body).status_code, 422)

    def test_provider_failure_is_503(self):
        stats = EmbedStats()
        client = make_client(FakeEmbedder(fail=True), stats=stats)
        with self.assertLogs("api.helpers.embedding", level="ERROR"):
            response = client.post(URL, json={"texts": ["x"]})
        self.assertEqual(response.status_code, 503)
        self.assertEqual(response.json(), {"detail": "Embedding provider unavailable."})
        self.assertEqual(stats.snapshot()["errors"], 1)

    def test_wrong_shape_is_503(self):
        with self.assertLogs("api.helpers.embedding", level="ERROR"):
            response = make_client(FakeEmbedder(bad_shape=True)).post(URL, json={"texts": ["x"]})
        self.assertEqual(response.status_code, 503)

    def test_without_an_embedding_service_is_503(self):
        response = make_client().post(URL, json={"texts": ["x"]})
        self.assertEqual(response.status_code, 503)
        self.assertEqual(response.json(), {"detail": "Embedding provider unavailable."})

    def test_stats_are_recorded(self):
        stats = EmbedStats()
        make_client(FakeEmbedder(), stats=stats).post(URL, json={"texts": ["a", "b"], "kind": "search"})
        snap = stats.snapshot()
        self.assertEqual((snap["calls"], snap["texts"], snap["by_kind"], snap["last_provider"]), (1, 2, {"search": 2}, "fake"))


if __name__ == "__main__":
    unittest.main()
