"""Key rotation across accounts: Google (Places, Geocoding) and Gemini. No network."""

from types import SimpleNamespace

import httpx
import pytest
from google.genai import errors

from ingest import quota
from ingest.adapters import google_places as gp_mod
from ingest.adapters.google_places import NEARBY_MASK, NEARBY_URL, GooglePlacesAdapter
from ingest.agent.gemini import Gemini, GeminiQuotaExhausted
from ingest.config import require_keys
from ingest.enrich import geocode as geo_mod
from ingest.enrich.geocode import Geocoder
from ingest.keys import KeyRing


@pytest.fixture
def two_google_keys(monkeypatch):
    for name in ("GOOGLE_MAPS_API_KEY", "GOOGLE_MAPS_API_KEYS", "GOOGLE_CLOUD_API_KEY"):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("GOOGLE_CLOUD_API_KEYS", "key-one, key-two")


def test_require_keys_splits_strips_and_dedupes(monkeypatch):
    monkeypatch.setenv("GEMINI_API_KEY", " 'a' ,b,, a ")
    assert require_keys("GEMINI_API_KEY") == ["a", "b"]


def test_ring_moves_to_next_key_at_cap_then_stops(test_db, two_google_keys, monkeypatch):
    monkeypatch.setitem(quota.CAPS, "google_text", ("month", 2))
    ring = KeyRing("GOOGLE_MAPS_API_KEY", test_db)
    assert ring.keys == ["key-one", "key-two"]
    for _ in range(2):
        ring.reserve("google_text")
    assert ring.key == "key-one"
    ring.reserve("google_text")
    assert ring.key == "key-two" and ring.cache_key == "key-one"
    ring.reserve("google_text")
    with pytest.raises(quota.QuotaExceeded, match="every"):
        ring.reserve("google_text")
    rows = {r["api"]: r["used"] for r in quota.usage(test_db) if r["api"].startswith("google_text")}
    assert rows == {"google_text": 2, "google_text@2": 2}


def _places(city, db, tmp_path, responses):
    """Places adapter whose network sends are recorded; `responses` are status codes in order."""
    a = GooglePlacesAdapter(city, db)
    a.http.cache_dir = tmp_path
    sent = []

    def send(method, url, params, body, headers):
        sent.append(headers["X-Goog-Api-Key"])
        status = responses.pop(0)
        if status != 200:
            raise httpx.HTTPStatusError("x", request=httpx.Request(method, url),
                                        response=httpx.Response(status, json={"error": {"message": "quota"}}))
        return {"places": []}

    a.http._send = send
    return a, sent


def test_places_rotates_on_429_and_caches_under_key_one(test_db, city, two_google_keys, monkeypatch, tmp_path):
    a, sent = _places(city, test_db, tmp_path, [429, 200])
    body = {"includedTypes": ["museum"]}
    assert a._post(NEARBY_URL, body, NEARBY_MASK, "google_nearby") == {"places": []}
    assert sent == ["key-one", "key-two"]
    # The response is cached as if key one fetched it, so a later run with key one hits the cache.
    assert a._cached(NEARBY_URL, body, NEARBY_MASK)
    assert a._post(NEARBY_URL, body, NEARBY_MASK, "google_nearby") == {"places": []}
    assert len(sent) == 2
    assert quota.remaining(test_db, "google_nearby@2") == quota.CAPS["google_nearby"][1] - 1


def test_places_gives_up_when_every_key_refuses(test_db, city, two_google_keys, monkeypatch, tmp_path):
    a, sent = _places(city, test_db, tmp_path, [403, 403])
    with pytest.raises(gp_mod.GooglePlacesError):
        a._post(NEARBY_URL, {"q": 1}, NEARBY_MASK, "google_nearby")
    assert sent == ["key-one", "key-two"]


def test_geocoder_rotates_on_over_query_limit(test_db, city, two_google_keys, monkeypatch, tmp_path):
    monkeypatch.setattr(geo_mod, "CACHE_DIR", tmp_path)
    g = Geocoder(city, test_db)
    replies = [{"status": "OVER_QUERY_LIMIT"}, {"status": "ZERO_RESULTS", "results": []}]
    used = []
    g.http.get_json = lambda url, params, **kw: used.append(params["key"]) or replies.pop(0)
    assert g.geocode("1 Nowhere St, Atlanta, GA") is None
    assert used == ["key-one", "key-two"]


class FakeModels:
    def __init__(self, outcomes):
        self.outcomes, self.calls = outcomes, 0

    def generate_content(self, **kw):
        self.calls += 1
        out = self.outcomes.pop(0)
        if isinstance(out, Exception):
            raise out
        return out


def _per_day_429():
    return errors.APIError(429, {"error": {"code": 429, "message": "quota", "status": "RESOURCE_EXHAUSTED",
                                           "details": [{"violations": [{"quotaId": "GenerateRequestsPerDay"}]}]}})


def _gemini(monkeypatch, *outcomes):
    monkeypatch.setenv("GEMINI_API_KEY", ",".join(f"k{i}" for i in range(len(outcomes))))
    g = Gemini()
    assert len(g.clients) == len(outcomes)
    g.clients = [SimpleNamespace(models=FakeModels(out)) for out in outcomes]
    return g


def test_gemini_moves_to_next_key_when_one_hits_its_daily_limit(monkeypatch):
    g = _gemini(monkeypatch, [_per_day_429()], ["ok-2", "ok-2b"])
    assert g._call("m", "p", None) == "ok-2"
    assert g._call("m", "p", None) == "ok-2b"  # key 1 stays dropped for this model
    assert [c.models.calls for c in g.clients] == [1, 2]


def test_gemini_spreads_calls_over_keys(monkeypatch):
    g = _gemini(monkeypatch, ["a1", "a2"], ["b1", "b2"])
    g.rpm = {"m": 600}  # 0.1s gap per key: the second call goes to the idle key instead of waiting
    assert [g._call("m", "p", None) for _ in range(4)] == ["a1", "b1", "a2", "b2"]


def test_gemini_raises_when_every_key_is_spent(monkeypatch):
    g = _gemini(monkeypatch, [_per_day_429()], [_per_day_429()])
    with pytest.raises(GeminiQuotaExhausted, match="every key"):
        g._call("m", "p", None)
