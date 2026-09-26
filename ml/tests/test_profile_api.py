"""`POST /v1/user-profile` and `POST /v1/search-profile` with a fake embedder."""

import json
import math
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

from api import create_app
from api.helpers.embedding import EmbeddingService
from api.helpers.ranking import EventRankingService
from compatibility import CosineCompatibilityModel
from profiles import TEMPLATE_VERSION, profile_hash

from .fakes import DIM, FakeEmbedder

FIXTURES = Path(__file__).parent / "fixtures"
USER_URL = "/v1/user-profile"
SEARCH_URL = "/v1/search-profile"


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


def norm(vector) -> float:
    return math.sqrt(sum(x * x for x in vector))


def make_client(embedder=None):
    ranking = EventRankingService(CosineCompatibilityModel())
    if embedder is None:
        return TestClient(create_app(ranking))
    return TestClient(create_app(ranking, embedding_service=EmbeddingService(embedder)))


class UserProfileTests(unittest.TestCase):
    def test_golden_a_through_the_api(self):
        golden = fixture("profile_jordan.json")
        embedder = FakeEmbedder()
        response = make_client(embedder).post(USER_URL, json=golden["request"])
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual(body["positive_text"], golden["positive_text"])
        self.assertEqual(body["negative_text"], golden["negative_text"])
        self.assertEqual(body["profile_text_hash"], golden["profile_text_hash"])
        self.assertEqual({k: body[k] for k in ("template_version", "model", "dim", "provider")}, {"template_version": TEMPLATE_VERSION, "model": "fake-model", "dim": DIM, "provider": "fake"})
        self.assertAlmostEqual(norm(body["positive_embedding"]), 1.0, places=12)
        self.assertAlmostEqual(norm(body["negative_embedding"]), 1.0, places=12)
        self.assertEqual(embedder.calls, [([golden["positive_text"], golden["negative_text"]], "user")])

    def test_no_dislikes_gives_a_zero_negative_vector(self):
        body = make_client(FakeEmbedder()).post(USER_URL, json={"ratings": {"outdoors": 5}, "company": "solo"}).json()
        self.assertEqual(body["negative_text"], "")
        self.assertEqual(body["negative_embedding"], [0.0] * DIM)
        self.assertAlmostEqual(norm(body["positive_embedding"]), 1.0)

    def test_all_empty_profile_is_unranked(self):
        body = make_client(FakeEmbedder()).post(USER_URL, json={}).json()
        self.assertEqual((body["positive_text"], body["negative_text"]), ("", ""))
        self.assertEqual(body["positive_embedding"], [0.0] * DIM)
        self.assertEqual(body["negative_embedding"], [0.0] * DIM)
        self.assertEqual(body["profile_text_hash"], profile_hash("", ""))

    def test_embed_false_only_renders(self):
        embedder = FakeEmbedder()
        body = make_client(embedder).post(USER_URL, json={"ratings": {"food": 5}, "embed": False}).json()
        self.assertIsNone(body["positive_embedding"])
        self.assertIsNone(body["negative_embedding"])
        self.assertEqual(body["provider"], "none")
        self.assertTrue(body["positive_text"].startswith("Interests:\n- local food"))
        self.assertEqual(embedder.calls, [])

    def test_aliases_and_unknown_rating_keys(self):
        body = make_client(FakeEmbedder()).post(USER_URL, json={"ratings": {"bogus": 5, "liveMusic": 4, "big_crowds": 1}, "pace": "chill"}).json()
        self.assertIn("- live music", body["positive_text"])
        self.assertIn("- relaxed rhythm", body["positive_text"])
        self.assertIn("- large crowds", body["negative_text"])

    def test_schema_errors_are_422(self):
        client = make_client(FakeEmbedder())
        cases = {
            "stars 0": {"rated_events": [{"stars": 0}]},
            "stars 6": {"rated_events": [{"stars": 6}]},
            "company": {"company": "duo"},
            "spend": {"spend": "lots"},
            "flexibility": {"flexibility": "whatever"},
            "ratings not ints": {"ratings": {"food": "five"}},
            "answers not an object": {"answers": "none"},
        }
        for name, body in cases.items():
            with self.subTest(name):
                self.assertEqual(client.post(USER_URL, json=body).status_code, 422)

    def test_provider_failure_is_503(self):
        with self.assertLogs("api.helpers.embedding", level="ERROR"):
            response = make_client(FakeEmbedder(fail=True)).post(USER_URL, json={"ratings": {"food": 5}})
        self.assertEqual(response.status_code, 503)
        self.assertEqual(response.json(), {"detail": "Embedding provider unavailable."})

    def test_text_only_works_without_an_embedding_service(self):
        client = make_client()
        self.assertEqual(client.post(USER_URL, json={"ratings": {"food": 5}}).status_code, 503)
        body = client.post(USER_URL, json={"ratings": {"food": 5}, "embed": False}).json()
        self.assertTrue(body["positive_text"].startswith("Interests:\n- local food"))
        self.assertEqual(body["model"], "Qwen/Qwen3-Embedding-0.6B")
        self.assertEqual(body["dim"], 1024)


class SearchProfileTests(unittest.TestCase):
    def test_golden_c_through_the_api(self):
        golden = fixture("search_jordan.json")
        embedder = FakeEmbedder()
        response = make_client(embedder).post(SEARCH_URL, json=golden["request"])
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual(body["search_text"], golden["search_text"])
        self.assertAlmostEqual(norm(body["search_embedding"]), 1.0, places=12)
        self.assertEqual({k: body[k] for k in ("model", "dim", "provider")}, {"model": "fake-model", "dim": DIM, "provider": "fake"})
        self.assertEqual(embedder.calls, [([golden["search_text"]], "search")])

    def test_empty_request_has_no_embedding_and_no_provider_call(self):
        embedder = FakeEmbedder()
        body = make_client(embedder).post(SEARCH_URL, json={}).json()
        self.assertEqual(body, {"search_text": "", "search_embedding": None, "model": "fake-model", "dim": DIM, "provider": "none"})
        self.assertEqual(embedder.calls, [])

    def test_embed_false(self):
        body = make_client(FakeEmbedder()).post(SEARCH_URL, json={"tags": ["Chill"], "embed": False}).json()
        self.assertEqual(body["search_text"], "Environment:\n- laid-back atmosphere\n\nPace:\n- relaxed rhythm")
        self.assertIsNone(body["search_embedding"])

    def test_schema_errors_are_422(self):
        client = make_client(FakeEmbedder())
        cases = {
            "naive datetime": {"start_time": "2026-09-25T14:10:00"},
            "budget 4": {"budget": 4},
            "who": {"who": "everyone"},
            "pace": {"pace": "frantic"},
            "tags not a list": {"tags": "Outdoors"},
        }
        for name, body in cases.items():
            with self.subTest(name):
                self.assertEqual(client.post(SEARCH_URL, json=body).status_code, 422)

    def test_provider_failure_is_503_only_when_there_is_text(self):
        client = make_client(FakeEmbedder(fail=True))
        self.assertEqual(client.post(SEARCH_URL, json={}).status_code, 200)
        with self.assertLogs("api.helpers.embedding", level="ERROR"):
            self.assertEqual(client.post(SEARCH_URL, json={"tags": ["Food"]}).status_code, 503)


if __name__ == "__main__":
    unittest.main()
