import unittest

from fastapi.testclient import TestClient

from api import create_app
from api.helpers.compatibility import MatchService
from api.helpers.ranking import EventRankingService
from compatibility import CompatibilityModel, CosineCompatibilityModel


def me(positive=(1.0, 0.0, 0.0), negative=(0.0, 0.0, 1.0)) -> dict:
    return {"positive_embedding": list(positive), "negative_embedding": list(negative)}


def candidate(cid: str, positive, negative=(0.0, 0.0, 0.0)) -> dict:
    return {"id": cid, **me(positive, negative)}


class FailingModel(CompatibilityModel):
    version = "failing-v1"

    def _score_batch(self, user_embedding, event_embeddings):
        raise RuntimeError("secret internal detail")


class CountingModel(CosineCompatibilityModel):
    def __init__(self):
        super().__init__()
        self.calls = []

    def _score_batch(self, user_embedding, event_embeddings):
        self.calls.append(len(event_embeddings))
        return super()._score_batch(user_embedding, event_embeddings)


def client_for(model=None) -> TestClient:
    model = model or CosineCompatibilityModel(negative_weight=0.5)
    service = EventRankingService(model, filters=[])
    return TestClient(create_app(service, match_service=MatchService(model)))


class UserMatchTests(unittest.TestCase):
    def setUp(self):
        self.client = client_for()

    def post(self, body):
        return self.client.post("/v1/compatibility/users", json=body)

    def test_sorted_best_first_with_clash_penalty(self):
        response = self.post(
            {
                "user": me(),
                "candidates": [
                    candidate("orthogonal", [0.0, 1.0, 0.0]),
                    candidate("twin", [1.0, 0.0, 0.0]),
                    # likes what I dislike
                    candidate("clash", [0.0, 0.0, 1.0]),
                    # likes what I like but dislikes it too
                    candidate("torn", [1.0, 0.0, 0.0], [1.0, 0.0, 0.0]),
                ],
            }
        )
        self.assertEqual(response.status_code, 200, response.text)
        results = response.json()["results"]
        self.assertEqual([r["id"] for r in results], ["twin", "torn", "orthogonal", "clash"])
        self.assertAlmostEqual(results[0]["score"], 1.0)
        self.assertAlmostEqual(results[1]["score"], 0.75)
        self.assertAlmostEqual(results[-1]["score"], -0.25)
        self.assertEqual(results[0]["percent"], 100)
        self.assertEqual(results[-1]["percent"], 0)

    def test_symmetric(self):
        a = me([1.0, 0.5, 0.0], [0.0, 0.2, 1.0])
        b = me([0.3, 1.0, 0.1], [1.0, 0.0, 0.0])
        ab = self.post({"user": a, "candidates": [{"id": "b", **b}]}).json()["results"][0]["score"]
        ba = self.post({"user": b, "candidates": [{"id": "a", **a}]}).json()["results"][0]["score"]
        self.assertAlmostEqual(ab, ba)

    def test_zero_dislikes_add_nothing(self):
        body = {"user": me(negative=(0.0, 0.0, 0.0)), "candidates": [candidate("x", [1.0, 1.0, 0.0])]}
        self.assertAlmostEqual(self.post(body).json()["results"][0]["score"], 2**-0.5)

    def test_percent_is_whole_raw_score(self):
        body = {"user": me(negative=(0.0, 0.0, 0.0)), "candidates": [candidate("x", [1.0, 1.0, 0.0])]}
        self.assertEqual(self.post(body).json()["results"][0]["percent"], 71)

    def test_empty_candidates(self):
        self.assertEqual(self.post({"user": me(), "candidates": []}).json(), {"results": []})

    def test_request_errors_are_400(self):
        for body in (
            {"user": me(), "candidates": [candidate("x", [1.0, 0.0])]},
            {"user": me(), "candidates": [candidate("x", [1.0, 0.0, 0.0]), candidate("x", [0.0, 1.0, 0.0])]},
            {"user": me(positive=(0.0, 0.0, 0.0)), "candidates": []},
        ):
            self.assertEqual(self.post(body).status_code, 400, body)


class ItineraryMatchTests(unittest.TestCase):
    def setUp(self):
        self.model = CountingModel()
        self.client = client_for(self.model)

    def post(self, body):
        return self.client.post("/v1/compatibility/itineraries", json=body)

    def test_mean_of_stop_scores_best_first(self):
        response = self.post(
            {
                "user": me(),
                "itineraries": [
                    {"id": "mixed", "events": [{"id": "a", "embedding": [1.0, 0.0, 0.0]}, {"id": "b", "embedding": [0.0, 1.0, 0.0]}]},
                    {"id": "great", "events": [{"id": "a", "embedding": [1.0, 0.0, 0.0]}]},
                    {"id": "empty", "events": []},
                ],
            }
        )
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual(body["model_version"], "cosine-v1")
        self.assertEqual([r["id"] for r in body["results"]], ["great", "mixed"])
        self.assertAlmostEqual(body["results"][0]["score"], 1.0)
        self.assertAlmostEqual(body["results"][1]["score"], 0.5)
        self.assertEqual(body["results"][1]["percent"], 50)
        self.assertEqual(body["results"][1]["scored_events"], 2)
        # one batch; the shared stop is scored once
        self.assertEqual(self.model.calls, [2])

    def test_percent_is_clamped(self):
        body = {"user": me(), "itineraries": [{"id": "bad", "events": [{"id": "d", "embedding": [0.0, 0.0, 1.0]}]}]}
        result = self.post(body).json()["results"][0]
        self.assertLess(result["score"], 0)
        self.assertEqual(result["percent"], 0)

    def test_request_errors_are_400(self):
        for body in (
            {"user": me(), "itineraries": [{"id": "x", "events": [{"id": "a", "embedding": [1.0, 0.0]}]}]},
            {"user": me(), "itineraries": [{"id": "x", "events": [{"id": "a", "embedding": [0.0, 0.0, 0.0]}]}]},
            {"user": me(), "itineraries": [{"id": "x", "events": []}, {"id": "x", "events": []}]},
        ):
            self.assertEqual(self.post(body).status_code, 400, body)

    def test_inference_failure_is_opaque_500(self):
        client = client_for(FailingModel())
        body = {"user": me(), "itineraries": [{"id": "x", "events": [{"id": "a", "embedding": [1.0, 0.0, 0.0]}]}]}
        response = client.post("/v1/compatibility/itineraries", json=body)
        self.assertEqual(response.status_code, 500)
        self.assertNotIn("secret", response.text)


if __name__ == "__main__":
    unittest.main()
