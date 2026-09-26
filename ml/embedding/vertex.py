"""Qwen3-Embedding on our Vertex AI Model Garden endpoint, over REST.

`POST https://{host}/v1/projects/{project}/locations/{location}/endpoints/{endpoint}:predict` with
`{"instances": [{<input_key>: text}, ...]}`; the serving container decides the input key (`inputs`
for TEI, `prompt`/`text` for others) and the prediction shape, so both come from `tools/vertex_probe.py`.
Model Garden endpoints usually need their dedicated DNS name as `host`. Credentials are a service
account key (`GOOGLE_APPLICATION_CREDENTIALS`) with `roles/aiplatform.user`; the google libraries are
imported lazily so the service runs without them.

In `EMBED_PROVIDER=auto` the provider is gated: it only serves requests after its startup probe
returned a `dim`-d vector (`require_probe`). **No instruction prefix**, as everywhere else.
"""

from __future__ import annotations

import logging
import os
import time
from collections.abc import Callable

import httpx
import numpy as np

from .base import (
    CANARY_TEXT,
    DIM,
    MODEL,
    EmbedderError,
    EmbedKind,
    Embedder,
    EmbedResult,
    ProviderStatus,
    embed_with_blanks,
    unit_rows,
)

logger = logging.getLogger(__name__)

DEFAULT_PROJECT = "586468035526"
DEFAULT_LOCATION = "us-central1"
DEFAULT_ENDPOINT_ID = "mg-endpoint-91611e42-bb0c-45e2-aa0d-3a0b81535852"
DEFAULT_INPUT_KEY = "inputs"
INPUT_KEYS: tuple[str, ...] = ("inputs", "prompt", "text")
SCOPE = "https://www.googleapis.com/auth/cloud-platform"

TokenProvider = Callable[[], str]


def regional_host(location: str) -> str:
    return f"{location}-aiplatform.googleapis.com"


def predict_url(host: str, project: str, location: str, endpoint_id: str) -> str:
    return f"https://{host}/v1/projects/{project}/locations/{location}/endpoints/{endpoint_id}:predict"


def service_account_token_provider(credentials_path: str) -> TokenProvider:
    """Access tokens for a service-account key file, refreshed when they expire."""
    try:
        from google.auth.transport.requests import Request
        from google.oauth2 import service_account
    except ImportError as exc:
        raise EmbedderError("not_configured", f"google-auth is not installed: {exc}", retryable=False, provider="vertex") from exc
    try:
        credentials = service_account.Credentials.from_service_account_file(credentials_path, scopes=[SCOPE])
    except (OSError, ValueError) as exc:
        raise EmbedderError("not_configured", f"cannot read service account key: {exc}", retryable=False, provider="vertex") from exc

    def provider() -> str:
        if not credentials.valid:
            credentials.refresh(Request())
        return credentials.token

    return provider


def unwrap_prediction(prediction) -> list[float]:
    """Containers differ: `[floats]`, `[[floats]]`, `{"embedding": [...]}` or `{"embeddings": [...]}`."""
    value = prediction
    if isinstance(value, dict):
        value = value.get("embedding") or value.get("embeddings") or next(iter(value.values()), None)
    if isinstance(value, list) and value and isinstance(value[0], list):
        value = value[0]
    if not isinstance(value, list) or not value or not all(isinstance(x, (int, float)) for x in value):
        raise EmbedderError("bad_response", "prediction is not a vector", retryable=False, provider="vertex")
    return value


class VertexEmbedder(Embedder):
    name = "vertex"

    def __init__(
        self,
        *,
        project: str = DEFAULT_PROJECT,
        location: str = DEFAULT_LOCATION,
        endpoint_id: str = DEFAULT_ENDPOINT_ID,
        host: str | None = None,
        input_key: str = DEFAULT_INPUT_KEY,
        credentials_path: str | None = None,
        model: str = MODEL,
        dim: int = DIM,
        timeout: float = 20.0,
        max_batch: int = 16,
        max_chars: int = 2000,
        require_probe: bool = False,
        transport: httpx.BaseTransport | None = None,
        token_provider: TokenProvider | None = None,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if not (project and location and endpoint_id):
            raise EmbedderError("not_configured", "VERTEX_PROJECT, VERTEX_LOCATION and VERTEX_ENDPOINT_ID are required", retryable=False, provider=self.name)
        if input_key not in INPUT_KEYS:
            raise EmbedderError("not_configured", f"VERTEX_INPUT_KEY must be one of {INPUT_KEYS}", retryable=False, provider=self.name)
        if token_provider is None:
            if not credentials_path:
                raise EmbedderError("not_configured", "GOOGLE_APPLICATION_CREDENTIALS is not set", retryable=False, provider=self.name)
            if not os.path.isfile(credentials_path):
                raise EmbedderError("not_configured", f"credentials file not found: {credentials_path}", retryable=False, provider=self.name)
            token_provider = service_account_token_provider(credentials_path)
        self.model = model
        self.dim = dim
        self.project, self.location, self.endpoint_id = project, location, endpoint_id
        self.host = host or regional_host(location)
        self.input_key = input_key
        self.url = predict_url(self.host, project, location, endpoint_id)
        self.max_batch = max(1, max_batch)
        self.max_chars = max_chars
        self.require_probe = require_probe
        self._probe_ok = False
        self._token_provider = token_provider
        self._client = httpx.Client(timeout=timeout, transport=transport)
        self._clock = clock
        self._status = ProviderStatus(self.name, "disabled" if require_probe else "unknown", "awaiting startup probe" if require_probe else None)

    def ready(self) -> bool:
        return self._probe_ok or not self.require_probe

    def probe(self) -> EmbedResult:
        """Embed the canary directly; in gated mode a `dim`-d answer enables the provider."""
        try:
            vectors = self.embed([CANARY_TEXT])
        except EmbedderError:
            self._probe_ok = False
            raise
        self._probe_ok = True
        return EmbedResult(vectors, self.name)

    def warmup(self) -> None:
        self.probe()

    def status(self) -> dict[str, ProviderStatus]:
        st = self._status
        detail = st.detail
        if self.require_probe and not self._probe_ok:
            detail = f"disabled until a probe returns a {self.dim}-d vector" + (f"; {st.detail}" if st.detail else "")
            return {self.name: ProviderStatus(st.name, "disabled", detail, st.last_ok_at, st.last_error_at, st.latency_ms)}
        return {self.name: ProviderStatus(st.name, st.status, detail, st.last_ok_at, st.last_error_at, st.latency_ms)}

    def embed(self, texts: list[str], kind: EmbedKind = "activity") -> np.ndarray:
        return embed_with_blanks(self._embed_nonblank, texts, self.dim, self.name)

    def close(self) -> None:
        self._client.close()

    # -- internals --------------------------------------------------------------------------------

    def _embed_nonblank(self, texts: list[str]) -> np.ndarray:
        texts = [t[: self.max_chars] for t in texts]
        started = self._clock()
        rows: list[list[float]] = []
        try:
            for start in range(0, len(texts), self.max_batch):
                rows.extend(self._predict(texts[start : start + self.max_batch]))
            vectors = np.asarray(rows, dtype=np.float64)
            if vectors.ndim != 2 or vectors.shape != (len(texts), self.dim):
                raise EmbedderError("bad_response", f"expected {len(texts)}x{self.dim}, got {vectors.shape}", retryable=False, provider=self.name)
        except EmbedderError as exc:
            self._status.mark_error(f"{exc.kind}: {exc.message}"[:200])
            raise
        self._status.mark_ok((self._clock() - started) * 1000.0, f"host={self.host} key={self.input_key}")
        return unit_rows(vectors)

    def _predict(self, batch: list[str]) -> list[list[float]]:
        try:
            token = self._token_provider()
        except EmbedderError:
            raise
        except Exception as exc:
            raise EmbedderError("auth", f"token refresh failed: {exc}", retryable=False, provider=self.name) from exc
        body = {"instances": [{self.input_key: text} for text in batch]}
        for attempt in range(2):
            try:
                response = self._client.post(self.url, json=body, headers={"Authorization": f"Bearer {token}"})
            except httpx.TimeoutException as exc:
                raise EmbedderError("timeout", type(exc).__name__, retryable=True, provider=self.name) from exc
            except httpx.HTTPError as exc:
                raise EmbedderError("unavailable", f"{type(exc).__name__}: {exc}", retryable=True, provider=self.name) from exc
            code = response.status_code
            if code == 200:
                break
            snippet = response.text[:200]
            if code in (401, 403):
                raise EmbedderError("auth", f"HTTP {code}: {snippet}", retryable=False, provider=self.name)
            if code == 429 and attempt == 0:
                time.sleep(1.0)
                continue
            if code == 429:
                raise EmbedderError("rate_limit", f"HTTP 429: {snippet}", retryable=True, provider=self.name)
            if code >= 500:
                raise EmbedderError("unavailable", f"HTTP {code}: {snippet}", retryable=True, provider=self.name)
            raise EmbedderError("bad_request", f"HTTP {code}: {snippet}", retryable=False, provider=self.name)
        try:
            predictions = response.json()["predictions"]
        except (ValueError, KeyError, TypeError) as exc:
            raise EmbedderError("bad_response", f"no predictions: {exc!r}", retryable=False, provider=self.name) from exc
        if not isinstance(predictions, list) or len(predictions) != len(batch):
            raise EmbedderError("bad_response", f"expected {len(batch)} predictions", retryable=False, provider=self.name)
        return [unwrap_prediction(p) for p in predictions]
