"""Shared HTTP client: retries, per-domain rate limit and an on-disk response cache (§2.6)."""

import hashlib
import json
import logging
import threading
import time
from typing import Any, Callable
from urllib.parse import urlparse

import httpx
from tenacity import retry, retry_if_exception, stop_after_attempt, wait_exponential

from .config import CACHE_DIR, env

log = logging.getLogger(__name__)

# Minimum seconds between requests to one host. Ticketmaster allows 2 req/s.
MIN_INTERVAL = {
    "app.ticketmaster.com": 0.55,
    "places.googleapis.com": 0.2,
    "nominatim.openstreetmap.org": 1.1,
    "maps.googleapis.com": 0.05,
    "overpass-api.de": 1.0,
    "api.opentopodata.org": 1.1,  # public API allows 1 req/s
}
DEFAULT_INTERVAL = 1.0

_last_call: dict[str, float] = {}
_lock = threading.Lock()


def _throttle(host: str) -> None:
    interval = MIN_INTERVAL.get(host, DEFAULT_INTERVAL)
    with _lock:
        wait = _last_call.get(host, 0) + interval - time.monotonic()
        if wait > 0:
            time.sleep(wait)
        _last_call[host] = time.monotonic()


def _retryable(exc: BaseException) -> bool:
    if isinstance(exc, httpx.TransportError):
        return True
    if isinstance(exc, httpx.HTTPStatusError):
        return exc.response.status_code == 429 or exc.response.status_code >= 500
    return False


class Http:
    """One per adapter; `name` picks the cache folder cache/<name>/."""

    def __init__(self, name: str, timeout: float = 30):
        self.name = name
        self.client = httpx.Client(
            timeout=timeout,
            headers={"User-Agent": f"hackgt13-freetime-ingest ({env('CONTACT_EMAIL', 'no-contact-set')})"},
        )
        self.cache_dir = CACHE_DIR / name
        self.network_calls = 0
        self.cache_hits = 0

    def is_cached(self, method: str, url: str, params: dict | None = None, body: dict | None = None,
                  headers: dict | None = None, cache_ttl: float = 0) -> bool:
        """True when this exact request would be answered from the disk cache (no network, no quota)."""
        return self._cache_read(self._cache_key(method, url, params, body, headers), cache_ttl) is not None

    def get_json(self, url: str, params: dict | None = None, **kw) -> Any:
        return self.request("GET", url, params=params, **kw)

    def post_json(self, url: str, body: dict, **kw) -> Any:
        return self.request("POST", url, body=body, **kw)

    def request(
        self,
        method: str,
        url: str,
        params: dict | None = None,
        body: dict | None = None,
        headers: dict | None = None,
        cache_ttl: float | None = None,
        before_network: Callable[[], None] | None = None,
        auth: Callable[[], dict] | None = None,
    ) -> Any:
        """Return parsed JSON. `before_network` runs only on a cache miss, so quota
        is reserved for real calls only. `auth` headers are added at send time (after
        `before_network`, which may rotate keys) and are not part of the cache key.
        Raises httpx.HTTPStatusError on non-2xx."""
        key = self._cache_key(method, url, params, body, headers)
        if cache_ttl:
            cached = self._cache_read(key, cache_ttl)
            if cached is not None:
                self.cache_hits += 1
                return cached
        if before_network:
            before_network()
        if auth:
            headers = {**(headers or {}), **auth()}
        data = self._send(method, url, params, body, headers)
        self.network_calls += 1
        if cache_ttl:
            self._cache_write(key, data)
        return data

    @retry(
        retry=retry_if_exception(_retryable),
        stop=stop_after_attempt(4),
        wait=wait_exponential(multiplier=1, min=1, max=20),
        reraise=True,
    )
    def _send(self, method, url, params, body, headers) -> Any:
        _throttle(urlparse(url).hostname or "")
        resp = self.client.request(method, url, params=params, json=body, headers=headers)
        resp.raise_for_status()
        return resp.json()

    # Cache files hold only the response body, never the request (which carries API keys).
    def _cache_key(self, method, url, params, body, headers) -> str:
        blob = json.dumps([method, url, params, body, headers], sort_keys=True, default=str)
        return hashlib.sha1(blob.encode()).hexdigest()

    def _cache_read(self, key: str, ttl: float) -> Any:
        path = self.cache_dir / f"{key}.json"
        if not path.exists() or time.time() - path.stat().st_mtime > ttl:
            return None
        try:
            return json.loads(path.read_text())
        except json.JSONDecodeError:
            return None

    def _cache_write(self, key: str, data: Any) -> None:
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        (self.cache_dir / f"{key}.json").write_text(json.dumps(data))
