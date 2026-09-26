import math
import unittest

import numpy as np
from fastapi.testclient import TestClient

from compatibility import CosineCompatibilityModel
from compatibility.user_embedding import EmbeddingUpdateError, MovingAverageUpdater, UserEmbeddingUpdater, l2_normalize
from ranking import EventRankingService, create_app

URL = "/v1/compatibility/user-embedding/update"
CURRENT = [0.12, -0.42, 0.31]
EVENT = [0.09, -0.30, 0.55]


def expected(current, event, alpha):
    v = alpha * np.asarray(current) + (1 - alpha) * np.asarray(event)
    return (v / np.linalg.norm(v)).tolist()


def norm(v):
    return math.sqrt(sum(x * x for x in v))


class UpdaterTests(unittest.TestCase):
    def test_matches_moving_average_formula(self):
        for alpha in (0.0, 0.3, 0.8, 1.0):
            with self.subTest(alpha=alpha):
                updated = MovingAverageUpdater(alpha).update(CURRENT, EVENT)
                np.testing.assert_allclose(updated, expected(CURRENT, EVENT, alpha), rtol=1e-12)

    def test_default_alpha_is_0_8(self):
        np.testing.assert_allclose(MovingAverageUpdater().update(CURRENT, EVENT), expected(CURRENT, EVENT, 0.8))

    def test_zero_current_becomes_event_direction(self):
        np.testing.assert_allclose(MovingAverageUpdater().update([0.0, 0.0], [0.0, 3.0]), [0.0, 1.0])

    def test_zero_combination_returns_zero_vector_not_nan(self):
        self.assertEqual(MovingAverageUpdater().update([0.0, 0.0], [0.0, 0.0]), [0.0, 0.0])
        # 0.5 * u + 0.5 * (-u) == 0
        self.assertEqual(MovingAverageUpdater(0.5).update([1.0, -2.0], [-1.0, 2.0]), [0.0, 0.0])

    def test_normalize_does_not_overflow(self):
        np.testing.assert_allclose(l2_normalize(np.array([1e300, 1e300])), [2**-0.5, 2**-0.5])

    def test_dimension_mismatch_raises(self):
        with self.assertRaises(EmbeddingUpdateError):
            MovingAverageUpdater().update([1.0, 0.0], [1.0, 0.0, 0.0])

    def test_rejects_invalid_alpha(self):
        for alpha in (-0.1, 1.1):
            with self.assertRaises(ValueError):
                MovingAverageUpdater(alpha)


class ApiTests(unittest.TestCase):
    def setUp(self):
        self.client = TestClient(create_app(EventRankingService(CosineCompatibilityModel())))

    def post(self, **overrides):
        body = {"embedding": CURRENT, "kind": "positive", "event_embedding": EVENT, **overrides}
        return self.client.post(URL, json=body)

    def test_positive_and_negative_updates_echo_kind(self):
        for kind in ("positive", "negative"):
            with self.subTest(kind=kind):
                response = self.post(kind=kind)
                self.assertEqual(response.status_code, 200, response.text)
                body = response.json()
                self.assertEqual(body["kind"], kind)
                self.assertEqual(len(body["embedding"]), len(CURRENT))
                self.assertAlmostEqual(norm(body["embedding"]), 1.0)
                np.testing.assert_allclose(body["embedding"], expected(CURRENT, EVENT, 0.8))

    def test_route_uses_injected_updater(self):
        class Constant(UserEmbeddingUpdater):
            def update(self, current_embedding, event_embedding):
                return [1.0] + [0.0] * (len(current_embedding) - 1)

        client = TestClient(create_app(EventRankingService(CosineCompatibilityModel()), Constant()))
        body = {"embedding": CURRENT, "kind": "negative", "event_embedding": EVENT}
        self.assertEqual(client.post(URL, json=body).json(), {"embedding": [1.0, 0.0, 0.0], "kind": "negative"})

    def test_zero_vectors_are_safe(self):
        response = self.post(embedding=[0.0, 0.0, 0.0], event_embedding=[0.0, 0.0, 0.0])
        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["embedding"], [0.0, 0.0, 0.0])

    def test_dimension_mismatch_is_400(self):
        response = self.post(event_embedding=[0.1, 0.2])
        self.assertEqual(response.status_code, 400)
        self.assertIn("dimension", response.json()["detail"])

    def test_malformed_input_is_422(self):
        cases = {
            "invalid kind": {"kind": "neutral"},
            "kind wrong case": {"kind": "Positive"},
            "empty embedding": {"embedding": []},
            "empty event_embedding": {"event_embedding": []},
            "missing embedding": {"embedding": None},
            "non-numeric value": {"embedding": [0.1, "x", 0.3]},
            "numeric string": {"embedding": [0.1, "0.2", 0.3]},
            "boolean value": {"event_embedding": [True, 0.2, 0.3]},
            "not a list": {"event_embedding": 0.5},
        }
        for name, overrides in cases.items():
            with self.subTest(name):
                self.assertEqual(self.post(**overrides).status_code, 422)

    def test_missing_fields_are_422(self):
        for field in ("embedding", "kind", "event_embedding"):
            with self.subTest(field=field):
                body = {"embedding": CURRENT, "kind": "positive", "event_embedding": EVENT}
                del body[field]
                self.assertEqual(self.client.post(URL, json=body).status_code, 422)

    def test_non_finite_values_are_422(self):
        body = '{"embedding": [0.1, NaN, 0.3], "kind": "positive", "event_embedding": [0.1, 0.2, 0.3]}'
        response = self.client.post(URL, content=body, headers={"content-type": "application/json"})
        self.assertEqual(response.status_code, 422)


if __name__ == "__main__":
    unittest.main()
