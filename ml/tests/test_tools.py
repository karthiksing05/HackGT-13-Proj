"""The probes and the parity check, with fake transports (no network, no secrets)."""

import io
import json
import unittest
from pathlib import Path

import httpx
import numpy as np

from tools import hf_probe, parity_check, rank_smoke, vertex_probe

FIXTURES = Path(__file__).parent / "fixtures"
GOLDEN = json.loads((FIXTURES / "qwen3_golden.json").read_text())
CANARY = GOLDEN["embeddings"][GOLDEN["texts"].index("canary")]


def denied(provider: str) -> httpx.Response:
    return httpx.Response(403, json={"error": f"This authentication method does not have sufficient permissions ({provider})"})


class HFProbeTests(unittest.TestCase):
    WHOAMI = {
        "name": "someone",
        "auth": {"accessToken": {"displayName": "sidequests", "role": "fineGrained", "fineGrained": {"global": ["discussion.write"]}}},
    }
    MAPPING = [
        {"provider": "deepinfra", "status": "live", "provider_id": "Qwen/Qwen3-Embedding-0.6B", "task": "feature-extraction"},
        {"provider": "hf-inference", "status": "error", "provider_id": "Qwen/Qwen3-Embedding-0.6B", "task": "feature-extraction"},
    ]

    def run_probe(self, handler, env=None, argv=None):
        out = io.StringIO()
        code = hf_probe.run(
            ["--json"] if argv is None else argv, env=env if env is not None else {"HF_TOKEN": "hf_secret_value"},
            whoami=lambda token: self.WHOAMI, mapping_lookup=lambda: self.MAPPING,
            transport=httpx.MockTransport(handler), out=out,
        )
        return code, out.getvalue()

    def test_missing_permission_fails_every_route(self):
        with self.assertLogs("embedding.hf_router", level="WARNING"):
            code, output = self.run_probe(lambda request: denied(request.url.path.split("/")[1]))
        report = json.loads(output)
        self.assertEqual(code, 1)
        self.assertFalse(report["token"]["inference_providers_permission"])
        self.assertEqual([r["kind"] for r in report["routes"]], ["auth", "auth"])
        self.assertEqual(report["mapping"]["providers"][0]["status"], "live")
        self.assertNotIn("hf_secret_value", output)

    def test_a_working_route_reports_parity(self):
        def handler(request):
            if "/deepinfra/" in request.url.path:
                return httpx.Response(200, json={"data": [{"index": 0, "embedding": CANARY}]})
            return denied("hf-inference")

        with self.assertLogs("embedding.hf_router", level="WARNING"):
            code, output = self.run_probe(handler, argv=[])
        self.assertEqual(code, 0)
        self.assertIn("route deepinfra: OK dim=1024", output)
        self.assertIn("cosine_vs_local=1.0", output)
        self.assertIn("route hf-inference: FAILED (auth)", output)
        self.assertNotIn("hf_secret_value", output)

    def test_no_token(self):
        code, output = self.run_probe(lambda request: httpx.Response(500), env={}, argv=[])
        self.assertEqual(code, 1)
        self.assertIn("HF_TOKEN is not set", output)


class VertexProbeTests(unittest.TestCase):
    ENDPOINT = "mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852"

    def run_probe(self, handler, resolve=lambda host: ["10.0.0.1"], argv=None):
        out = io.StringIO()
        code = vertex_probe.run(
            ["--json"] if argv is None else argv, env={}, token_provider=lambda: "ya29.secret", transport=httpx.MockTransport(handler),
            resolve=resolve, out=out,
        )
        return code, out.getvalue()

    def test_permission_denied_everywhere(self):
        code, output = self.run_probe(lambda request: httpx.Response(403, json={"error": {"status": "PERMISSION_DENIED"}}))
        report = json.loads(output)
        self.assertEqual(code, 1)
        self.assertEqual(report["describe"]["status"], 403)
        self.assertEqual({p["kind"] for p in report["predict"]}, {"auth"})
        self.assertEqual(len(report["predict"]), 2)  # one try per resolvable host: a 403 does not depend on the key
        self.assertNotIn("ya29.secret", output)

    def test_described_dedicated_host_and_key_are_found(self):
        dns = f"{self.ENDPOINT}.us-central1-586468035526.prediction.vertexai.goog"

        def handler(request):
            if request.method == "GET" and request.url.path.endswith(self.ENDPOINT):
                return httpx.Response(200, json={"displayName": "qwen3-embedding", "dedicatedEndpointEnabled": True,
                                                 "dedicatedEndpointDns": dns, "deployedModels": []})
            if request.url.host == dns and request.url.path.endswith(":predict"):
                instance = json.loads(request.content)["instances"][0]
                if "prompt" in instance:
                    return httpx.Response(200, json={"predictions": [[CANARY]]})
                return httpx.Response(400, json={"error": "unknown field"})
            return httpx.Response(400, json={"error": "This endpoint is a dedicated endpoint"})

        code, output = self.run_probe(handler, argv=[])
        self.assertEqual(code, 0, output)
        self.assertIn(f"set VERTEX_HOST={dns} VERTEX_INPUT_KEY=prompt", output)
        self.assertIn("cosine_vs_local=1.0", output)

    def test_unresolvable_hosts_are_skipped(self):
        def resolve(host):
            if host.endswith("aiplatform.googleapis.com"):
                return ["10.0.0.2"]
            raise OSError("nodename nor servname provided")

        code, output = self.run_probe(lambda request: httpx.Response(403), resolve=resolve)
        report = json.loads(output)
        self.assertEqual([h["resolves"] for h in report["hosts"]], [False, True])
        self.assertEqual({p["host"] for p in report["predict"]}, {"us-central1-aiplatform.googleapis.com"})


class ParityCheckTests(unittest.TestCase):
    def service(self, perturb: float = 0.0):
        def handler(request):
            texts = json.loads(request.content)["texts"]
            vectors = np.asarray(GOLDEN["embeddings"][: len(texts)], dtype=np.float64)
            if perturb:
                vectors = vectors + perturb * np.random.RandomState(0).randn(*vectors.shape)
            return httpx.Response(200, json={"embeddings": vectors.tolist(), "model": GOLDEN["model"], "dim": 1024, "provider": "hf:deepinfra", "cached": 0})

        return httpx.MockTransport(handler)

    def test_identical_vectors_pass(self):
        out = io.StringIO()
        code = parity_check.run(["--ml-url", "http://ml", "--json"], transport=self.service(), out=out)
        report = json.loads(out.getvalue())
        self.assertEqual(code, 0)
        self.assertEqual(report["min_cosine"], 1.0)
        self.assertEqual(report["provider"], "hf:deepinfra")
        if "reference_activity" in GOLDEN:
            self.assertGreater(report["reference_activity_vs_stored"], 0.995)

    def test_drifted_vectors_fail(self):
        out = io.StringIO()
        code = parity_check.run(["--ml-url", "http://ml"], transport=self.service(perturb=0.01), out=out)
        self.assertEqual(code, 1)
        self.assertIn("FAIL", out.getvalue())

    def test_service_errors_exit_2(self):
        out = io.StringIO()
        transport = httpx.MockTransport(lambda request: httpx.Response(503, json={"detail": "Embedding provider unavailable."}))
        self.assertEqual(parity_check.run(["--ml-url", "http://ml"], transport=transport, out=out), 2)

    def test_a_missing_provider_config_exits_2(self):
        out = io.StringIO()
        self.assertEqual(parity_check.run(["--provider", "hf"], env={}, out=out), 2)
        self.assertIn("HF_TOKEN", out.getvalue())


class RankSmokeTests(unittest.TestCase):
    ACTIVITIES = [
        {"_id": f"a{i}", "name": f"Activity {i}", "category": "park" if i % 2 else "bar", "embedding": [float(i + 1), 1.0]}
        for i in range(20)
    ]

    def test_prints_the_best_and_the_worst(self):
        requests = []

        def handler(request):
            body = json.loads(request.content)
            requests.append((request.url.path, body))
            if request.url.path == "/v1/user-profile":
                return httpx.Response(200, json={"positive_text": "Interests:\n- hiking", "negative_text": "", "positive_embedding": [1.0, 0.0],
                                                 "negative_embedding": [0.0, 0.0], "profile_text_hash": "profile-v1:abc", "provider": "local"})
            if request.url.path == "/v1/search-profile":
                return httpx.Response(200, json={"search_text": "Pace:\n- chill", "search_embedding": [0.0, 1.0]})
            events = sorted(body["events"], key=lambda e: -e["embedding"][0])
            return httpx.Response(200, json={"events": [{"event_id": e["id"], "score": 1 - i / 20, "rerank_score": None} for i, e in enumerate(events)],
                                             "model_version": "classifier-v1", "reranked": False})

        out = io.StringIO()
        code = rank_smoke.run(["--mood", "something chill", "--top", "5"], activities=self.ACTIVITIES, transport=httpx.MockTransport(handler), out=out)
        output = out.getvalue()
        self.assertEqual(code, 0, output)
        self.assertIn("   1. 1.000  park             Activity 19", output)
        self.assertIn("  20. 0.050  bar              Activity 0", output)
        self.assertIn("search: Pace: | - chill", output)
        rank_body = requests[-1][1]
        self.assertEqual(rank_body["options"], {"rerank": False})
        self.assertEqual(rank_body["search_text"], "Pace:\n- chill")
        self.assertNotIn("start_time", rank_body["events"][0])

    def test_service_errors_exit_1(self):
        out = io.StringIO()
        transport = httpx.MockTransport(lambda request: httpx.Response(503, json={"detail": "Embedding provider unavailable."}))
        self.assertEqual(rank_smoke.run([], activities=self.ACTIVITIES, transport=transport, out=out), 1)
        self.assertIn("HTTP 503", out.getvalue())


if __name__ == "__main__":
    unittest.main()
