"""Qwen3-Embedding through the Hugging Face Inference Providers router.

Two routes, tried in order (`HF_EMBED_ROUTES`), each an HTTP call with the same `HF_TOKEN`:

| route          | URL                                                               | body                          |
|----------------|-------------------------------------------------------------------|-------------------------------|
| `deepinfra`    | `{router}/deepinfra/v1/openai/embeddings`                         | OpenAI embeddings request     |
| `hf-inference` | `{router}/hf-inference/models/{model}/pipeline/feature-extraction` | `{"inputs": [...], ...}`      |

The Hub's inference-provider mapping (looked up once per process, refreshed every 10 minutes, never
fatal) supplies each provider's model id and whether the model is `live` there; live routes are
tried first and `error` ones last. The token needs the "Make calls to Inference Providers"
permission: without it every route answers 403, which is reported as an `auth` error and the route
is left alone for `HF_AUTH_BACKOFF` seconds.
"""

from __future__ import annotations

import logging
import time
from collections.abc import Callable
from dataclasses import dataclass

import httpx
import numpy as np

from .base import DIM, MODEL, EmbedderError, EmbedKind, Embedder, EmbedResult, ProviderStatus, embed_with_blanks, unit_rows

logger = logging.getLogger(__name__)

DEFAULT_ROUTES: tuple[str, ...] = ("deepinfra", "hf-inference")
DEFAULT_ROUTER_BASE = "https://router.huggingface.co"
MAPPING_TTL_SECONDS = 600.0
_STATUS_RANK = {"live": 0, "staging": 1, "unknown": 1, "error": 2}


@dataclass(frozen=True)
class RouteMapping:
    """What the Hub says about the model on one provider."""

    provider_id: str
    status: str  # live | staging | error | unknown


MappingLookup = Callable[[], dict[str, RouteMapping]]


class HFRouterEmbedder(Embedder):
    name = "hf"

    def __init__(
        self,
        token: str | None,
        *,
        model: str = MODEL,
        dim: int = DIM,
        routes: tuple[str, ...] = DEFAULT_ROUTES,
        router_base: str = DEFAULT_ROUTER_BASE,
        timeout: float = 20.0,
        max_batch: int = 32,
        max_retries: int = 3,
        max_chars: int = 2000,
        auth_backoff: float = 600.0,
        transport: httpx.BaseTransport | None = None,
        mapping_lookup: MappingLookup | None = None,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
    ) -> None:
        if not token:
            raise EmbedderError("not_configured", "HF_TOKEN is not set", retryable=False, provider=self.name)
        if not routes:
            raise EmbedderError("not_configured", "no HF routes configured", retryable=False, provider=self.name)
        self.model = model
        self.dim = dim
        self.routes = tuple(routes)
        self.router_base = router_base.rstrip("/")
        self.max_batch = max(1, max_batch)
        self.max_retries = max(0, max_retries)
        self.max_chars = max_chars
        self.auth_backoff = auth_backoff
        self._token = token
        self._client = httpx.Client(timeout=timeout, transport=transport, headers={"Authorization": f"Bearer {token}"})
        self._mapping_lookup = mapping_lookup or self._hub_mapping
        self._mapping: dict[str, RouteMapping] = {}
        self._mapping_at: float | None = None
        self._mapping_error: str | None = None
        self._clock = clock
        self._sleep = sleep
        self._auth_blocked_until: dict[str, float] = {}
        self._status = {r: ProviderStatus(f"hf:{r}", "unknown") for r in self.routes}
        self.prompt_tokens = 0

    # -- public -----------------------------------------------------------------------------------

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        return self.embed_result(texts, kind).vectors

    def embed_result(self, texts: list[str], kind: EmbedKind = "activity") -> EmbedResult:
        used: list[str] = []  # the route that answered, captured per call (no shared state)

        def run(nonblank: list[str]) -> np.ndarray:
            vectors, route = self._embed_nonblank(nonblank)
            used.append(route)
            return vectors

        vectors = embed_with_blanks(run, texts, self.dim, self.name)
        return EmbedResult(vectors, f"hf:{used[0]}" if used else self.name)

    def status(self) -> dict[str, ProviderStatus]:
        out = {}
        for route in self.routes:
            st = self._status[route]
            mapping = self._mapping.get(route)
            extra = f"mapping={mapping.status}" if mapping else "mapping=unknown"
            if self._mapping_error:
                extra += f" ({self._mapping_error})"
            blocked = self._auth_blocked_until.get(route)
            if blocked is not None and self._clock() < blocked:
                extra += f"; auth backoff {blocked - self._clock():.0f}s"
            out[f"hf:{route}"] = ProviderStatus(
                st.name, st.status, f"{st.detail}; {extra}" if st.detail else extra, st.last_ok_at, st.last_error_at, st.latency_ms
            )
        return out

    def close(self) -> None:
        self._client.close()

    # -- routing ----------------------------------------------------------------------------------

    def _embed_nonblank(self, texts: list[str]) -> tuple[np.ndarray, str]:
        """Vectors for non-blank texts and the route that produced them."""
        texts = [t[: self.max_chars] for t in texts]
        errors: list[EmbedderError] = []
        for route in self._ordered_routes():
            blocked = self._auth_blocked_until.get(route)
            if blocked is not None and self._clock() < blocked:
                errors.append(EmbedderError("auth", "in auth backoff", retryable=False, provider=f"hf:{route}"))
                continue
            try:
                return self._embed_route(route, texts), route
            except EmbedderError as exc:
                errors.append(exc)
                self._status[route].mark_error(f"{exc.kind}: {exc.message}"[:200])
                if exc.kind == "auth":
                    self._auth_blocked_until[route] = self._clock() + self.auth_backoff
                logger.warning("hf route %s failed (%s): %s", route, exc.kind, exc.message)
        # One error kind for the whole provider: the routes' kind when they agree (so two 403s stay
        # an auth error and get the long circuit-breaker backoff), otherwise "unavailable".
        kinds = {e.kind for e in errors}
        kind = kinds.pop() if len(kinds) == 1 else "unavailable"
        message = "; ".join(f"{e.provider}: {e.kind}: {e.message}" for e in errors) or "no route"
        raise EmbedderError(kind, message, retryable=any(e.retryable for e in errors), provider=self.name)

    def _ordered_routes(self) -> list[str]:
        self._refresh_mapping()
        order = {r: i for i, r in enumerate(self.routes)}

        def rank(route: str) -> tuple[int, int]:
            mapping = self._mapping.get(route)
            return (_STATUS_RANK.get(mapping.status if mapping else "unknown", 1), order[route])

        return sorted(self.routes, key=rank)

    def _refresh_mapping(self) -> None:
        now = self._clock()
        if self._mapping_at is not None and now - self._mapping_at < MAPPING_TTL_SECONDS:
            return
        self._mapping_at = now
        try:
            self._mapping = dict(self._mapping_lookup())
            self._mapping_error = None
        except Exception as exc:  # the mapping is an optimization; defaults work without it
            self._mapping_error = f"{type(exc).__name__}: {exc}"[:120]
            logger.warning("hf provider mapping lookup failed, using defaults: %s", exc)

    def _hub_mapping(self) -> dict[str, RouteMapping]:
        from huggingface_hub import HfApi

        info = HfApi(token=self._token).model_info(self.model, expand=["inferenceProviderMapping"])
        raw = getattr(info, "inference_provider_mapping", None) or {}
        entries = raw.values() if isinstance(raw, dict) else raw
        out = {}
        for m in entries:
            provider = getattr(m, "provider", None)
            if provider:
                out[provider] = RouteMapping(getattr(m, "provider_id", None) or self.model, getattr(m, "status", "unknown"))
        return out

    # -- one route --------------------------------------------------------------------------------

    def _embed_route(self, route: str, texts: list[str]) -> np.ndarray:
        rows: list[list[float]] = []
        started = self._clock()
        for start in range(0, len(texts), self.max_batch):
            rows.extend(self._request(route, texts[start : start + self.max_batch]))
        vectors = np.asarray(rows, dtype=np.float64)
        if vectors.ndim != 2 or vectors.shape != (len(texts), self.dim):
            raise EmbedderError(
                "bad_response", f"expected {len(texts)}x{self.dim} vectors, got {vectors.shape}", retryable=False, provider=f"hf:{route}"
            )
        self._status[route].mark_ok((self._clock() - started) * 1000.0)
        return unit_rows(vectors)

    def _request(self, route: str, batch: list[str]) -> list[list[float]]:
        url, body = self._build(route, batch)
        provider = f"hf:{route}"
        attempt = 0
        while True:
            try:
                response = self._client.post(url, json=body)
            except httpx.TimeoutException as exc:
                error = EmbedderError("timeout", f"{type(exc).__name__}", retryable=True, provider=provider)
            except httpx.HTTPError as exc:
                error = EmbedderError("unavailable", f"{type(exc).__name__}: {exc}", retryable=True, provider=provider)
            else:
                error = self._classify(response, provider)
                if error is None:
                    return self._parse(route, response, len(batch))
            if not error.retryable or attempt >= self.max_retries:
                raise error
            attempt += 1
            self._sleep(self._backoff(error, attempt))

    def _classify(self, response: httpx.Response, provider: str) -> EmbedderError | None:
        code = response.status_code
        if code == 200:
            return None
        snippet = response.text[:200]
        if code in (401, 403):
            return EmbedderError("auth", f"HTTP {code}: {snippet}", retryable=False, provider=provider)
        if code == 429:
            return EmbedderError(
                "rate_limit", f"HTTP 429: {snippet}", retryable=True, provider=provider, retry_after=_retry_after(response)
            )
        if code >= 500:
            return EmbedderError("unavailable", f"HTTP {code}: {snippet}", retryable=True, provider=provider)
        return EmbedderError("bad_request", f"HTTP {code}: {snippet}", retryable=False, provider=provider)

    @staticmethod
    def _backoff(error: EmbedderError, attempt: int) -> float:
        if error.retry_after is not None:
            return min(error.retry_after, 30.0)
        return min(0.5 * (2 ** (attempt - 1)), 8.0)

    def _build(self, route: str, batch: list[str]) -> tuple[str, dict]:
        if route == "deepinfra":
            mapping = self._mapping.get(route)
            model = mapping.provider_id if mapping else self.model
            return (
                f"{self.router_base}/deepinfra/v1/openai/embeddings",
                {"model": model, "input": batch, "encoding_format": "float"},
            )
        if route == "hf-inference":
            return (
                f"{self.router_base}/hf-inference/models/{self.model}/pipeline/feature-extraction",
                {"inputs": batch, "normalize": True, "truncate": True},
            )
        raise EmbedderError("not_configured", f"unknown HF route {route!r}", retryable=False, provider=f"hf:{route}")

    def _parse(self, route: str, response: httpx.Response, n: int) -> list[list[float]]:
        provider = f"hf:{route}"
        try:
            payload = response.json()
        except ValueError as exc:
            raise EmbedderError("bad_response", f"not JSON: {exc}", retryable=False, provider=provider) from exc
        try:
            if route == "deepinfra":
                data = sorted(payload["data"], key=lambda d: d["index"])
                rows = [d["embedding"] for d in data]
                usage = payload.get("usage") or {}
                self.prompt_tokens += int(usage.get("prompt_tokens") or 0)
            else:
                rows = payload
                if rows and isinstance(rows[0], (int, float)):  # one text: a flat list
                    rows = [rows]
        except (KeyError, TypeError, IndexError) as exc:
            raise EmbedderError("bad_response", f"unexpected payload: {exc!r}", retryable=False, provider=provider) from exc
        if not isinstance(rows, list) or len(rows) != n or not all(isinstance(r, list) for r in rows):
            raise EmbedderError("bad_response", f"expected {n} vectors", retryable=False, provider=provider)
        return rows


def _retry_after(response: httpx.Response) -> float | None:
    value = response.headers.get("Retry-After")
    if value is None:
        return None
    try:
        return max(0.0, float(value))
    except ValueError:
        return None
