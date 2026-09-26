"""tools.embed_missing against a real MongoDB (skipped when none answers) and a fake ML service."""

import contextlib
import io
import json
import os
import unittest
import uuid
from datetime import datetime, timezone

import httpx
import numpy as np

from tools import embed_missing as em

MONGO_URI = os.environ.get("ML_TEST_MONGO_URI", "mongodb://127.0.0.1:27017/?directConnection=true")
DIM = 8


def unit(seed: int) -> list[float]:
    v = np.random.RandomState(seed).rand(DIM) + 0.1
    return (v / np.linalg.norm(v)).tolist()


class FakeService:
    """`/healthz` and `/v1/embed` of the ML service; records the embedded texts."""

    def __init__(self, model: str = "test-model", fail_embed: bool = False) -> None:
        self.model = model
        self.fail_embed = fail_embed
        self.embedded: list[list[str]] = []

    def handler(self, request: httpx.Request) -> httpx.Response:
        if request.url.path == "/healthz":
            return httpx.Response(200, json={"status": "ok", "embedding": {"model": self.model, "dim": DIM}})
        if request.url.path == "/v1/embed":
            if self.fail_embed:
                return httpx.Response(503, json={"detail": "Embedding provider unavailable."})
            body = json.loads(request.content)
            assert body["kind"] == "activity"
            self.embedded.append(body["texts"])
            vectors = [unit(len(t)) for t in body["texts"]]
            return httpx.Response(200, json={"embeddings": vectors, "model": self.model, "dim": DIM, "provider": "local", "cached": 0})
        return httpx.Response(404)

    def client(self) -> httpx.Client:
        return httpx.Client(transport=httpx.MockTransport(self.handler))


def args(**overrides):
    base = em.parse_args(["--collections", "acts", "demo", "--model", "test-model", "--dim", str(DIM)])
    for key, value in overrides.items():
        setattr(base, key, value)
    return base


class EmbedMissingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        try:
            from pymongo import MongoClient

            cls.mongo = MongoClient(MONGO_URI, serverSelectionTimeoutMS=1000, tz_aware=True)
            cls.mongo.admin.command("ping")
        except Exception as exc:  # no Mongo on this machine: skip, as the design's env-guarded tests do
            raise unittest.SkipTest(f"MongoDB not reachable at {MONGO_URI}: {exc}")

    @classmethod
    def tearDownClass(cls):
        cls.mongo.close()

    def setUp(self):
        self.db_name = f"sq_test_embed_missing_{uuid.uuid4().hex[:8]}"
        self.db = self.mongo[self.db_name]
        self.addCleanup(self.mongo.drop_database, self.db_name)
        text = "Interests:\n- live jazz"
        changed = "Interests:\n- live blues"
        self.docs = {
            "missing": {"_id": "missing", "embeddingText": text, "embeddingTextHash": em.text_hash(text)},
            "current": {"_id": "current", "embeddingText": text, "embeddingTextHash": em.text_hash(text),
                        "embedding": unit(1), "embeddingMeta": {"textHash": em.text_hash(text), "source": "backfill"}},
            "stale": {"_id": "stale", "embeddingText": changed, "embeddingTextHash": em.text_hash(changed),
                      "embedding": unit(2), "embeddingMeta": {"textHash": "0" * 40, "source": "backfill"}},
            "mismatch": {"_id": "mismatch", "embeddingText": "Interests:\n- opera", "embeddingTextHash": "f" * 40},
            "foreign": {"_id": "foreign", "embeddingText": changed, "embeddingTextHash": em.text_hash(changed), "embedding": unit(3)},
            "no-text": {"_id": "no-text", "name": "a parking lot"},
        }
        self.db.acts.insert_many([dict(d) for d in self.docs.values()])
        self.db.demo.insert_one({"_id": "demo-missing", "embeddingText": text, "embeddingTextHash": em.text_hash(text)})

    def run_tool(self, service: FakeService, **overrides) -> tuple[int, str]:
        out = io.StringIO()
        with service.client() as client:
            code = em.run(args(**overrides), client=client, db=self.db, out=out)
        return code, out.getvalue()

    def test_embeds_missing_and_stale_vectors_once_per_text(self):
        service = FakeService()
        code, output = self.run_tool(service)
        self.assertEqual(code, 0, output)
        self.assertEqual(service.embedded, [["Interests:\n- live jazz", "Interests:\n- live blues"]])
        missing = self.db.acts.find_one({"_id": "missing"})
        self.assertEqual(missing["embedding"], unit(len("Interests:\n- live jazz")))
        self.assertEqual(missing["embeddingModel"], "test-model")
        meta = missing["embeddingMeta"]
        self.assertEqual(
            {k: meta[k] for k in ("model", "dimension", "normalized", "prompt", "maxSeqLength", "textHash", "source", "provider")},
            {"model": "test-model", "dimension": DIM, "normalized": True, "prompt": None, "maxSeqLength": 512,
             "textHash": em.text_hash("Interests:\n- live jazz"), "source": "embed_missing", "provider": "local"},
        )
        self.assertIsInstance(meta["generatedAt"], datetime)
        self.assertEqual(self.db.demo.find_one({"_id": "demo-missing"})["embeddingMeta"]["source"], "embed_missing")
        self.assertEqual(self.db.acts.find_one({"_id": "stale"})["embeddingMeta"]["textHash"], em.text_hash("Interests:\n- live blues"))
        self.assertIn("wrote 2 of 2 planned", output)

    def test_leaves_current_foreign_and_mismatched_documents_alone(self):
        self.run_tool(FakeService())
        self.assertEqual(self.db.acts.find_one({"_id": "current"})["embedding"], unit(1))
        self.assertEqual(self.db.acts.find_one({"_id": "foreign"})["embedding"], unit(3))
        self.assertNotIn("embedding", self.db.acts.find_one({"_id": "mismatch"}))
        self.assertNotIn("embedding", self.db.acts.find_one({"_id": "no-text"}))

    def test_no_stale_only_fills_missing_vectors(self):
        service = FakeService()
        self.run_tool(service, no_stale=True)
        self.assertEqual(service.embedded, [["Interests:\n- live jazz"]])
        self.assertEqual(self.db.acts.find_one({"_id": "stale"})["embedding"], unit(2))

    def test_dry_run_calls_nothing_and_writes_nothing(self):
        service = FakeService()
        code, output = self.run_tool(service, dry_run=True)
        self.assertEqual((code, service.embedded), (0, []))
        self.assertNotIn("embedding", self.db.acts.find_one({"_id": "missing"}))
        self.assertIn("acts: 3 need a vector; 2 to embed; skipped: 0 blank, 1 hash mismatch", output)

    def test_limit_caps_unique_texts(self):
        service = FakeService()
        self.run_tool(service, limit=1)
        self.assertEqual(service.embedded, [["Interests:\n- live jazz"]])

    def test_a_text_changed_meanwhile_is_not_overwritten(self):
        todo = em.plan(self.db, ["acts"])
        self.db.acts.update_one({"_id": "missing"}, {"$set": {"embeddingText": "new", "embeddingTextHash": em.text_hash("new")}})
        vectors = {h: (unit(9), "local") for h in todo.texts}
        written = em.write(self.db, todo, vectors, "test-model", datetime.now(timezone.utc))
        self.assertEqual(written["acts"], (2, 1))
        self.assertNotIn("embedding", self.db.acts.find_one({"_id": "missing"}))

    def test_wrong_model_is_refused(self):
        service = FakeService(model="other-model")
        code, output = self.run_tool(service)
        self.assertEqual(code, 1)
        self.assertIn("embeds with 'other-model'", output)
        self.assertEqual(service.embedded, [])

    def test_embed_failure_exits_1_without_writes(self):
        code, output = self.run_tool(FakeService(fail_embed=True))
        self.assertEqual(code, 1)
        self.assertIn("HTTP 503", output)
        self.assertNotIn("embedding", self.db.acts.find_one({"_id": "missing"}))

    def test_unreachable_service_exits_1(self):
        def refuse(request):
            raise httpx.ConnectError("connection refused")

        out = io.StringIO()
        with httpx.Client(transport=httpx.MockTransport(refuse)) as client:
            code = em.run(args(), client=client, db=self.db, out=out)
        self.assertEqual(code, 1)
        self.assertIn("unreachable", out.getvalue())


class ArgTests(unittest.TestCase):
    def test_defaults_match_the_timer(self):
        a = em.parse_args([])
        self.assertEqual((a.db, a.collections, a.ml_url, a.batch, a.model, a.dim), ("freetime", ["activities", "demo_activities"], "http://127.0.0.1:8000", 32, "Qwen/Qwen3-Embedding-0.6B", 1024))

    def test_batch_must_fit_the_service(self):
        with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
            em.parse_args(["--batch", "65"])


if __name__ == "__main__":
    unittest.main()
