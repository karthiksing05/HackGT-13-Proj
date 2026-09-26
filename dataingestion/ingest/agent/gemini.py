"""Thin Gemini wrapper for the agents: per-model pacing, 429/503 backoff, grounding metadata.

Verified on our key (2026-09-25): Google Search grounding works on gemini-2.5-flash only;
gemini-flash-latest / flash-lite-latest / 3.5-flash return 429 with the search tool (no
free-tier grounding), but work without tools. A 429 body doesn't say whether the
per-minute or per-day limit was hit, so we back off a couple of times and then give up.
"""

import logging
import re
import threading
import time
from dataclasses import dataclass, field

import httpx
from google import genai
from google.genai import errors, types

from ..config import env, require_env

log = logging.getLogger(__name__)

RETRY_WAITS_429 = (20, 60)   # seconds; after these, treat the quota as spent for this run
RETRY_WAITS_5XX = (5,)  # then the caller falls back to another model


class GeminiQuotaExhausted(RuntimeError):
    pass


@dataclass
class Result:
    text: str
    model: str
    search_queries: list[str] = field(default_factory=list)
    sources: list[dict] = field(default_factory=list)  # [{title, url}] with redirects resolved
    tokens_in: int = 0
    tokens_out: int = 0


class Gemini:
    def __init__(self, rpm: dict[str, float] | None = None):
        self.client = genai.Client(
            api_key=require_env("GEMINI_API_KEY", "Get one from AI Studio."),
            # Our own retry loop below handles 429/503; the SDK's would hammer a spent quota.
            http_options=types.HttpOptions(retry_options=types.HttpRetryOptions(attempts=1)),
        )
        self.rpm = rpm or {}
        self._last: dict[str, float] = {}
        self._lock = threading.Lock()
        self._http = httpx.Client(timeout=10, follow_redirects=False)

    def generate(self, model: str, prompt: str, *, search: bool = False, url_context: bool = False,
                 system: str | None = None, temperature: float | None = None) -> Result:
        tools = []
        if search:
            tools.append(types.Tool(google_search=types.GoogleSearch()))
        if url_context:
            tools.append(types.Tool(url_context=types.UrlContext()))
        config = types.GenerateContentConfig(
            tools=tools or None, system_instruction=system, temperature=temperature,
        )
        resp = self._call(model, prompt, config)
        cand = (resp.candidates or [None])[0]
        usage = resp.usage_metadata
        out = Result(
            text=(resp.text or "").strip(),
            model=model,
            tokens_in=(usage.prompt_token_count or 0) if usage else 0,
            tokens_out=(usage.candidates_token_count or 0) if usage else 0,
        )
        if cand is not None:
            out.search_queries, out.sources = self._grounding(cand)
        return out

    def _call(self, model: str, prompt: str, config: types.GenerateContentConfig):
        waits_429, waits_5xx = list(RETRY_WAITS_429), list(RETRY_WAITS_5XX)
        while True:
            self._pace(model)
            try:
                return self.client.models.generate_content(model=model, contents=prompt, config=config)
            except errors.APIError as e:
                if e.code == 429:
                    if "PerDay" in str(e.details):  # daily cap: waiting won't help today
                        raise GeminiQuotaExhausted(f"{model}: daily free-tier limit reached") from None
                    if not waits_429:
                        raise GeminiQuotaExhausted(f"{model}: {e.message or 'quota exhausted'}") from None
                    wait = waits_429.pop(0)
                elif e.code and e.code >= 500:
                    if not waits_5xx:
                        raise
                    wait = waits_5xx.pop(0)
                else:
                    raise
                log.info("%s: HTTP %s, retrying in %ss", model, e.code, wait)
                time.sleep(wait)

    def _pace(self, model: str) -> None:
        rpm = self.rpm.get(model)
        if not rpm:
            return
        with self._lock:
            wait = self._last.get(model, 0) + 60 / rpm - time.monotonic()
            if wait > 0:
                time.sleep(wait)
            self._last[model] = time.monotonic()

    def _grounding(self, cand) -> tuple[list[str], list[dict]]:
        sources: dict[str, dict] = {}
        gm = cand.grounding_metadata
        for ch in (gm.grounding_chunks or []) if gm else []:
            if ch.web and ch.web.uri:
                url = self._resolve(ch.web.uri)
                if not _is_search_page(url):
                    sources.setdefault(url, {"title": ch.web.title, "url": url})
        um = cand.url_context_metadata
        for m in (um.url_metadata or []) if um else []:
            if m.retrieved_url and "SUCCESS" in str(m.url_retrieval_status):
                sources.setdefault(m.retrieved_url, {"title": None, "url": m.retrieved_url})
        queries = list(gm.web_search_queries or []) if gm else []
        return queries, list(sources.values())

    def _resolve(self, uri: str) -> str:
        """Search-grounding URIs are vertexaisearch redirects; store where they point."""
        if "grounding-api-redirect" not in uri:
            return uri
        try:
            return self._http.head(uri).headers.get("location") or uri
        except httpx.HTTPError:
            return uri


def _is_search_page(url: str) -> bool:
    """Grounding sometimes cites a Google search results page (e.g. a time-zone lookup)."""
    return bool(re.match(r"https?://(www\.)?google\.[a-z.]+/search", url))


def write_models(cfg: dict) -> list[str]:
    """Primary write model, then fallbacks for when it's overloaded (503)."""
    first = cfg.get("write_model") or env("GEMINI_MODEL") or "gemini-flash-latest"
    return [first] + [m for m in cfg.get("write_fallback_models") or [] if m != first]
