"""The Vertex provider without Google: injected tokens, a fake predict endpoint, the probe gate."""

import json
import unittest

import httpx
import numpy as np

from embedding import EmbedderError, VertexEmbedder
from embedding.vertex import INPUT_KEYS, predict_url, regional_host, unwrap_prediction

D = 8


class FakeVertex:
    """A fake `:predict` endpoint. `shape` picks the prediction layout the container would use."""

    def __init__(self, shape: str = "nested", dim: int = D, status: int = 200) -> None:
        self.shape = shape
        self.dim = dim
        self.status = status
        self.requests: list[httpx.Request] = []

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        if self.status != 200:
            return httpx.Response(self.status, json={"error": {"code": self.status, "message": "denied"}})
        instances = json.loads(request.content)["instances"]
        predictions = []
        for i, instance in enumerate(instances):
            vector = [float(i + 1)] * self.dim
            if self.shape == "flat":
                predictions.append(vector)
            elif self.shape == "dict":
                predictions.append({"embedding": vector})
            else:
                predictions.append([vector])
        return httpx.Response(200, json={"predictions": predictions})

    def transport(self) -> httpx.MockTransport:
        return httpx.MockTransport(self.handler)


def embedder(fake: FakeVertex, **kwargs) -> VertexEmbedder:
    return VertexEmbedder(dim=D, transport=fake.transport(), token_provider=lambda: "tok-123", **kwargs)


class VertexTests(unittest.TestCase):
    def test_not_configured_without_credentials(self):
        with self.assertRaises(EmbedderError) as ctx:
            VertexEmbedder(credentials_path=None)
        self.assertEqual(ctx.exception.kind, "not_configured")
        with self.assertRaises(EmbedderError) as ctx:
            VertexEmbedder(credentials_path="/nonexistent/gcp-sa.json")
        self.assertEqual(ctx.exception.kind, "not_configured")
        with self.assertRaises(EmbedderError):
            VertexEmbedder(endpoint_id="", token_provider=lambda: "t")
        with self.assertRaises(EmbedderError):
            VertexEmbedder(input_key="bogus", token_provider=lambda: "t")

    def test_predict_request_and_normalized_rows(self):
        fake = FakeVertex()
        e = embedder(fake, host="1.2.3.4.us-central1-586468035526.prediction.vertexai.goog", input_key="prompt")
        result = e.embed_result(["a", "b"], "search")
        self.assertEqual(result.provider, "vertex")
        request = fake.requests[0]
        self.assertEqual(
            str(request.url),
            "https://1.2.3.4.us-central1-586468035526.prediction.vertexai.goog/v1/projects/586468035526/locations/us-central1/endpoints/mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852:predict",
        )
        self.assertEqual(request.headers["Authorization"], "Bearer tok-123")
        self.assertEqual(json.loads(request.content), {"instances": [{"prompt": "a"}, {"prompt": "b"}]})
        np.testing.assert_allclose(np.linalg.norm(result.vectors.astype(np.float64), axis=1), [1.0, 1.0], atol=1e-6)
        self.assertEqual(e.status()["vertex"].status, "ok")

    def test_every_prediction_shape_unwraps(self):
        for shape in ("flat", "nested", "dict"):
            with self.subTest(shape=shape):
                vectors = embedder(FakeVertex(shape)).embed(["a"])
                np.testing.assert_allclose(vectors[0], [1 / np.sqrt(D)] * D, rtol=1e-6)
        self.assertEqual(unwrap_prediction({"embeddings": [[1.0, 2.0]]}), [1.0, 2.0])
        with self.assertRaises(EmbedderError):
            unwrap_prediction({"error": "x"})

    def test_batches_and_blanks(self):
        fake = FakeVertex()
        vectors = embedder(fake, max_batch=2).embed(["a", "", "b", "c"])
        self.assertEqual([len(json.loads(r.content)["instances"]) for r in fake.requests], [2, 1])
        self.assertEqual(vectors[1].tolist(), [0.0] * D)

    def test_403_is_auth_error(self):
        e = embedder(FakeVertex(status=403))
        with self.assertRaises(EmbedderError) as ctx:
            e.embed(["a"])
        self.assertEqual(ctx.exception.kind, "auth")
        self.assertEqual(e.status()["vertex"].status, "error")

    def test_wrong_dimension_is_bad_response(self):
        with self.assertRaises(EmbedderError) as ctx:
            embedder(FakeVertex(dim=5)).embed(["a"])
        self.assertEqual(ctx.exception.kind, "bad_response")

    def test_gated_provider_needs_a_passing_probe(self):
        denied = embedder(FakeVertex(status=403), require_probe=True)
        self.assertFalse(denied.ready())
        self.assertEqual(denied.status()["vertex"].status, "disabled")
        with self.assertRaises(EmbedderError):
            denied.probe()
        self.assertFalse(denied.ready())

        ok = embedder(FakeVertex(), require_probe=True)
        self.assertFalse(ok.ready())
        result = ok.probe()
        self.assertEqual(result.vectors.shape, (1, D))
        self.assertTrue(ok.ready())
        self.assertEqual(ok.status()["vertex"].status, "ok")

    def test_token_provider_failure_is_auth(self):
        def broken():
            raise RuntimeError("refresh failed")

        e = VertexEmbedder(dim=D, transport=FakeVertex().transport(), token_provider=broken)
        with self.assertRaises(EmbedderError) as ctx:
            e.embed(["a"])
        self.assertEqual(ctx.exception.kind, "auth")

    def test_url_helpers(self):
        self.assertEqual(regional_host("us-central1"), "us-central1-aiplatform.googleapis.com")
        self.assertEqual(
            predict_url("h", "p", "l", "e"), "https://h/v1/projects/p/locations/l/endpoints/e:predict"
        )
        self.assertEqual(INPUT_KEYS, ("inputs", "prompt", "text"))


if __name__ == "__main__":
    unittest.main()
