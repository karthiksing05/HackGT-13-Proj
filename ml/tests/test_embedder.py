"""The embedding package with fake transports: HF router routes, fallback chain, breaker, cache."""

import hashlib
import json
import tempfile
import unittest
from pathlib import Path

import httpx
import numpy as np

from embedding import (
    CachingEmbedder,
    Embedder,
    EmbedderError,
    EmbedResult,
    EmbedStats,
    FallbackEmbedder,
    HFRouterEmbedder,
    NullCache,
    RouteMapping,
    SqliteEmbeddingCache,
    embed_with_blanks,
    unit_rows,
)
from embedding.base import CANARY_TEXT

D = 8


def raw_vector(text: str, dim: int = D) -> list[float]:
    """A deterministic, deliberately non-unit vector for a text."""
    seed = int(hashlib.sha1(text.encode()).hexdigest()[:8], 16)
    return (np.random.RandomState(seed).rand(dim) * 3 + 0.1).tolist()


class FakeRouter:
    """A fake router.huggingface.co that records requests and answers per route.

    `queue[route]` holds canned `(status, json, headers)` answers used before the default
    behaviour: deepinfra returns `data` in reverse index order (so callers must sort), hf-inference
    returns a flat list for one text and a list of lists otherwise.
    """

    def __init__(self, dim: int = D) -> None:
        self.dim = dim
        self.requests: list[httpx.Request] = []
        self.queue: dict[str, list] = {"deepinfra": [], "hf-inference": []}

    def route_of(self, request: httpx.Request) -> str:
        return "deepinfra" if "/deepinfra/" in request.url.path else "hf-inference"

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        route = self.route_of(request)
        if self.queue[route]:
            status, payload, headers = self.queue[route].pop(0)
            return httpx.Response(status, json=payload, headers=headers)
        body = json.loads(request.content)
        texts = body["input"] if route == "deepinfra" else body["inputs"]
        rows = [raw_vector(t, self.dim) for t in texts]
        if route == "deepinfra":
            data = [{"index": i, "embedding": r, "object": "embedding"} for i, r in enumerate(rows)]
            data.reverse()
            return httpx.Response(200, json={"data": data, "usage": {"prompt_tokens": 3 * len(texts)}})
        return httpx.Response(200, json=rows[0] if len(rows) == 1 else rows)

    def transport(self) -> httpx.MockTransport:
        return httpx.MockTransport(self.handler)

    def requests_for(self, route: str) -> list[httpx.Request]:
        return [r for r in self.requests if self.route_of(r) == route]


def router(fake: FakeRouter, **kwargs) -> HFRouterEmbedder:
    kwargs.setdefault("mapping_lookup", lambda: {})
    kwargs.setdefault("sleep", lambda seconds: None)
    return HFRouterEmbedder("hf_test_token", dim=D, transport=fake.transport(), **kwargs)


def expected(text: str) -> np.ndarray:
    return unit_rows(np.asarray(raw_vector(text)))[0]


class HFRouterTests(unittest.TestCase):
    def test_deepinfra_rows_follow_index_not_response_order(self):
        fake = FakeRouter()
        result = router(fake).embed_result(["alpha", "beta"], "user")
        np.testing.assert_allclose(result.vectors[0], expected("alpha"), rtol=1e-6)
        np.testing.assert_allclose(result.vectors[1], expected("beta"), rtol=1e-6)
        self.assertEqual(result.provider, "hf:deepinfra")
        self.assertEqual(result.vectors.dtype, np.float32)
        self.assertEqual(json.loads(fake.requests[0].content)["model"], "Qwen/Qwen3-Embedding-0.6B")
        self.assertEqual(fake.requests[0].headers["Authorization"], "Bearer hf_test_token")

    def test_hf_inference_shape_including_flat_single_text(self):
        fake = FakeRouter()
        embedder = router(fake, routes=("hf-inference",))
        one = embedder.embed(["solo"])
        np.testing.assert_allclose(one[0], expected("solo"), rtol=1e-6)
        two = embedder.embed(["a", "b"])
        self.assertEqual(two.shape, (2, D))
        body = json.loads(fake.requests[0].content)
        self.assertEqual(body, {"inputs": ["solo"], "normalize": True, "truncate": True})
        self.assertIn("/hf-inference/models/Qwen/Qwen3-Embedding-0.6B/pipeline/feature-extraction", str(fake.requests[0].url))

    def test_rows_are_unit_norm(self):
        vectors = router(FakeRouter()).embed(["x", "y", "z"])
        np.testing.assert_allclose(np.linalg.norm(vectors.astype(np.float64), axis=1), [1.0, 1.0, 1.0], atol=1e-6)

    def test_batches_of_max_batch(self):
        fake = FakeRouter()
        vectors = router(fake, max_batch=32).embed([f"text {i}" for i in range(70)])
        self.assertEqual(vectors.shape, (70, D))
        self.assertEqual([len(json.loads(r.content)["input"]) for r in fake.requests], [32, 32, 6])
        np.testing.assert_allclose(vectors[69], expected("text 69"), rtol=1e-6)

    def test_429_then_200_waits_for_retry_after(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((429, {"error": "slow down"}, {"Retry-After": "2"}))
        sleeps = []
        result = router(fake, sleep=sleeps.append).embed_result(["a"])
        self.assertEqual(sleeps, [2.0])
        self.assertEqual(len(fake.requests_for("deepinfra")), 2)
        self.assertEqual(result.provider, "hf:deepinfra")

    def test_403_is_auth_error_and_next_route_is_used(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((403, {"error": "This authentication method does not have sufficient permissions"}, {}))
        clock = [100.0]
        embedder = router(fake, auth_backoff=600.0, clock=lambda: clock[0])
        with self.assertLogs("embedding.hf_router", level="WARNING"):
            result = embedder.embed_result(["a"])
        self.assertEqual(result.provider, "hf:hf-inference")
        np.testing.assert_allclose(result.vectors[0], expected("a"), rtol=1e-6)
        status = embedder.status()
        self.assertEqual(status["hf:deepinfra"].status, "error")
        self.assertIn("auth", status["hf:deepinfra"].detail)
        self.assertEqual(status["hf:hf-inference"].status, "ok")

        # Within the backoff the auth-failed route is not tried again.
        embedder.embed(["b"])
        self.assertEqual(len(fake.requests_for("deepinfra")), 1)
        clock[0] += 601
        embedder.embed(["c"])
        self.assertEqual(len(fake.requests_for("deepinfra")), 2)

    def test_all_routes_failing_raises_unavailable(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((500, {"error": "boom"}, {}))
        fake.queue["hf-inference"].append((503, {"error": "cold"}, {}))
        with self.assertLogs("embedding.hf_router", level="WARNING"), self.assertRaises(EmbedderError) as ctx:
            router(fake, max_retries=0).embed(["a"])
        self.assertEqual(ctx.exception.kind, "unavailable")
        self.assertEqual(ctx.exception.provider, "hf")
        self.assertTrue(ctx.exception.retryable)

    def test_all_routes_403_is_one_auth_error(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((403, {}, {}))
        fake.queue["hf-inference"].append((401, {}, {}))
        with self.assertLogs("embedding.hf_router", level="WARNING"), self.assertRaises(EmbedderError) as ctx:
            router(fake).embed(["a"])
        self.assertEqual(ctx.exception.kind, "auth")
        self.assertFalse(ctx.exception.retryable)

    def test_5xx_is_retried_then_succeeds(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((502, {"error": "bad gateway"}, {}))
        sleeps = []
        result = router(fake, max_retries=3, sleep=sleeps.append).embed_result(["a"])
        self.assertEqual(result.provider, "hf:deepinfra")
        self.assertEqual(sleeps, [0.5])

    def test_other_4xx_is_bad_request_without_retry(self):
        fake = FakeRouter()
        fake.queue["deepinfra"].append((400, {"error": "input too long"}, {}))
        with self.assertLogs("embedding.hf_router", level="WARNING"):
            result = router(fake).embed_result(["a"])
        self.assertEqual(result.provider, "hf:hf-inference")
        self.assertEqual(len(fake.requests_for("deepinfra")), 1)

    def test_wrong_dimension_is_bad_response(self):
        fake = FakeRouter(dim=5)
        with self.assertLogs("embedding.hf_router", level="WARNING"), self.assertRaises(EmbedderError) as ctx:
            router(fake, routes=("deepinfra",)).embed(["a"])
        self.assertEqual(ctx.exception.kind, "bad_response")
        self.assertIn("expected 1x8", ctx.exception.message)

    def test_mapping_failure_falls_back_to_defaults(self):
        fake = FakeRouter()

        def broken():
            raise ConnectionError("hub down")

        with self.assertLogs("embedding.hf_router", level="WARNING"):
            embedder = router(fake, mapping_lookup=broken)
            embedder.embed(["a"])
        self.assertEqual(fake.route_of(fake.requests[0]), "deepinfra")
        self.assertIn("hub down", embedder.status()["hf:deepinfra"].detail)

    def test_mapping_orders_live_first_and_error_last(self):
        fake = FakeRouter()
        mapping = {"deepinfra": RouteMapping("Qwen/Qwen3-Embedding-0.6B", "error"), "hf-inference": RouteMapping("x", "live")}
        router(fake, mapping_lookup=lambda: mapping).embed(["a"])
        self.assertEqual(fake.route_of(fake.requests[0]), "hf-inference")

    def test_mapping_provider_id_becomes_deepinfra_model(self):
        fake = FakeRouter()
        mapping = {"deepinfra": RouteMapping("Qwen/Qwen3-Embedding-0.6B", "live")}
        router(fake, mapping_lookup=lambda: mapping).embed(["a"])
        self.assertEqual(json.loads(fake.requests[0].content)["model"], "Qwen/Qwen3-Embedding-0.6B")

    def test_mapping_is_looked_up_once(self):
        calls = []

        def lookup():
            calls.append(1)
            return {}

        embedder = router(FakeRouter(), mapping_lookup=lookup)
        embedder.embed(["a"])
        embedder.embed(["b"])
        self.assertEqual(len(calls), 1)

    def test_blank_texts_are_zero_rows_and_never_sent(self):
        fake = FakeRouter()
        vectors = router(fake).embed(["keep", "", "   "])
        self.assertEqual(json.loads(fake.requests[0].content)["input"], ["keep"])
        np.testing.assert_allclose(vectors[0], expected("keep"), rtol=1e-6)
        self.assertEqual(vectors[1].tolist(), [0.0] * D)
        self.assertEqual(vectors[2].tolist(), [0.0] * D)

    def test_all_blank_makes_no_request(self):
        fake = FakeRouter()
        result = router(fake).embed_result(["", " "])
        self.assertEqual(fake.requests, [])
        self.assertEqual(result.vectors.tolist(), [[0.0] * D] * 2)
        self.assertEqual(result.provider, "hf")

    def test_long_texts_are_truncated_client_side(self):
        fake = FakeRouter()
        router(fake, max_chars=10).embed(["x" * 50])
        self.assertEqual(json.loads(fake.requests[0].content)["input"], ["x" * 10])

    def test_prompt_tokens_are_counted(self):
        embedder = router(FakeRouter())
        embedder.embed(["a", "b"])
        self.assertEqual(embedder.prompt_tokens, 6)

    def test_missing_token_is_not_configured(self):
        with self.assertRaises(EmbedderError) as ctx:
            HFRouterEmbedder(None)
        self.assertEqual(ctx.exception.kind, "not_configured")


# --- fallback chain and circuit breaker ----------------------------------------------------------


class ConstEmbedder(Embedder):
    """Answers with a fixed direction; counts calls."""

    def __init__(self, name: str, index: int = 0, dim: int = D) -> None:
        self.name = name
        self.dim = dim
        self.index = index
        self.calls = 0

    def embed(self, texts, kind="activity"):
        self.calls += 1
        out = np.zeros((len(texts), self.dim), dtype=np.float32)
        out[:, self.index] = 1.0
        return out


class FailingEmbedder(Embedder):
    def __init__(self, name: str, kind: str = "unavailable", retryable: bool = True, dim: int = D) -> None:
        self.name = name
        self.dim = dim
        self.kind = kind
        self.retryable = retryable
        self.calls = 0

    def embed(self, texts, kind="activity"):
        self.calls += 1
        raise EmbedderError(self.kind, "nope", retryable=self.retryable, provider=self.name)


class FallbackTests(unittest.TestCase):
    def test_uses_next_provider_and_counts_fallbacks(self):
        stats = EmbedStats()
        bad, good = FailingEmbedder("bad"), ConstEmbedder("good")
        chain = FallbackEmbedder([bad, good], stats=stats)
        with self.assertLogs("embedding.fallback", level="WARNING"):
            result = chain.embed_result(["a"], "user")
        self.assertEqual(result.provider, "good")
        self.assertEqual((bad.calls, good.calls), (1, 1))
        self.assertEqual(stats.snapshot()["fallbacks"], 1)

    def test_circuit_opens_after_failures_and_half_opens_later(self):
        clock = [0.0]
        bad, good = FailingEmbedder("bad"), ConstEmbedder("good")
        chain = FallbackEmbedder([bad, good], open_after=2, reset_after=60.0, clock=lambda: clock[0])
        with self.assertLogs("embedding.fallback", level="WARNING"):
            for _ in range(5):
                chain.embed(["a"])
        self.assertEqual(bad.calls, 2)  # opened after two failures, then skipped
        self.assertEqual(chain.status()["bad"].status, "error")
        self.assertIn("circuit open", chain.status()["bad"].detail)
        clock[0] = 61.0
        with self.assertLogs("embedding.fallback", level="WARNING"):
            chain.embed(["a"])  # half-open trial call fails and reopens the circuit at once
            chain.embed(["a"])
        self.assertEqual(bad.calls, 3)

    def test_success_resets_the_breaker(self):
        flaky = FailingEmbedder("flaky")
        chain = FallbackEmbedder([flaky, ConstEmbedder("good")], open_after=3)
        with self.assertLogs("embedding.fallback", level="WARNING"):
            chain.embed(["a"])
            chain.embed(["a"])
        flaky.embed = lambda texts, kind="activity": np.ones((len(texts), D), dtype=np.float32)
        self.assertEqual(chain.embed_result(["a"]).provider, "flaky")
        self.assertEqual(chain.status()["flaky"].status, "unknown")

    def test_auth_error_opens_the_circuit_at_once_for_longer(self):
        clock = [0.0]
        bad = FailingEmbedder("bad", kind="auth", retryable=False)
        chain = FallbackEmbedder([bad, ConstEmbedder("good")], open_after=3, reset_after=60.0, auth_reset_after=600.0, clock=lambda: clock[0])
        with self.assertLogs("embedding.fallback", level="WARNING"):
            chain.embed(["a"])
        chain.embed(["a"])
        self.assertEqual(bad.calls, 1)
        clock[0] = 61.0
        chain.embed(["a"])
        self.assertEqual(bad.calls, 1)
        clock[0] = 601.0
        with self.assertLogs("embedding.fallback", level="WARNING"):
            chain.embed(["a"])
        self.assertEqual(bad.calls, 2)

    def test_every_provider_failing_raises_and_counts_error(self):
        stats = EmbedStats()
        chain = FallbackEmbedder([FailingEmbedder("a"), FailingEmbedder("b", kind="timeout")], stats=stats)
        with self.assertLogs("embedding.fallback", level="ERROR"), self.assertRaises(EmbedderError) as ctx:
            chain.embed(["x"])
        self.assertEqual(ctx.exception.kind, "unavailable")
        self.assertIn("a: unavailable: nope", ctx.exception.message)
        self.assertIn("b: timeout: nope", ctx.exception.message)
        self.assertEqual(stats.snapshot()["errors"], 1)

    def test_not_ready_provider_is_skipped(self):
        gated = ConstEmbedder("gated")
        gated.ready = lambda: False
        chain = FallbackEmbedder([gated, ConstEmbedder("good", index=1)])
        self.assertEqual(chain.embed_result(["a"]).provider, "good")
        self.assertEqual(gated.calls, 0)

    def test_warmup_survives_a_failing_provider(self):
        bad, good = FailingEmbedder("bad"), ConstEmbedder("good")
        with self.assertLogs("embedding.fallback", level="WARNING"):
            FallbackEmbedder([bad, good]).warmup()
        self.assertEqual(good.calls, 1)

    def test_empty_chain_is_rejected(self):
        with self.assertRaises(ValueError):
            FallbackEmbedder([])


# --- cache ---------------------------------------------------------------------------------------


class CacheTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.cache = SqliteEmbeddingCache(Path(self.tmp.name) / "cache" / "emb.sqlite", max_entries=100)
        self.addCleanup(self.cache.close)

    def tearDown(self):
        self.tmp.cleanup()

    def test_hit_skips_the_inner_embedder(self):
        inner = ConstEmbedder("inner")
        embedder = CachingEmbedder(inner, self.cache)
        first = embedder.embed_result(["a", "b"], "user")
        self.assertEqual((first.provider, first.cached, inner.calls), ("inner", 0, 1))
        second = embedder.embed_result(["a", "b"], "user")
        self.assertEqual((second.provider, second.cached, inner.calls), ("cache", 2, 1))
        np.testing.assert_array_equal(first.vectors, second.vectors)
        self.assertEqual(self.cache.info()["hits"], 2)

    def test_only_misses_go_to_the_inner_embedder(self):
        seen = []

        class Recording(ConstEmbedder):
            def embed(self, texts, kind="activity"):
                seen.append(list(texts))
                return super().embed(texts, kind)

        embedder = CachingEmbedder(Recording("inner"), self.cache)
        embedder.embed(["a"])
        result = embedder.embed_result(["a", "b", "", "c"])
        self.assertEqual(seen, [["a"], ["b", "c"]])
        self.assertEqual(result.cached, 1)
        self.assertEqual(result.vectors[2].tolist(), [0.0] * D)
        self.assertEqual(result.provider, "inner")

    def test_blanks_are_never_cached_and_need_no_provider(self):
        inner = ConstEmbedder("inner")
        result = CachingEmbedder(inner, self.cache).embed_result(["", " "])
        self.assertEqual((result.provider, inner.calls, self.cache.size()), ("none", 0, 0))

    def test_key_includes_model(self):
        self.cache.put_many("m1", ["a"], np.ones((1, D), dtype=np.float32))
        self.assertIsNone(self.cache.get_many("m2", ["a"])[0])
        self.assertIsNotNone(self.cache.get_many("m1", ["a"])[0])

    def test_eviction_keeps_the_newest(self):
        cache = SqliteEmbeddingCache(Path(self.tmp.name) / "small.sqlite", max_entries=2)
        self.addCleanup(cache.close)
        for text in ("a", "b", "c"):
            cache.put_many("m", [text], np.ones((1, D), dtype=np.float32))
        self.assertEqual(cache.size(), 2)
        self.assertIsNone(cache.get_many("m", ["a"])[0])
        self.assertIsNotNone(cache.get_many("m", ["c"])[0])

    def test_probe_bypasses_the_cache(self):
        inner = ConstEmbedder("inner")
        embedder = CachingEmbedder(inner, self.cache)
        embedder.embed([CANARY_TEXT])
        result = embedder.probe()
        self.assertEqual((result.provider, result.cached, inner.calls), ("inner", 0, 2))

    def test_null_cache_always_misses(self):
        inner = ConstEmbedder("inner")
        embedder = CachingEmbedder(inner, NullCache())
        embedder.embed(["a"])
        embedder.embed(["a"])
        self.assertEqual(inner.calls, 2)

    def test_inner_errors_propagate(self):
        with self.assertRaises(EmbedderError):
            CachingEmbedder(FailingEmbedder("bad"), self.cache).embed(["a"])


# --- helpers -------------------------------------------------------------------------------------


class HelperTests(unittest.TestCase):
    def test_unit_rows_zero_rows_stay_zero(self):
        out = unit_rows(np.array([[3.0, 4.0], [0.0, 0.0]]))
        self.assertEqual(out.dtype, np.float32)
        np.testing.assert_allclose(out, [[0.6, 0.8], [0.0, 0.0]], rtol=1e-6)

    def test_embed_with_blanks_checks_shape(self):
        with self.assertRaises(EmbedderError) as ctx:
            embed_with_blanks(lambda texts: np.zeros((len(texts), 3)), ["a"], dim=D, provider="p")
        self.assertEqual((ctx.exception.kind, ctx.exception.provider), ("bad_response", "p"))

    def test_embed_result_default_provider(self):
        result = ConstEmbedder("c").embed_result(["a"])
        self.assertIsInstance(result, EmbedResult)
        self.assertEqual(result.provider, "c")

    def test_stats_snapshot(self):
        stats = EmbedStats()
        stats.record(kind="user", n=2, cached=1, provider="hf:deepinfra", ms=10)
        stats.record(kind="search", n=1, cached=1, provider="cache", ms=2)
        snap = stats.snapshot()
        self.assertEqual((snap["calls"], snap["texts"], snap["cached"]), (2, 3, 2))
        self.assertEqual(snap["by_provider"], {"hf:deepinfra": 1})
        self.assertEqual(snap["last_provider"], "hf:deepinfra")
        self.assertEqual(snap["avg_ms"], 6.0)


if __name__ == "__main__":
    unittest.main()
