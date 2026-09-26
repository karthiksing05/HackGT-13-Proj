"""`GET /healthz`: the report, the live probe, degraded states and the 503 on a failed probe."""

import tempfile
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

from api import create_app
from api.helpers.embedding import EmbeddingService
from api.helpers.health import HealthState
from api.helpers.ranking import EventRankingService
from compatibility import CosineCompatibilityModel
from embedding import CANARY_TEXT, CachingEmbedder, EmbedStats, NullCache, SqliteEmbeddingCache
from profiles import TEMPLATE_VERSION

from .fakes import DIM, FakeEmbedder


async def fake_jev(request):
    return {"answers": {}}


def make(embedder=None, cache=None, jev=None, mode="auto", clock=None):
    ranking = EventRankingService(CosineCompatibilityModel(), jev=jev)
    if embedder is None:
        return TestClient(create_app(ranking))
    service = EmbeddingService(CachingEmbedder(embedder, cache or NullCache()), EmbedStats())
    health = HealthState(ranking_service=ranking, embedding_service=service, mode=mode, **({"clock": clock} if clock else {}))
    return TestClient(create_app(ranking, embedding_service=service, health=health))


class HealthTests(unittest.TestCase):
    def test_report(self):
        response = make(FakeEmbedder(), jev=fake_jev).get("/healthz")
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual(body["status"], "ok")
        self.assertTrue(body["jev"])
        self.assertEqual(body["profile_template_version"], TEMPLATE_VERSION)
        self.assertEqual(body["ranking"], {"model_version": "cosine-v1", "embedding_dim": None})
        embedding = body["embedding"]
        self.assertEqual((embedding["model"], embedding["dim"], embedding["mode"], embedding["provider"]), ("fake-model", DIM, "auto", "none"))
        self.assertEqual(embedding["providers"]["fake"]["status"], "unknown")
        self.assertEqual(embedding["cache"], {"enabled": False})
        self.assertEqual(embedding["stats"]["calls"], 0)
        self.assertGreaterEqual(body["uptime_seconds"], 0)

    def test_probe_embeds_the_canary_bypassing_the_cache(self):
        embedder = FakeEmbedder(status="ok")
        with tempfile.TemporaryDirectory() as tmp:
            cache = SqliteEmbeddingCache(Path(tmp) / "c.sqlite")
            client = make(embedder, cache)
            first = client.get("/healthz?probe=1")
            second = client.get("/healthz", params={"probe": "true"})
            cache_info = second.json()["embedding"]["cache"]
            cache.close()
        self.assertEqual(first.status_code, 200, first.text)
        self.assertEqual(embedder.calls, [([CANARY_TEXT], "activity")] * 2)
        self.assertEqual(first.json()["embedding"]["provider"], "fake")
        self.assertEqual(second.json()["embedding"]["stats"]["calls"], 2)
        self.assertTrue(cache_info["enabled"])
        self.assertEqual(cache_info["entries"], 0)  # probes never fill the cache

    def test_probe_failure_is_503_with_the_report(self):
        with self.assertLogs("api.helpers.embedding", level="ERROR"):
            response = make(FakeEmbedder(fail=True)).get("/healthz?probe=1")
        self.assertEqual(response.status_code, 503)
        body = response.json()
        self.assertEqual(body["status"], "degraded")
        self.assertIn("fake", body["embedding"]["providers"])
        self.assertEqual(body["embedding"]["stats"]["errors"], 1)

    def test_degraded_when_every_provider_is_down_but_still_200(self):
        response = make(FakeEmbedder(status="error")).get("/healthz")
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["status"], "degraded")
        response = make(FakeEmbedder(status="disabled")).get("/healthz")
        self.assertEqual(response.json()["status"], "degraded")

    def test_without_an_embedding_service(self):
        client = make()
        body = client.get("/healthz").json()
        self.assertEqual(body["status"], "degraded")
        self.assertIsNone(body["embedding"]["model"])
        self.assertEqual(body["embedding"]["providers"], {})
        self.assertFalse(body["jev"])
        self.assertEqual(client.get("/healthz?probe=1").status_code, 503)

    def test_uptime_uses_the_clock(self):
        ticks = iter([1000.0, 1042.5, 1042.5])
        client = make(FakeEmbedder(), clock=lambda: next(ticks))
        self.assertEqual(client.get("/healthz").json()["uptime_seconds"], 42.5)


if __name__ == "__main__":
    unittest.main()
