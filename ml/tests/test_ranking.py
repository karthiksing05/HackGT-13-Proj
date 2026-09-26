import unittest
from datetime import datetime, timedelta, timezone

from fastapi.testclient import TestClient

from compatibility import CompatibilityModel, CosineCompatibilityModel, ScoringResult
from api import create_app
from api.helpers.filters import (
    AvailabilityFilter,
    ExcludedCategoryFilter,
    MaxDistanceFilter,
    MaxPriceFilter,
    UpcomingFilter,
    haversine_miles,
)
from api.helpers.ranking import EventRankingService
from api.schemas.ranking import EventInput, RankingOptions, UserInput

NOW = datetime(2026, 9, 26, 12, tzinfo=timezone.utc)


def user(**kwargs) -> dict:
    return {"positive_embedding": [1.0, 0.0, 0.0], "negative_embedding": [0.0, 0.0, 1.0], **kwargs}


def event(event_id: str, embedding, **kwargs) -> dict:
    return {"id": event_id, "embedding": embedding, **kwargs}


class FailingModel(CompatibilityModel):
    version = "failing-v1"

    def _score_batch(self, user_embedding, event_embeddings):
        raise RuntimeError("secret internal detail")


class NaNModel(CompatibilityModel):
    version = "nan-v1"

    def _score_batch(self, user_embedding, event_embeddings):
        return [ScoringResult(score=float("nan")) for _ in event_embeddings]


class FixedDimModel(CosineCompatibilityModel):
    embedding_dim = 4


class ApiTests(unittest.TestCase):
    def setUp(self):
        self.model = CosineCompatibilityModel(negative_weight=0.5)
        self.service = EventRankingService(self.model, filters=[MaxPriceFilter(), UpcomingFilter(lambda: NOW)])
        self.client = TestClient(create_app(self.service))

    def post(self, body: dict, client: TestClient | None = None):
        return (client or self.client).post("/v1/events/rank", json=body)

    def test_ranks_by_score_descending(self):
        response = self.post(
            {
                "user": user(),
                "events": [
                    event("far", [0.0, 1.0, 0.0]),
                    event("best", [1.0, 0.0, 0.0]),
                    event("disliked", [0.0, 0.0, 1.0]),
                    event("mixed", [1.0, 1.0, 0.0]),
                ],
            }
        )
        self.assertEqual(response.status_code, 200, response.text)
        body = response.json()
        self.assertEqual(body["model_version"], "cosine-v1")
        self.assertEqual([e["event_id"] for e in body["events"]], ["best", "mixed", "far", "disliked"])
        self.assertAlmostEqual(body["events"][0]["score"], 1.0)
        self.assertAlmostEqual(body["events"][-1]["score"], -0.5)

    def test_min_score_then_limit(self):
        events = [event(f"e{i}", [1.0, float(i), 0.0]) for i in range(5)]
        response = self.post({"user": user(), "events": events, "options": {"min_score": 0.4, "limit": 2}})
        self.assertEqual(response.status_code, 200, response.text)
        # cos(e_i) = 1/sqrt(1 + i^2): e0=1, e1=.71, e2=.45, e3=.32, e4=.24
        self.assertEqual([e["event_id"] for e in response.json()["events"]], ["e0", "e1"])

    def test_hard_filters_run_before_scoring(self):
        events = [
            event("cheap", [1.0, 0.0, 0.0], price=20),
            event("pricey", [1.0, 0.0, 0.0], price=100),
            event("unpriced", [1.0, 0.0, 0.0]),
            event("past", [1.0, 0.0, 0.0], start_time=(NOW - timedelta(days=1)).isoformat()),
        ]
        response = self.post({"user": user(max_price=30), "events": events})
        self.assertEqual({e["event_id"] for e in response.json()["events"]}, {"cheap", "unpriced"})

    def test_empty_inputs_return_empty_list(self):
        for events in ([], [event("pricey", [1.0, 0.0, 0.0], price=100)]):
            with self.subTest(events=events):
                response = self.post({"user": user(max_price=10), "events": events})
                self.assertEqual(response.status_code, 200)
                self.assertEqual(response.json(), {"events": [], "model_version": "cosine-v1"})

    def test_zero_negative_embedding_means_no_negative_signal(self):
        body = {"user": user(negative_embedding=[0.0, 0.0, 0.0]), "events": [event("e", [0.0, 0.0, 1.0])]}
        self.assertAlmostEqual(self.post(body).json()["events"][0]["score"], 0.0)

    def test_request_errors_are_400(self):
        cases = {
            "user dims differ": {"user": user(negative_embedding=[0.0, 1.0]), "events": []},
            "event dim": {"user": user(), "events": [event("e", [1.0, 0.0])]},
            "duplicate id": {"user": user(), "events": [event("e", [1.0, 0, 0]), event("e", [0, 1.0, 0])]},
            "zero positive": {"user": user(positive_embedding=[0.0, 0.0, 0.0]), "events": []},
            "zero event": {"user": user(), "events": [event("e", [0.0, 0.0, 0.0])]},
            "min_score range": {"user": user(), "events": [], "options": {"min_score": 5}},
        }
        for name, body in cases.items():
            with self.subTest(name):
                response = self.post(body)
                self.assertEqual(response.status_code, 400, response.text)
                self.assertIn("detail", response.json())

    def test_event_dimension_message(self):
        response = self.post({"user": user(), "events": [event("e", [1.0, 0.0])]})
        self.assertIn("Event embeddings must have dimension 3", response.json()["detail"])

    def test_model_embedding_dim_is_enforced(self):
        client = TestClient(create_app(EventRankingService(FixedDimModel(), filters=[])))
        response = self.post({"user": user(), "events": []}, client)
        self.assertEqual(response.status_code, 400)
        self.assertIn("dimension 4", response.json()["detail"])

    def test_schema_errors_are_422(self):
        cases = {
            "missing user": {"events": []},
            "missing embedding": {"user": user(), "events": [{"id": "e"}]},
            "empty embedding": {"user": user(positive_embedding=[]), "events": []},
            "limit zero": {"user": user(), "events": [], "options": {"limit": 0}},
            "empty id": {"user": user(), "events": [event("", [1.0, 0, 0])]},
            "naive datetime": {"user": user(), "events": [event("e", [1.0, 0, 0], start_time="2026-09-26T19:00:00")]},
        }
        for name, body in cases.items():
            with self.subTest(name):
                self.assertEqual(self.post(body).status_code, 422)

    def test_non_finite_embedding_is_rejected(self):
        payload = '{"user": {"positive_embedding": [NaN, 1], "negative_embedding": [0, 1]}, "events": []}'
        response = self.client.post(
            "/v1/events/rank", content=payload, headers={"content-type": "application/json"}
        )
        self.assertEqual(response.status_code, 422)

    def test_inference_failures_are_opaque_500(self):
        for model in (FailingModel(), NaNModel()):
            with self.subTest(model=model.version):
                client = TestClient(create_app(EventRankingService(model, filters=[])))
                with self.assertLogs("api.helpers.ranking", level="ERROR"):
                    response = self.post({"user": user(), "events": [event("e", [1.0, 0.0, 0.0])]}, client)
                self.assertEqual(response.status_code, 500)
                self.assertEqual(response.json(), {"detail": "Compatibility inference failed."})

    def test_scores_in_one_batch(self):
        calls = []
        model = CosineCompatibilityModel()
        original = model._score_batch
        model._score_batch = lambda u, events: calls.append(len(events)) or original(u, events)
        service = EventRankingService(model, filters=[])
        events = [EventInput(id=f"e{i}", embedding=[1.0, float(i), 0.0]) for i in range(10)]
        service.rank(UserInput(**user()), events, RankingOptions())
        self.assertEqual(calls, [10])


class FilterTests(unittest.TestCase):
    def test_distance(self):
        # Midtown to downtown Atlanta is roughly 2 miles.
        self.assertAlmostEqual(haversine_miles(33.7810, -84.3831, 33.7537, -84.3863), 1.9, delta=0.2)
        f = MaxDistanceFilter()
        u = UserInput(**user(latitude=33.77, longitude=-84.39, max_distance_miles=5))
        self.assertTrue(f.keep(u, EventInput(id="near", embedding=[1.0], latitude=33.78, longitude=-84.40)))
        self.assertFalse(f.keep(u, EventInput(id="athens", embedding=[1.0], latitude=33.95, longitude=-83.38)))
        self.assertTrue(f.keep(u, EventInput(id="unknown", embedding=[1.0])))

    def test_upcoming_uses_end_time_when_present(self):
        f = UpcomingFilter(lambda: NOW)
        u = UserInput(**user())
        ongoing = EventInput(id="e", embedding=[1.0], start_time=NOW - timedelta(hours=1), end_time=NOW + timedelta(hours=1))
        self.assertTrue(f.keep(u, ongoing))
        self.assertFalse(f.keep(u, EventInput(id="e", embedding=[1.0], start_time=NOW - timedelta(minutes=1))))

    def test_availability_window(self):
        f = AvailabilityFilter()
        u = UserInput(**user(available_start=NOW, available_end=NOW + timedelta(hours=4)))
        inside = EventInput(id="e", embedding=[1.0], start_time=NOW + timedelta(hours=1), end_time=NOW + timedelta(hours=2))
        too_early = EventInput(id="e", embedding=[1.0], start_time=NOW - timedelta(hours=1))
        runs_late = EventInput(id="e", embedding=[1.0], start_time=NOW + timedelta(hours=3), end_time=NOW + timedelta(hours=5))
        self.assertTrue(f.keep(u, inside))
        self.assertFalse(f.keep(u, too_early))
        self.assertFalse(f.keep(u, runs_late))

    def test_excluded_category_is_case_insensitive(self):
        f = ExcludedCategoryFilter()
        u = UserInput(**user(excluded_categories=["Nightlife"]))
        self.assertFalse(f.keep(u, EventInput(id="e", embedding=[1.0], category="nightlife ")))
        self.assertTrue(f.keep(u, EventInput(id="e", embedding=[1.0], category="music")))


if __name__ == "__main__":
    unittest.main()
